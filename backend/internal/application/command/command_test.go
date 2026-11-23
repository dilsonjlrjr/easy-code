package command_test

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/application/command"
	"easycode/internal/domain/failure"
)

type directTx struct{}

func (directTx) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type memoryQueue struct {
	pending []command.Command
	status  map[string]string
}

func (q *memoryQueue) Next(context.Context) (*command.Command, error) {
	for _, cmd := range q.pending {
		if q.status[cmd.ID] == "" {
			return &cmd, nil
		}
	}
	return nil, nil
}

func (q *memoryQueue) Applied(_ context.Context, id string) error {
	q.status[id] = command.StateApplied
	return nil
}

func (q *memoryQueue) Refused(_ context.Context, id, reason string) error {
	q.status[id] = command.StateRefused + ":" + reason
	return nil
}

func TestDrainAppliesRefusesAndStopsOnInternalErrors(t *testing.T) {
	queue := &memoryQueue{status: map[string]string{}, pending: []command.Command{
		{ID: "a", Type: "start"}, {ID: "b", Type: "start"}, {ID: "c", Type: "unknown"}, {ID: "d", Type: "boom"},
	}}
	dispatcher := command.NewDispatcher(directTx{}, queue)
	dispatcher.Register("start", func(_ context.Context, cmd command.Command) error {
		if cmd.ID == "b" {
			return failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
		}
		return nil
	})
	dispatcher.Register("boom", func(context.Context, command.Command) error { return errors.New("db down") })
	processed, err := dispatcher.Drain(context.Background())
	if err == nil || processed != 3 {
		t.Fatalf("processed %d, err %v", processed, err)
	}
	want := map[string]string{"a": "applied", "b": "refused:state-conflict", "c": "refused:unsupported-command"}
	for id, status := range want {
		if queue.status[id] != status {
			t.Errorf("%s: %q, want %q", id, queue.status[id], status)
		}
	}
	if queue.status["d"] != "" {
		t.Error("an internal failure leaves the command pending")
	}
}

func TestQualifiedHandlerWinsForItsTarget(t *testing.T) {
	queue := &memoryQueue{status: map[string]string{}, pending: []command.Command{
		{ID: "w", Type: "cancel", TargetType: "wave"}, {ID: "p", Type: "cancel", TargetType: "project"},
	}}
	dispatcher := command.NewDispatcher(directTx{}, queue)
	got := map[string]string{}
	dispatcher.Register("cancel", func(_ context.Context, cmd command.Command) error { got[cmd.ID] = "plain"; return nil })
	dispatcher.Register("project.cancel", func(_ context.Context, cmd command.Command) error { got[cmd.ID] = "project"; return nil })
	if _, err := dispatcher.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got["w"] != "plain" || got["p"] != "project" {
		t.Fatalf("routing: %v", got)
	}
}
