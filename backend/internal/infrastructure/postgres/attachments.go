package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	appattachment "easycode/internal/application/attachment"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Attachments stores the attachments of the business need (project_attachments, US-03).
type Attachments struct {
	pool *pgxpool.Pool
}

var _ appattachment.Repository = Attachments{}

// NewAttachments builds the adapter.
func NewAttachments(pool *pgxpool.Pool) Attachments { return Attachments{pool: pool} }

// Insert implements appattachment.Repository. The trigger project_attachments_rules refuses a project out
// of created (a race with the start of the workflow), mapped to state-conflict.
func (a Attachments) Insert(ctx context.Context, p identity.Principal, projectID string, in appattachment.Attachment) (appattachment.Attachment, error) {
	out := in
	err := conn(ctx, a.pool).QueryRow(WithOperation(ctx, "project_attachments.insert"), `
INSERT INTO easycode.project_attachments (public_id, project_id, file_name, media_type, size_bytes, sha256, path, submitted_by)
SELECT $3::uuid, pr.id, $4, $5, $6, $7, $8, u.id
  FROM easycode.projects pr JOIN easycode.users u ON u.organization_id = pr.organization_id AND u.sub = $2
 WHERE pr.public_id = $1::uuid
RETURNING submitted_at, (SELECT display_name FROM easycode.users WHERE id = submitted_by)`,
		projectID, p.Subject, in.ID, in.FileName, in.MediaType, in.SizeBytes, in.SHA256, in.Path).Scan(&out.SubmittedAt, &out.SubmittedBy.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return appattachment.Attachment{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "project_attachments_rules" {
		return appattachment.Attachment{}, failure.New(failure.KindConflict, failure.CodeStateConflict, err).With("available_actions", []string{})
	}
	if err != nil {
		return appattachment.Attachment{}, fmt.Errorf("insert attachment: %w", classify(err))
	}
	out.SubmittedBy.Subject = p.Subject
	return out, nil
}

// List implements appattachment.Repository.
func (a Attachments) List(ctx context.Context, projectID string) ([]appattachment.Attachment, error) {
	rows, err := conn(ctx, a.pool).Query(WithOperation(ctx, "project_attachments.list"), `
SELECT pa.public_id::text, pa.file_name, pa.media_type, pa.size_bytes, pa.sha256, pa.path, pa.submitted_at, u.sub, u.display_name
  FROM easycode.project_attachments pa JOIN easycode.projects pr ON pr.id = pa.project_id
  JOIN easycode.users u ON u.id = pa.submitted_by
 WHERE pr.public_id = $1::uuid
 ORDER BY pa.submitted_at, pa.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", classify(err))
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appattachment.Attachment, error) {
		var x appattachment.Attachment
		err := row.Scan(&x.ID, &x.FileName, &x.MediaType, &x.SizeBytes, &x.SHA256, &x.Path, &x.SubmittedAt, &x.SubmittedBy.Subject, &x.SubmittedBy.Name)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan attachments: %w", err)
	}
	if items == nil {
		items = []appattachment.Attachment{}
	}
	return items, nil
}
