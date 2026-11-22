package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appevent "easycode/internal/application/event"
	"easycode/internal/domain/event"
	"easycode/internal/domain/failure"
)

// NOTIFY channels of the event bus.
const (
	// ChannelOutbox wakes the outbox publisher when a transaction with new events commits.
	ChannelOutbox = "easycode_outbox"
	// ChannelEvents wakes the api listeners: published domain events and telemetry (ADR-11).
	ChannelEvents = "easycode_events"
)

// Codes of the outbox adapter.
const (
	CodeEventInvalid   = "event-invalid"
	CodeUnknownProject = "event-unknown-project"
)

// Outbox writes and reads domain events in easycode.events (outbox and audit trail; never deleted).
type Outbox struct {
	pool *pgxpool.Pool
	tx   TxRunner
}

var (
	_ appevent.Outbox = Outbox{}
	_ appevent.Store  = Outbox{}
)

// NewOutbox builds the adapter.
func NewOutbox(pool *pgxpool.Pool) Outbox {
	return Outbox{pool: pool, tx: NewTxRunner(pool)}
}

// Append implements event.Outbox. The project event counter row stays locked until the
// transaction ends, so a project's events commit in sequence order without gaps (US-09/CA-3).
// Outside a transaction it opens its own.
func (o Outbox) Append(ctx context.Context, draft event.Draft) (event.Envelope, error) {
	var envelope event.Envelope
	err := o.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		envelope, err = o.append(ctx, conn(ctx, o.pool), draft)
		return err
	})
	return envelope, err
}

func (o Outbox) append(ctx context.Context, q querier, draft event.Draft) (event.Envelope, error) {
	if draft.Time.IsZero() {
		draft.Time = time.Now()
	}
	// PostgreSQL keeps microseconds: the envelope keeps the same instant as occurred_at.
	draft.Time = draft.Time.Truncate(time.Microsecond)
	var projectID, organizationID int64
	err := q.QueryRow(WithOperation(ctx, "outbox.resolve_project"),
		"SELECT id, organization_id FROM easycode.projects WHERE public_id = $1::uuid", draft.ProjectID).Scan(&projectID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return event.Envelope{}, failure.New(failure.KindNotFound, CodeUnknownProject, nil)
	}
	if err != nil {
		return event.Envelope{}, fmt.Errorf("resolve event project: %w", classify(err))
	}
	var sequence int64
	err = q.QueryRow(WithOperation(ctx, "outbox.next_sequence"), `
INSERT INTO easycode.project_counters (project_id, name, value) VALUES ($1, 'event', 1)
ON CONFLICT (project_id, name) DO UPDATE SET value = project_counters.value + 1
RETURNING value`, projectID).Scan(&sequence)
	if err != nil {
		return event.Envelope{}, fmt.Errorf("next event sequence: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return event.Envelope{}, fmt.Errorf("event id: %w", err)
	}
	envelope, err := event.NewDomain(draft, id.String(), sequence)
	if err != nil {
		return event.Envelope{}, failure.New(failure.KindInvalidInput, CodeEventInvalid, err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return event.Envelope{}, fmt.Errorf("encode envelope: %w", err)
	}
	_, err = q.Exec(WithOperation(ctx, "outbox.insert"), `
INSERT INTO easycode.events (public_id, organization_id, project_id, sequence, type, subject, waveid,
  correlationid, causationid, occurred_at, envelope)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::uuid, $9::uuid, $10, $11::jsonb)`,
		envelope.ID, organizationID, projectID, sequence, envelope.Type, envelope.Subject, nullable(envelope.WaveID),
		envelope.CorrelationID, nullable(envelope.CausationID), envelope.Time, string(encoded))
	if err != nil {
		return event.Envelope{}, fmt.Errorf("insert event: %w", classify(err))
	}
	// Delivered at commit; repeated payloads in one transaction collapse into one.
	if _, err := q.Exec(WithOperation(ctx, "outbox.wake_publisher"), "SELECT pg_notify($1, '')", ChannelOutbox); err != nil {
		return event.Envelope{}, fmt.Errorf("notify outbox: %w", err)
	}
	return envelope, nil
}

// ProjectEvents implements event.Store: committed events after a sequence, in order.
func (o Outbox) ProjectEvents(ctx context.Context, projectID string, afterSequence int64, limit int) ([]event.Envelope, error) {
	rows, err := conn(ctx, o.pool).Query(WithOperation(ctx, "outbox.project_events"), `
SELECT e.envelope FROM easycode.events e
  JOIN easycode.projects p ON p.id = e.project_id
 WHERE p.public_id = $1::uuid AND e.sequence > $2
 ORDER BY e.sequence
 LIMIT $3`, projectID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("read project events: %w", classify(err))
	}
	envelopes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (event.Envelope, error) {
		var envelope event.Envelope
		var raw []byte
		if err := row.Scan(&raw); err != nil {
			return envelope, err
		}
		return envelope, json.Unmarshal(raw, &envelope)
	})
	if err != nil {
		return nil, fmt.Errorf("scan project events: %w", err)
	}
	return envelopes, nil
}

// FirstSequenceSince returns the smallest project sequence of the events after since (the
// replay window, sse.md section 6); ok is false when there is none.
func (o Outbox) FirstSequenceSince(ctx context.Context, projectID string, since time.Time) (int64, bool, error) {
	var first *int64
	err := conn(ctx, o.pool).QueryRow(WithOperation(ctx, "outbox.first_in_window"), `
SELECT min(e.sequence) FROM easycode.events e JOIN easycode.projects p ON p.id = e.project_id
 WHERE p.public_id = $1::uuid AND e.occurred_at >= $2`, projectID, since).Scan(&first)
	if err != nil {
		return 0, false, fmt.Errorf("replay window: %w", classify(err))
	}
	if first == nil {
		return 0, false, nil
	}
	return *first, true, nil
}

// LastSequence returns the last project sequence (0 without events).
func (o Outbox) LastSequence(ctx context.Context, projectID string) (int64, error) {
	var last int64
	err := conn(ctx, o.pool).QueryRow(WithOperation(ctx, "outbox.last_sequence"), `
SELECT coalesce(max(e.sequence), 0) FROM easycode.events e JOIN easycode.projects p ON p.id = e.project_id WHERE p.public_id = $1::uuid`,
		projectID).Scan(&last)
	if err != nil {
		return 0, fmt.Errorf("last sequence: %w", classify(err))
	}
	return last, nil
}
