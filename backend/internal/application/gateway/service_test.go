package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/provider/fake"
)

var scope = Scope{ProjectID: "3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14", WaveID: "REQ-01", Provider: fake.Name, Model: "fake-model"}

func userRequest(text string) provider.Request {
	return provider.Request{Messages: []provider.Message{{Role: "user", Text: text}}, MaxTokens: 100}
}

func TestCallUsesScopeAndReturnsUsage(t *testing.T) {
	service := NewService(fake.KeyOpener{}, time.Minute, &fake.Provider{})
	id, err := service.Open(context.Background(), scope, []byte("sk-test"))
	if err != nil {
		t.Fatal(err)
	}
	var chunks int
	response, err := service.Call(context.Background(), id, userRequest("hello there"), func(provider.Chunk) { chunks++ })
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Text != "echo: hello there" || response.Stop != provider.StopEnd {
		t.Fatalf("unexpected response %+v", response)
	}
	if response.Usage != (provider.Usage{Input: 3, Output: 5}) {
		t.Fatalf("usage = %+v", response.Usage)
	}
	if chunks == 0 {
		t.Fatal("expected streaming chunks")
	}
}

func TestCallRefusesAnotherModelAndUnknownSession(t *testing.T) {
	service := NewService(fake.KeyOpener{}, time.Minute, &fake.Provider{})
	id, _ := service.Open(context.Background(), scope, []byte("sk-test"))

	request := userRequest("hi")
	request.Model = "other-model"
	if _, err := service.Call(context.Background(), id, request, nil); failure.From(err).Code != CodeModelOutOfScope {
		t.Fatalf("expected model out of scope, got %v", err)
	}
	if _, err := service.Call(context.Background(), "nope", userRequest("hi"), nil); failure.From(err).Code != CodeUnknownSession {
		t.Fatalf("expected unknown session, got %v", err)
	}
	if _, err := service.Open(context.Background(), Scope{ProjectID: "p", WaveID: "REQ-01", Provider: "anthropic", Model: "m"}, []byte("k")); failure.From(err).Code != CodeUnknownProvider {
		t.Fatalf("expected unknown provider, got %v", err)
	}
}

func TestCloseAbortsCallInFlightAndZeroesKey(t *testing.T) {
	blocking := &blockingProvider{started: make(chan provider.Secret, 1)}
	service := NewService(fake.KeyOpener{}, time.Minute, blocking)
	id, _ := service.Open(context.Background(), Scope{ProjectID: "p", WaveID: "REQ-01", Provider: blocking.Name(), Model: "m"}, []byte("sk-test"))

	result := make(chan error, 1)
	go func() {
		_, err := service.Call(context.Background(), id, userRequest("hi"), nil)
		result <- err
	}()
	key := <-blocking.started
	if err := service.Close(id); err != nil {
		t.Fatal(err)
	}
	var providerErr *provider.Error
	if err := <-result; !errors.As(err, &providerErr) || providerErr.Class != provider.ErrorAborted {
		t.Fatalf("expected aborted, got %v", err)
	}
	for _, b := range key.Reveal() {
		if b != 0 {
			t.Fatal("key not zeroed after close")
		}
	}
	if err := service.Close(id); failure.From(err).Code != CodeUnknownSession {
		t.Fatalf("second close: %v", err)
	}
}

func TestExpireIdle(t *testing.T) {
	service := NewService(fake.KeyOpener{}, time.Minute, &fake.Provider{})
	now := time.Now()
	service.now = func() time.Time { return now }
	id, _ := service.Open(context.Background(), scope, []byte("sk-test"))

	now = now.Add(2 * time.Minute)
	if expired := service.ExpireIdle(); expired != 1 {
		t.Fatalf("expired %d sessions, want 1", expired)
	}
	if _, err := service.Session(id); failure.From(err).Code != CodeUnknownSession {
		t.Fatalf("session should be gone, got %v", err)
	}
}

func TestTestKey(t *testing.T) {
	service := NewService(fake.KeyOpener{}, time.Minute, &fake.Provider{})
	if err := service.TestKey(context.Background(), fake.Name, []byte("sk-test")); err != nil {
		t.Fatal(err)
	}
	var providerErr *provider.Error
	err := service.TestKey(context.Background(), fake.Name, []byte(fake.InvalidKey))
	if !errors.As(err, &providerErr) || providerErr.Class != provider.ErrorAuthentication {
		t.Fatalf("expected authentication error, got %v", err)
	}
}

type blockingProvider struct{ started chan provider.Secret }

func (*blockingProvider) Name() string { return "blocking" }

func (p *blockingProvider) Complete(ctx context.Context, _ provider.Request, _ func(provider.Chunk)) (provider.Response, error) {
	key, _ := provider.KeyFromContext(ctx)
	p.started <- key
	<-ctx.Done()
	return provider.Response{}, &provider.Error{Class: provider.ErrorAborted, PartialUsage: provider.Usage{Input: 1}}
}

func (*blockingProvider) Capabilities(context.Context, string) (provider.Capabilities, error) {
	return provider.Capabilities{}, nil
}

func (*blockingProvider) TestKey(context.Context, provider.Secret) error { return nil }
