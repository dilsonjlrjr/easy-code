package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apptrail "easycode/internal/application/trail"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/pricing"
)

// AuditReader reads the append-only trail (US-35). The api role has only SELECT and INSERT on
// easycode.audit_records; nothing here changes a record (CA-3, RNF-13).
type AuditReader struct {
	pool *pgxpool.Pool
}

var _ apptrail.Reader = AuditReader{}

// NewAuditReader builds the adapter.
func NewAuditReader(pool *pgxpool.Pool) AuditReader { return AuditReader{pool: pool} }

const auditColumns = `ar.public_id::text, ar.recorded_at, ar.actor_type, ar.actor_id, coalesce(u.display_name, ''), coalesce(ar.origin, ''),
       ar.action, ar.target, coalesce(p.public_id::text, ''), coalesce(ar.justification, ''), coalesce(ar.decision, ''),
       coalesce(ar.lost_cost::text, ''), o.currency, coalesce(ar.correlationid::text, '')
  FROM easycode.audit_records ar JOIN easycode.organizations o ON o.id = ar.organization_id
  LEFT JOIN easycode.projects p ON p.id = ar.project_id
  LEFT JOIN easycode.users u ON ar.actor_type = 'human' AND u.organization_id = ar.organization_id AND u.sub = ar.actor_id`

// The filters of both trails: $1 scope, $2 actions, $3 from, $4 until, $5/$6 cursor, $7 limit.
const auditFilters = `
   AND (coalesce(cardinality($2::text[]), 0) = 0 OR ar.action = ANY($2::text[]))
   AND ($3::timestamptz IS NULL OR ar.recorded_at >= $3::timestamptz)
   AND ($4::timestamptz IS NULL OR ar.recorded_at <= $4::timestamptz)`

// ProjectTrail implements apptrail.Reader.
func (r AuditReader) ProjectTrail(ctx context.Context, projectID string, filter apptrail.Filter) (apptrail.Page, error) {
	return r.page(ctx, "audit.project_trail", "ar.project_id = (SELECT x.id FROM easycode.projects x WHERE x.public_id = $1::uuid)", projectID, filter)
}

// OrganizationTrail implements apptrail.Reader: records of the organization without a project.
func (r AuditReader) OrganizationTrail(ctx context.Context, organizationID string, filter apptrail.Filter) (apptrail.Page, error) {
	return r.page(ctx, "audit.organization_trail",
		"ar.organization_id = (SELECT x.id FROM easycode.organizations x WHERE x.public_id = $1::uuid) AND ar.project_id IS NULL", organizationID, filter)
}

func (r AuditReader) page(ctx context.Context, operation, scope, scopeID string, filter apptrail.Filter) (apptrail.Page, error) {
	page := apptrail.Page{Items: []apptrail.Entry{}}
	after, afterID, err := decodeCursor(filter.Cursor)
	if err != nil {
		return page, failure.Invalid(failure.FieldError{Field: "cursor", Message: "invalid"})
	}
	direction, comparison := "ASC", ">"
	if filter.Order == apptrail.OrderDesc {
		direction, comparison = "DESC", "<"
	}
	// The cursor is the (when, public id) of the last record shown: keyset pagination without repeats (CA-27-8).
	sql := `SELECT ` + auditColumns + `
 WHERE ` + scope + auditFilters + `
   AND ($5::timestamptz IS NULL OR (ar.recorded_at, ar.public_id) ` + comparison + ` ($5::timestamptz, $6::uuid))
 ORDER BY ar.recorded_at ` + direction + `, ar.public_id ` + direction + `
 LIMIT $7`
	var cursorID any
	if after != nil {
		cursorID = afterID
	}
	rows, err := conn(ctx, r.pool).Query(WithOperation(ctx, operation), sql, scopeID, filter.Actions, filter.From, filter.Until, after, cursorID, filter.Limit+1)
	if err != nil {
		return page, fmt.Errorf("read audit trail: %w", classify(err))
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (apptrail.Entry, error) {
		var e apptrail.Entry
		err := row.Scan(&e.ID, &e.When, &e.ActorType, &e.ActorID, &e.ActorName, &e.Origin, &e.Action, &e.Target, &e.ProjectID,
			&e.Reason, &e.Decision, &e.LostCost, &e.Currency, &e.CorrelationID)
		if e.LostCost != "" {
			e.LostCost = pricing.NormalizeDecimal(e.LostCost)
		}
		return e, err
	})
	if err != nil {
		return page, fmt.Errorf("scan audit trail: %w", err)
	}
	if len(items) > filter.Limit {
		last := items[filter.Limit-1]
		page.NextCursor = encodeCursor(last.When, last.ID)
		items = items[:filter.Limit]
	}
	page.Items = items
	return page, nil
}
