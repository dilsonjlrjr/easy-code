package gateway

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/provider/fake"
)

// forwardingProvider is a provider that forwards: it answers with a body that blocks until ctx ends
// when block is set, and records the key it saw.
type forwardingProvider struct {
	blockingProvider
	block bool
	seen  chan provider.Secret
}

func (*forwardingProvider) Name() string { return provider.ClaudeCode }

func (p *forwardingProvider) Forward(ctx context.Context, key provider.Secret, _ ForwardRequest) (ForwardResponse, error) {
	p.seen <- key
	if !p.block {
		return ForwardResponse{Status: 200, ContentType: "application/json", Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}
	reader, writer := io.Pipe()
	go func() {
		<-ctx.Done()
		_ = writer.CloseWithError(ctx.Err())
	}()
	return ForwardResponse{Status: 200, ContentType: "text/event-stream", Body: reader}, nil
}

var forwardScope = Scope{ProjectID: "p", WaveID: "DEV-01", Provider: provider.ClaudeCode, Model: "claude-test"}

func openForward(t *testing.T, p *forwardingProvider) (*Service, string) {
	t.Helper()
	service := NewService(fake.KeyOpener{}, time.Minute, p, &fake.Provider{})
	id, err := service.Open(context.Background(), forwardScope, []byte("sk-ant-oat-test"))
	if err != nil {
		t.Fatal(err)
	}
	return service, id
}

func TestForwardChecksScopePathAndModel(t *testing.T) {
	p := &forwardingProvider{seen: make(chan provider.Secret, 4)}
	service, id := openForward(t, p)
	ctx := context.Background()
	body := []byte(`{"model":"claude-test","max_tokens":1}`)

	response, err := service.Forward(ctx, id, ForwardRequest{Path: "v1/messages", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if key := <-p.seen; string(key.Reveal()) != "sk-ant-oat-test" {
		t.Fatal("forwarder did not get the session key")
	}

	cases := []struct {
		name string
		id   string
		req  ForwardRequest
		code string
	}{
		{"unknown session", "nope", ForwardRequest{Path: "v1/messages", Body: body}, CodeUnknownSession},
		{"path", id, ForwardRequest{Path: "v1/models", Body: body}, CodeForwardPathNotAllowed},
		{"model", id, ForwardRequest{Path: "v1/messages/count_tokens", Body: []byte(`{"model":"other"}`)}, CodeModelOutOfScope},
		{"body", id, ForwardRequest{Path: "v1/messages", Body: []byte(`not json`)}, CodeInvalidBody},
	}
	for _, c := range cases {
		if _, err := service.Forward(ctx, c.id, c.req); failure.From(err).Code != c.code {
			t.Errorf("%s: got %v, want %s", c.name, err, c.code)
		}
	}

	other, err := service.Open(ctx, Scope{ProjectID: "p", WaveID: "DEV-01", Provider: fake.Name, Model: "fake-model"}, []byte("k"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Forward(ctx, other, ForwardRequest{Path: "v1/messages", Body: []byte(`{"model":"fake-model"}`)}); failure.From(err).Code != CodeForwardNotAllowed {
		t.Fatalf("provider that does not forward: %v", err)
	}
}

func TestCloseAbortsForwardInFlight(t *testing.T) {
	p := &forwardingProvider{block: true, seen: make(chan provider.Secret, 1)}
	service, id := openForward(t, p)
	response, err := service.Forward(context.Background(), id, ForwardRequest{Path: "v1/messages", Body: []byte(`{"model":"claude-test"}`)})
	if err != nil {
		t.Fatal(err)
	}
	<-p.seen
	service.now = func() time.Time { return time.Now().Add(time.Hour) }
	if expired := service.ExpireIdle(); expired != 0 {
		t.Fatal("a session with a forward in flight must not expire")
	}
	if err := service.Close(id); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(response.Body); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the body to end with the canceled context, got %v", err)
	}
	_ = response.Body.Close()
	_ = response.Body.Close() // idempotent
}
