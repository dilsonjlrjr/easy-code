package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

	appgate "easycode/internal/application/gate"
	"easycode/internal/domain/identity"
)

// ReleaseUseCases is getReleaseSummary (US-08, US-33 CA-4).
type ReleaseUseCases interface {
	Summary(ctx context.Context, principal identity.Principal, projectID string) (appgate.ReleaseSummary, error)
}

// getReleaseSummary consolidates the Release: gates, tests, findings, accepted risks with who accepted and
// why, total cost and the blocks of the approval (also while the release stage is blocked, PD-14).
func (a *api) getReleaseSummary(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	summary, err := a.deps.Release.Summary(RequestContext(rc), principal, projectID)
	if err != nil {
		return err
	}
	now := time.Now()
	gates := make([]map[string]any, 0, len(summary.Gates))
	for _, g := range summary.Gates {
		item := map[string]any{"gate": g.Gate, "state": nil, "decision": nil}
		if g.Round != nil {
			round := a.namedRound(*g.Round)
			item["state"], item["decision"], item["round"] = round["state"], round["decision"], g.Round.Round
		}
		gates = append(gates, item)
	}
	open := make([]map[string]any, 0, len(summary.OpenFindings))
	for _, f := range summary.OpenFindings {
		open = append(open, findingSummary(f, now))
	}
	accepted := make([]map[string]any, 0, len(summary.Accepted))
	for _, f := range summary.Accepted {
		acceptance := map[string]any{"per": userRefJSON{ID: f.AcceptedBy, Name: f.AcceptedByName}, "justification": f.Justification,
			"origin": "web", "valid_until": nil}
		if f.AcceptedAt != nil {
			acceptance["when"] = *f.AcceptedAt
		}
		if f.ValidUntil != nil {
			acceptance["valid_until"] = f.ValidUntil.Format("2006-01-02")
		}
		accepted = append(accepted, map[string]any{"finding": findingSummary(f, now), "acceptance": acceptance})
	}
	blocks := make([]map[string]any, 0, len(summary.Blocks))
	for _, b := range summary.Blocks {
		block := map[string]any{"type": b.Type, "reference": b.Reference, "valid_until": nil}
		if b.ValidUntil != nil {
			block["valid_until"] = b.ValidUntil.Format("2006-01-02")
		}
		blocks = append(blocks, block)
	}
	tests := map[string]any{"ct_total": summary.Tests.Total, "passed": summary.Tests.Passed, "failed": summary.Tests.Failed}
	if summary.Report != "" {
		tests["report"] = summary.Report
	}
	useful := subtractDecimal(summary.Cost, summary.Lost)
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{
		"gates": gates, "tests": tests, "open_findings": open, "accepted_risks": accepted, "blocks": blocks, "stage_state": summary.StageState,
		"release_artifact": optional(summary.ReleaseArtifact), "mark_name": summary.MarkName,
		"cost_total": map[string]any{"useful": moneyJSON{useful, summary.Currency}, "lost": moneyJSON{summary.Lost, summary.Currency},
			"total": moneyJSON{summary.Cost, summary.Currency}},
	})
	return nil
}
