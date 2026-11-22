package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"go.opentelemetry.io/otel/propagation"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/provider"
)

// maxLineBytes bounds one NDJSON line of a call answer.
const maxLineBytes = 16 << 20

// Client talks to easycode-gateway over its Unix socket. The worker never sees a key in clear.
type Client struct {
	http       *http.Client
	propagator propagation.TextMapPropagator
}

// NewClient builds a client for the socket at socketPath. The propagator carries the trace
// context to the gateway spans (RNF-14).
func NewClient(socketPath string, propagator propagation.TextMapPropagator) *Client {
	var dialer net.Dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{http: &http.Client{Transport: transport}, propagator: propagator}
}

// OpenSession opens the wave scope and returns the session id.
func (c *Client) OpenSession(ctx context.Context, scope appgateway.Scope, sealedKey []byte) (string, error) {
	var answer OpenSessionResponse
	err := c.do(ctx, http.MethodPost, routeSessions, OpenSessionRequest{Scope: scope, SealedKey: sealedKey}, http.StatusCreated, &answer)
	return answer.ID, err
}

// CloseSession closes the scope; calls in flight end with ErrorAborted.
func (c *Client) CloseSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, routeSessions+"/"+id, nil, http.StatusNoContent, nil)
}

// TestKey returns whether the sealed key is valid and a short message.
func (c *Client) TestKey(ctx context.Context, providerName string, sealedKey []byte) (bool, string, error) {
	var answer KeyTestResponse
	if err := c.do(ctx, http.MethodPost, routeKeyTests, KeyTestRequest{Provider: providerName, SealedKey: sealedKey}, http.StatusOK, &answer); err != nil {
		return false, "", err
	}
	return answer.Result == KeyValid, answer.Message, nil
}

// Health returns the gateway status.
func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	var answer HealthResponse
	err := c.do(ctx, http.MethodGet, routeHealth, nil, http.StatusOK, &answer)
	return answer, err
}

// Call makes one model call in the session. Errors are *provider.Error (classified by the
// provider) or a failure from the gateway. If ctx is canceled mid-stream, the error is
// ErrorAborted with the last partial usage seen, so the lost cost can still be recorded.
func (c *Client) Call(ctx context.Context, sessionID string, req provider.Request, onChunk func(provider.Chunk)) (provider.Response, error) {
	resp, err := c.send(ctx, http.MethodPost, routeSessions+"/"+sessionID+"/calls", req)
	if err != nil {
		return provider.Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return provider.Response{}, readProblem(resp)
	}
	var partial provider.Usage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	for scanner.Scan() {
		var line CallLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return provider.Response{}, fmt.Errorf("decode gateway line: %w", err)
		}
		switch {
		case line.Chunk != nil:
			partial = line.Chunk.PartialUsage
			if onChunk != nil {
				onChunk(*line.Chunk)
			}
		case line.Response != nil:
			return *line.Response, nil
		case line.Error != nil:
			return provider.Response{}, line.Error
		case line.Failure != nil:
			return provider.Response{}, line.Failure.asError()
		}
	}
	if ctx.Err() != nil {
		return provider.Response{}, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted", PartialUsage: partial}
	}
	if err := scanner.Err(); err != nil {
		return provider.Response{}, &provider.Error{Class: provider.ErrorTransient, Message: "gateway stream broken", PartialUsage: partial}
	}
	return provider.Response{}, &provider.Error{Class: provider.ErrorTransient, Message: "gateway stream ended without answer", PartialUsage: partial}
}

func (c *Client) do(ctx context.Context, method, path string, body any, want int, answer any) error {
	resp, err := c.send(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		return readProblem(resp)
	}
	if answer == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(answer); err != nil {
		return fmt.Errorf("decode gateway answer: %w", err)
	}
	return nil
}

func (c *Client) send(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode gateway request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	// The host is ignored: the transport always dials the socket.
	request, err := http.NewRequestWithContext(ctx, method, "http://easycode-gateway"+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	c.propagator.Inject(ctx, propagation.HeaderCarrier(request.Header))
	resp, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted"}
		}
		return nil, &provider.Error{Class: provider.ErrorTransient, Message: "gateway unreachable"}
	}
	return resp, nil
}

func readProblem(resp *http.Response) error {
	var problem Problem
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&problem); err != nil {
		return errors.New("gateway answered " + resp.Status)
	}
	if problem.Status == 0 {
		problem.Status = resp.StatusCode
	}
	if problem.Class != "" {
		return &provider.Error{Class: problem.Class, Message: problem.Code}
	}
	return problem.asError()
}
