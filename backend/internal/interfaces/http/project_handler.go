package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
)

// ProjectUseCases are the project use cases (US-03, US-50 basics, US-16 configuration).
type ProjectUseCases interface {
	Create(ctx context.Context, principal identity.Principal, input project.NewProject) (appproject.View, error)
	Get(ctx context.Context, principal identity.Principal, projectID string) (appproject.View, error)
	List(ctx context.Context, principal identity.Principal, filter appproject.Filter) (appproject.Page, error)
}

type moneyJSON struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type budgetJSON struct {
	StopLimit        *moneyJSON `json:"stop_limit"`
	AlertPercentages []int      `json:"percentage_alert_limits,omitempty"`
}

type constraintItemJSON struct {
	ID          string `json:"id,omitempty"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

type newConstraintsJSON struct {
	Items []constraintItemJSON `json:"items"`
}

type newProjectJSON struct {
	Name            string              `json:"name"`
	BusinessNeed    string              `json:"business_need"`
	Acknowledgement bool                `json:"provider_submission_acknowledgment"`
	Budget          *budgetJSON         `json:"budget"`
	Constraints     *newConstraintsJSON `json:"constraints"`
}

type actorJSON struct {
	Type string       `json:"type"`
	ID   string       `json:"id,omitempty"`
	User *userRefJSON `json:"user,omitempty"`
}

type decisionJSON struct {
	Per    actorJSON `json:"per"`
	When   time.Time `json:"when"`
	Origin *string   `json:"origin"`
}

type stageStateJSON struct {
	Stage string `json:"stage"`
	State string `json:"state"`
}

type activeWaveJSON struct {
	Wave  string `json:"wave"`
	Agent string `json:"agent"`
	State string `json:"state"`
}

type summaryJSON struct {
	CurrentStages   []stageStateJSON `json:"current_stages"`
	ApprovedStages  int              `json:"approved_stages"`
	StagesTotal     int              `json:"stages_total"`
	ActiveWaves     []activeWaveJSON `json:"active_waves"`
	GatesAwaiting   int              `json:"gates_awaiting"`
	OpenQuestions   int              `json:"open_questions"`
	AccumulatedCost moneyJSON        `json:"accumulated_cost"`
	StopLimit       *moneyJSON       `json:"stop_limit"`
}

type projectJSON struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	State            string          `json:"state"`
	PauseReason      *string         `json:"pause_reason"`
	BusinessNeed     string          `json:"business_need"`
	Workflow         string          `json:"workflow"`
	Acknowledgement  decisionJSON    `json:"provider_submission_acknowledgment"`
	CreatedAt        time.Time       `json:"created_at"`
	CreatedBy        userRefJSON     `json:"created_by"`
	UpdatedAt        time.Time       `json:"updated_at"`
	MyRoles          []identity.Role `json:"my_roles"`
	Lead             userRefJSON     `json:"lead"`
	AvailableActions []string        `json:"available_actions"`
	Summary          summaryJSON     `json:"summary"`
	LastSequence     int64           `json:"last_sequence"`
}

type projectsPageJSON struct {
	Items        []projectJSON  `json:"items"`
	NextCursor   *string        `json:"next_cursor"`
	CountByState map[string]int `json:"count_by_state"`
}

func projectOf(v appproject.View) projectJSON {
	web := "web"
	out := projectJSON{
		ID: v.ID, Name: v.Name, State: v.State, BusinessNeed: v.BusinessNeed, Workflow: v.Workflow,
		Acknowledgement: decisionJSON{
			Per:  actorJSON{Type: "human", User: &userRefJSON{ID: v.AcknowledgedBy.Subject, Name: v.AcknowledgedBy.Name}},
			When: v.AcknowledgedAt, Origin: &web,
		},
		CreatedAt: v.CreatedAt, CreatedBy: userRefJSON{ID: v.CreatedBy.Subject, Name: v.CreatedBy.Name}, UpdatedAt: v.UpdatedAt,
		MyRoles: v.MyRoles, Lead: userRefJSON{ID: v.Lead.Subject, Name: v.Lead.Name}, AvailableActions: v.AvailableActions,
		LastSequence: v.LastSequence,
	}
	if v.PauseReason != "" {
		out.PauseReason = &v.PauseReason
	}
	s := v.Summary
	out.Summary = summaryJSON{
		ApprovedStages: s.ApprovedStages, StagesTotal: s.StagesTotal, GatesAwaiting: s.GatesAwaiting, OpenQuestions: s.OpenQuestions,
		AccumulatedCost: moneyJSON(s.AccumulatedCost), CurrentStages: []stageStateJSON{}, ActiveWaves: []activeWaveJSON{},
	}
	if s.StopLimit != nil {
		limit := moneyJSON(*s.StopLimit)
		out.Summary.StopLimit = &limit
	}
	for _, stage := range s.CurrentStages {
		out.Summary.CurrentStages = append(out.Summary.CurrentStages, stageStateJSON(stage))
	}
	for _, wave := range s.ActiveWaves {
		out.Summary.ActiveWaves = append(out.Summary.ActiveWaves, activeWaveJSON{Wave: wave.Code, Agent: wave.Agent, State: wave.State})
	}
	if out.MyRoles == nil {
		out.MyRoles = []identity.Role{}
	}
	return out
}

func (body newProjectJSON) toDomain() project.NewProject {
	input := project.NewProject{Name: body.Name, BusinessNeed: body.BusinessNeed, Acknowledgement: body.Acknowledgement}
	if body.Budget != nil {
		input.Budget = budgetOf(body.Budget)
	}
	if body.Constraints != nil {
		input.Constraints = constraintsOf(body.Constraints)
	}
	return input
}

func budgetOf(body *budgetJSON) *project.Budget {
	budget := &project.Budget{AlertPercentages: body.AlertPercentages}
	if body.StopLimit != nil {
		budget.StopLimit = project.Money(*body.StopLimit)
	}
	return budget
}

func constraintsOf(body *newConstraintsJSON) []project.Constraint {
	items := make([]project.Constraint, 0, len(body.Items))
	for _, item := range body.Items {
		items = append(items, project.Constraint(item))
	}
	return items
}

func (a *api) createProject(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body newProjectJSON
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	return a.idempotent(rc, principal, "createProject", func(ctx context.Context) (created, error) {
		view, err := a.deps.Projects.Create(ctx, principal, body.toDomain())
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusCreated, body: projectOf(view), location: APIPrefix + "/projects/" + view.ID}, nil
	})
}

func (a *api) getProject(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	view, err := a.deps.Projects.Get(RequestContext(rc), principal, projectID)
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, projectOf(view))
	return nil
}

func (a *api) listProjects(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	if scope := string(rc.QueryArgs().Peek("scope")); scope != "" && scope != "my" {
		if scope != "organization" {
			return failure.Invalid(failure.FieldError{Field: "scope", Message: "enum"})
		}
		// The organization scope of the administrator (US-02 CA-5, US-51) arrives with MARCO-2.
		return failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	limit, err := queryLimit(rc)
	if err != nil {
		return err
	}
	page, err := a.deps.Projects.List(RequestContext(rc), principal, appproject.Filter{
		States: queryList(rc, "state"), Limit: limit, Cursor: string(rc.QueryArgs().Peek("cursor")),
	})
	if err != nil {
		return err
	}
	out := projectsPageJSON{Items: []projectJSON{}, CountByState: page.CountByState}
	for _, view := range page.Items {
		out.Items = append(out.Items, projectOf(view))
	}
	if page.NextCursor != "" {
		out.NextCursor = &page.NextCursor
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}
