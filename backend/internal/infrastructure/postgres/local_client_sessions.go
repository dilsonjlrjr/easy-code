package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/localclient"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// LocalClientSessions implements localclient.Store over local_client_sessions (US-45, ADR-20).
type LocalClientSessions struct {
	pool *pgxpool.Pool
}

var _ localclient.Store = LocalClientSessions{}

// NewLocalClientSessions builds the adapter.
func NewLocalClientSessions(pool *pgxpool.Pool) LocalClientSessions {
	return LocalClientSessions{pool: pool}
}

const localClientColumns = `s.id, s.public_id::text, u.sub, o.public_id::text, s.idp_session_id, s.device, coalesce(s.ip_address, ''),
       s.started_at, s.last_used_at, s.idp_checked_at, s.acknowledged_at IS NOT NULL, s.ended_at IS NOT NULL
  FROM easycode.local_client_sessions s
  JOIN easycode.users u ON u.id = s.user_id
  JOIN easycode.organizations o ON o.id = u.organization_id`

func scanLocalClient(row pgx.Row) (localclient.Record, error) {
	var r localclient.Record
	err := row.Scan(&r.ID, &r.PublicID, &r.Subject, &r.OrganizationID, &r.SessionID, &r.Device, &r.IPAddress,
		&r.StartedAt, &r.LastUsedAt, &r.CheckedAt, &r.Acknowledged, &r.Ended)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return r, fmt.Errorf("local client session: %w", classify(err))
	}
	return r, nil
}

// Find implements localclient.Store.
func (l LocalClientSessions) Find(ctx context.Context, subject, sessionID string) (localclient.Record, error) {
	return scanLocalClient(conn(ctx, l.pool).QueryRow(WithOperation(ctx, "local_client_sessions.find"),
		"SELECT "+localClientColumns+" WHERE u.sub = $1 AND s.idp_session_id = $2", subject, sessionID))
}

// Open implements localclient.Store: a concurrent first request finds the row of the other one.
func (l LocalClientSessions) Open(ctx context.Context, o localclient.Opening) (localclient.Record, error) {
	_, err := conn(ctx, l.pool).Exec(WithOperation(ctx, "local_client_sessions.open"), `
INSERT INTO easycode.local_client_sessions (user_id, idp_session_id, device, ip_address, started_at, last_used_at, idp_checked_at)
VALUES (`+principalUserIDSQLAt(1)+`, $3, $4, $5, $6, $6, $6)
ON CONFLICT (user_id, idp_session_id) DO NOTHING`, o.OrganizationID, o.Subject, o.SessionID, o.Device, nullable(o.IPAddress), o.At)
	if err != nil {
		return localclient.Record{}, fmt.Errorf("open local client session: %w", classify(err))
	}
	return l.Find(ctx, o.Subject, o.SessionID)
}

// Get implements localclient.Store.
func (l LocalClientSessions) Get(ctx context.Context, id int64) (localclient.Record, error) {
	return scanLocalClient(conn(ctx, l.pool).QueryRow(WithOperation(ctx, "local_client_sessions.get"),
		"SELECT "+localClientColumns+" WHERE s.id = $1", id))
}

// Touch implements localclient.Store.
func (l LocalClientSessions) Touch(ctx context.Context, id int64, usedAt time.Time, checkedAt *time.Time, ipAddress string) error {
	_, err := conn(ctx, l.pool).Exec(WithOperation(ctx, "local_client_sessions.touch"), `
UPDATE easycode.local_client_sessions
   SET last_used_at = greatest(last_used_at, $2), idp_checked_at = coalesce($3, idp_checked_at), ip_address = coalesce($4, ip_address)
 WHERE id = $1 AND ended_at IS NULL`, id, usedAt, checkedAt, nullable(ipAddress))
	if err != nil {
		return fmt.Errorf("touch local client session: %w", classify(err))
	}
	return nil
}

// End implements localclient.Store.
func (l LocalClientSessions) End(ctx context.Context, id int64, reason string) error {
	_, err := conn(ctx, l.pool).Exec(WithOperation(ctx, "local_client_sessions.end"),
		"UPDATE easycode.local_client_sessions SET ended_at = now(), end_reason = $2 WHERE id = $1 AND ended_at IS NULL", id, reason)
	if err != nil {
		return fmt.Errorf("end local client session: %w", classify(err))
	}
	return nil
}

// List implements localclient.Store.
func (l LocalClientSessions) List(ctx context.Context, p identity.Principal) ([]localclient.Record, error) {
	rows, err := conn(ctx, l.pool).Query(WithOperation(ctx, "local_client_sessions.list"),
		"SELECT "+localClientColumns+" WHERE s.user_id = "+principalUserIDSQLAt(1)+" AND s.ended_at IS NULL ORDER BY s.started_at DESC, s.id DESC LIMIT 100",
		p.OrganizationID, p.Subject)
	if err != nil {
		return nil, fmt.Errorf("list local client sessions: %w", classify(err))
	}
	defer rows.Close()
	var out []localclient.Record
	for rows.Next() {
		r, err := scanLocalClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Own implements localclient.Store.
func (l LocalClientSessions) Own(ctx context.Context, p identity.Principal, publicID string) (localclient.Record, error) {
	return scanLocalClient(conn(ctx, l.pool).QueryRow(WithOperation(ctx, "local_client_sessions.own"),
		"SELECT "+localClientColumns+" WHERE s.public_id = $3::uuid AND s.user_id = "+principalUserIDSQLAt(1)+" AND s.ended_at IS NULL",
		p.OrganizationID, p.Subject, publicID))
}

// Acknowledge implements localclient.Store.
func (l LocalClientSessions) Acknowledge(ctx context.Context, id int64, at time.Time) error {
	_, err := conn(ctx, l.pool).Exec(WithOperation(ctx, "local_client_sessions.acknowledge"),
		"UPDATE easycode.local_client_sessions SET acknowledged_at = $2 WHERE id = $1 AND acknowledged_at IS NULL", id, at)
	if err != nil {
		return fmt.Errorf("acknowledge local client session: %w", classify(err))
	}
	return nil
}
