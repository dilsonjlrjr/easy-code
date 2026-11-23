package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/pricing"
	"easycode/internal/domain/project"
)

var _ appworkflow.Lifecycle = Workflow{}

// activeFindingStates are the finding states that block (achado.md: accepted, closed and discarded do not).
const activeFindingStates = `('open','in_rework','revalidating','escalated','awaiting_scope')`

// Status implements appworkflow.Lifecycle (guards of pause, resume, cancel, completion and budget).
func (w Workflow) Status(ctx context.Context, projectID string) (appworkflow.Status, error) {
	var s appworkflow.Status
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "projects.lifecycle_status"), `
SELECT pr.name, pr.state, coalesce(pr.pause_reason, ''), pr.budget_stop_limit IS NULL OR `+spentSQL+` < pr.budget_stop_limit,
       `+spentSQL+`::text, coalesce(pr.budget_stop_limit::text, ''), o.currency,
       EXISTS (SELECT 1 FROM easycode.stages s WHERE s.project_id = pr.id AND s.stage = 'release' AND s.state = 'approved')
         AND EXISTS (SELECT 1 FROM easycode.gate_rounds r WHERE r.project_id = pr.id AND r.stage = 'release' AND r.state = 'approved'
                       AND r.number = (SELECT max(x.number) FROM easycode.gate_rounds x WHERE x.project_id = pr.id AND x.stage = 'release')),
       coalesce((SELECT array_agg(v.code ORDER BY v.id) FROM easycode.waves v WHERE v.project_id = pr.id
                   AND v.state IN ('queued','running','awaiting_user','pausing','paused')), '{}'),
       coalesce((SELECT array_agg(a.code ORDER BY a.code) FROM easycode.findings f JOIN easycode.artifacts a ON a.id = f.artifact_id
                  WHERE f.project_id = pr.id AND f.severity IN ('critical','high') AND f.status IN `+activeFindingStates+`), '{}')
  FROM easycode.projects pr JOIN easycode.organizations o ON o.id = pr.organization_id
 WHERE pr.public_id = $1::uuid`, projectID).Scan(&s.Name, &s.State, &s.PauseReason, &s.Balance, &s.Spend, &s.StopLimit, &s.Currency,
		&s.ReleaseApproved, &s.ActiveWaves, &s.BlockingFindings)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return s, fmt.Errorf("project status: %w", classify(err))
	}
	s.Spend, s.StopLimit = pricing.NormalizeDecimal(s.Spend), project.TrimDecimal(s.StopLimit)
	return s, nil
}

const controlledWave = `v.code, v.stage, v.agent, v.purpose, coalesce(v.task, ''),
       coalesce((SELECT a.code FROM easycode.artifacts a WHERE a.id = v.finding_id), '')`

func scanControlled(rows pgx.Rows, from string) ([]appworkflow.ControlledWave, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appworkflow.ControlledWave, error) {
		c := appworkflow.ControlledWave{From: from}
		return c, row.Scan(&c.Code, &c.Stage, &c.Agent, &c.Purpose, &c.Task, &c.Finding)
	})
}

// PauseProject implements appworkflow.Lifecycle: in_progress → paused, running waves → pausing (pause_by_policy).
// A running wave nobody executes (no lease, or the lease of a fallen worker) has no step to finish: it reaches
// paused at once from its last checkpoint (checkpoint_reached), the call left in flight by a fallen worker lost
// with its estimated cost (RNF-16).
func (w Workflow) PauseProject(ctx context.Context, projectID, reason, waveCause string) ([]appworkflow.ControlledWave, error) {
	q := conn(ctx, w.pool)
	var internalID int64
	err := q.QueryRow(WithOperation(ctx, "projects.pause"), `
UPDATE easycode.projects SET state = 'paused', pause_reason = $2 WHERE public_id = $1::uuid AND state = 'in_progress' RETURNING id`,
		projectID, reason).Scan(&internalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("pause project: %w", classify(err))
	}
	rows, err := q.Query(WithOperation(ctx, "waves.pause_by_policy"), `
UPDATE easycode.waves v SET state = 'pausing', pause_cause = $2 WHERE v.project_id = $1 AND v.state = 'running'
RETURNING `+controlledWave+`, v.lease_owner IS NULL OR v.lease_until < now()`, internalID, waveCause)
	if err != nil {
		return nil, fmt.Errorf("pause waves of the project: %w", classify(err))
	}
	type held struct {
		appworkflow.ControlledWave
		idle bool
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (held, error) {
		var h held
		h.From = "running"
		return h, row.Scan(&h.Code, &h.Stage, &h.Agent, &h.Purpose, &h.Task, &h.Finding, &h.idle)
	})
	if err != nil {
		return nil, err
	}
	out := make([]appworkflow.ControlledWave, 0, len(list))
	for _, h := range list {
		if h.idle {
			if _, err := q.Exec(WithOperation(ctx, "model_calls.abort_idle_in_flight"), `
WITH aborted AS (
  UPDATE easycode.model_calls c SET result = 'aborted', lost = true, estimated_cost = true, finished_at = now(), error_summary = 'worker restarted'
    FROM easycode.waves v WHERE v.id = c.wave_id AND v.project_id = $1 AND v.code = $2 AND c.result IS NULL RETURNING c.wave_id, c.cost)
UPDATE easycode.waves v SET lost_cost = v.lost_cost + coalesce((SELECT sum(cost) FROM aborted), 0),
       estimated_lost_cost = v.estimated_lost_cost OR EXISTS (SELECT 1 FROM aborted)
 WHERE v.project_id = $1 AND v.code = $2`, internalID, h.Code); err != nil {
				return nil, fmt.Errorf("abort idle call: %w", classify(err))
			}
			if _, err := q.Exec(WithOperation(ctx, "waves.pause_idle"), `
UPDATE easycode.waves SET state = 'paused', pause_mode = 'cooperative', lease_owner = NULL, lease_until = NULL
 WHERE project_id = $1 AND code = $2 AND state = 'pausing'`, internalID, h.Code); err != nil {
				return nil, fmt.Errorf("pause idle wave: %w", classify(err))
			}
			h.Paused = true
		}
		out = append(out, h.ControlledWave)
	}
	return out, nil
}

// ResumeProject implements appworkflow.Lifecycle: paused → in_progress, waves paused by the project or the
// budget → running without lease (resume_by_policy; the executor claims them from the last checkpoint).
func (w Workflow) ResumeProject(ctx context.Context, projectID string) ([]appworkflow.ControlledWave, error) {
	q := conn(ctx, w.pool)
	var internalID int64
	err := q.QueryRow(WithOperation(ctx, "projects.resume"), `
UPDATE easycode.projects SET state = 'in_progress', pause_reason = NULL WHERE public_id = $1::uuid AND state = 'paused' RETURNING id`,
		projectID).Scan(&internalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("resume project: %w", classify(err))
	}
	rows, err := q.Query(WithOperation(ctx, "waves.resume_by_project"), `
UPDATE easycode.waves v SET state = 'running', pause_cause = NULL, pause_mode = NULL
 WHERE v.project_id = $1 AND v.state = 'paused' AND v.pause_cause IN ('project_paused', 'budget_exhausted')
RETURNING `+controlledWave, internalID)
	if err != nil {
		return nil, fmt.Errorf("resume waves of the project: %w", classify(err))
	}
	waves, err := scanControlled(rows, "paused")
	if err == nil && len(waves) > 0 {
		_, err = q.Exec(WithOperation(ctx, "waves.notify"), "SELECT pg_notify($1, $2)", ChannelWaves, projectID)
	}
	return waves, err
}

// ResumeHeld implements appworkflow.Lifecycle.
func (w Workflow) ResumeHeld(ctx context.Context) (map[string][]appworkflow.ControlledWave, error) {
	q := conn(ctx, w.pool)
	rows, err := q.Query(WithOperation(ctx, "waves.resume_held"), `
UPDATE easycode.waves v SET state = 'running', pause_cause = NULL, pause_mode = NULL
  FROM easycode.projects pr
 WHERE pr.id = v.project_id AND pr.state = 'in_progress' AND v.state = 'paused' AND v.pause_cause IN ('project_paused', 'budget_exhausted')
RETURNING pr.public_id::text, `+controlledWave)
	if err != nil {
		return nil, fmt.Errorf("resume held waves: %w", classify(err))
	}
	out := map[string][]appworkflow.ControlledWave{}
	type entry struct {
		project string
		wave    appworkflow.ControlledWave
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (entry, error) {
		var e entry
		e.wave.From = "paused"
		return e, row.Scan(&e.project, &e.wave.Code, &e.wave.Stage, &e.wave.Agent, &e.wave.Purpose, &e.wave.Task, &e.wave.Finding)
	})
	if err != nil {
		return nil, err
	}
	for _, e := range list {
		out[e.project] = append(out[e.project], e.wave)
	}
	for projectID := range out {
		if _, err := q.Exec(WithOperation(ctx, "waves.notify"), "SELECT pg_notify($1, $2)", ChannelWaves, projectID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CancelProject implements appworkflow.Lifecycle: every wave not completed nor canceled is canceled as the
// cancel command does (call in flight aborted, lost cost recorded, lease freed), then the project.
func (w Workflow) CancelProject(ctx context.Context, projectID string) ([]appworkflow.ControlledWave, error) {
	q := conn(ctx, w.pool)
	rows, err := q.Query(WithOperation(ctx, "waves.active_of_project"), `
SELECT v.code FROM easycode.waves v JOIN easycode.projects pr ON pr.id = v.project_id
 WHERE pr.public_id = $1::uuid AND v.state IN ('queued','running','awaiting_user','pausing','paused','failed') ORDER BY v.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("active waves: %w", classify(err))
	}
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	waves := Waves{pool: w.pool}
	out := make([]appworkflow.ControlledWave, 0, len(codes))
	for _, code := range codes {
		canceled, err := waves.Cancel(ctx, projectID, code)
		if err != nil {
			return nil, err
		}
		c := appworkflow.ControlledWave{Code: canceled.Code, Stage: canceled.Stage, Agent: canceled.Agent, Purpose: canceled.Purpose,
			Task: canceled.Task, From: canceled.From, LostCost: canceled.LostCost}
		_ = q.QueryRow(WithOperation(ctx, "waves.finding_code"), `
SELECT coalesce(a.code, '') FROM easycode.waves v JOIN easycode.projects pr ON pr.id = v.project_id LEFT JOIN easycode.artifacts a ON a.id = v.finding_id
 WHERE pr.public_id = $1::uuid AND v.code = $2`, projectID, code).Scan(&c.Finding)
		out = append(out, c)
	}
	tag, err := q.Exec(WithOperation(ctx, "projects.cancel"), `
UPDATE easycode.projects SET state = 'canceled', pause_reason = NULL, finished_at = now()
 WHERE public_id = $1::uuid AND state IN ('created','in_progress','paused')`, projectID)
	if err != nil {
		return nil, fmt.Errorf("cancel project: %w", classify(err))
	}
	if tag.RowsAffected() == 0 {
		return nil, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	return out, nil
}

// CompleteProject implements appworkflow.Lifecycle (record_publication; the database rechecks the guard,
// projects_valid_completion).
func (w Workflow) CompleteProject(ctx context.Context, projectID string) (appworkflow.ReleaseMark, error) {
	q := conn(ctx, w.pool)
	var mark appworkflow.ReleaseMark
	err := q.QueryRow(WithOperation(ctx, "artifacts.release_mark"), `
SELECT a.code, a.current_version, coalesce(v.header->'entrega'->>'tag', '')
  FROM easycode.artifacts a JOIN easycode.projects pr ON pr.id = a.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version
 WHERE pr.public_id = $1::uuid AND a.type = 'RELEASE' AND a.state = 'approved' ORDER BY a.code DESC LIMIT 1`, projectID).
		Scan(&mark.Artifact, &mark.Version, &mark.Tag)
	if errors.Is(err, pgx.ErrNoRows) {
		return mark, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	if err != nil {
		return mark, fmt.Errorf("release of the project: %w", classify(err))
	}
	tag, err := q.Exec(WithOperation(ctx, "projects.complete"), `
UPDATE easycode.projects SET state = 'completed', finished_at = now() WHERE public_id = $1::uuid AND state = 'in_progress'`, projectID)
	if err != nil {
		return mark, fmt.Errorf("complete project: %w", classify(err))
	}
	if tag.RowsAffected() == 0 {
		return mark, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	return mark, nil
}

// RecordAlert implements appworkflow.Lifecycle.
func (w Workflow) RecordAlert(ctx context.Context, projectID string, percentage int) error {
	_, err := conn(ctx, w.pool).Exec(WithOperation(ctx, "projects.budget_alert"),
		"UPDATE easycode.projects SET last_alert_percentage = $2 WHERE public_id = $1::uuid", projectID, percentage)
	if err != nil {
		return fmt.Errorf("record budget alert: %w", classify(err))
	}
	return nil
}

// Alerts implements appworkflow.Lifecycle.
func (w Workflow) Alerts(ctx context.Context, projectID string) ([]int, int, error) {
	var alerts []int
	var last int
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "projects.budget_alerts"),
		"SELECT budget_alert_limits, coalesce(last_alert_percentage, 0) FROM easycode.projects WHERE public_id = $1::uuid", projectID).Scan(&alerts, &last)
	if err != nil {
		return nil, 0, fmt.Errorf("budget alerts: %w", classify(err))
	}
	return alerts, last, nil
}

// BudgetCandidates implements appworkflow.Lifecycle.
func (w Workflow) BudgetCandidates(ctx context.Context) ([]string, error) {
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "projects.budget_candidates"), `
SELECT pr.public_id::text FROM easycode.projects pr
 WHERE pr.state = 'in_progress' AND pr.budget_stop_limit IS NOT NULL
   AND (`+spentSQL+` >= pr.budget_stop_limit
        OR EXISTS (SELECT 1 FROM unnest(pr.budget_alert_limits) p(v)
                    WHERE p.v > coalesce(pr.last_alert_percentage, 0) AND `+spentSQL+` * 100 >= pr.budget_stop_limit * p.v))`)
	if err != nil {
		return nil, fmt.Errorf("budget candidates: %w", classify(err))
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
