// Package command is the worker side of the command queue (interno.md section 1): the api
// records a command and wakes the worker, which claims it, rechecks the full guard, applies the
// transition with audit and outbox in one transaction and marks the command applied or refused.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"easycode/internal/application/transaction"
	"easycode/internal/domain/failure"
)

// Command states (commands.status).
const (
	StatePending = "pending"
	StateApplied = "applied"
	StateRefused = "refused"
)

// Command is a queued human control action.
type Command struct {
	ID             string // public id: the correlationid of the events it causes
	Type           string
	TargetType     string
	OrganizationID string // public ids
	ProjectID      string
	ProviderKeyID  int64
	RequestedBy    string // subject of who asked
	Payload        json.RawMessage
	RequestedAt    time.Time
}

// Queue is the commands table seen by the worker.
type Queue interface {
	// Next claims the oldest pending command (FOR UPDATE SKIP LOCKED) in the caller's
	// transaction, or returns nil when there is none.
	Next(ctx context.Context) (*Command, error)
	// Applied and Refused finish the claimed command in the same transaction.
	Applied(ctx context.Context, id string) error
	Refused(ctx context.Context, id, reason string) error
}

// Handler applies one command type. A classified client failure (state conflict, invalid input…)
// refuses the command; any other error rolls back and leaves it pending for the next pass.
// A handler must check its guards before writing, because a refusal commits.
type Handler func(ctx context.Context, cmd Command) error

// Dispatcher routes commands to their handlers.
type Dispatcher struct {
	tx       transaction.Runner
	queue    Queue
	handlers map[string]Handler
}

// NewDispatcher builds the dispatcher.
func NewDispatcher(tx transaction.Runner, queue Queue) *Dispatcher {
	return &Dispatcher{tx: tx, queue: queue, handlers: map[string]Handler{}}
}

// Register sets the handler of a command type.
func (d *Dispatcher) Register(commandType string, handler Handler) {
	d.handlers[commandType] = handler
}

// Drain processes pending commands until none is left or one fails; it returns how many
// were applied or refused.
func (d *Dispatcher) Drain(ctx context.Context) (int, error) {
	processed := 0
	for ctx.Err() == nil {
		done := false
		err := d.tx.InTx(ctx, func(ctx context.Context) error {
			cmd, err := d.queue.Next(ctx)
			if err != nil || cmd == nil {
				done = cmd == nil
				return err
			}
			return d.process(ctx, *cmd)
		})
		if err != nil {
			return processed, err
		}
		if done {
			return processed, nil
		}
		processed++
	}
	return processed, ctx.Err()
}

func (d *Dispatcher) process(ctx context.Context, cmd Command) error {
	handler, ok := d.handlers[cmd.Type]
	if !ok {
		return d.queue.Refused(ctx, cmd.ID, "unsupported-command")
	}
	err := handler(ctx, cmd)
	if err == nil {
		return d.queue.Applied(ctx, cmd.ID)
	}
	classified := failure.From(err)
	if classified.Kind == failure.KindInternal || classified.Kind == failure.KindUnavailable {
		return fmt.Errorf("command %s %s: %w", cmd.Type, cmd.ID, err)
	}
	return d.queue.Refused(ctx, cmd.ID, classified.Code)
}
