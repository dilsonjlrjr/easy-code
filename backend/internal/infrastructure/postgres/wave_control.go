package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
)

var (
	_ appwave.Controls     = Waves{}
	_ appwave.ControlStore = Waves{}
)

// EnqueueControl implements appwave.Controls (US-12, US-13): pause_now and restart carry the confirmation.
func (w Waves) EnqueueControl(ctx context.Context, p identity.Principal, projectID, waveCode, commandType string, payload map[string]any, key, bodyHash string) (appworkflow.CommandView, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	return w.enqueueWaveCommand(ctx, p, projectID, waveCode, commandType, encoded, key, bodyHash)
}

// spentSQL is the accumulated cost of a project (useful and lost, estimated included; policy.project_budget).
const spentSQL = `(SELECT coalesce(sum(x.useful_cost + x.lost_cost), 0) FROM easycode.waves x WHERE x.project_id = pr.id)`

// Balance implements appwave.Controls: the spend is below the stop limit (a project without limit has balance).
func (w Waves) Balance(ctx context.Context, projectID string) (bool, error) {
	var balance bool
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "projects.balance"), `
SELECT pr.budget_stop_limit IS NULL OR `+spentSQL+` < pr.budget_stop_limit FROM easycode.projects pr WHERE pr.public_id = $1::uuid`, projectID).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return false, fmt.Errorf("project balance: %w", classify(err))
	}
	return balance, nil
}

// lockControlled locks the wave and reads what the control events carry, with the project state, its
// balance and the stage state (guards of resume).
func (w Waves) lockControlled(ctx context.Context, projectID, waveCode string) (int64, appwave.Controlled, controlGuard, error) {
	var out appwave.Controlled
	var g controlGuard
	var id int64
	var task, finding *string
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "waves.lock_for_control"), `
SELECT w.id, w.code, w.stage, w.agent, w.purpose, w.state, w.task, a.code, w.attempt, w.currency, pr.state, s.state,
       pr.budget_stop_limit IS NULL OR `+spentSQL+` < pr.budget_stop_limit
  FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id JOIN easycode.stages s ON s.id = w.stage_id
  LEFT JOIN easycode.artifacts a ON a.id = w.finding_id
 WHERE pr.public_id = $1::uuid AND w.code = $2 FOR UPDATE OF w`, projectID, waveCode).
		Scan(&id, &out.Code, &out.Stage, &out.Agent, &out.Purpose, &out.From, &task, &finding, &out.Attempt, &out.Currency, &g.project, &g.stage, &g.balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, out, g, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return 0, out, g, fmt.Errorf("lock wave: %w", classify(err))
	}
	out.Task, out.Finding = deref(task), deref(finding)
	return id, out, g, nil
}

type controlGuard struct {
	project, stage string
	balance        bool
}

func (w Waves) notifyWaves(ctx context.Context, projectID string) error {
	_, err := conn(ctx, w.pool).Exec(WithOperation(ctx, "waves.notify"), "SELECT pg_notify($1, $2)", ChannelWaves, projectID)
	return err
}

// RequestPause implements appwave.ControlStore (pause and pause_by_policy: running → pausing).
func (w Waves) RequestPause(ctx context.Context, projectID, waveCode, cause string) (appwave.Controlled, error) {
	id, out, _, err := w.lockControlled(ctx, projectID, waveCode)
	if err != nil {
		return out, err
	}
	if out.From != "running" {
		return out, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	if _, err := conn(ctx, w.pool).Exec(WithOperation(ctx, "waves.pause_requested"),
		"UPDATE easycode.waves SET state = 'pausing', pause_cause = $2 WHERE id = $1", id, cause); err != nil {
		return out, fmt.Errorf("request pause: %w", classify(err))
	}
	return out, nil
}

// PauseNow implements appwave.ControlStore (pause_now): the call in flight is closed as aborted and lost
// with its estimated cost (RNF-16), and the lease is freed, so the fenced writes of the worker that runs the
// wave fail and it aborts the call within RNF-10.
func (w Waves) PauseNow(ctx context.Context, projectID, waveCode string) (appwave.Controlled, error) {
	id, out, _, err := w.lockControlled(ctx, projectID, waveCode)
	if err != nil {
		return out, err
	}
	if out.From != "running" && out.From != "pausing" {
		return out, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	q := conn(ctx, w.pool)
	var lost string
	var aborted int
	err = q.QueryRow(WithOperation(ctx, "model_calls.abort_for_pause"), `
WITH aborted AS (
  UPDATE easycode.model_calls SET result = 'aborted', lost = true, estimated_cost = true, finished_at = now(), error_summary = 'wave paused now'
   WHERE wave_id = $1 AND result IS NULL RETURNING cost)
SELECT coalesce(sum(cost), 0)::text, count(*) FROM aborted`, id).Scan(&lost, &aborted)
	if err != nil {
		return out, fmt.Errorf("abort calls of paused wave: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "waves.pause_now"), `
UPDATE easycode.waves SET state = 'paused', pause_mode = 'immediate', pause_cause = 'user_request', lease_owner = NULL, lease_until = NULL,
       lost_cost = lost_cost + $2::numeric, estimated_lost_cost = estimated_lost_cost OR $3
 WHERE id = $1`, id, lost, aborted > 0); err != nil {
		return out, fmt.Errorf("pause wave now: %w", classify(err))
	}
	if aborted > 0 {
		out.LostCost = pricing.NormalizeDecimal(lost)
	}
	return out, nil
}

// ResumeFromPause implements appwave.ControlStore (resume: paused → running from the last checkpoint).
func (w Waves) ResumeFromPause(ctx context.Context, projectID, waveCode string) (appwave.Controlled, error) {
	id, out, guard, err := w.lockControlled(ctx, projectID, waveCode)
	if err != nil {
		return out, err
	}
	switch {
	case out.From != "paused":
		return out, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	case !guard.balance:
		return out, failure.New(failure.KindConflict, appwave.CodeBudgetExhausted, nil)
	case guard.project != "in_progress" || guard.stage == "blocked":
		return out, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	if _, err := conn(ctx, w.pool).Exec(WithOperation(ctx, "waves.resume"),
		"UPDATE easycode.waves SET state = 'running', pause_cause = NULL, pause_mode = NULL WHERE id = $1", id); err != nil {
		return out, fmt.Errorf("resume wave: %w", classify(err))
	}
	return out, w.notifyWaves(ctx, projectID)
}

// Restart implements appwave.ControlStore (restart: failed → queued). The new attempt starts from no
// checkpoint with the retry counter at zero; calls, checkpoints and events of the previous attempt stay.
func (w Waves) Restart(ctx context.Context, projectID, waveCode string) (appwave.Controlled, error) {
	id, out, _, err := w.lockControlled(ctx, projectID, waveCode)
	if err != nil {
		return out, err
	}
	if out.From != "failed" {
		return out, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	err = conn(ctx, w.pool).QueryRow(WithOperation(ctx, "waves.restart"), `
UPDATE easycode.waves SET state = 'queued', attempt = attempt + 1, retries_used = 0, failure_cause = NULL, current_step = 0
 WHERE id = $1 RETURNING attempt`, id).Scan(&out.Attempt)
	if err != nil {
		return out, fmt.Errorf("restart wave: %w", classify(err))
	}
	return out, w.notifyWaves(ctx, projectID)
}

// control reads the pause and the previous attempts of a wave (getWave; US-12 CA-4, US-13 CA-1).
func (w Waves) control(ctx context.Context, waveID int64, v *appwave.View) error {
	q := conn(ctx, w.pool)
	if v.State == "pausing" || v.State == "paused" {
		var pause appwave.PauseView
		var mode, cause *string
		var at *time.Time
		err := q.QueryRow(WithOperation(ctx, "waves.pause"), `
SELECT w.pause_mode, w.pause_cause,
       (SELECT max(e.occurred_at) FROM easycode.events e WHERE e.project_id = w.project_id AND e.waveid = w.code
           AND e.type IN ('br.easycode.wave.pause_requested.v1', 'br.easycode.wave.paused.v1'))
  FROM easycode.waves w WHERE w.id = $1`, waveID).Scan(&mode, &cause, &at)
		if err != nil {
			return fmt.Errorf("wave pause: %w", classify(err))
		}
		pause.Mode, pause.Cause = deref(mode), deref(cause)
		if at != nil {
			pause.RequestedAt = *at
		}
		v.Pause = &pause
	}
	if v.Attempt <= 1 {
		return nil
	}
	rows, err := q.Query(WithOperation(ctx, "waves.attempts"), `
WITH failed AS (
  SELECT row_number() OVER (ORDER BY e.sequence) AS n, e.envelope->'data'->>'cause' AS cause, e.occurred_at, e.sequence
    FROM easycode.events e JOIN easycode.waves w ON w.project_id = e.project_id AND w.code = e.waveid
   WHERE w.id = $1 AND e.type = 'br.easycode.wave.failed.v1'),
started AS (
  SELECT coalesce((e.envelope->'data'->>'attempt')::int, 1) AS n, min(e.occurred_at) AS occurred_at, min(e.sequence) AS sequence
    FROM easycode.events e JOIN easycode.waves w ON w.project_id = e.project_id AND w.code = e.waveid
   WHERE w.id = $1 AND e.type = 'br.easycode.wave.started.v1' GROUP BY 1),
costs AS (
  SELECT c.attempt AS n, coalesce(sum(c.cost) FILTER (WHERE NOT c.lost), 0) AS useful, coalesce(sum(c.cost) FILTER (WHERE c.lost), 0) AS lost,
         bool_or(c.lost AND c.estimated_cost) AS estimated
    FROM easycode.model_calls c WHERE c.wave_id = $1 GROUP BY c.attempt)
SELECT g.n, coalesce(f.cause, ''), s.occurred_at, f.occurred_at, coalesce(c.useful, 0)::text, coalesce(c.lost, 0)::text, coalesce(c.estimated, false),
       coalesce(s.sequence, 0), coalesce(f.sequence, 0)
  FROM generate_series(1, $2::int - 1) g(n) LEFT JOIN failed f ON f.n = g.n LEFT JOIN started s ON s.n = g.n LEFT JOIN costs c ON c.n = g.n
 ORDER BY g.n`, waveID, v.Attempt)
	if err != nil {
		return fmt.Errorf("wave attempts: %w", classify(err))
	}
	v.Attempts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (appwave.AttemptView, error) {
		a := appwave.AttemptView{FinalState: "failed"}
		err := row.Scan(&a.Number, &a.FailureCause, &a.Start, &a.End, &a.Cost.Useful, &a.Cost.Lost, &a.Cost.EstimatedLost, &a.FirstSequence, &a.LastSequence)
		a.Cost.Useful, a.Cost.Lost, a.Cost.Currency = pricing.NormalizeDecimal(a.Cost.Useful), pricing.NormalizeDecimal(a.Cost.Lost), v.Cost.Currency
		return a, err
	})
	return err
}
