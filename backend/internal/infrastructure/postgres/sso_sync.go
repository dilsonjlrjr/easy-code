package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/ssosync"
	"easycode/internal/domain/failure"
)

// AnonymizedDisplayName replaces the name of whoever left the organization (D-31).
const AnonymizedDisplayName = "Usuário anonimizado"

// SSOSync implements ssosync.Store for the worker.
type SSOSync struct {
	pool *pgxpool.Pool
}

var _ ssosync.Store = SSOSync{}

// NewSSOSync builds the adapter.
func NewSSOSync(pool *pgxpool.Pool) SSOSync { return SSOSync{pool: pool} }

// Organization implements ssosync.Store.
func (s SSOSync) Organization(ctx context.Context, issuer string) (string, error) {
	var id string
	err := conn(ctx, s.pool).QueryRow(WithOperation(ctx, "organizations.by_issuer"),
		"SELECT public_id::text FROM easycode.organizations WHERE oidc_issuer = $1", issuer).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return "", fmt.Errorf("organization of issuer: %w", classify(err))
	}
	return id, nil
}

// Cursor implements ssosync.Store: the latest session action recorded from the SSO.
func (s SSOSync) Cursor(ctx context.Context, organizationID string) (time.Time, error) {
	var cursor *time.Time
	err := conn(ctx, s.pool).QueryRow(WithOperation(ctx, "audit.sso_cursor"), `
SELECT max(a.recorded_at) FROM easycode.audit_records a JOIN easycode.organizations o ON o.id = a.organization_id
 WHERE o.public_id = $1::uuid AND a.external_event_id IS NOT NULL AND a.action LIKE 'session.%'`, organizationID).Scan(&cursor)
	if err != nil {
		return time.Time{}, fmt.Errorf("sso cursor: %w", classify(err))
	}
	if cursor == nil {
		return time.Time{}, nil
	}
	return *cursor, nil
}

// RecordedAt implements ssosync.Store.
func (s SSOSync) RecordedAt(ctx context.Context, organizationID string, at time.Time) (map[string]bool, error) {
	rows, err := conn(ctx, s.pool).Query(WithOperation(ctx, "audit.sso_recorded_at"), `
SELECT a.external_event_id FROM easycode.audit_records a JOIN easycode.organizations o ON o.id = a.organization_id
 WHERE o.public_id = $1::uuid AND a.recorded_at = $2 AND a.external_event_id IS NOT NULL`, organizationID, at)
	if err != nil {
		return nil, fmt.Errorf("sso recorded at: %w", classify(err))
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("sso recorded at: %w", classify(err))
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		seen[id] = true
	}
	return seen, nil
}

// ActiveSubjects implements ssosync.Store.
func (s SSOSync) ActiveSubjects(ctx context.Context, organizationID string) ([]string, error) {
	rows, err := conn(ctx, s.pool).Query(WithOperation(ctx, "users.active_subjects"), `
SELECT u.sub FROM easycode.users u JOIN easycode.organizations o ON o.id = u.organization_id
 WHERE o.public_id = $1::uuid AND u.active ORDER BY u.id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("active subjects: %w", classify(err))
	}
	subjects, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("active subjects: %w", classify(err))
	}
	return subjects, nil
}

// Offboard implements ssosync.Store.
func (s SSOSync) Offboard(ctx context.Context, organizationID, subject string) error {
	return NewTxRunner(s.pool).InTx(ctx, func(ctx context.Context) error {
		q := conn(ctx, s.pool)
		var userID int64
		err := q.QueryRow(WithOperation(ctx, "users.offboard"), `
UPDATE easycode.users u SET active = false, display_name = $3, anonymized_at = now()
  FROM easycode.organizations o
 WHERE o.id = u.organization_id AND o.public_id = $1::uuid AND u.sub = $2 AND u.active
RETURNING u.id`, organizationID, subject, AnonymizedDisplayName).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("offboard user: %w", classify(err))
		}
		return EndSessionsOf(ctx, q, userID, "offboarded")
	})
}
