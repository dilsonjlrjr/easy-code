package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

	appartifact "easycode/internal/application/artifact"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// WorkflowUseCases are the workflow use cases (US-05, US-49 basics).
type WorkflowUseCases interface {
	Start(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (appworkflow.CommandView, error)
	Pause(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (appworkflow.CommandView, error)
	Resume(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (appworkflow.CommandView, error)
	Cancel(ctx context.Context, principal identity.Principal, projectID, nameConfirmation, key, bodyHash string) (appworkflow.CommandView, error)
	Complete(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (appworkflow.CommandView, error)
	Command(ctx context.Context, principal identity.Principal, projectID, commandID string) (appworkflow.CommandView, error)
	Workflow(ctx context.Context, principal identity.Principal, projectID string) (appworkflow.View, error)
	WorkflowAsAdministrator(ctx context.Context, projectID string) (appworkflow.View, error)
}

type commandTargetJSON struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type commandJSON struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Target      commandTargetJSON `json:"target"`
	State       string            `json:"state"`
	RequestedBy userRefJSON       `json:"requested_by"`
	Origin      string            `json:"origin"`
	RequestedAt time.Time         `json:"requested_at"`
	CompletedAt *time.Time        `json:"completed_at"`
	Refusal     *Problem          `json:"refusal,omitempty"`
}

type stageSummaryJSON struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	ShortName            string    `json:"short_name"`
	Type                 string    `json:"type"`
	Agent                string    `json:"agent"`
	DependsOn            []string  `json:"depends_on"`
	RequiredCapabilities []string  `json:"required_capabilities"`
	State                *string   `json:"state"`
	Gate                 *string   `json:"gate"`
	ApprovedInGate       *string   `json:"approved_in_gate"`
	CurrentRound         *int      `json:"current_round"`
	GateState            *string   `json:"gate_state"`
	ReevaluationPending  bool      `json:"reevaluation_pending"`
	ActiveWaves          int       `json:"active_waves"`
	Cost                 moneyJSON `json:"cost"`
	ReopenedByFinding    *string   `json:"reopened_by_finding"`
}

type workflowJSON struct {
	ID      string             `json:"id"`
	Version int                `json:"version"`
	Mode    string             `json:"mode"`
	Stages  []stageSummaryJSON `json:"stages"`
}

func (a *api) commandOf(rc *fasthttp.RequestCtx, c appworkflow.CommandView) commandJSON {
	out := commandJSON{
		ID: c.ID, Type: c.Type, Target: commandTargetJSON{Type: c.TargetType, ID: c.TargetID}, State: c.State,
		RequestedBy: userRefJSON{ID: c.RequestedBy.Subject, Name: c.RequestedBy.Name}, Origin: "web",
		RequestedAt: c.RequestedAt, CompletedAt: c.CompletedAt,
	}
	if c.Refusal != "" {
		classified := failure.From(failure.New(failure.KindConflict, c.Refusal, nil))
		problem := a.responder.problemBody(rc, classified, fasthttp.StatusConflict)
		out.Refusal = &problem
	}
	return out
}

func commandLocation(projectID, commandID string) string {
	return APIPrefix + "/projects/" + projectID + "/commands/" + commandID
}

// startWorkflow is startWorkflow: 202 with the command (US-05 CA-1, CA-2).
func (a *api) startWorkflow(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	req, err := idempotencyRequest(rc, principal, "startWorkflow")
	if err != nil {
		return err
	}
	return a.idempotent(rc, principal, "startWorkflow", func(ctx context.Context) (created, error) {
		queued, err := a.deps.Workflow.Start(ctx, principal, projectID, req.Key, req.BodyHash)
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusAccepted, body: a.commandOf(rc, queued), location: commandLocation(projectID, queued.ID)}, nil
	})
}

// projectCommand answers a project lifecycle command with 202 and the Command (US-51, US-52).
func (a *api) projectCommand(rc *fasthttp.RequestCtx, principal identity.Principal, operation string,
	apply func(ctx context.Context, projectID, key, bodyHash string) (appworkflow.CommandView, error)) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	req, err := idempotencyRequest(rc, principal, operation)
	if err != nil {
		return err
	}
	return a.idempotent(rc, principal, operation, func(ctx context.Context) (created, error) {
		queued, err := apply(ctx, projectID, req.Key, req.BodyHash)
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusAccepted, body: a.commandOf(rc, queued), location: commandLocation(projectID, queued.ID)}, nil
	})
}

func (a *api) pauseProject(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	return a.projectCommand(rc, principal, "pauseProject", func(ctx context.Context, projectID, key, hash string) (appworkflow.CommandView, error) {
		return a.deps.Workflow.Pause(ctx, principal, projectID, key, hash)
	})
}

func (a *api) resumeProject(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	return a.projectCommand(rc, principal, "resumeProject", func(ctx context.Context, projectID, key, hash string) (appworkflow.CommandView, error) {
		return a.deps.Workflow.Resume(ctx, principal, projectID, key, hash)
	})
}

func (a *api) completeProject(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	return a.projectCommand(rc, principal, "completeProject", func(ctx context.Context, projectID, key, hash string) (appworkflow.CommandView, error) {
		return a.deps.Workflow.Complete(ctx, principal, projectID, key, hash)
	})
}

// cancelProject needs the project name typed again (US-51 RN-1).
func (a *api) cancelProject(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body struct {
		NameConfirmation string `json:"name_confirmation"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	if n := len([]rune(body.NameConfirmation)); n < 1 || n > 120 {
		return failure.New(failure.KindInvalidInput, appworkflow.CodeConfirmationRequired, nil).WithField("name_confirmation", "required")
	}
	return a.projectCommand(rc, principal, "cancelProject", func(ctx context.Context, projectID, key, hash string) (appworkflow.CommandView, error) {
		return a.deps.Workflow.Cancel(ctx, principal, projectID, body.NameConfirmation, key, hash)
	})
}

func (a *api) getCommand(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	commandID, err := pathUUID(rc, "commandId")
	if err != nil {
		return err
	}
	view, err := a.deps.Workflow.Command(RequestContext(rc), principal, projectID, commandID)
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, a.commandOf(rc, view))
	return nil
}

func optional[T comparable](value T) *T {
	var zero T
	if value == zero {
		return nil
	}
	return &value
}

// getWorkflow lists the stages of the workflow in force, with state null before the start (US-05 CA-5).
func (a *api) getWorkflow(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	ctx := RequestContext(rc)
	view, err := a.deps.Workflow.Workflow(ctx, principal, projectID)
	if err != nil && a.administratorWithoutRole(ctx, principal, projectID, err) {
		view, err = a.deps.Workflow.WorkflowAsAdministrator(ctx, projectID)
	}
	if err != nil {
		return err
	}
	out := workflowJSON{ID: view.ID, Version: view.Version, Mode: view.Mode, Stages: a.stageSummaries(view)}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

func (a *api) stageSummaries(view appworkflow.View) []stageSummaryJSON {
	stages := []stageSummaryJSON{}
	for _, stage := range view.Stages {
		item := stageSummaryJSON{
			ID: stage.ID, Name: stage.Name, ShortName: stage.ShortName, Type: "stage", Agent: stage.Agent, DependsOn: stage.DependsOn, ReopenedByFinding: optional(stage.ReopenedByFinding),
			RequiredCapabilities: stage.RequiredCapabilities, State: optional(stage.State), Gate: optional(stage.Gate),
			CurrentRound: optional(stage.CurrentRound), GateState: optional(stage.GateState), ReevaluationPending: stage.ReevaluationPending,
			ActiveWaves: stage.ActiveWaves, Cost: moneyJSON(stage.Cost),
		}
		if stage.Type == "parallel_activity" {
			item.Type, item.Gate, item.ApprovedInGate = stage.Type, nil, optional(stage.ApprovedInGate)
		} else if stage.State == "approved" {
			item.ApprovedInGate = item.Gate
		}
		stages = append(stages, item)
	}
	return stages
}

type stageApprovalJSON struct {
	Mode         string `json:"mode"`
	RequiredRole string `json:"required_role"`
}

type stageJSON struct {
	stageSummaryJSON
	Waves         []waveSummaryJSON     `json:"waves"`
	Artifacts     []artifactSummaryJSON `json:"artifacts"`
	Findings      []any                 `json:"findings"`
	VersionInputs []any                 `json:"version_inputs"`
	Tokens        tokensJSON            `json:"tokens"`
	DetailedCost  detailedCostJSON      `json:"detailed_cost"`
	Time          map[string]int64      `json:"time"`
	Approval      stageApprovalJSON     `json:"approval"`
	GateChecks    []gateCheckJSON       `json:"gate_checks"`
}

// gateCheckJSON is openapi `GateCheck`: a check the gate of the stage runs (PC-20, PL-46).
type gateCheckJSON struct {
	Check           string `json:"check"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	FailureSeverity string `json:"failure_severity"`
}

// maxStageItems bounds the waves and artifacts of the stage view (a stage has a handful of each).
const maxStageItems = 100

// getStage is getStage: agent, waves (rework included), artifacts, time, tokens, cost and the
// approval the stage needs (US-49 CA-2). Findings join with the governance engine (US-31, Phase 2).
func (a *api) getStage(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	stageID := PathParam(rc, "stage")
	ctx := RequestContext(rc)
	view, err := a.deps.Workflow.Workflow(ctx, principal, projectID)
	if err != nil {
		return err
	}
	summary, found := stageSummaryJSON{}, false
	for _, item := range a.stageSummaries(view) {
		if item.ID == stageID {
			summary, found = item, true
		}
	}
	if !found {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	out := stageJSON{stageSummaryJSON: summary, Waves: []waveSummaryJSON{}, Artifacts: []artifactSummaryJSON{}, Findings: []any{},
		VersionInputs: []any{}, Approval: stageApprovalJSON{Mode: "human", RequiredRole: "approver"}, GateChecks: []gateCheckJSON{}}
	if summary.Gate != nil && a.deps.Configuration != nil {
		// The mode a new round of the gate takes: the project choice (gate_configurations) where the workflow
		// stage allows it, else its default (PD-05; gate.validation is automatic by default).
		config, err := a.deps.Configuration.Get(ctx, principal, projectID)
		if err != nil {
			return err
		}
		if mode, ok := config.GateApproval[*summary.Gate]; ok && mode.Mode != "" {
			out.Approval.Mode = mode.Mode
		}
	}
	if summary.Gate != nil && a.deps.GateChecks != nil {
		checks, err := a.deps.GateChecks.Checks(ctx, principal, projectID, *summary.Gate)
		if err != nil {
			return err
		}
		for _, c := range checks {
			out.GateChecks = append(out.GateChecks, gateCheckJSON{Check: c.Check, Name: c.Name, Type: c.Type, FailureSeverity: c.Severity})
		}
	}
	waves, err := a.deps.Waves.List(ctx, principal, projectID, stageID, "", nil, maxStageItems)
	if err != nil {
		return err
	}
	cost := appwave.Cost{Useful: "0", Lost: "0", Currency: summary.Cost.Currency}
	var executionMs, cycleMs int64
	for _, w := range waves {
		out.Waves = append(out.Waves, waveSummaryOf(w))
		detail, err := a.deps.Waves.Get(ctx, principal, projectID, w.Code)
		if err != nil {
			return err
		}
		out.Tokens.Input += int64(detail.Tokens.Input)
		out.Tokens.Output += int64(detail.Tokens.Output)
		out.Tokens.ReadCache += int64(detail.Tokens.CacheRead)
		out.Tokens.WriteCache += int64(detail.Tokens.CacheWrite)
		cost.Useful, cost.Lost = addDecimal(cost.Useful, detail.Cost.Useful), addDecimal(cost.Lost, detail.Cost.Lost)
		cost.EstimatedLost = cost.EstimatedLost || detail.Cost.EstimatedLost
		if detail.Cost.Currency != "" {
			cost.Currency = detail.Cost.Currency
		}
		executionMs += detail.ExecutionMs
		cycleMs += detail.CycleMs
	}
	out.DetailedCost = costOf(cost)
	out.Time = map[string]int64{"execution_ms": executionMs, "cycle_ms": cycleMs}
	if a.deps.Artifacts != nil {
		page, err := a.deps.Artifacts.List(ctx, principal, projectID, appartifact.Filter{Stage: stageID, Limit: maxStageItems})
		if err != nil {
			return err
		}
		for _, item := range page.Items {
			out.Artifacts = append(out.Artifacts, artifactSummaryOf(item))
		}
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

// administratorWithoutRole reports whether a not-found read comes from the organization
// administrator without a role in the project; the access is then authorized and recorded once
// per session by the member service (US-02 CA-6).
func (a *api) administratorWithoutRole(ctx context.Context, principal identity.Principal, projectID string, err error) bool {
	if !principal.Administrator || a.deps.Members == nil || failure.From(err).Kind != failure.KindNotFound {
		return false
	}
	_, openErr := a.deps.Members.Open(ctx, principal, projectID)
	return openErr == nil
}
