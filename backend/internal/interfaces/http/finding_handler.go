package http

import (
	"context"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	appfinding "easycode/internal/application/finding"
	"easycode/internal/application/traceability"
	"easycode/internal/domain/failure"
	domainfinding "easycode/internal/domain/finding"
	"easycode/internal/domain/identity"
)

// FindingUseCases are the findings of a project and the human decisions on them (US-32, US-33).
type FindingUseCases interface {
	List(ctx context.Context, principal identity.Principal, projectID string, filter appfinding.Filter) (appfinding.Page, error)
	Get(ctx context.Context, principal identity.Principal, projectID, code string) (appfinding.View, error)
	AcceptRisk(ctx context.Context, principal identity.Principal, session identity.Session, decision appfinding.Decision) (appfinding.View, error)
	AuthorizeExtraRound(ctx context.Context, principal identity.Principal, session identity.Session, decision appfinding.Decision) (appfinding.View, error)
	DecideScopeChange(ctx context.Context, principal identity.Principal, session identity.Session, decision appfinding.Decision) (appfinding.View, error)
	Discard(ctx context.Context, principal identity.Principal, session identity.Session, decision appfinding.Decision) (appfinding.View, error)
}

// TraceabilityUseCases are the traceability graph and the coverage matrix (US-28).
type TraceabilityUseCases interface {
	Graph(ctx context.Context, principal identity.Principal, projectID, root string, depth int, direction string) (traceability.Graph, error)
	Coverage(ctx context.Context, principal identity.Principal, projectID string, priorities []string) ([]traceability.Coverage, error)
}

func findingSummary(f domainfinding.Finding, now time.Time) map[string]any {
	return map[string]any{"id": f.Code, "title": f.Title, "severity": f.Severity, "category": f.Category, "status": f.Status,
		"destination_stage": f.DestinationStage, "origin_stage": f.OriginStage, "author": f.Author, "blocking": f.Blocking(),
		"cycles": len(f.Cycles), "opened_at": f.OpenedAt, "acceptance_expired": f.AcceptanceExpired(now)}
}

func findingDetail(v appfinding.View, now time.Time) map[string]any {
	f := v.Finding
	out := findingSummary(f, now)
	out["artifact"] = f.Code
	out["origin_wave"] = f.OriginWave
	out["available_actions"] = v.Actions
	out["resolution"] = nullableString(f.Resolution)
	out["discard_reason"] = nullableString(f.DiscardReason)
	out["modified_artifacts"] = orEmpty(f.ModifiedArtifacts)
	affected := make([]map[string]any, 0, len(f.Affected))
	for _, ref := range f.Affected {
		item := map[string]any{"artifact": ref}
		if code := domainfinding.ArtifactOf(ref); code != "" && code != ref {
			item["artifact"], item["item"] = code, ref
		}
		affected = append(affected, item)
	}
	out["affected_artifacts"] = affected
	cycles := make([]map[string]any, 0, len(f.Cycles))
	for _, c := range f.Cycles {
		cycle := map[string]any{"number": c.Number, "rework_wave": c.ReworkWave, "result": nullableString(c.Result)}
		if c.RevalidationWave != "" {
			cycle["revalidation_wave"] = c.RevalidationWave
		}
		if c.CommitOrVersion != "" {
			cycle["commit_or_version"] = c.CommitOrVersion
		}
		cycles = append(cycles, cycle)
	}
	out["cycle_history"] = cycles
	escalations := make([]map[string]any, 0, len(f.Escalations))
	for _, e := range f.Escalations {
		escalation := map[string]any{"when": e.At, "reason": e.Reason, "decision": nullableString(e.Decision), "decided_at": e.DecidedAt,
			"justification": nullableString(e.Justification), "modified_by": nullableString(e.ModifiedBy), "starting_point_artifacts": orEmpty(e.StartingPoint)}
		if e.AuthorizedBy != "" {
			escalation["authorized_by"] = userRefJSON{ID: e.AuthorizedBy, Name: e.AuthorizedByName}
		}
		escalations = append(escalations, escalation)
	}
	out["escalations"] = escalations
	out["acceptance"] = nil
	if f.AcceptedAt != nil && f.AcceptedBy != "" {
		acceptance := map[string]any{"per": userRefJSON{ID: f.AcceptedBy, Name: f.AcceptedByName}, "when": *f.AcceptedAt,
			"justification": f.Justification, "origin": "web", "valid_until": nil}
		if f.ValidUntil != nil {
			acceptance["valid_until"] = f.ValidUntil.Format("2006-01-02")
		}
		out["acceptance"] = acceptance
	}
	return out
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (a *api) listFindings(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	limit, err := queryLimit(rc)
	if err != nil {
		return err
	}
	args := rc.QueryArgs()
	page, err := a.deps.Findings.List(RequestContext(rc), principal, projectID, appfinding.Filter{
		Statuses: queryList(rc, "status"), Severities: queryList(rc, "severity"), Origin: string(args.Peek("origin_stage")),
		Destination: string(args.Peek("destination_stage")), Author: string(args.Peek("author")), Limit: limit, Cursor: string(args.Peek("cursor")),
	})
	if err != nil {
		return err
	}
	now := time.Now()
	items := make([]map[string]any, 0, len(page.Items))
	for _, f := range page.Items {
		items = append(items, findingSummary(f, now))
	}
	var next any
	if page.NextCursor != "" {
		next = page.NextCursor
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items, "next_cursor": next})
	return nil
}

func findingPath(rc *fasthttp.RequestCtx) (string, string, error) {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return "", "", err
	}
	code := PathParam(rc, "findingId")
	if !findingIDPattern(code) {
		return "", "", failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return projectID, code, nil
}

func findingIDPattern(code string) bool {
	if len(code) < 6 || code[:4] != "ACH-" {
		return false
	}
	_, err := strconv.Atoi(code[4:])
	return err == nil
}

func (a *api) getFinding(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := findingPath(rc)
	if err != nil {
		return err
	}
	view, err := a.deps.Findings.Get(RequestContext(rc), principal, projectID, code)
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, findingDetail(view, time.Now()))
	return nil
}

// decideFinding runs a synchronous decision with idempotency (DecidedFinding).
func (a *api) decideFinding(rc *fasthttp.RequestCtx, principal identity.Principal, operation string, body any,
	apply func(ctx context.Context, decision appfinding.Decision) (appfinding.View, error), fill func(*appfinding.Decision) error) error {
	projectID, code, err := findingPath(rc)
	if err != nil {
		return err
	}
	if err := decodeJSON(rc, body); err != nil {
		return err
	}
	decision := appfinding.Decision{ProjectID: projectID, Finding: code, CSRFToken: string(rc.Request.Header.Peek("X-CSRF-Token"))}
	if err := fill(&decision); err != nil {
		return err
	}
	return a.idempotent(rc, principal, operation, func(ctx context.Context) (created, error) {
		view, err := apply(ctx, decision)
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusOK, body: findingDetail(view, time.Now())}, nil
	})
}

func (a *api) acceptFindingRisk(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body struct {
		Justification string `json:"justification"`
		ValidUntil    string `json:"valid_until"`
	}
	session := currentSession(rc)
	return a.decideFinding(rc, principal, "acceptFindingRisk", &body, func(ctx context.Context, d appfinding.Decision) (appfinding.View, error) {
		return a.deps.Findings.AcceptRisk(ctx, principal, session, d)
	}, func(d *appfinding.Decision) error {
		d.Justification = body.Justification
		if body.ValidUntil != "" {
			date, err := time.Parse("2006-01-02", body.ValidUntil)
			if err != nil {
				return failure.Invalid(failure.FieldError{Field: "valid_until", Message: "date"})
			}
			d.ValidUntil = &date
		}
		return nil
	})
}

func (a *api) authorizeFindingExtraRound(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body struct {
		Justification string `json:"justification"`
	}
	session := currentSession(rc)
	return a.decideFinding(rc, principal, "authorizeFindingExtraRound", &body, func(ctx context.Context, d appfinding.Decision) (appfinding.View, error) {
		return a.deps.Findings.AuthorizeExtraRound(ctx, principal, session, d)
	}, func(d *appfinding.Decision) error {
		d.Justification = body.Justification
		return nil
	})
}

func (a *api) decideFindingScopeChange(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body struct {
		Justification string   `json:"justification"`
		ModifiedBy    string   `json:"modified_by"`
		StartingPoint []string `json:"starting_point_artifacts"`
	}
	session := currentSession(rc)
	return a.decideFinding(rc, principal, "decideFindingScopeChange", &body, func(ctx context.Context, d appfinding.Decision) (appfinding.View, error) {
		return a.deps.Findings.DecideScopeChange(ctx, principal, session, d)
	}, func(d *appfinding.Decision) error {
		d.Justification, d.ModifiedBy, d.StartingPoint = body.Justification, body.ModifiedBy, body.StartingPoint
		return nil
	})
}

func (a *api) discardFinding(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body struct {
		Reason        string `json:"reason"`
		Justification string `json:"justification"`
		DuplicateOf   string `json:"duplicate_of"`
	}
	session := currentSession(rc)
	return a.decideFinding(rc, principal, "discardFinding", &body, func(ctx context.Context, d appfinding.Decision) (appfinding.View, error) {
		return a.deps.Findings.Discard(ctx, principal, session, d)
	}, func(d *appfinding.Decision) error {
		d.Reason, d.Justification, d.DuplicateOf = body.Reason, body.Justification, body.DuplicateOf
		return nil
	})
}

func (a *api) getArtifactTraceability(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	depth := 0
	if raw := string(rc.QueryArgs().Peek("depth")); raw != "" {
		if depth, err = strconv.Atoi(raw); err != nil {
			return failure.Invalid(failure.FieldError{Field: "depth", Message: "integer"})
		}
	}
	graph, err := a.deps.Traceability.Graph(RequestContext(rc), principal, projectID, PathParam(rc, "artifactId"), depth,
		string(rc.QueryArgs().Peek("direction")))
	if err != nil {
		return err
	}
	nodes := make([]map[string]any, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		node := map[string]any{"id": n.ID, "type": n.Type, "exists": n.Exists}
		if n.State != "" {
			node["state"] = n.State
		}
		if n.Title != "" {
			node["title"] = n.Title
		}
		nodes = append(nodes, node)
	}
	edges := make([]map[string]any, 0, len(graph.Edges))
	for _, e := range graph.Edges {
		edges = append(edges, map[string]any{"from": e.From, "to": e.To, "rule": e.Rule})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"root": graph.Root, "nodes": nodes, "edges": edges})
	return nil
}

func (a *api) getCoverageMatrix(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	rows, err := a.deps.Traceability.Coverage(RequestContext(rc), principal, projectID, queryList(rc, "priority"))
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		criteria := make([]map[string]any, 0, len(r.Criteria))
		for _, c := range r.Criteria {
			cts := make([]map[string]any, 0, len(c.CTs))
			for _, ct := range c.CTs {
				cts = append(cts, map[string]any{"ct": ct.CT, "result": ct.Result})
			}
			criteria = append(criteria, map[string]any{"ca": c.ID, "cts": cts})
		}
		items = append(items, map[string]any{"us": r.US, "priority": r.Priority, "status": r.Status, "criteria": criteria})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items})
	return nil
}
