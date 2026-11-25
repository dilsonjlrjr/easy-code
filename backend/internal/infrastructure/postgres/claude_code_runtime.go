package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"easycode/internal/application/providerkey"
)

var _ providerkey.RuntimeStore = ProviderKeys{}

// ClaudeCodeRuntime implements providerkey.Repository: the last detection recorded by the worker
// (claude_code_runtime, one row); without the row Claude Code is not detected (US-58 CA-2).
func (k ProviderKeys) ClaudeCodeRuntime(ctx context.Context) (providerkey.Runtime, error) {
	return claudeCodeRuntime(ctx, conn(ctx, k.pool))
}

// RecordClaudeCodeRuntime implements providerkey.RuntimeStore (worker role).
func (k ProviderKeys) RecordClaudeCodeRuntime(ctx context.Context, detected bool, version string) error {
	_, err := conn(ctx, k.pool).Exec(WithOperation(ctx, "claude_code_runtime.record"), `
INSERT INTO easycode.claude_code_runtime (id, detected, version, checked_at) VALUES (1, $1, nullif($2, ''), now())
ON CONFLICT (id) DO UPDATE SET detected = excluded.detected, version = excluded.version, checked_at = excluded.checked_at`,
		detected, version)
	if err != nil {
		return fmt.Errorf("record claude code runtime: %w", classify(err))
	}
	return nil
}

// ProviderUsable implements the project ModelLookup check of the model pair: claude-code needs the
// binary detected and an active key in the organization (US-58 CA-2); the other providers pass.
func (c Catalog) ProviderUsable(ctx context.Context, organizationID, providerName string) (bool, error) {
	if providerName != providerkey.ProviderClaudeCode {
		return true, nil
	}
	q := conn(ctx, c.pool)
	runtime, err := claudeCodeRuntime(ctx, q)
	if err != nil {
		return false, err
	}
	var active bool
	err = q.QueryRow(WithOperation(ctx, "provider_keys.has_active"), `
SELECT EXISTS (SELECT 1 FROM easycode.provider_keys pk JOIN easycode.organizations o ON o.id = pk.organization_id
                WHERE o.public_id = $1::uuid AND pk.provider = $2 AND pk.state = 'active')`, organizationID, providerName).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("read active key: %w", classify(err))
	}
	return providerkey.Usable(providerName, runtime, active), nil
}

func claudeCodeRuntime(ctx context.Context, q querier) (providerkey.Runtime, error) {
	var (
		runtime   providerkey.Runtime
		version   *string
		checkedAt time.Time
	)
	err := q.QueryRow(WithOperation(ctx, "claude_code_runtime.read"),
		"SELECT detected, version, checked_at FROM easycode.claude_code_runtime WHERE id = 1").Scan(&runtime.Detected, &version, &checkedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return providerkey.Runtime{}, nil
	}
	if err != nil {
		return providerkey.Runtime{}, fmt.Errorf("read claude code runtime: %w", classify(err))
	}
	if version != nil {
		runtime.Version = *version
	}
	runtime.CheckedAt = &checkedAt
	return runtime, nil
}
