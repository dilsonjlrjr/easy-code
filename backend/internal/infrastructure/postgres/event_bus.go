package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appevent "easycode/internal/application/event"
	"easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/infrastructure/logging"
)

// publisherLockID makes the outbox publisher run one at a time across worker instances, so
// global positions follow publication order (ADR-11).
const publisherLockID int64 = 0x45634f7574626f78 // "EcOutbox"

// maxNotifyPayload stays under the 8000-byte limit of NOTIFY.
const maxNotifyPayload = 7900

// Notification kinds on ChannelEvents.
const (
	kindDomain    = "domain"
	kindTelemetry = "telemetry"
)

// CodeTelemetryTooLarge refuses telemetry that does not fit a NOTIFY payload.
const CodeTelemetryTooLarge = "telemetry-too-large"

// notifyPayload is the thin message on ChannelEvents: positions only for domain events, the
// whole envelope for telemetry (which has no other transport).
type notifyPayload struct {
	Kind      string          `json:"kind"`
	ProjectID string          `json:"projectid,omitempty"`
	Sequence  int64           `json:"sequence,omitempty"`
	Position  int64           `json:"position,omitempty"`
	Envelope  *event.Envelope `json:"envelope,omitempty"`
}

// OutboxPublisher marks committed events as published, gives each a global position and
// notifies the api listeners (at least once, ordered per project). It runs in the worker.
type OutboxPublisher struct {
	pool *pgxpool.Pool
}

// NewOutboxPublisher builds the publisher over a pool opened with the worker role.
func NewOutboxPublisher(pool *pgxpool.Pool) OutboxPublisher {
	return OutboxPublisher{pool: pool}
}

type pendingEvent struct {
	id         int64
	occurredAt time.Time
	projectID  string
	sequence   int64
}

// PublishPending publishes up to limit pending events and returns how many. When another
// publisher holds the lock it returns 0 at once.
func (p OutboxPublisher) PublishPending(ctx context.Context, limit int) (int, error) {
	published := 0
	err := pgx.BeginTxFunc(ctx, p.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var locked bool
		if err := tx.QueryRow(WithOperation(ctx, "outbox.publisher_lock"), "SELECT pg_try_advisory_xact_lock($1)", publisherLockID).Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return nil
		}
		pending, err := pendingEvents(ctx, tx, limit)
		if err != nil || len(pending) == 0 {
			return err
		}
		positions, err := assignPositions(ctx, tx, pending)
		if err != nil {
			return err
		}
		batch := &pgx.Batch{}
		for i, current := range pending {
			payload, err := json.Marshal(notifyPayload{Kind: kindDomain, ProjectID: current.projectID, Sequence: current.sequence, Position: positions[i]})
			if err != nil {
				return err
			}
			batch.Queue("SELECT pg_notify($1, $2)", ChannelEvents, string(payload))
		}
		if err := tx.SendBatch(WithOperation(ctx, "outbox.notify"), batch).Close(); err != nil {
			return err
		}
		published = len(pending)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("publish outbox: %w", err)
	}
	return published, nil
}

func pendingEvents(ctx context.Context, tx pgx.Tx, limit int) ([]pendingEvent, error) {
	rows, err := tx.Query(WithOperation(ctx, "outbox.pending"), `
SELECT e.id, e.occurred_at, coalesce(p.public_id::text, ''), e.sequence
  FROM easycode.events e
  LEFT JOIN easycode.projects p ON p.id = e.project_id
 WHERE e.published_at IS NULL
 ORDER BY e.id
 LIMIT $1
   FOR UPDATE OF e`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (pendingEvent, error) {
		var current pendingEvent
		err := row.Scan(&current.id, &current.occurredAt, &current.projectID, &current.sequence)
		return current, err
	})
}

// assignPositions updates one row at a time, in id order (sequence order within a project),
// so global positions grow in the same order.
func assignPositions(ctx context.Context, tx pgx.Tx, pending []pendingEvent) ([]int64, error) {
	batch := &pgx.Batch{}
	for _, current := range pending {
		batch.Queue(`
UPDATE easycode.events SET published_at = now(), global_position = nextval('easycode.events_global_position_seq')
 WHERE id = $1 AND occurred_at = $2
RETURNING global_position`, current.id, current.occurredAt)
	}
	results := tx.SendBatch(WithOperation(ctx, "outbox.mark_published"), batch)
	positions := make([]int64, len(pending))
	for i := range pending {
		if err := results.QueryRow().Scan(&positions[i]); err != nil {
			_ = results.Close()
			return nil, err
		}
	}
	return positions, results.Close()
}

// TelemetryNotifier sends best-effort telemetry on ChannelEvents, outside any transaction.
type TelemetryNotifier struct {
	pool *pgxpool.Pool
}

var _ appevent.TelemetryEmitter = TelemetryNotifier{}

// NewTelemetryNotifier builds the emitter.
func NewTelemetryNotifier(pool *pgxpool.Pool) TelemetryNotifier {
	return TelemetryNotifier{pool: pool}
}

// Emit implements event.TelemetryEmitter.
func (n TelemetryNotifier) Emit(ctx context.Context, draft event.Draft) (event.Envelope, error) {
	if draft.Time.IsZero() {
		draft.Time = time.Now()
	}
	id, err := uuid.NewV7()
	if err != nil {
		return event.Envelope{}, fmt.Errorf("event id: %w", err)
	}
	envelope, err := event.NewTelemetry(draft, id.String())
	if err != nil {
		return event.Envelope{}, failure.New(failure.KindInvalidInput, CodeEventInvalid, err)
	}
	payload, err := json.Marshal(notifyPayload{Kind: kindTelemetry, ProjectID: envelope.ProjectID, Envelope: &envelope})
	if err != nil {
		return event.Envelope{}, fmt.Errorf("encode telemetry: %w", err)
	}
	if len(payload) > maxNotifyPayload {
		return event.Envelope{}, failure.New(failure.KindInvalidInput, CodeTelemetryTooLarge, nil)
	}
	if _, err := n.pool.Exec(WithOperation(ctx, "telemetry.notify"), "SELECT pg_notify($1, $2)", ChannelEvents, string(payload)); err != nil {
		return event.Envelope{}, fmt.Errorf("notify telemetry: %w", err)
	}
	return envelope, nil
}

// Listener holds one dedicated LISTEN connection (outside the pool) and reconnects after a
// failure. onConnect runs after every (re)connection: notifications sent while disconnected
// are lost, so consumers catch up by reading the outbox.
type Listener struct {
	pool      *pgxpool.Pool
	channels  []string
	logger    *slog.Logger
	retry     time.Duration
	onConnect func(ctx context.Context)
}

// NewListener builds a listener for channels with the connection settings of pool.
func NewListener(pool *pgxpool.Pool, logger *slog.Logger, onConnect func(ctx context.Context), channels ...string) *Listener {
	return &Listener{pool: pool, channels: channels, logger: logger, retry: time.Second, onConnect: onConnect}
}

// Run calls handle for each notification until ctx is canceled. handle must not block.
func (l *Listener) Run(ctx context.Context, handle func(channel, payload string)) error {
	for {
		err := l.listenOnce(ctx, handle)
		if ctx.Err() != nil {
			return nil
		}
		l.logger.WarnContext(ctx, "db listener disconnected", logging.ErrorAttr(err), slog.String("retry_in", l.retry.String()))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(l.retry):
		}
	}
}

func (l *Listener) listenOnce(ctx context.Context, handle func(channel, payload string)) error {
	connection, err := pgx.ConnectConfig(ctx, l.pool.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close(context.WithoutCancel(ctx)) }()
	for _, channel := range l.channels {
		if _, err := connection.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
			return err
		}
	}
	if l.onConnect != nil {
		l.onConnect(ctx)
	}
	for {
		notification, err := connection.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		handle(notification.Channel, notification.Payload)
	}
}

// EventListener implements event.Listener on ChannelEvents.
type EventListener struct {
	listener *Listener
	logger   *slog.Logger
}

var _ appevent.Listener = EventListener{}

// NewEventListener builds the api side of the bus. onConnect should trigger a catch-up read.
func NewEventListener(pool *pgxpool.Pool, logger *slog.Logger, onConnect func(ctx context.Context)) EventListener {
	return EventListener{listener: NewListener(pool, logger, onConnect, ChannelEvents), logger: logger}
}

// Listen implements event.Listener.
func (l EventListener) Listen(ctx context.Context, handle func(appevent.Notification)) error {
	return l.listener.Run(ctx, func(_, payload string) {
		notification, err := decodeNotification(payload)
		if err != nil {
			l.logger.WarnContext(ctx, "invalid event notification", logging.ErrorAttr(err))
			return
		}
		handle(notification)
	})
}

func decodeNotification(payload string) (appevent.Notification, error) {
	var message notifyPayload
	if err := json.Unmarshal([]byte(payload), &message); err != nil {
		return appevent.Notification{}, err
	}
	switch message.Kind {
	case kindDomain:
		return appevent.Notification{ProjectID: message.ProjectID, Sequence: message.Sequence, Position: message.Position}, nil
	case kindTelemetry:
		if message.Envelope == nil {
			return appevent.Notification{}, errors.New("telemetry without envelope")
		}
		return appevent.Notification{ProjectID: message.ProjectID, Telemetry: message.Envelope}, nil
	default:
		return appevent.Notification{}, fmt.Errorf("unknown notification kind %q", message.Kind)
	}
}
