package runtime_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/application/runtime"
	"easycode/internal/domain/event"
	"easycode/internal/domain/provider"
	"easycode/internal/domain/sandbox"
)

type inline struct{}

func (inline) InTx(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

type discardOutbox struct{}

func (discardOutbox) Append(context.Context, event.Draft) (event.Envelope, error) {
	return event.Envelope{}, nil
}

// cancelLedger is a one-wave ledger whose lease is freed when canceled is set (the cancel command of
// the worker, US-14). Methods the test does not reach panic through the nil embedded interface.
type cancelLedger struct {
	runtime.Ledger
	claimed  atomic.Bool
	canceled atomic.Bool
	mu       sync.Mutex
	saved    int
}

func (l *cancelLedger) Claim(context.Context, string, []string, time.Duration) (*runtime.Claimed, error) {
	if l.claimed.Swap(true) {
		return nil, nil
	}
	return &runtime.Claimed{Lease: runtime.Lease{WaveID: 1, Owner: "w", Epoch: 1}, PublicID: "0193a5c0-44aa-7e01-b2c9-000000000001",
		Code: "DEV-01", ProjectID: "p", Stage: "implementation", Agent: "developer-agent", Purpose: "execution", Attempt: 1}, nil
}

func (l *cancelLedger) Inputs(context.Context, runtime.Claimed) (runtime.Inputs, error) {
	return runtime.Inputs{InstructionHash: "h"}, nil
}

func (l *cancelLedger) LastCheckpoint(context.Context, runtime.Lease, int) (*runtime.Checkpoint, error) {
	return nil, nil
}

func (l *cancelLedger) SetInputsDigest(context.Context, runtime.Lease, string) error { return nil }

func (l *cancelLedger) Pause(context.Context, runtime.Lease) (string, bool, error) {
	return "", false, nil
}

func (l *cancelLedger) Renew(context.Context, runtime.Lease, time.Duration) error {
	if l.canceled.Load() {
		return runtime.ErrLeaseLost
	}
	return nil
}

func (l *cancelLedger) Held(context.Context, runtime.Lease) (bool, error) {
	return !l.canceled.Load(), nil
}

func (l *cancelLedger) SaveStep(context.Context, runtime.Lease, int, runtime.StepWrite) (runtime.Totals, error) {
	if l.canceled.Load() {
		return runtime.Totals{}, runtime.ErrLeaseLost
	}
	l.mu.Lock()
	l.saved++
	l.mu.Unlock()
	return runtime.Totals{}, nil
}

// blockingModel answers the first call with a command and blocks on the second until it is aborted.
type blockingModel struct {
	first   provider.Message
	calls   atomic.Int32
	started chan struct{}
	aborted chan struct{}
}

func (m *blockingModel) Call(ctx context.Context, _ runtime.WaveContext, _ int, role string, _ provider.Request) (provider.Response, runtime.CallRecord, error) {
	if m.calls.Add(1) == 1 && m.first.Calls != nil {
		return provider.Response{Message: m.first}, runtime.CallRecord{Role: role, Result: "success"}, nil
	}
	close(m.started)
	<-ctx.Done()
	close(m.aborted)
	return provider.Response{}, runtime.CallRecord{Role: role, Result: "aborted"}, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted"}
}

// blockingRunner is a sandbox command that only ends when its context is canceled (the runner then
// removes the container, interno.md section 2).
type blockingRunner struct {
	started chan struct{}
	aborted chan struct{}
}

func (r *blockingRunner) Run(ctx context.Context, _ sandbox.ExecutionRequest, _ map[string]string) (runtime.RunOutcome, error) {
	close(r.started)
	<-ctx.Done()
	close(r.aborted)
	return runtime.RunOutcome{}, ctx.Err()
}

func cancelExecutor(t *testing.T, ledger *cancelLedger, steps runtime.Steps) *runtime.Executor {
	t.Helper()
	return runtime.NewExecutor(inline{}, ledger, map[string]runtime.Agent{"developer-agent": {Steps: steps, Policy: policy(t, "developer-agent")}},
		func(string) (string, string, error) { return "Dev", "h", nil }, discardOutbox{}, nil, nil, nil, func(string) []string { return nil },
		runtime.Settings{Worker: "w", Lease: 15 * time.Second, Poll: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// RNF-10, US-14 CA-1: once the wave is canceled (its lease freed), the step in flight is aborted and the
// executor stops within 5 s: a model call and a sandbox command alike.
func TestCancelAbortsTheStepInFlight(t *testing.T) {
	cases := map[string]func(m *blockingModel) (runtime.Steps, chan struct{}, chan struct{}){
		"model call": func(m *blockingModel) (runtime.Steps, chan struct{}, chan struct{}) {
			return runtime.NewToolAgent(m, 1000, nil), m.started, m.aborted
		},
		"sandbox command": func(m *blockingModel) (runtime.Steps, chan struct{}, chan struct{}) {
			m.first = provider.Message{Calls: []provider.ToolCall{{ID: "c", Name: runtime.ToolExecuteCommand,
				Args: args(map[string]any{"category": "tests", "argv": []string{"go", "test", "./..."}, "directory": "app"})}}}
			runner := &blockingRunner{started: make(chan struct{}), aborted: make(chan struct{})}
			return runtime.NewToolAgent(m, 1000, runner), runner.started, runner.aborted
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			ledger := &cancelLedger{}
			model := &blockingModel{started: make(chan struct{}), aborted: make(chan struct{})}
			steps, started, aborted := build(model)
			executor := cancelExecutor(t, ledger, steps)
			done := make(chan error, 1)
			go func() {
				_, err := executor.RunOnce(context.Background())
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("the step never started")
			}
			canceledAt := time.Now()
			ledger.canceled.Store(true)
			select {
			case <-aborted:
			case <-time.After(5 * time.Second):
				t.Fatal("RNF-10: the step in flight was not aborted within 5 s")
			}
			select {
			case err := <-done:
				if !errors.Is(err, runtime.ErrLeaseLost) {
					t.Fatalf("the executor stops without writing: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the executor did not stop")
			}
			if elapsed := time.Since(canceledAt); elapsed > 5*time.Second {
				t.Fatalf("RNF-10: %s", elapsed)
			}
		})
	}
}
