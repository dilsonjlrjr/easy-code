package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
)

// Projects stores projects, their members and constraints.
type Projects struct {
	pool          *pgxpool.Pool
	workflowID    string
	stagesInPhase int
}

var _ appproject.Repository = Projects{}

// NewProjects builds the adapter. workflowID and stagesInPhase describe the workflow in force,
// shown before the project starts (stages are instantiated only at start, US-03 CA-1).
func NewProjects(pool *pgxpool.Pool, workflowID string, stagesInPhase int) Projects {
	return Projects{pool: pool, workflowID: workflowID, stagesInPhase: stagesInPhase}
}

type constraintJSON struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// Create implements appproject.Repository.
func (r Projects) Create(ctx context.Context, p identity.Principal, input project.NewProject) (string, error) {
	q := conn(ctx, r.pool)
	var stopLimit *string
	alerts := project.DefaultAlertPercentages
	if input.Budget != nil {
		stopLimit = &input.Budget.StopLimit.Value
		alerts = input.Budget.AlertPercentages
	}
	var internalID, userID int64
	var publicID string
	err := q.QueryRow(WithOperation(ctx, "projects.insert"), `
INSERT INTO easycode.projects (organization_id, name, business_need, budget_stop_limit, budget_alert_limits,
  provider_acknowledgment_by, provider_acknowledgment_at, created_by)
SELECT u.organization_id, $3, $4, $5::numeric, $6::smallint[], u.id, now(), u.id
  FROM easycode.users u JOIN easycode.organizations o ON o.id = u.organization_id
 WHERE o.public_id = $1::uuid AND u.sub = $2
RETURNING id, public_id::text, created_by`, p.OrganizationID, p.Subject, input.Name, input.BusinessNeed, stopLimit, alerts).Scan(&internalID, &publicID, &userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "projects_name_org_uk" {
			return "", failure.New(failure.KindConflict, project.CodeDuplicateName, err)
		}
		return "", fmt.Errorf("insert project: %w", classify(err))
	}
	// The creator is lead and approver (US-02 RN-1).
	_, err = q.Exec(WithOperation(ctx, "project_members.insert_creator"), `
INSERT INTO easycode.project_members (project_id, user_id, role, assigned_by) VALUES ($1, $2, 'lead', $2), ($1, $2, 'approver', $2)`,
		internalID, userID)
	if err != nil {
		return "", fmt.Errorf("insert project members: %w", classify(err))
	}
	if len(input.Constraints) > 0 {
		if err := insertConstraints(ctx, q, internalID, userID, 1, input.Constraints); err != nil {
			return "", err
		}
	}
	return publicID, nil
}

func insertConstraints(ctx context.Context, q querier, projectID, userID int64, version int, items []project.Constraint) error {
	encoded := make([]constraintJSON, 0, len(items))
	for _, item := range items {
		encoded = append(encoded, constraintJSON{ID: item.ID, Category: item.Category, Description: item.Description})
	}
	raw, err := json.Marshal(encoded)
	if err != nil {
		return err
	}
	_, err = q.Exec(WithOperation(ctx, "project_constraints.insert"), `
INSERT INTO easycode.project_constraints (project_id, version, items, defined_by) VALUES ($1, $2, $3::jsonb, $4)`,
		projectID, version, string(raw), userID)
	if err != nil {
		return fmt.Errorf("insert constraints: %w", classify(err))
	}
	return nil
}

// Access implements appproject.Repository: no role (or a deleted project) is not found (RNF-04).
func (r Projects) Access(ctx context.Context, p identity.Principal, projectID string) (appproject.Access, error) {
	access := appproject.Access{ProjectID: projectID}
	var roles []string
	err := conn(ctx, r.pool).QueryRow(WithOperation(ctx, "projects.access"), `
SELECT pr.state, array_agg(m.role ORDER BY m.role)
  FROM easycode.projects pr JOIN easycode.project_members m ON m.project_id = pr.id
 WHERE pr.public_id = $3::uuid AND pr.deletion_requested_at IS NULL AND m.user_id = `+principalUserIDSQL+`
 GROUP BY pr.state`, p.OrganizationID, p.Subject, projectID).Scan(&access.State, &roles)
	if errors.Is(err, pgx.ErrNoRows) {
		return access, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return access, fmt.Errorf("project access: %w", classify(err))
	}
	for _, role := range roles {
		access.Roles = append(access.Roles, identity.Role(role))
	}
	return access, nil
}

// BudgetRules implements appproject.Repository.
func (r Projects) BudgetRules(ctx context.Context, organizationID string) (string, *string, error) {
	var currency string
	var ceiling *string
	err := conn(ctx, r.pool).QueryRow(WithOperation(ctx, "organizations.budget_rules"),
		"SELECT currency, stop_limit_ceiling::text FROM easycode.organizations WHERE public_id = $1::uuid", organizationID).Scan(&currency, &ceiling)
	if err != nil {
		return "", nil, fmt.Errorf("budget rules: %w", classify(err))
	}
	if ceiling != nil {
		trimmed := project.TrimDecimal(*ceiling)
		ceiling = &trimmed
	}
	return currency, ceiling, nil
}

const updatedAtSQL = `coalesce((SELECT max(e.occurred_at) FROM easycode.events e WHERE e.project_id = pr.id), pr.created_at)`

// Get implements appproject.Repository.
func (r Projects) Get(ctx context.Context, p identity.Principal, projectID string) (appproject.View, error) {
	access, err := r.Access(ctx, p, projectID)
	if err != nil {
		return appproject.View{}, err
	}
	view := appproject.View{ID: projectID, State: access.State, MyRoles: access.Roles, Workflow: r.workflowID}
	var pause, workflowID *string
	var stopLimit *string
	var currency string
	var internalID int64
	err = conn(ctx, r.pool).QueryRow(WithOperation(ctx, "projects.get"), `
SELECT pr.id, pr.name, pr.pause_reason, pr.business_need, pr.provider_acknowledgment_at, ack.sub, ack.display_name,
       pr.created_at, cr.sub, cr.display_name, `+updatedAtSQL+`, ld.sub, ld.display_name,
       pr.budget_stop_limit::text, o.currency, wc.workflow_id,
       coalesce((SELECT c.value FROM easycode.project_counters c WHERE c.project_id = pr.id AND c.name = 'event'), 0)
  FROM easycode.projects pr
  JOIN easycode.organizations o ON o.id = pr.organization_id
  JOIN easycode.users ack ON ack.id = pr.provider_acknowledgment_by
  JOIN easycode.users cr ON cr.id = pr.created_by
  JOIN easycode.project_members lm ON lm.project_id = pr.id AND lm.role = 'lead'
  JOIN easycode.users ld ON ld.id = lm.user_id
  LEFT JOIN easycode.workflow_configurations wc ON wc.project_id = pr.id
 WHERE pr.public_id = $1::uuid`, projectID).Scan(&internalID, &view.Name, &pause, &view.BusinessNeed, &view.AcknowledgedAt,
		&view.AcknowledgedBy.Subject, &view.AcknowledgedBy.Name, &view.CreatedAt, &view.CreatedBy.Subject, &view.CreatedBy.Name,
		&view.UpdatedAt, &view.Lead.Subject, &view.Lead.Name, &stopLimit, &currency, &workflowID, &view.LastSequence)
	if err != nil {
		return appproject.View{}, fmt.Errorf("read project: %w", classify(err))
	}
	if pause != nil {
		view.PauseReason = *pause
	}
	if workflowID != nil {
		view.Workflow = *workflowID
	}
	if view.State == project.StateCreated {
		view.AvailableActions = []string{"start"}
	} else {
		view.AvailableActions = []string{}
	}
	view.Summary, err = r.summary(ctx, internalID, currency, stopLimit)
	return view, err
}

func (r Projects) summary(ctx context.Context, projectID int64, currency string, stopLimit *string) (appproject.Summary, error) {
	q := conn(ctx, r.pool)
	summary := appproject.Summary{StagesTotal: r.stagesInPhase, CurrentStages: []appproject.StageState{}, ActiveWaves: []appproject.WaveRef{}}
	if stopLimit != nil {
		summary.StopLimit = &project.Money{Value: project.TrimDecimal(*stopLimit), Currency: currency}
	}
	rows, err := q.Query(WithOperation(ctx, "projects.summary_stages"),
		"SELECT stage, state FROM easycode.stages WHERE project_id = $1 AND type = 'stage' ORDER BY id", projectID)
	if err != nil {
		return summary, fmt.Errorf("summary stages: %w", classify(err))
	}
	stages, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appproject.StageState, error) {
		var s appproject.StageState
		return s, row.Scan(&s.Stage, &s.State)
	})
	if err != nil {
		return summary, fmt.Errorf("scan summary stages: %w", err)
	}
	if len(stages) > 0 {
		summary.StagesTotal = len(stages)
	}
	for _, stage := range stages {
		switch stage.State {
		case "approved":
			summary.ApprovedStages++
		case "ready", "running", "awaiting_gate", "returned", "reopened":
			summary.CurrentStages = append(summary.CurrentStages, stage)
		}
	}
	rows, err = q.Query(WithOperation(ctx, "projects.summary_waves"), `
SELECT code, agent, state FROM easycode.waves
 WHERE project_id = $1 AND state IN ('queued','running','awaiting_user','pausing','paused') ORDER BY created_at`, projectID)
	if err != nil {
		return summary, fmt.Errorf("summary waves: %w", classify(err))
	}
	summary.ActiveWaves, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (appproject.WaveRef, error) {
		var w appproject.WaveRef
		return w, row.Scan(&w.Code, &w.Agent, &w.State)
	})
	if err != nil {
		return summary, fmt.Errorf("scan summary waves: %w", err)
	}
	var cost string
	err = q.QueryRow(WithOperation(ctx, "projects.summary_counts"), `
SELECT (SELECT count(*) FROM easycode.gate_rounds g WHERE g.project_id = $1 AND g.state = 'awaiting_human'),
       (SELECT count(*) FROM easycode.wave_questions wq JOIN easycode.waves w ON w.id = wq.wave_id WHERE w.project_id = $1 AND wq.answered_at IS NULL),
       (SELECT coalesce(sum(useful_cost + lost_cost), 0)::text FROM easycode.waves WHERE project_id = $1)`, projectID).
		Scan(&summary.GatesAwaiting, &summary.OpenQuestions, &cost)
	if err != nil {
		return summary, fmt.Errorf("summary counts: %w", classify(err))
	}
	summary.AccumulatedCost = project.Money{Value: project.TrimDecimal(cost), Currency: currency}
	return summary, nil
}

// List implements appproject.Repository (projects where the person has a role, updated_at descending).
func (r Projects) List(ctx context.Context, p identity.Principal, filter appproject.Filter) (appproject.Page, error) {
	page := appproject.Page{CountByState: map[string]int{}}
	for _, state := range project.States {
		page.CountByState[state] = 0
	}
	q := conn(ctx, r.pool)
	mine := fmt.Sprintf(`FROM easycode.projects pr
 WHERE pr.deletion_requested_at IS NULL
   AND EXISTS (SELECT 1 FROM easycode.project_members m WHERE m.project_id = pr.id AND m.user_id = %s)`, principalUserIDSQL)
	if filter.Organization {
		// scope=organization of the administrator (US-02 CA-6): every project of the organization.
		mine = `FROM easycode.projects pr JOIN easycode.organizations oo ON oo.id = pr.organization_id
 WHERE pr.deletion_requested_at IS NULL AND oo.public_id = $1::uuid AND $2::text IS NOT NULL`
	}
	rows, err := q.Query(WithOperation(ctx, "projects.count_by_state"),
		"SELECT pr.state, count(*) "+mine+" GROUP BY pr.state", p.OrganizationID, p.Subject)
	if err != nil {
		return page, fmt.Errorf("count projects: %w", classify(err))
	}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			rows.Close()
			return page, err
		}
		page.CountByState[state] = n
	}
	rows.Close()
	after, afterID, err := decodeCursor(filter.Cursor)
	if err != nil {
		return page, failure.Invalid(failure.FieldError{Field: "cursor", Message: "invalid"})
	}
	rows, err = q.Query(WithOperation(ctx, "projects.list"), `
SELECT id, updated_at FROM (
  SELECT pr.public_id::text AS id, `+updatedAtSQL+` AS updated_at, pr.state `+mine+`
) x
 WHERE (coalesce(cardinality($3::text[]), 0) = 0 OR x.state = ANY($3::text[]))
   AND ($4::timestamptz IS NULL OR (x.updated_at, x.id) < ($4::timestamptz, $5::text))
 ORDER BY x.updated_at DESC, x.id DESC
 LIMIT $6`, p.OrganizationID, p.Subject, filter.States, after, afterID, filter.Limit+1)
	if err != nil {
		return page, fmt.Errorf("list projects: %w", classify(err))
	}
	type key struct {
		id string
		at time.Time
	}
	keys, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (key, error) {
		var k key
		return k, row.Scan(&k.id, &k.at)
	})
	if err != nil {
		return page, fmt.Errorf("scan projects: %w", err)
	}
	if len(keys) > filter.Limit {
		last := keys[filter.Limit-1]
		page.NextCursor = encodeCursor(last.at, last.id)
		keys = keys[:filter.Limit]
	}
	for _, k := range keys {
		get := r.Get
		if filter.Organization {
			get = func(ctx context.Context, _ identity.Principal, projectID string) (appproject.View, error) {
				return r.Overview(ctx, projectID)
			}
		}
		view, err := get(ctx, p, k.id)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, view)
	}
	return page, nil
}

func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(cursor string) (*time.Time, string, error) {
	if cursor == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", err
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, "", errors.New("bad cursor")
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, "", err
	}
	return &parsed, id, nil
}

// Overview implements appproject.Repository: metadata only (id, name, state, lead, dates), what
// the administrator sees of a project without a role in it (US-02 CA-6, scope=organization).
func (r Projects) Overview(ctx context.Context, projectID string) (appproject.View, error) {
	view := appproject.View{ID: projectID, Workflow: r.workflowID, MyRoles: []identity.Role{}, MetadataOnly: true}
	err := conn(ctx, r.pool).QueryRow(WithOperation(ctx, "projects.overview"), `
SELECT pr.name, pr.state, pr.created_at, cr.sub, cr.display_name, `+updatedAtSQL+`, ld.sub, ld.display_name
  FROM easycode.projects pr
  JOIN easycode.users cr ON cr.id = pr.created_by
  JOIN easycode.project_members lm ON lm.project_id = pr.id AND lm.role = 'lead'
  JOIN easycode.users ld ON ld.id = lm.user_id
 WHERE pr.public_id = $1::uuid AND pr.deletion_requested_at IS NULL`, projectID).Scan(&view.Name, &view.State, &view.CreatedAt,
		&view.CreatedBy.Subject, &view.CreatedBy.Name, &view.UpdatedAt, &view.Lead.Subject, &view.Lead.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return view, fmt.Errorf("project overview: %w", classify(err))
	}
	return view, nil
}
