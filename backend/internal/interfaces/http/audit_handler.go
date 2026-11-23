package http

import (
	"context"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	apptrail "easycode/internal/application/trail"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// AuditUseCases are the reads of the audit trail (US-35). There is no route that changes or deletes a record (CA-3).
type AuditUseCases interface {
	ProjectTrail(ctx context.Context, principal identity.Principal, projectID string, filter apptrail.Filter) (apptrail.Page, error)
	OrganizationTrail(ctx context.Context, principal identity.Principal, filter apptrail.Filter) (apptrail.Page, error)
}

type auditTargetJSON struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type auditRecordJSON struct {
	ID            string          `json:"id"`
	When          time.Time       `json:"when"`
	Actor         actorJSON       `json:"actor"`
	Origin        *string         `json:"origin"`
	Action        string          `json:"action"`
	Target        auditTargetJSON `json:"target"`
	Project       *string         `json:"project"`
	Reason        *string         `json:"reason"`
	Decision      *string         `json:"decision"`
	LostCost      *moneyJSON      `json:"lost_cost"`
	CorrelationID *string         `json:"correlationid,omitempty"`
}

type auditPageJSON struct {
	Items      []auditRecordJSON `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

// targetTypes are the singular names of the subjects written in audit_records.target.
var targetTypes = map[string]string{
	"waves": "wave", "gates": "gate_round", "findings": "finding", "members": "member", "agents": "agent",
	"models": "model", "prices": "price", "skills": "skill", "sessions": "session", "artifacts": "artifact", "commands": "command",
	"local-client-sessions": "local_client_session", "projects": "project",
}

// targetOf splits the stored target ("waves/REQ-01", "gates/gate.data/rounds/2", "project", "budget") into the
// AuditRecord target: a type and an id (the project id when the target is the project or one of its settings).
func targetOf(target, projectID string) auditTargetJSON {
	head, rest, found := strings.Cut(target, "/")
	if !found {
		if projectID != "" {
			return auditTargetJSON{Type: target, ID: projectID}
		}
		return auditTargetJSON{Type: target, ID: target}
	}
	kind, ok := targetTypes[head]
	if !ok {
		kind = head
	}
	if head == "providers" { // providers/{p}/key, providers/{p}/models/{m}, providers/{p}/models/{m}/prices/{n}
		switch {
		case strings.Contains(rest, "/prices/"):
			kind = "price"
		case strings.Contains(rest, "/models/"):
			kind = "model"
		case strings.HasSuffix(rest, "/key"):
			kind = "provider_key"
		default:
			kind = "provider"
		}
	}
	return auditTargetJSON{Type: kind, ID: rest}
}

func auditRecordOf(e apptrail.Entry) auditRecordJSON {
	out := auditRecordJSON{ID: e.ID, When: e.When, Actor: actorJSON{Type: e.ActorType, ID: e.ActorID}, Origin: optional(e.Origin),
		Action: e.Action, Target: targetOf(e.Target, e.ProjectID), Project: optional(e.ProjectID), Reason: optional(e.Reason),
		Decision: optional(e.Decision), CorrelationID: optional(e.CorrelationID)}
	if e.ActorType == "human" {
		out.Actor = actorJSON{Type: "human", ID: e.ActorID, User: &userRefJSON{ID: e.ActorID, Name: e.ActorName}}
	}
	if e.LostCost != "" {
		out.LostCost = &moneyJSON{e.LostCost, e.Currency}
	}
	return out
}

func queryTime(rc *fasthttp.RequestCtx, name string) (*time.Time, error) {
	raw := string(rc.QueryArgs().Peek(name))
	if raw == "" {
		return nil, nil
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, failure.Invalid(failure.FieldError{Field: name, Message: "date-time"})
	}
	return &at, nil
}

func auditFilterOf(rc *fasthttp.RequestCtx) (apptrail.Filter, error) {
	limit, err := queryLimit(rc)
	if err != nil {
		return apptrail.Filter{}, err
	}
	from, err := queryTime(rc, "from")
	if err != nil {
		return apptrail.Filter{}, err
	}
	until, err := queryTime(rc, "until")
	if err != nil {
		return apptrail.Filter{}, err
	}
	return apptrail.Filter{Actions: queryList(rc, "action"), From: from, Until: until, Order: string(rc.QueryArgs().Peek("order")),
		Limit: limit, Cursor: string(rc.QueryArgs().Peek("cursor"))}, nil
}

func (a *api) writeAuditPage(rc *fasthttp.RequestCtx, page apptrail.Page) {
	out := auditPageJSON{Items: make([]auditRecordJSON, 0, len(page.Items))}
	for _, e := range page.Items {
		out.Items = append(out.Items, auditRecordOf(e))
	}
	if page.NextCursor != "" {
		out.NextCursor = &page.NextCursor
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
}

// listProjectAudit is the trail of the project (US-35 CA-1, CA-2), for approvers and the lead.
func (a *api) listProjectAudit(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	filter, err := auditFilterOf(rc)
	if err != nil {
		return err
	}
	page, err := a.deps.Audit.ProjectTrail(RequestContext(rc), principal, projectID, filter)
	if err != nil {
		return err
	}
	a.writeAuditPage(rc, page)
	return nil
}

// listOrganizationAudit is the trail of the organization (administrator).
func (a *api) listOrganizationAudit(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	filter, err := auditFilterOf(rc)
	if err != nil {
		return err
	}
	page, err := a.deps.Audit.OrganizationTrail(RequestContext(rc), principal, filter)
	if err != nil {
		return err
	}
	a.writeAuditPage(rc, page)
	return nil
}
