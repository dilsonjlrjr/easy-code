// Package event holds the ports of the live event bus (ADR-11): the outbox written in the
// transaction of each state change, the replay store, best-effort telemetry and the
// notification listener, plus the in-memory broker that fans notifications out to the
// subscribers of one process (one LISTEN per process).
package event

import (
	"context"

	"easycode/internal/domain/event"
)

// Outbox appends a project domain event in the caller's transaction (transaction.Runner):
// it assigns the id and the next project sequence. The event is published later, at least once.
type Outbox interface {
	Append(ctx context.Context, draft event.Draft) (event.Envelope, error)
}

// Store reads committed domain events of a project in sequence order (SSE replay by Last-Event-ID).
type Store interface {
	ProjectEvents(ctx context.Context, projectID string, afterSequence int64, limit int) ([]event.Envelope, error)
}

// TelemetryEmitter sends best-effort telemetry (agent progress, model call cost): no sequence,
// no replay, lost if nobody listens.
type TelemetryEmitter interface {
	Emit(ctx context.Context, draft event.Draft) (event.Envelope, error)
}

// Notification wakes subscribers. A domain notification carries only positions: subscribers
// read the events from the Store. Telemetry travels inline.
type Notification struct {
	ProjectID string
	Sequence  int64           // project sequence of a published domain event
	Position  int64           // global outbox position (/me/events)
	Telemetry *event.Envelope // set only for telemetry
}

// Listener delivers notifications to handle until ctx is canceled. handle must not block.
type Listener interface {
	Listen(ctx context.Context, handle func(Notification)) error
}
