package gateway

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/provider/fake"
)

func startGateway(t *testing.T) *Client {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	propagator := propagation.TraceContext{}
	service := appgateway.NewService(fake.KeyOpener{}, time.Minute, &fake.Provider{})
	server := &http.Server{
		Handler:           NewHandler(service, slog.New(slog.NewJSONHandler(io.Discard, nil)), noop.NewTracerProvider().Tracer("test"), propagator),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return NewClient(socket, propagator)
}

var testScope = appgateway.Scope{ProjectID: "3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14", WaveID: "REQ-01", Provider: fake.Name, Model: "fake-model"}

func request(text string) provider.Request {
	return provider.Request{Model: "fake-model", Messages: []provider.Message{{Role: "user", Text: text}}, MaxTokens: 100}
}

func TestCallStreamsChunksAndReturnsUsage(t *testing.T) {
	client := startGateway(t)
	ctx := context.Background()
	id, err := client.OpenSession(ctx, testScope, []byte("sk-test"))
	if err != nil {
		t.Fatal(err)
	}
	var texts string
	response, err := client.Call(ctx, id, request("hello there"), func(chunk provider.Chunk) { texts += chunk.PartialText })
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Text != "echo: hello there" || texts != response.Message.Text {
		t.Fatalf("response %q, streamed %q", response.Message.Text, texts)
	}
	if response.Usage != (provider.Usage{Input: 3, Output: 5}) || response.ProviderID == "" {
		t.Fatalf("unexpected response %+v", response)
	}
	if err := client.CloseSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Call(ctx, id, request("again"), nil); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("call after close: %v", err)
	}
}

func TestCallReturnsClassifiedErrors(t *testing.T) {
	client := startGateway(t)
	ctx := context.Background()
	id, _ := client.OpenSession(ctx, testScope, []byte("sk-test"))

	_, err := client.Call(ctx, id, request("fake:error:rate_limit"), nil)
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Class != provider.ErrorRateLimit || providerErr.RetryAfter != time.Second {
		t.Fatalf("expected rate limit with retry-after, got %v", err)
	}
	if providerErr.PartialUsage.Input == 0 {
		t.Fatal("expected partial usage for the lost cost")
	}

	other := request("hi")
	other.Model = "another-model"
	if _, err := client.Call(ctx, id, other, nil); failure.From(err).Code != appgateway.CodeModelOutOfScope {
		t.Fatalf("expected model out of scope, got %v", err)
	}
}

func TestOpenSessionRefusesUnknownProvider(t *testing.T) {
	client := startGateway(t)
	scope := testScope
	scope.Provider = "anthropic"
	_, err := client.OpenSession(context.Background(), scope, []byte("sk-test"))
	if classified := failure.From(err); classified.Kind != failure.KindInvalidInput || classified.Code != appgateway.CodeUnknownProvider {
		t.Fatalf("expected unknown provider, got %v", err)
	}
}

func TestKeyTestAndHealth(t *testing.T) {
	client := startGateway(t)
	ctx := context.Background()
	if valid, _, err := client.TestKey(ctx, fake.Name, []byte("sk-test")); err != nil || !valid {
		t.Fatalf("valid key: %v %v", valid, err)
	}
	if valid, message, err := client.TestKey(ctx, fake.Name, []byte(fake.InvalidKey)); err != nil || valid || message == "" {
		t.Fatalf("invalid key: %v %q %v", valid, message, err)
	}
	health, err := client.Health(ctx)
	if err != nil || health.Status != "ok" || len(health.Providers) != 1 {
		t.Fatalf("health %+v, %v", health, err)
	}
}

func TestSecretNeverMarshals(t *testing.T) {
	encoded, err := (provider.NewSecret([]byte("sk-live"))).MarshalJSON()
	if err != nil || string(encoded) != `"***"` {
		t.Fatalf("secret marshaled as %s", encoded)
	}
}
