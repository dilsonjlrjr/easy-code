package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// updatedAtSQL is the time of the last event of the project, kept by the outbox in the event counter of the
// project (project_counters.updated_at): no read of the partitioned events per listed project (RNF-09).
const updatedAtSQL = `coalesce((SELECT c.updated_at FROM easycode.project_counters c WHERE c.project_id = pr.id AND c.name = 'event'), pr.created_at)`

// Get implements appproject.Repository.
func (r Projects) Get(ctx context.Context, p identity.Principal, projectID string) (appproject.View, error) {
	views, err := r.views(ctx, p, []string{projectID})
	if err != nil {
		return appproject.View{}, err
	}
	if len(views) == 0 {
		return appproject.View{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return views[0], nil
}

// views reads the projects (in the order of ids) where the person has a role, with the dashboard summary, in
// one query whatever the number of projects: each indicator is a subquery of the row (JSON aggregates), so a
// page of listProjects costs one round trip (RNF-09). A project without a role or deleted is left out (RNF-04).
func (r Projects) views(ctx context.Context, p identity.Principal, ids []string) ([]appproject.View, error) {
	rows, err := conn(ctx, r.pool).Query(WithOperation(ctx, "projects.get"), `
SELECT pr.public_id::text, pr.state, roles.list, pr.name, pr.pause_reason, pr.business_need, pr.provider_acknowledgment_at,
       ack.sub, ack.display_name, pr.created_at, cr.sub, cr.display_name, `+updatedAtSQL+`, ld.sub, ld.display_name,
       pr.budget_stop_limit::text, o.currency, wc.workflow_id, pr.last_alert_percentage,
       coalesce((SELECT c.value FROM easycode.project_counters c WHERE c.project_id = pr.id AND c.name = 'event'), 0),
       (SELECT coalesce(json_agg(json_build_array(s.stage, s.state) ORDER BY s.id), '[]') FROM easycode.stages s
         WHERE s.project_id = pr.id AND s.type = 'stage'),
       (SELECT coalesce(json_agg(json_build_array(w.code, w.agent, w.state) ORDER BY w.created_at), '[]') FROM easycode.waves w
         WHERE w.project_id = pr.id AND w.state IN ('queued','running','awaiting_user','pausing','paused')),
       (SELECT count(*) FROM easycode.gate_rounds g WHERE g.project_id = pr.id AND g.state = 'awaiting_human'),
       (SELECT count(*) FROM easycode.wave_questions wq JOIN easycode.waves w ON w.id = wq.wave_id WHERE w.project_id = pr.id AND wq.answered_at IS NULL),
       (SELECT coalesce(sum(w.useful_cost + w.lost_cost), 0)::text FROM easycode.waves w WHERE w.project_id = pr.id),
       -- by severity, and apart the escalated ones (shown in text, US-48 RN-1; the severity counts include them)
       (SELECT coalesce(json_object_agg(f.severity, f.n), '{}') FROM (SELECT severity, count(*) AS n FROM easycode.findings
         WHERE project_id = pr.id AND status IN ('open','in_rework','revalidating','escalated','awaiting_scope') GROUP BY severity) f),
       (SELECT count(*) FROM easycode.findings WHERE project_id = pr.id AND status = 'escalated'),
       pause.occurred_at, coalesce(pause.envelope->'data'->>'pause_reason', ''), coalesce(pause.envelope->'data'->'actor'->>'type', ''),
       coalesce(pause.envelope->'data'->'actor'->>'id', ''), coalesce(pu.display_name, ''), coalesce(pause.envelope->'data'->'actor'->>'origin', ''),
       coalesce(budget.envelope->'data'->>'cause', ''), coalesce(budget.envelope->'data'->'next_wave_estimate'->>'value', ''),
       coalesce(budget.envelope->'data'->'next_wave_estimate'->>'currency', '')
  FROM easycode.projects pr
  JOIN LATERAL (SELECT array_agg(m.role ORDER BY m.role) AS list FROM easycode.project_members m
                 WHERE m.project_id = pr.id AND m.user_id = `+principalUserIDSQL+`) roles ON roles.list IS NOT NULL
  JOIN easycode.organizations o ON o.id = pr.organization_id
  JOIN easycode.users ack ON ack.id = pr.provider_acknowledgment_by
  JOIN easycode.users cr ON cr.id = pr.created_by
  JOIN easycode.project_members lm ON lm.project_id = pr.id AND lm.role = 'lead'
  JOIN easycode.users ld ON ld.id = lm.user_id
  LEFT JOIN easycode.workflow_configurations wc ON wc.project_id = pr.id
  -- who paused the project and when (ESP-06 section 6): its last project.paused; a pause by the budget follows its
  -- budget_exhausted, with the cause and the estimate that exceeded the balance
  LEFT JOIN LATERAL (SELECT ev.occurred_at, ev.envelope FROM easycode.events ev
                      WHERE pr.state = 'paused' AND ev.project_id = pr.id AND ev.type = 'br.easycode.project.paused.v1'
                      ORDER BY ev.sequence DESC LIMIT 1) pause ON true
  LEFT JOIN easycode.users pu ON pu.organization_id = pr.organization_id AND pu.sub = pause.envelope->'data'->'actor'->>'id'
                              AND pause.envelope->'data'->'actor'->>'type' = 'human'
  LEFT JOIN LATERAL (SELECT be.envelope FROM easycode.events be
                      WHERE pause.envelope->'data'->>'pause_reason' = 'budget' AND be.project_id = pr.id
                        AND be.type = 'br.easycode.project.budget_exhausted.v1'
                      ORDER BY be.sequence DESC LIMIT 1) budget ON true
 WHERE pr.public_id = ANY($3::uuid[]) AND pr.deletion_requested_at IS NULL`, p.OrganizationID, p.Subject, ids)
	if err != nil {
		return nil, fmt.Errorf("read projects: %w", classify(err))
	}
	defer rows.Close()
	byID := map[string]*appproject.View{}
	for rows.Next() {
		view := &appproject.View{Workflow: r.workflowID}
		var roles []string
		var pause, workflowID, stopLimit *string
		var lastAlert *int16
		var currency, cost string
		var stages, active, findings []byte
		var escalated int
		var pausedAt *time.Time
		var record appproject.PauseRecord
		if err := rows.Scan(&view.ID, &view.State, &roles, &view.Name, &pause, &view.BusinessNeed, &view.AcknowledgedAt,
			&view.AcknowledgedBy.Subject, &view.AcknowledgedBy.Name, &view.CreatedAt, &view.CreatedBy.Subject, &view.CreatedBy.Name,
			&view.UpdatedAt, &view.Lead.Subject, &view.Lead.Name, &stopLimit, &currency, &workflowID, &lastAlert, &view.LastSequence,
			&stages, &active, &view.Summary.GatesAwaiting, &view.Summary.OpenQuestions, &cost, &findings, &escalated,
			&pausedAt, &record.Reason, &record.ActorType, &record.ActorID, &record.ActorName, &record.Origin,
			&record.Cause, &record.NextWaveEstimate.Value, &record.NextWaveEstimate.Currency); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		for _, role := range roles {
			view.MyRoles = append(view.MyRoles, identity.Role(role))
		}
		if pause != nil {
			view.PauseReason = *pause
		}
		if workflowID != nil {
			view.Workflow = *workflowID
		}
		view.AvailableActions = project.Actions(view.State, view.MyRoles, p.Administrator)
		if err := r.fillSummary(view, currency, cost, stopLimit, lastAlert, stages, active, findings, escalated); err != nil {
			return nil, err
		}
		if pausedAt != nil {
			record.At = *pausedAt
			view.Pause = &record
		}
		byID[view.ID] = view
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read projects: %w", classify(err))
	}
	out := make([]appproject.View, 0, len(byID))
	for _, id := range ids {
		if view, ok := byID[id]; ok {
			out = append(out, *view)
		}
	}
	return out, nil
}

// fillSummary decodes the indicators of the dashboard read by views (ProjectSummary).
func (r Projects) fillSummary(view *appproject.View, currency, cost string, stopLimit *string, lastAlert *int16, stages, active, findings []byte, escalated int) error {
	summary := &view.Summary
	summary.StagesTotal, summary.CurrentStages, summary.ActiveWaves = r.stagesInPhase, []appproject.StageState{}, []appproject.WaveRef{}
	summary.AccumulatedCost = project.Money{Value: project.TrimDecimal(cost), Currency: currency}
	if stopLimit != nil {
		summary.StopLimit = &project.Money{Value: project.TrimDecimal(*stopLimit), Currency: currency}
	}
	if lastAlert != nil {
		n := int(*lastAlert)
		summary.LastBudgetAlert = &n
	}
	var stageRows, waveRows [][]string
	if err := json.Unmarshal(stages, &stageRows); err != nil {
		return fmt.Errorf("summary stages: %w", err)
	}
	if err := json.Unmarshal(active, &waveRows); err != nil {
		return fmt.Errorf("summary waves: %w", err)
	}
	if len(stageRows) > 0 {
		summary.StagesTotal = len(stageRows)
	}
	for _, row := range stageRows {
		s := appproject.StageState{Stage: row[0], State: row[1]}
		switch s.State {
		case "approved":
			summary.ApprovedStages++
		case "ready", "running", "awaiting_gate", "returned", "reopened":
			summary.CurrentStages = append(summary.CurrentStages, s)
		}
	}
	for _, row := range waveRows {
		summary.ActiveWaves = append(summary.ActiveWaves, appproject.WaveRef{Code: row[0], Agent: row[1], State: row[2]})
	}
	summary.OpenFindings = map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
	if err := json.Unmarshal(findings, &summary.OpenFindings); err != nil {
		return fmt.Errorf("summary findings: %w", err)
	}
	summary.OpenFindings["escalated"] = escalated
	return nil
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
	after, afterID, err := decodeCursor(filter.Cursor)
	if err != nil {
		return page, failure.Invalid(failure.FieldError{Field: "cursor", Message: "invalid"})
	}
	// The counts by state (without the state filter, PL-15) and the page come in one round trip (RNF-09): the
	// rows of the page, then one row per state with its count (id NULL).
	rows, err := q.Query(WithOperation(ctx, "projects.list"), `
WITH mine AS (
  SELECT pr.public_id::text AS id, `+updatedAtSQL+` AS updated_at, pr.state `+mine+`
), page AS (
  SELECT x.id, x.updated_at FROM mine x
   WHERE (coalesce(cardinality($3::text[]), 0) = 0 OR x.state = ANY($3::text[]))
     AND ($4::timestamptz IS NULL OR (x.updated_at, x.id) < ($4::timestamptz, $5::text))
   ORDER BY x.updated_at DESC, x.id DESC
   LIMIT $6
)
SELECT p.id, p.updated_at, NULL::text, 0 FROM page p
UNION ALL
SELECT NULL, NULL, m.state, count(*)::int FROM mine m GROUP BY m.state`, p.OrganizationID, p.Subject, filter.States, after, afterID, filter.Limit+1)
	if err != nil {
		return page, fmt.Errorf("list projects: %w", classify(err))
	}
	type key struct {
		id string
		at time.Time
	}
	var keys []key
	for rows.Next() {
		var id, state *string
		var at *time.Time
		var n int
		if err := rows.Scan(&id, &at, &state, &n); err != nil {
			rows.Close()
			return page, fmt.Errorf("scan projects: %w", err)
		}
		if id == nil {
			page.CountByState[*state] = n
			continue
		}
		keys = append(keys, key{id: *id, at: *at})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("list projects: %w", classify(err))
	}
	sort.Slice(keys, func(i, j int) bool {
		if !keys[i].at.Equal(keys[j].at) {
			return keys[i].at.After(keys[j].at)
		}
		return keys[i].id > keys[j].id
	})
	if len(keys) > filter.Limit {
		last := keys[filter.Limit-1]
		page.NextCursor = encodeCursor(last.at, last.id)
		keys = keys[:filter.Limit]
	}
	if filter.Organization {
		for _, k := range keys {
			view, err := r.Overview(ctx, k.id)
			if err != nil {
				return page, err
			}
			page.Items = append(page.Items, view)
		}
		return page, nil
	}
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.id)
	}
	page.Items, err = r.views(ctx, p, ids)
	return page, err
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
	view.AvailableActions = project.Actions(view.State, nil, true)
	return view, nil
}
