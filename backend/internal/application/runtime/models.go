package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/provider"
)

// Gateway is the worker client of easycode-gateway (interno.md section 3).
type Gateway interface {
	OpenSession(ctx context.Context, scope appgateway.Scope, sealedKey []byte) (string, error)
	Call(ctx context.Context, sessionID string, req provider.Request, onChunk func(provider.Chunk)) (provider.Response, error)
	CloseSession(ctx context.Context, sessionID string) error
}

// KeyVault reads the sealed active key of the project organization (US-15 CA-4).
type KeyVault interface {
	ActiveSealedKey(ctx context.Context, projectID, providerName string) ([]byte, error)
}

// PriceLookup gives the price period in force (US-37 CA-1, CA-2).
type PriceLookup interface {
	CurrentPriceID(ctx context.Context, projectID string, modelID int64, at time.Time) (int64, error)
}

// CallLog opens a model_calls row before the call, in its own transaction: a worker that dies
// in the call leaves it open, and the resume marks it lost with an estimated cost (RNF-16).
type CallLog interface {
	OpenCall(ctx context.Context, waveID string, attempt, step int, modelID, priceID int64, role, inputHash string, estimatedInput int) (int64, error)
}

// GatewayModels implements ModelCaller over easycode-gateway, one session per wave and model.
type GatewayModels struct {
	gateway Gateway
	keys    KeyVault
	prices  PriceLookup
	calls   CallLog
	now     func() time.Time

	mu       sync.Mutex
	sessions map[string]string // wave id + model → session id
}

// NewGatewayModels builds the caller.
func NewGatewayModels(gateway Gateway, keys KeyVault, prices PriceLookup, calls CallLog) *GatewayModels {
	return &GatewayModels{gateway: gateway, keys: keys, prices: prices, calls: calls, now: time.Now, sessions: map[string]string{}}
}

var errNoModel = errors.New("wave has no model for the role")

// Call implements ModelCaller.
func (g *GatewayModels) Call(ctx context.Context, wave WaveContext, step int, role string, req provider.Request) (provider.Response, CallRecord, error) {
	model, ok := wave.Models[role]
	if !ok {
		return provider.Response{}, CallRecord{}, errNoModel
	}
	record := CallRecord{Role: role, Provider: model.Provider, Model: model.Name}
	started := g.now()
	priceID, err := g.prices.CurrentPriceID(ctx, wave.ProjectID, model.ID, started)
	if err != nil {
		return provider.Response{}, CallRecord{}, err // model-without-current-price: no call
	}
	session, err := g.session(ctx, wave, model)
	if err != nil {
		return provider.Response{}, CallRecord{}, err // provider-without-key: no call (US-15 CA-4)
	}
	req.Model = model.Name
	encoded, _ := json.Marshal(req)
	sum := sha256.Sum256(encoded)
	if record.ID, err = g.calls.OpenCall(ctx, wave.WaveID, wave.Attempt, step, model.ID, priceID, role, hex.EncodeToString(sum[:]), estimateInput(req)); err != nil {
		return provider.Response{}, CallRecord{}, err
	}
	resp, err := g.gateway.Call(ctx, session, req, nil)
	record.Duration = g.now().Sub(started).Milliseconds()
	if err != nil {
		var providerErr *provider.Error
		record.Result = "error"
		if errors.As(err, &providerErr) {
			record.Usage, record.Error = providerErr.PartialUsage, string(providerErr.Class)
			if providerErr.Class == provider.ErrorAborted {
				record.Result = "aborted"
			}
			if providerErr.Class == provider.ErrorAuthentication {
				g.forget(ctx, wave, model)
			}
		} else {
			record.Error = failure.From(err).Code
		}
		return provider.Response{}, record, err
	}
	record.Result, record.Usage = "success", resp.Usage
	return resp, record, nil
}

func (g *GatewayModels) session(ctx context.Context, wave WaveContext, model ModelRef) (string, error) {
	key := wave.WaveID + "/" + model.Provider + "/" + model.Name
	g.mu.Lock()
	id, ok := g.sessions[key]
	g.mu.Unlock()
	if ok {
		return id, nil
	}
	sealed, err := g.keys.ActiveSealedKey(ctx, wave.ProjectID, model.Provider)
	if err != nil {
		return "", err
	}
	id, err = g.gateway.OpenSession(ctx, appgateway.Scope{ProjectID: wave.ProjectID, WaveID: wave.WaveID, Provider: model.Provider, Model: model.Name}, sealed)
	if err != nil {
		return "", err
	}
	g.mu.Lock()
	g.sessions[key] = id
	g.mu.Unlock()
	return id, nil
}

func (g *GatewayModels) forget(ctx context.Context, wave WaveContext, model ModelRef) {
	key := wave.WaveID + "/" + model.Provider + "/" + model.Name
	g.mu.Lock()
	id, ok := g.sessions[key]
	delete(g.sessions, key)
	g.mu.Unlock()
	if ok {
		_ = g.gateway.CloseSession(context.WithoutCancel(ctx), id)
	}
}

// CloseWave ends the gateway sessions of a wave (the key is zeroed in the gateway).
func (g *GatewayModels) CloseWave(ctx context.Context, waveID string) {
	g.mu.Lock()
	var ids []string
	for key, id := range g.sessions {
		if len(key) > len(waveID) && key[:len(waveID)+1] == waveID+"/" {
			ids = append(ids, id)
			delete(g.sessions, key)
		}
	}
	g.mu.Unlock()
	for _, id := range ids {
		_ = g.gateway.CloseSession(context.WithoutCancel(ctx), id)
	}
}

// estimateInput counts the input tokens before the call (4 bytes per token), the base of the
// estimated lost cost when the worker dies in the call.
func estimateInput(req provider.Request) int {
	size := len(req.System)
	for _, m := range req.Messages {
		size += len(m.Text) + len(m.Opaque)
		for _, r := range m.Results {
			size += len(r.Content)
		}
		for _, c := range m.Calls {
			size += len(c.Args)
		}
	}
	for _, t := range req.Tools {
		size += len(t.Description) + len(t.Schema)
	}
	return (size + 3) / 4
}
