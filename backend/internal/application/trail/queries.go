// Package trail holds the reads of the audit trail (US-35, RNF-13).
package trail

import (
	"context"
	"regexp"
	"time"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Orders of the trail (PL-08).
const (
	OrderAsc  = "asc"
	OrderDesc = "desc"
)

var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

// Filter narrows the trail (US-35 CA-2): actions, period [From, Until] and order by "when".
type Filter struct {
	Actions []string
	From    *time.Time
	Until   *time.Time
	Order   string
	Limit   int
	Cursor  string
}

// Entry is one stored record as the trail shows it (AuditRecord).
type Entry struct {
	ID            string
	When          time.Time
	ActorType     string
	ActorID       string // subject of a human, agent id or "orchestrator"
	ActorName     string // display name of a human (anonymized name after RNF-20), "" otherwise
	Origin        string // "" when the action is not human
	Action        string
	Target        string
	ProjectID     string // "" for organization actions
	Reason        string
	Decision      string
	LostCost      string // "" when not applicable
	Currency      string
	CorrelationID string
}

// Page is one page of the trail.
type Page struct {
	Items      []Entry
	NextCursor string
}

// Reader reads the append-only trail (api role: SELECT only, RNF-13).
type Reader interface {
	ProjectTrail(ctx context.Context, projectID string, filter Filter) (Page, error)
	// OrganizationTrail lists the records of the organization without a project (keys, prices, models,
	// skills, sessions, ceiling).
	OrganizationTrail(ctx context.Context, organizationID string, filter Filter) (Page, error)
}

// Queries are the reads of the audit trail (US-35). There is no operation that changes or deletes a record
// (CA-3): the trail has only these reads and Trail.Append.
type Queries struct {
	projects appproject.Repository
	reader   Reader
}

// NewQueries builds the queries.
func NewQueries(projects appproject.Repository, reader Reader) *Queries {
	return &Queries{projects: projects, reader: reader}
}

// Validate checks the filter of the trail and applies the defaults.
func (f *Filter) Validate() error {
	var fields []failure.FieldError
	switch f.Order {
	case "":
		f.Order = OrderAsc
	case OrderAsc, OrderDesc:
	default:
		fields = append(fields, failure.FieldError{Field: "order", Message: "enum"})
	}
	for _, action := range f.Actions {
		if !actionPattern.MatchString(action) {
			fields = append(fields, failure.FieldError{Field: "action", Message: "pattern"})
			break
		}
	}
	if f.From != nil && f.Until != nil && f.Until.Before(*f.From) {
		fields = append(fields, failure.FieldError{Field: "until", Message: "range"})
	}
	if len(fields) > 0 {
		return failure.Invalid(fields...)
	}
	return nil
}

// ProjectTrail is listProjectAudit: approver and lead only (member → 403, no role → 404).
func (q *Queries) ProjectTrail(ctx context.Context, principal identity.Principal, projectID string, filter Filter) (Page, error) {
	if _, err := appproject.Authorize(ctx, q.projects, principal, projectID, identity.RoleApprover, identity.RoleLead); err != nil {
		return Page{}, err
	}
	if err := filter.Validate(); err != nil {
		return Page{}, err
	}
	return q.reader.ProjectTrail(ctx, projectID, filter)
}

// OrganizationTrail is listOrganizationAudit: the organization administrator only.
func (q *Queries) OrganizationTrail(ctx context.Context, principal identity.Principal, filter Filter) (Page, error) {
	if !principal.Administrator {
		return Page{}, failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	if err := filter.Validate(); err != nil {
		return Page{}, err
	}
	return q.reader.OrganizationTrail(ctx, principal.OrganizationID, filter)
}
