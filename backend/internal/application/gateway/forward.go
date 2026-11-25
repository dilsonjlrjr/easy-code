package gateway

import (
	"context"
	"encoding/json"
	"io"
	"sync"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/provider"
)

// Failure codes of the forward route (ADR-23).
const (
	CodeForwardNotAllowed     = "gateway-forward-not-allowed"
	CodeForwardPathNotAllowed = "gateway-forward-path-not-allowed"
	CodeInvalidBody           = "gateway-invalid-body"
)

// ForwardPaths are the provider paths an agent session may reach: nothing else leaves the gateway.
var ForwardPaths = map[string]bool{"v1/messages": true, "v1/messages/count_tokens": true}

// ForwardRequest is one raw provider request of an agent session. Only the API version and the beta
// flags pass from the agent; the credential is added by the Forwarder.
type ForwardRequest struct {
	Path     string // one of ForwardPaths
	RawQuery string // for example "beta=true"
	Version  string // anthropic-version
	Beta     string // anthropic-beta
	Body     []byte
}

// ForwardResponse is the upstream answer, unchanged. The caller must close Body.
type ForwardResponse struct {
	Status      int
	ContentType string
	RequestID   string
	RetryAfter  string
	Body        io.ReadCloser
}

// Forwarder sends a raw request upstream with the opened key. It is implemented by the adapters that
// accept forwarded requests (provider.ClaudeCode). It never keeps or logs the key.
type Forwarder interface {
	Forward(ctx context.Context, key provider.Secret, req ForwardRequest) (ForwardResponse, error)
}

// Forward sends one raw request in the session scope: the scope's provider must forward, the path must
// be allowed and the body's model must be the scope's model. The session counts the request as in
// flight until Body is closed, so Close aborts it.
func (s *Service) Forward(ctx context.Context, id string, req ForwardRequest) (ForwardResponse, error) {
	scope, err := s.Session(id)
	if err != nil {
		return ForwardResponse{}, err
	}
	forwarder, ok := s.forwarders[scope.Provider]
	if !ok {
		return ForwardResponse{}, failure.New(failure.KindInvalidInput, CodeForwardNotAllowed, nil)
	}
	if !ForwardPaths[req.Path] {
		return ForwardResponse{}, failure.New(failure.KindNotFound, CodeForwardPathNotAllowed, nil)
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return ForwardResponse{}, failure.New(failure.KindInvalidInput, CodeInvalidBody, nil)
	}
	callCtx, _, done, err := s.begin(ctx, id, body.Model)
	if err != nil {
		return ForwardResponse{}, err
	}
	key, _ := provider.KeyFromContext(callCtx)
	response, err := forwarder.Forward(callCtx, key, req)
	if err != nil {
		done()
		return ForwardResponse{}, err
	}
	response.Body = &doneOnClose{ReadCloser: response.Body, done: done}
	return response, nil
}

// doneOnClose ends the in-flight request when the body is closed.
type doneOnClose struct {
	io.ReadCloser
	once sync.Once
	done func()
}

func (d *doneOnClose) Close() error {
	err := d.ReadCloser.Close()
	d.once.Do(d.done)
	return err
}
