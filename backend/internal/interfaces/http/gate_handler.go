package http

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/valyala/fasthttp"

	appgate "easycode/internal/application/gate"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// GateUseCases are the gate rounds and the human decisions (MARCO-1 gate.requirements).
type GateUseCases interface {
	Rounds(ctx context.Context, principal identity.Principal, projectID, gate string) ([]appgate.RoundView, error)
	Round(ctx context.Context, principal identity.Principal, projectID, gate string, round int) (appgate.RoundView, error)
	Decide(ctx context.Context, principal identity.Principal, session identity.Session, decision appgate.Decision) (appgate.RoundView, error)
}

func roundOf(r appgate.RoundView) map[string]any {
	out := map[string]any{"gate": r.Gate, "stage": r.Stage, "round": r.Round, "state": r.State, "opened_at": r.OpenedAt,
		"approval_mode": r.ApprovalMode, "min_approvals": 1, "partial_approvals": []any{}, "blocking_findings": findingRefs(r.BlockingFindings, true),
		"non_blocking_findings": findingRefs(r.NonBlockingFindings, false), "available_actions": []string{}, "decision": nil}
	outputs := make([]map[string]any, 0, len(r.Outputs))
	for _, o := range r.Outputs {
		outputs = append(outputs, map[string]any{"artifact": o.Code, "version": o.Version, "type": o.Type, "state": o.State})
	}
	out["outputs"] = outputs
	checks := make([]map[string]any, 0, len(r.Checks))
	for _, c := range r.Checks {
		check := map[string]any{"check": c.Check, "name": c.Check, "type": c.Type, "failure_severity": c.Severity, "status": c.Status, "confirmed_by": nil}
		if c.ConfirmedBy != nil {
			check["confirmed_by"] = decisionJSON{Per: actorJSON{Type: "human", User: &userRefJSON{ID: c.ConfirmedBy.By.Subject, Name: c.ConfirmedBy.By.Name}},
				When: c.ConfirmedBy.At, Origin: optional("web")}
		}
		evidence := json.RawMessage("[]")
		if len(c.Evidence) > 0 {
			evidence = c.Evidence
		}
		check["evidence"] = evidence
		checks = append(checks, check)
	}
	out["checks"] = checks
	out["reevaluation"] = nil
	if r.ReevaluatedBy != "" {
		out["reevaluation"] = map[string]any{"cause": "finding_opened", "finding": r.ReevaluatedBy, "evaluation": r.Evaluation}
	}
	out["rework_wave"] = nil
	if r.ReworkWave != "" {
		out["rework_wave"] = r.ReworkWave
	}
	if r.State == appgate.StateAwaitingHuman {
		out["available_actions"] = []string{"approve", "reject"}
	}
	if d := r.Decision; d != nil {
		per := actorJSON{Type: d.ByType}
		if d.ByType == "human" {
			per.User = &userRefJSON{ID: d.By.Subject, Name: d.By.Name}
		} else {
			per.ID = "orchestrator"
		}
		decision := map[string]any{"result": d.Result, "record": decisionJSON{Per: per, When: d.At, Origin: originOf(d.ByType)},
			"failed_checks": orEmpty(d.FailedChecks), "blocking_findings": findingCodes(r.BlockingFindings)}
		if d.Reason != "" {
			decision["reason"] = d.Reason
		}
		out["decision"] = decision
	}
	return out
}

func originOf(byType string) *string {
	if byType == "human" {
		return optional("web")
	}
	return nil
}

func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func (a *api) listGateRounds(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	rounds, err := a.deps.Gates.Rounds(RequestContext(rc), principal, projectID, PathParam(rc, "gateId"))
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rounds))
	for _, r := range rounds {
		full := roundOf(r)
		items = append(items, map[string]any{"gate": full["gate"], "stage": full["stage"], "round": full["round"], "state": full["state"],
			"opened_at": full["opened_at"], "decision": full["decision"]})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items})
	return nil
}

func roundPath(rc *fasthttp.RequestCtx) (string, int, error) {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return "", 0, err
	}
	round, err := strconv.Atoi(PathParam(rc, "round"))
	if err != nil || round < 1 {
		return "", 0, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return projectID, round, nil
}

func (a *api) getGateRound(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, round, err := roundPath(rc)
	if err != nil {
		return err
	}
	view, err := a.deps.Gates.Round(RequestContext(rc), principal, projectID, PathParam(rc, "gateId"), round)
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, roundOf(view))
	return nil
}

func (a *api) decideGate(rc *fasthttp.RequestCtx, principal identity.Principal, approve bool) error {
	projectID, round, err := roundPath(rc)
	if err != nil {
		return err
	}
	decision := appgate.Decision{ProjectID: projectID, Gate: PathParam(rc, "gateId"), Round: round, Approve: approve,
		CSRFToken: string(rc.Request.Header.Peek("X-CSRF-Token"))}
	if approve {
		var body struct {
			Confirmed []string `json:"confirmed_human_checks"`
			Comment   string   `json:"comment"`
		}
		if err := decodeJSON(rc, &body); err != nil {
			return err
		}
		decision.ConfirmedChecks, decision.Comment = body.Confirmed, body.Comment
	} else {
		var body struct {
			Reason      string   `json:"reason"`
			Unconfirmed []string `json:"unconfirmed_human_checks"`
		}
		if err := decodeJSON(rc, &body); err != nil {
			return err
		}
		decision.Reason = body.Reason
	}
	operation := "approveGate"
	if !approve {
		operation = "rejectGate"
	}
	session := currentSession(rc)
	return a.idempotent(rc, principal, operation, func(ctx context.Context) (created, error) {
		view, err := a.deps.Gates.Decide(ctx, principal, session, decision)
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusOK, body: roundOf(view)}, nil
	})
}

func (a *api) approveGate(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	return a.decideGate(rc, principal, true)
}

func (a *api) rejectGate(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	return a.decideGate(rc, principal, false)
}

// findingRefs renders the findings of a round as FindingSummary.
func findingRefs(refs []appgate.FindingRef, blocking bool) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for _, f := range refs {
		out = append(out, map[string]any{"id": f.Code, "title": f.Title, "severity": f.Severity, "category": f.Category, "status": f.Status,
			"destination_stage": f.DestinationStage, "origin_stage": f.OriginStage, "author": f.Author, "blocking": blocking, "cycles": f.Cycles,
			"opened_at": f.OpenedAt, "acceptance_expired": false})
	}
	return out
}

func findingCodes(refs []appgate.FindingRef) []string {
	out := make([]string, 0, len(refs))
	for _, f := range refs {
		out = append(out, f.Code)
	}
	return out
}
