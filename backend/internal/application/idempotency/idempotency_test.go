package idempotency_test

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/application/idempotency"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

type directTx struct{}

func (directTx) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type memoryStore struct{ keys map[string]idempotency.Stored }

func (m *memoryStore) Find(_ context.Context, _ identity.Principal, key string) (*idempotency.Stored, error) {
	if stored, ok := m.keys[key]; ok {
		return &stored, nil
	}
	return nil, nil
}

func (m *memoryStore) Save(_ context.Context, req idempotency.Request, resp idempotency.Response) error {
	m.keys[req.Key] = idempotency.Stored{Operation: req.Operation, BodyHash: req.BodyHash, Response: resp}
	return nil
}

func TestRepeatedRequestReturnsTheFirstAnswer(t *testing.T) {
	guard := idempotency.NewGuard(directTx{}, &memoryStore{keys: map[string]idempotency.Stored{}})
	runs := 0
	effect := func(context.Context) (idempotency.Response, error) {
		runs++
		return idempotency.Response{Status: 201, Body: []byte(`{"n":1}`)}, nil
	}
	req := idempotency.Request{Key: "k", Operation: "createProject", BodyHash: idempotency.HashBody([]byte("a"))}
	first, err := guard.Run(context.Background(), req, effect)
	if err != nil {
		t.Fatal(err)
	}
	second, err := guard.Run(context.Background(), req, effect)
	if err != nil || runs != 1 || string(second.Body) != string(first.Body) || second.Status != 201 {
		t.Fatalf("repeat must replay: runs=%d %+v %v", runs, second, err)
	}
	req.BodyHash = idempotency.HashBody([]byte("b"))
	_, err = guard.Run(context.Background(), req, effect)
	if failure.From(err).Code != idempotency.CodeConflict {
		t.Fatalf("other body must conflict, got %v", err)
	}
}

func TestFailedEffectStoresNothing(t *testing.T) {
	store := &memoryStore{keys: map[string]idempotency.Stored{}}
	guard := idempotency.NewGuard(directTx{}, store)
	_, err := guard.Run(context.Background(), idempotency.Request{Key: "k"}, func(context.Context) (idempotency.Response, error) {
		return idempotency.Response{}, errors.New("boom")
	})
	if err == nil || len(store.keys) != 0 {
		t.Fatal("failed effect must not be stored")
	}
}
