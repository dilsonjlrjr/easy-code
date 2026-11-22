// Package gateway holds the use cases of easycode-gateway (interno.md section 3): wave-scoped
// sessions that open a sealed provider key in memory, model calls restricted to the scope's
// provider and model, and key tests. It has no database: the worker sends what it reads.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/provider"
)

// Failure codes of the gateway.
const (
	CodeUnknownProvider = "gateway-unknown-provider"
	CodeUnknownSession  = "gateway-unknown-session"
	CodeModelOutOfScope = "gateway-model-out-of-scope"
	CodeInvalidScope    = "gateway-invalid-scope"
)

// KeyOpener opens a sealed provider key (ADR-13). Only easycode-gateway holds the private key.
type KeyOpener interface {
	Open(ctx context.Context, sealed []byte) (provider.Secret, error)
}

// Scope is what a session is allowed to do: one wave, one provider, one model.
type Scope struct {
	ProjectID string `json:"project_id"`
	WaveID    string `json:"wave_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
}

// Service manages the sessions of one gateway process.
type Service struct {
	providers map[string]provider.Provider
	opener    KeyOpener
	idle      time.Duration
	now       func() time.Time

	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	scope    Scope
	key      provider.Secret
	lastUsed time.Time
	inflight map[int]context.CancelFunc
	nextCall int
}

// NewService builds the service. idle is the inactivity after which a session expires (15 min).
func NewService(opener KeyOpener, idle time.Duration, providers ...provider.Provider) *Service {
	registry := make(map[string]provider.Provider, len(providers))
	for _, p := range providers {
		registry[p.Name()] = p
	}
	return &Service{providers: registry, opener: opener, idle: idle, now: time.Now, sessions: map[string]*session{}}
}

// Open opens the key and returns the session id.
func (s *Service) Open(ctx context.Context, scope Scope, sealedKey []byte) (string, error) {
	if scope.ProjectID == "" || scope.WaveID == "" || scope.Model == "" || len(sealedKey) == 0 {
		return "", failure.New(failure.KindInvalidInput, CodeInvalidScope, nil)
	}
	if _, err := s.provider(scope.Provider); err != nil {
		return "", err
	}
	key, err := s.opener.Open(ctx, sealedKey)
	if err != nil {
		return "", fmt.Errorf("open provider key: %w", err)
	}
	id := newSessionID()
	s.mu.Lock()
	s.sessions[id] = &session{scope: scope, key: key, lastUsed: s.now(), inflight: map[int]context.CancelFunc{}}
	s.mu.Unlock()
	return id, nil
}

// Session returns the scope of an open session.
func (s *Service) Session(id string) (Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[id]
	if !ok {
		return Scope{}, failure.New(failure.KindNotFound, CodeUnknownSession, nil)
	}
	return current.scope, nil
}

// Call makes one model call in the session scope. An empty req.Model means the scope's model.
// Closing the session aborts the call (ErrorAborted with the partial usage).
func (s *Service) Call(ctx context.Context, id string, req provider.Request, onChunk func(provider.Chunk)) (provider.Response, error) {
	if req.Model == "" {
		scope, err := s.Session(id)
		if err != nil {
			return provider.Response{}, err
		}
		req.Model = scope.Model
	}
	callCtx, scope, done, err := s.begin(ctx, id, req.Model)
	if err != nil {
		return provider.Response{}, err
	}
	defer done()
	adapter, err := s.provider(scope.Provider)
	if err != nil {
		return provider.Response{}, err
	}
	return adapter.Complete(callCtx, req, onChunk)
}

func (s *Service) begin(ctx context.Context, id, model string) (context.Context, Scope, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[id]
	if !ok {
		return nil, Scope{}, nil, failure.New(failure.KindNotFound, CodeUnknownSession, nil)
	}
	if model != current.scope.Model {
		return nil, Scope{}, nil, failure.New(failure.KindInvalidInput, CodeModelOutOfScope, nil)
	}
	callCtx, cancel := context.WithCancel(provider.WithKey(ctx, current.key))
	current.nextCall++
	number := current.nextCall
	current.inflight[number] = cancel
	current.lastUsed = s.now()
	done := func() {
		cancel()
		s.mu.Lock()
		delete(current.inflight, number)
		current.lastUsed = s.now()
		s.mu.Unlock()
	}
	return callCtx, current.scope, done, nil
}

// Close aborts calls in flight, zeroes the key and forgets the session.
func (s *Service) Close(id string) error {
	s.mu.Lock()
	current, ok := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	if !ok {
		return failure.New(failure.KindNotFound, CodeUnknownSession, nil)
	}
	s.end(current)
	return nil
}

// ExpireIdle closes the sessions idle for longer than the idle period and returns how many.
func (s *Service) ExpireIdle() int {
	s.mu.Lock()
	var expired []*session
	for id, current := range s.sessions {
		if len(current.inflight) == 0 && s.now().Sub(current.lastUsed) > s.idle {
			expired = append(expired, current)
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
	for _, current := range expired {
		s.end(current)
	}
	return len(expired)
}

// TestKey opens a sealed key and asks the provider to validate it (US-15).
func (s *Service) TestKey(ctx context.Context, providerName string, sealedKey []byte) error {
	adapter, err := s.provider(providerName)
	if err != nil {
		return err
	}
	key, err := s.opener.Open(ctx, sealedKey)
	if err != nil {
		return fmt.Errorf("open provider key: %w", err)
	}
	defer key.Zero()
	return adapter.TestKey(ctx, key)
}

// Providers lists the configured provider names (health).
func (s *Service) Providers() []string {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	return names
}

func (s *Service) end(current *session) {
	s.mu.Lock()
	for _, cancel := range current.inflight {
		cancel()
	}
	s.mu.Unlock()
	current.key.Zero()
}

func (s *Service) provider(name string) (provider.Provider, error) {
	adapter, ok := s.providers[name]
	if !ok {
		return nil, failure.New(failure.KindInvalidInput, CodeUnknownProvider, nil)
	}
	return adapter, nil
}

func newSessionID() string {
	buffer := make([]byte, 16)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
