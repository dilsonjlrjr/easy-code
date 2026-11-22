package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appaudit "easycode/internal/application/audit"
	"easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
)

// Codes of the audit trail adapter.
const (
	CodeAuditInvalid       = "audit-invalid"
	CodeAuditUnknownTarget = "audit-unknown-reference"
)

// AuditTrail appends to easycode.audit_records (RNF-13). The table is append-only: a trigger
// refuses UPDATE and DELETE except for the retention role, and the api and worker roles have
// only INSERT on it.
type AuditTrail struct {
	pool *pgxpool.Pool
}

var _ appaudit.Trail = AuditTrail{}

// NewAuditTrail builds the adapter.
func NewAuditTrail(pool *pgxpool.Pool) AuditTrail {
	return AuditTrail{pool: pool}
}

// The public ids are resolved to internal keys in the same statement. No row means an id
// that does not exist, or a project outside the given organization.
const insertAuditRecord = `
INSERT INTO easycode.audit_records (organization_id, project_id, recorded_at, actor_type, actor_id, origin, policy,
  action, target, decision, justification, reference_list, lost_cost, event_id, correlationid, external_event_id)
SELECT coalesce(p.organization_id, o.id), p.id, coalesce($3::timestamptz, now()), $4, $5, $6, $7,
  $8, $9, $10, $11, $12::jsonb, $13::numeric, ev.id, $15::uuid, $16
  FROM (SELECT 1) AS one
  LEFT JOIN easycode.organizations o ON o.public_id = $1::uuid
  LEFT JOIN easycode.projects p ON p.public_id = $2::uuid
  LEFT JOIN LATERAL (SELECT e.id FROM easycode.events e WHERE e.public_id = $14::uuid LIMIT 1) ev ON true
 WHERE ($1::uuid IS NULL OR o.id IS NOT NULL)
   AND ($2::uuid IS NULL OR p.id IS NOT NULL)
   AND (o.id IS NULL OR p.id IS NULL OR p.organization_id = o.id)
   AND ($14::uuid IS NULL OR ev.id IS NOT NULL)
RETURNING public_id::text, recorded_at`

// Append implements audit.Trail; inside transaction.Runner it joins the caller's transaction.
func (t AuditTrail) Append(ctx context.Context, record audit.Record) (audit.Entry, error) {
	if err := record.Validate(); err != nil {
		return audit.Entry{}, failure.New(failure.KindInvalidInput, CodeAuditInvalid, err)
	}
	references := record.References
	if references == nil {
		references = []string{}
	}
	referenceList, err := json.Marshal(references)
	if err != nil {
		return audit.Entry{}, fmt.Errorf("encode audit references: %w", err)
	}
	var recordedAt any
	if !record.RecordedAt.IsZero() {
		recordedAt = record.RecordedAt
	}
	var entry audit.Entry
	err = conn(ctx, t.pool).QueryRow(WithOperation(ctx, "audit.append"), insertAuditRecord,
		nullable(record.OrganizationID), nullable(record.ProjectID), recordedAt,
		string(record.Actor.Type), record.Actor.ID, nullable(string(record.Origin)), nullable(record.Policy),
		record.Action, record.Target, nullable(record.Decision), nullable(record.Justification), string(referenceList),
		nullable(record.LostCost), nullable(record.EventID), nullable(record.CorrelationID), nullable(record.ExternalEventID),
	).Scan(&entry.ID, &entry.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return audit.Entry{}, failure.New(failure.KindInvalidInput, CodeAuditUnknownTarget, nil)
	}
	if err != nil {
		return audit.Entry{}, fmt.Errorf("append audit record: %w", classify(err))
	}
	return entry, nil
}
