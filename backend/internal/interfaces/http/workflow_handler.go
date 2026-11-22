package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// WorkflowUseCases are the workflow use cases (US-05, US-49 basics).
type WorkflowUseCases interface {
	Start(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (appworkflow.CommandView, error)
	Command(ctx context.Context, principal identity.Principal, projectID, commandID string) (appworkflow.CommandView, error)
	Workflow(ctx context.Context, principal identity.Principal, projectID string) (appworkflow.View, error)
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
	view, err := a.deps.Workflow.Workflow(RequestContext(rc), principal, projectID)
	if err != nil {
		return err
	}
	out := workflowJSON{ID: view.ID, Version: view.Version, Mode: view.Mode, Stages: []stageSummaryJSON{}}
	for _, stage := range view.Stages {
		item := stageSummaryJSON{
			ID: stage.ID, Name: stage.Name, ShortName: stage.ShortName, Type: "stage", Agent: stage.Agent, DependsOn: stage.DependsOn,
			RequiredCapabilities: stage.RequiredCapabilities, State: optional(stage.State), Gate: optional(stage.Gate),
			CurrentRound: optional(stage.CurrentRound), GateState: optional(stage.GateState), ReevaluationPending: stage.ReevaluationPending,
			ActiveWaves: stage.ActiveWaves, Cost: moneyJSON(stage.Cost),
		}
		if stage.State == "approved" {
			item.ApprovedInGate = item.Gate
		}
		out.Stages = append(out.Stages, item)
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}
