package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/command"
)

// Commands is the command queue (commands, interno.md section 1).
type Commands struct {
	pool *pgxpool.Pool
}

var _ command.Queue = Commands{}

// NewCommands builds the adapter.
func NewCommands(pool *pgxpool.Pool) Commands { return Commands{pool: pool} }

// Next implements command.Queue.
func (c Commands) Next(ctx context.Context) (*command.Command, error) {
	var cmd command.Command
	var projectID *string
	var keyID *int64
	err := conn(ctx, c.pool).QueryRow(WithOperation(ctx, "commands.next"), `
SELECT c.public_id::text, c.type, c.target_type, o.public_id::text, p.public_id::text, c.provider_key_id, u.sub,
       c.payload, c.requested_at
  FROM easycode.commands c
  JOIN easycode.organizations o ON o.id = c.organization_id
  JOIN easycode.users u ON u.id = c.requested_by
  LEFT JOIN easycode.projects p ON p.id = c.project_id
 WHERE c.status = 'pending'
 ORDER BY c.requested_at, c.id
 LIMIT 1
 FOR UPDATE OF c SKIP LOCKED`).Scan(&cmd.ID, &cmd.Type, &cmd.TargetType, &cmd.OrganizationID, &projectID, &keyID,
		&cmd.RequestedBy, &cmd.Payload, &cmd.RequestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("next command: %w", classify(err))
	}
	if projectID != nil {
		cmd.ProjectID = *projectID
	}
	if keyID != nil {
		cmd.ProviderKeyID = *keyID
	}
	return &cmd, nil
}

// Applied implements command.Queue.
func (c Commands) Applied(ctx context.Context, id string) error {
	return c.finish(ctx, id, command.StateApplied, "")
}

// Refused implements command.Queue.
func (c Commands) Refused(ctx context.Context, id, reason string) error {
	return c.finish(ctx, id, command.StateRefused, reason)
}

func (c Commands) finish(ctx context.Context, id, status, reason string) error {
	_, err := conn(ctx, c.pool).Exec(WithOperation(ctx, "commands.finish"), `
UPDATE easycode.commands SET status = $2, processed_at = now(), refusal_reason = $3
 WHERE public_id = $1::uuid AND status = 'pending'`, id, status, nullable(reason))
	if err != nil {
		return fmt.Errorf("finish command: %w", classify(err))
	}
	return nil
}
