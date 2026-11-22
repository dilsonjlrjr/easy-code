package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/idempotency"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// IdempotencyKeys stores the first answers of POST requests (idempotency_keys, ADR-17).
type IdempotencyKeys struct {
	pool *pgxpool.Pool
}

var _ idempotency.Store = IdempotencyKeys{}

// NewIdempotencyKeys builds the adapter.
func NewIdempotencyKeys(pool *pgxpool.Pool) IdempotencyKeys { return IdempotencyKeys{pool: pool} }

// Find implements idempotency.Store.
func (k IdempotencyKeys) Find(ctx context.Context, p identity.Principal, key string) (*idempotency.Stored, error) {
	var stored idempotency.Stored
	var resource *string
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "idempotency.find"), `
SELECT operation, body_hash, http_status, coalesce(answer #>> '{}', ''), resource_id FROM easycode.idempotency_keys
 WHERE user_id = `+principalUserIDSQL+` AND key = $3::uuid AND expires_at > now()`,
		p.OrganizationID, p.Subject, key).Scan(&stored.Operation, &stored.BodyHash, &stored.Response.Status, &stored.Response.Body, &resource)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find idempotency key: %w", classify(err))
	}
	if resource != nil {
		stored.Response.ResourceID = *resource
	}
	if len(stored.Response.Body) == 0 {
		stored.Response.Body = nil
	}
	return &stored, nil
}

// Save implements idempotency.Store. An expired key is reused; one in its window is a race.
func (k IdempotencyKeys) Save(ctx context.Context, req idempotency.Request, resp idempotency.Response) error {
	// The body is kept as a JSON string, so the replay is byte for byte the first answer.
	body := resp.Body
	tag, err := conn(ctx, k.pool).Exec(WithOperation(ctx, "idempotency.save"), `
INSERT INTO easycode.idempotency_keys (user_id, key, operation, body_hash, http_status, answer, resource_id)
VALUES (`+principalUserIDSQL+`, $3::uuid, $4, $5, $6, to_jsonb($7::text), $8)
ON CONFLICT (user_id, key) DO UPDATE SET operation = excluded.operation, body_hash = excluded.body_hash,
  http_status = excluded.http_status, answer = excluded.answer, resource_id = excluded.resource_id,
  created_at = now(), expires_at = now() + interval '24 hours'
 WHERE idempotency_keys.expires_at <= now()`,
		req.Principal.OrganizationID, req.Principal.Subject, req.Key, req.Operation, req.BodyHash, resp.Status, string(body), nullable(resp.ResourceID))
	if err != nil {
		return fmt.Errorf("save idempotency key: %w", classify(err))
	}
	if tag.RowsAffected() == 0 {
		return failure.New(failure.KindConflict, idempotency.CodeRace, nil)
	}
	return nil
}
