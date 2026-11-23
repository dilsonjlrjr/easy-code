package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/session"
	"easycode/internal/domain/failure"
)

// WebSessions implements session.Store over web_sessions (ADR-12).
type WebSessions struct {
	pool *pgxpool.Pool
}

var _ session.Store = WebSessions{}

// NewWebSessions builds the adapter.
func NewWebSessions(pool *pgxpool.Pool) WebSessions { return WebSessions{pool: pool} }

// Create implements session.Store.
func (w WebSessions) Create(ctx context.Context, tokenHash []byte, r session.Record) (int64, error) {
	var id int64
	s := r.Session
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "web_sessions.create"), `
INSERT INTO easycode.web_sessions (token_hash, user_id, csrf_token, idp_session_id, sealed_refresh_token,
                                   authenticated_at, issued_at, last_seen_at, idp_checked_at)
VALUES ($1, `+principalUserIDSQLAt(2)+`, $4, $5, $6, $7, $8, $8, $8)
RETURNING id`, tokenHash, s.OrganizationID, s.Subject, s.CSRFToken, nullable(r.IDPSessionID), r.SealedRefreshToken,
		s.AuthenticatedAt, s.IssuedAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create web session: %w", classify(err))
	}
	return id, nil
}

// principalUserIDSQLAt resolves the user of parameters $n (organization public id) and $n+1 (sub).
func principalUserIDSQLAt(n int) string {
	return fmt.Sprintf(`(SELECT uu.id FROM easycode.users uu JOIN easycode.organizations oo ON oo.id = uu.organization_id
 WHERE oo.public_id = $%d::uuid AND uu.sub = $%d)`, n, n+1)
}

// Find implements session.Store.
func (w WebSessions) Find(ctx context.Context, tokenHash []byte) (session.Record, error) {
	var r session.Record
	var idpSession *string
	var ended *time.Time
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "web_sessions.find"), `
SELECT s.id, u.sub, o.public_id::text, s.csrf_token, s.idp_session_id, s.sealed_refresh_token,
       s.authenticated_at, s.issued_at, s.last_seen_at, s.idp_checked_at, s.ended_at
  FROM easycode.web_sessions s
  JOIN easycode.users u ON u.id = s.user_id
  JOIN easycode.organizations o ON o.id = u.organization_id
 WHERE s.token_hash = $1`, tokenHash).Scan(&r.Session.ID, &r.Session.Subject, &r.Session.OrganizationID, &r.Session.CSRFToken,
		&idpSession, &r.SealedRefreshToken, &r.Session.AuthenticatedAt, &r.Session.IssuedAt, &r.Session.LastSeenAt, &r.IDPCheckedAt, &ended)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return r, fmt.Errorf("find web session: %w", classify(err))
	}
	if idpSession != nil {
		r.IDPSessionID = *idpSession
	}
	r.Ended = ended != nil
	return r, nil
}

// Touch implements session.Store.
func (w WebSessions) Touch(ctx context.Context, id int64, seenAt time.Time, checkedAt *time.Time, sealed []byte) error {
	var err error
	if checkedAt == nil {
		_, err = conn(ctx, w.pool).Exec(WithOperation(ctx, "web_sessions.touch"),
			"UPDATE easycode.web_sessions SET last_seen_at = greatest(last_seen_at, $2) WHERE id = $1 AND ended_at IS NULL", id, seenAt)
	} else {
		_, err = conn(ctx, w.pool).Exec(WithOperation(ctx, "web_sessions.touch_checked"), `
UPDATE easycode.web_sessions SET last_seen_at = greatest(last_seen_at, $2), idp_checked_at = $3, sealed_refresh_token = $4
 WHERE id = $1 AND ended_at IS NULL`, id, seenAt, *checkedAt, sealed)
	}
	if err != nil {
		return fmt.Errorf("touch web session: %w", classify(err))
	}
	return nil
}

// End implements session.Store; ending an ended session changes nothing.
func (w WebSessions) End(ctx context.Context, id int64, reason string) ([]byte, error) {
	var sealed []byte
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "web_sessions.end"), `
UPDATE easycode.web_sessions SET ended_at = now(), end_reason = $2 WHERE id = $1 AND ended_at IS NULL
RETURNING sealed_refresh_token`, id, reason).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("end web session: %w", classify(err))
	}
	return sealed, nil
}

// Active implements session.Store.
func (w WebSessions) Active(ctx context.Context, id int64) (bool, error) {
	var active bool
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "web_sessions.active"),
		"SELECT EXISTS (SELECT 1 FROM easycode.web_sessions WHERE id = $1 AND ended_at IS NULL)", id).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("web session active: %w", classify(err))
	}
	return active, nil
}

// EndSessionsOf ends the open sessions of a person (offboarding, D-31); the worker calls it.
func EndSessionsOf(ctx context.Context, q querier, userID int64, reason string) error {
	_, err := q.Exec(WithOperation(ctx, "web_sessions.end_of_user"),
		"UPDATE easycode.web_sessions SET ended_at = now(), end_reason = $2 WHERE user_id = $1 AND ended_at IS NULL", userID, reason)
	if err != nil {
		return fmt.Errorf("end sessions of user: %w", classify(err))
	}
	// The sessions of the desktop app end too, and the last address seen goes (RNF-20).
	_, err = q.Exec(WithOperation(ctx, "local_client_sessions.end_of_user"), `
UPDATE easycode.local_client_sessions SET ended_at = coalesce(ended_at, now()), end_reason = coalesce(end_reason, $2), ip_address = NULL
 WHERE user_id = $1 AND (ended_at IS NULL OR ip_address IS NOT NULL)`, userID, reason)
	if err != nil {
		return fmt.Errorf("end local client sessions of user: %w", classify(err))
	}
	return nil
}
