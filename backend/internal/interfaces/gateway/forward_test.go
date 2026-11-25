package gateway

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/provider/claudecode"
	"easycode/internal/infrastructure/provider/fake"
)

const forwardKey = "sk-ant-oat01-forward-test"

var forwardScope = appgateway.Scope{ProjectID: "p", WaveID: "DEV-01", Provider: provider.ClaudeCode, Model: "claude-test"}

// startForwardGateway runs the gateway with the claude-code adapter pointed at upstream. logs gets
// every log line of the gateway.
func startForwardGateway(t *testing.T, upstream *httptest.Server, logs io.Writer) *Client {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	propagator := propagation.TraceContext{}
	adapter := claudecode.New(upstream.URL, upstream.Client(), 5*time.Second, "")
	service := appgateway.NewService(fake.KeyOpener{}, time.Minute, adapter, &fake.Provider{})
	server := &http.Server{
		Handler:           NewHandler(service, slog.New(slog.NewJSONHandler(logs, nil)), noop.NewTracerProvider().Tracer("test"), propagator),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return NewClient(socket, propagator)
}

func TestForwardStreamsTheUpstreamAnswer(t *testing.T) {
	release := make(chan struct{})
	var seen http.Header
	var seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, seenPath = r.Header.Clone(), r.URL.RequestURI()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Request-Id", "req_1")
		_, _ = io.WriteString(w, "event: message_start\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
	}))
	t.Cleanup(upstream.Close)
	defer close(release)
	client := startForwardGateway(t, upstream, io.Discard)
	ctx := context.Background()
	id, err := client.OpenSession(ctx, forwardScope, []byte(forwardKey))
	if err != nil {
		t.Fatal(err)
	}

	header := http.Header{}
	header.Set("anthropic-version", "2023-06-01")
	header.Set("anthropic-beta", "claude-code-20250219")
	header.Set("Authorization", "Bearer session-token")
	header.Set("x-stainless-os", "Linux")
	resp, err := client.Forward(ctx, id, "v1/messages", "beta=true", header, []byte(`{"model":"claude-test","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Request-Id") != "req_1" {
		t.Fatalf("status %d headers %v", resp.StatusCode, resp.Header)
	}
	reader := bufio.NewReader(resp.Body)
	first, err := reader.ReadString('\n')
	if err != nil || first != "event: message_start\n" {
		t.Fatalf("first event must arrive before the upstream ends: %q %v", first, err)
	}
	if seenPath != "/v1/messages?beta=true" || seen.Get("Authorization") != "Bearer "+forwardKey || seen.Get("x-stainless-os") != "" ||
		seen.Get("anthropic-version") != "2023-06-01" || seen.Get("anthropic-beta") != "claude-code-20250219,"+claudecode.OAuthBeta {
		t.Fatalf("upstream got %s %v", seenPath, seen)
	}
}

func TestForwardRefusalsAreGatewayFailures(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(upstream.Close)
	client := startForwardGateway(t, upstream, io.Discard)
	ctx := context.Background()
	id, _ := client.OpenSession(ctx, forwardScope, []byte(forwardKey))
	other, _ := client.OpenSession(ctx, appgateway.Scope{ProjectID: "p", WaveID: "DEV-01", Provider: fake.Name, Model: "fake-model"}, []byte("k"))

	cases := []struct {
		name, id, path, body, code string
	}{
		{"unknown session", "nope", "v1/messages", `{"model":"claude-test"}`, appgateway.CodeUnknownSession},
		{"path", id, "v1/models", `{"model":"claude-test"}`, appgateway.CodeForwardPathNotAllowed},
		{"model", id, "v1/messages/count_tokens", `{"model":"other"}`, appgateway.CodeModelOutOfScope},
		{"provider", other, "v1/messages", `{"model":"fake-model"}`, appgateway.CodeForwardNotAllowed},
		{"body", id, "v1/messages", `nope`, appgateway.CodeInvalidBody},
	}
	for _, c := range cases {
		resp, err := client.Forward(ctx, c.id, c.path, "", nil, []byte(c.body))
		if resp != nil {
			_ = resp.Body.Close()
		}
		if failure.From(err).Code != c.code {
			t.Errorf("%s: got %v, want %s", c.name, err, c.code)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("refused requests reached the upstream %d times", n)
	}
	big := bytes.Repeat([]byte("a"), maxBodyBytes+1)
	resp, err := client.Forward(ctx, id, "v1/messages", "", nil, big)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if failure.From(err).Code != CodeBodyTooLarge {
		t.Fatalf("body too large: %v", err)
	}
}

func TestForwardPassesUpstreamErrorsAndLogsOnlyTheClass(t *testing.T) {
	const answer = `{"type":"error","error":{"type":"authentication_error","message":"invalid bearer token"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(upstream.Close)
	var logs bytes.Buffer
	client := startForwardGateway(t, upstream, &logs)
	ctx := context.Background()
	id, _ := client.OpenSession(ctx, forwardScope, []byte(forwardKey))
	resp, err := client.Forward(ctx, id, "v1/messages", "", nil, []byte(`{"model":"claude-test"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || string(body) != answer {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	text := logs.String()
	if !strings.Contains(text, `"class":"authentication"`) || !strings.Contains(text, `"upstream_status":401`) {
		t.Fatalf("expected class and status in logs: %s", text)
	}
	if strings.Contains(text, forwardKey) || strings.Contains(text, "invalid bearer token") || strings.Contains(text, "claude-test\"}") {
		t.Fatalf("logs leak key or content: %s", text)
	}
}

func TestCloseSessionAbortsForwardStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: ping\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	client := startForwardGateway(t, upstream, io.Discard)
	ctx := context.Background()
	id, _ := client.OpenSession(ctx, forwardScope, []byte(forwardKey))
	resp, err := client.Forward(ctx, id, "v1/messages", "", nil, []byte(`{"model":"claude-test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, resp.Body); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("closing the session must end the forwarded stream")
	}
}
