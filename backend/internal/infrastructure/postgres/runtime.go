package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/runtime"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/pricing"
	"easycode/internal/domain/skill"
	"easycode/internal/domain/workflow"
)

// Ledger is the persistence of the wave executor (worker role).
type Ledger struct {
	pool *pgxpool.Pool
}

var (
	_ runtime.Ledger      = Ledger{}
	_ runtime.CallLog     = Ledger{}
	_ runtime.PriceLookup = Ledger{}
)

// NewLedger builds the adapter.
func NewLedger(pool *pgxpool.Pool) Ledger { return Ledger{pool: pool} }

// Claim implements runtime.Ledger.
func (l Ledger) Claim(ctx context.Context, worker string, agents []string, lease time.Duration) (*runtime.Claimed, error) {
	q := conn(ctx, l.pool)
	var c runtime.Claimed
	var state string
	var owner *string
	var until *time.Time
	err := q.QueryRow(WithOperation(ctx, "waves.claim"), `
SELECT w.id, w.public_id::text, w.code, pr.public_id::text, w.stage, w.agent, w.purpose, w.attempt, w.state,
       w.lease_owner, w.lease_until, w.lease_epoch, w.current_step, s.state
  FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id JOIN easycode.stages s ON s.id = w.stage_id
 WHERE w.agent = ANY($1::text[]) AND pr.state = 'in_progress'
   AND (w.state = 'queued' OR (w.state = 'running' AND (w.lease_owner IS NULL OR w.lease_until < now())))
   -- waves of a stage run one at a time, in order (one implementation wave per task, PL-20)
   AND NOT EXISTS (SELECT 1 FROM easycode.waves o WHERE o.project_id = w.project_id AND o.stage = w.stage AND o.id < w.id
                     AND o.state NOT IN ('completed', 'canceled'))
 ORDER BY w.created_at
 LIMIT 1
 FOR UPDATE OF w SKIP LOCKED`, agents).Scan(&c.WaveID, &c.PublicID, &c.Code, &c.ProjectID, &c.Stage, &c.Agent, &c.Purpose, &c.Attempt,
		&state, &owner, &until, &c.Epoch, &c.CurrentStep, &c.StageState)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim wave: %w", classify(err))
	}
	c.WasQueued = state == "queued"
	c.Crashed = state == "running" && owner != nil
	c.Owner, c.Epoch = worker, c.Epoch+1
	seconds := lease.Seconds()
	if c.WasQueued {
		_, err = q.Exec(WithOperation(ctx, "waves.dispatch"), `
UPDATE easycode.waves SET state = 'running', started_at = now(), cost_estimate = 0, estimate_basis = '{"method":"none"}',
       lease_owner = $2, lease_epoch = $3, lease_until = now() + make_interval(secs => $4), heartbeat_at = now()
 WHERE id = $1`, c.WaveID, worker, c.Epoch, seconds)
	} else {
		_, err = q.Exec(WithOperation(ctx, "waves.take_lease"), `
UPDATE easycode.waves SET lease_owner = $2, lease_epoch = $3, lease_until = now() + make_interval(secs => $4), heartbeat_at = now()
 WHERE id = $1`, c.WaveID, worker, c.Epoch, seconds)
	}
	if err != nil {
		return nil, fmt.Errorf("take wave lease: %w", classify(err))
	}
	if c.Crashed {
		// The call in flight when the worker fell is lost: its estimated input cost is lost cost (RNF-16).
		var lost string
		var redone int
		err = q.QueryRow(WithOperation(ctx, "model_calls.abort_in_flight"), `
WITH aborted AS (
  UPDATE easycode.model_calls SET result = 'aborted', lost = true, estimated_cost = true, finished_at = now(), error_summary = 'worker restarted'
   WHERE wave_id = $1 AND result IS NULL RETURNING cost)
SELECT coalesce(sum(cost), 0)::text, count(*) FROM aborted`, c.WaveID).Scan(&lost, &redone)
		if err != nil {
			return nil, fmt.Errorf("abort in-flight calls: %w", classify(err))
		}
		if redone > 0 {
			c.RedoneCall, c.LostCost = true, pricing.NormalizeDecimal(lost)
			_, err = q.Exec(WithOperation(ctx, "waves.add_estimated_lost"),
				"UPDATE easycode.waves SET lost_cost = lost_cost + $2::numeric, estimated_lost_cost = true WHERE id = $1", c.WaveID, lost)
			if err != nil {
				return nil, fmt.Errorf("record lost cost: %w", classify(err))
			}
		}
	}
	return &c, nil
}

func (l Ledger) fenced(ctx context.Context, operation, sql string, lease runtime.Lease, args ...any) pgx.Row {
	all := append([]any{lease.WaveID, lease.Owner, lease.Epoch}, args...)
	return conn(ctx, l.pool).QueryRow(WithOperation(ctx, operation), sql, all...)
}

func leaseLost(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return runtime.ErrLeaseLost
	}
	return classify(err)
}

const fence = ` WHERE id = $1 AND lease_owner = $2 AND lease_epoch = $3`

// Held implements runtime.Ledger: the lease is still this worker's and the wave still runs (a cancel
// frees the lease, US-14).
func (l Ledger) Held(ctx context.Context, lease runtime.Lease) (bool, error) {
	var held bool
	err := conn(ctx, l.pool).QueryRow(WithOperation(ctx, "waves.lease_held"), `
SELECT EXISTS (SELECT 1 FROM easycode.waves WHERE id = $1 AND lease_owner = $2 AND lease_epoch = $3 AND state IN ('running', 'pausing'))`,
		lease.WaveID, lease.Owner, lease.Epoch).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("check lease: %w", classify(err))
	}
	return held, nil
}

// Renew implements runtime.Ledger.
func (l Ledger) Renew(ctx context.Context, lease runtime.Lease, duration time.Duration) error {
	row := l.fenced(ctx, "waves.renew", `UPDATE easycode.waves SET lease_until = now() + make_interval(secs => $4), heartbeat_at = now()`+fence+` RETURNING id`,
		lease, duration.Seconds())
	var id int64
	return leaseLost(row.Scan(&id))
}

// Inputs implements runtime.Ledger.
func (l Ledger) Inputs(ctx context.Context, c runtime.Claimed) (runtime.Inputs, error) {
	q := conn(ctx, l.pool)
	var in runtime.Inputs
	var constraints []byte
	err := q.QueryRow(WithOperation(ctx, "waves.inputs"), `
SELECT pr.business_need, pr.artifact_language, ac.instruction_version, ac.instruction_hash, coalesce(w.task, ''),
       (SELECT c.items FROM easycode.project_constraints c WHERE c.project_id = pr.id AND c.defined_at <= w.created_at
         ORDER BY c.version DESC LIMIT 1),
       coalesce((SELECT g.reason FROM easycode.gate_rounds g WHERE g.project_id = pr.id AND g.stage = w.stage AND g.state = 'rejected'
                  ORDER BY g.number DESC LIMIT 1), '')
  FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
  JOIN easycode.agent_configurations ac ON ac.id = w.agent_configuration_id
 WHERE w.id = $1`, c.WaveID).Scan(&in.BusinessNeed, &in.Language, &in.InstructionVersion, &in.InstructionHash, &in.Task, &constraints, &in.Feedback)
	if err != nil {
		return in, fmt.Errorf("wave inputs: %w", classify(err))
	}
	if len(constraints) > 0 {
		var items []constraintJSON
		if err := json.Unmarshal(constraints, &items); err == nil {
			var text strings.Builder
			for _, item := range items {
				fmt.Fprintf(&text, "%s (%s): %s\n", item.ID, item.Category, item.Description)
			}
			in.Constraints = text.String()
		}
	}
	approved, err := q.Query(WithOperation(ctx, "artifacts.approved_paths"), `
SELECT v.path, v.author FROM easycode.waves w JOIN easycode.artifacts a ON a.project_id = w.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version
 WHERE w.id = $1 AND a.state = 'approved'`, c.WaveID)
	if err != nil {
		return in, fmt.Errorf("approved artifacts: %w", classify(err))
	}
	in.Approved = map[string]string{}
	for approved.Next() {
		var path, author string
		if err := approved.Scan(&path, &author); err != nil {
			approved.Close()
			return in, err
		}
		in.Approved[path] = author
	}
	approved.Close()
	if err := approved.Err(); err != nil {
		return in, err
	}
	if in.Opinions, err = l.opinions(ctx, c.WaveID); err != nil {
		return in, err
	}
	skills, err := loadedSkills(ctx, q, c.WaveID)
	if err != nil {
		return in, err
	}
	in.Skills = skill.Render(skills)
	in.SkillFiles = map[string]string{}
	for _, loaded := range skills {
		for file, content := range loaded.Files {
			in.SkillFiles[skill.Path(loaded.Name, file)] = content
		}
	}
	rows, err := q.Query(WithOperation(ctx, "waves.models"), `
SELECT wm.role, m.id, m.provider, m.name FROM easycode.wave_models wm JOIN easycode.models m ON m.id = wm.model_id WHERE wm.wave_id = $1`, c.WaveID)
	if err != nil {
		return in, fmt.Errorf("wave models: %w", classify(err))
	}
	defer rows.Close()
	in.Models = map[string]runtime.ModelRef{}
	for rows.Next() {
		var role string
		var ref runtime.ModelRef
		if err := rows.Scan(&role, &ref.ID, &ref.Provider, &ref.Name); err != nil {
			return in, err
		}
		in.Models[role] = ref
	}
	return in, rows.Err()
}

// opinions are the review opinions recorded by the earlier completed waves of other agents in the
// stage of the wave (the participants of the validation), the last one per agent.
func (l Ledger) opinions(ctx context.Context, waveID int64) ([]workflow.ReviewOpinion, error) {
	rows, err := conn(ctx, l.pool).Query(WithOperation(ctx, "waves.review_opinions"), `
SELECT DISTINCT ON (o.agent) ce.evidence FROM easycode.waves w
  JOIN easycode.waves o ON o.project_id = w.project_id AND o.stage = w.stage AND o.agent <> w.agent AND o.state = 'completed' AND o.id < w.id
  JOIN easycode.check_executions ce ON ce.wave_id = o.id AND ce.check_id = $2
 WHERE w.id = $1
 ORDER BY o.agent, o.id DESC`, waveID, runtime.CheckReviewOpinion)
	if err != nil {
		return nil, fmt.Errorf("review opinions: %w", classify(err))
	}
	defer rows.Close()
	var out []workflow.ReviewOpinion
	for rows.Next() {
		var raw []byte
		var opinion workflow.ReviewOpinion
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &opinion) == nil {
			out = append(out, opinion)
		}
	}
	return out, rows.Err()
}

// LastCheckpoint implements runtime.Ledger.
func (l Ledger) LastCheckpoint(ctx context.Context, lease runtime.Lease, attempt int) (*runtime.Checkpoint, error) {
	var cp runtime.Checkpoint
	var state []byte
	err := conn(ctx, l.pool).QueryRow(WithOperation(ctx, "wave_checkpoints.last"), `
SELECT step, after, agent_state FROM easycode.wave_checkpoints WHERE wave_id = $1 AND attempt = $2 ORDER BY step DESC LIMIT 1`,
		lease.WaveID, attempt).Scan(&cp.Number, &cp.After, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last checkpoint: %w", classify(err))
	}
	if state == nil {
		return nil, failure.New(failure.KindConflict, "checkpoint-purged", nil)
	}
	cp.State = state
	return &cp, nil
}

// LatestAnswer implements runtime.Ledger.
func (l Ledger) LatestAnswer(ctx context.Context, lease runtime.Lease) (*runtime.QuestionAnswer, error) {
	var answer runtime.QuestionAnswer
	err := conn(ctx, l.pool).QueryRow(WithOperation(ctx, "wave_questions.latest_answer"), `
SELECT q.code, array_agg(i.answer ORDER BY i.number) FROM easycode.wave_questions q JOIN easycode.question_items i ON i.question_id = q.id
 WHERE q.wave_id = $1 AND q.answered_at IS NOT NULL
 GROUP BY q.id, q.code, q.answered_at ORDER BY q.answered_at DESC LIMIT 1`, lease.WaveID).Scan(&answer.Code, &answer.Answers)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest answer: %w", classify(err))
	}
	return &answer, nil
}

// SaveStep implements runtime.Ledger: fenced, one checkpoint per model call or tool execution.
func (l Ledger) SaveStep(ctx context.Context, lease runtime.Lease, attempt int, step runtime.StepWrite) (runtime.Totals, error) {
	q := conn(ctx, l.pool)
	var createdAt time.Time
	row := l.fenced(ctx, "waves.step", `
UPDATE easycode.waves SET heartbeat_at = now(), execution_duration_ms = execution_duration_ms + $4,
       current_step = CASE WHEN $5 THEN $6 ELSE current_step END`+fence+` RETURNING created_at`,
		lease, step.DurationMs, step.Checkpointed, step.Checkpoint.Number)
	if err := row.Scan(&createdAt); err != nil {
		return runtime.Totals{}, leaseLost(err)
	}
	if call := step.Call; call != nil && call.ID != 0 {
		var cost, priceID string
		lost := call.Result != "success"
		err := q.QueryRow(WithOperation(ctx, "model_calls.finish"), `
UPDATE easycode.model_calls SET input_tokens = $2, output_tokens = $3, read_cache_tokens = $4, write_cache_tokens = $5,
       result = $6, lost = $7, error_summary = nullif($8, ''), summary = nullif(left($9, 200), ''), finished_at = now()
 WHERE id = $1 AND result IS NULL RETURNING cost::text, price_id::text`,
			call.ID, call.Usage.Input, call.Usage.Output, call.Usage.CacheRead, call.Usage.CacheWrite, call.Result, lost, call.Error, call.Summary).Scan(&cost, &priceID)
		if err != nil {
			return runtime.Totals{}, fmt.Errorf("finish model call: %w", classify(err))
		}
		call.Cost, call.PriceTableVersion = pricing.NormalizeDecimal(cost), priceID
		column := "useful_cost"
		if lost {
			column = "lost_cost"
		}
		_, err = q.Exec(WithOperation(ctx, "waves.add_call"), `
UPDATE easycode.waves SET input_tokens = input_tokens + $2, output_tokens = output_tokens + $3, read_cache_tokens = read_cache_tokens + $4,
       write_cache_tokens = write_cache_tokens + $5, `+column+` = `+column+` + $6::numeric WHERE id = $1`,
			lease.WaveID, call.Usage.Input, call.Usage.Output, call.Usage.CacheRead, call.Usage.CacheWrite, cost)
		if err != nil {
			return runtime.Totals{}, fmt.Errorf("add call cost: %w", classify(err))
		}
	}
	if tool := step.Tool; tool != nil {
		permission, status, refusal := "allowed", "success", any(nil)
		if !tool.Allowed {
			permission, status, refusal = "refused", "", tool.RefusalReason
		} else if !tool.Success {
			status = "error"
		}
		if tool.Execution != nil {
			if err := l.recordExecution(ctx, lease, attempt, max(step.Checkpoint.Number, 1), tool, step.DurationMs); err != nil {
				return runtime.Totals{}, err
			}
		} else {
			_, err := q.Exec(WithOperation(ctx, "tool_executions.insert"), `
INSERT INTO easycode.tool_executions (wave_id, attempt, step, tool, permission, refusal_reason, status, summary, duration_ms, finished_at)
VALUES ($1, $2, $3, $4, $5, $6, nullif($7, ''), nullif(left($8, 200), ''), $9, now())`,
				lease.WaveID, attempt, max(step.Checkpoint.Number, 1), tool.Tool, permission, refusal, status, tool.Summary, step.DurationMs)
			if err != nil {
				return runtime.Totals{}, fmt.Errorf("record tool: %w", classify(err))
			}
		}
	}
	if tool := step.Tool; tool != nil && tool.Skill != "" {
		_, err := q.Exec(WithOperation(ctx, "wave_skills.complete"), `
UPDATE easycode.wave_skills ws SET load = 'complete' FROM easycode.skill_versions v JOIN easycode.skills s ON s.id = v.skill_id
 WHERE ws.wave_id = $1 AND v.id = ws.skill_version_id AND s.name = $2`, lease.WaveID, tool.Skill)
		if err != nil {
			return runtime.Totals{}, fmt.Errorf("skill loaded: %w", classify(err))
		}
	}
	totals, err := l.totals(ctx, lease.WaveID)
	if err != nil || !step.Checkpointed {
		return totals, err
	}
	_, err = q.Exec(WithOperation(ctx, "wave_checkpoints.insert"), `
INSERT INTO easycode.wave_checkpoints (wave_id, attempt, step, plan_version, after, lease_epoch, worker, agent_state, state_hash,
  input_tokens, output_tokens, read_cache_tokens, write_cache_tokens, useful_cost, lost_cost, wave_created_at)
SELECT id, $2, $3, plan_version, $4, lease_epoch, $5, $6, $7, input_tokens, output_tokens, read_cache_tokens, write_cache_tokens,
       useful_cost, lost_cost, created_at FROM easycode.waves WHERE id = $1
ON CONFLICT (wave_id, attempt, step, wave_created_at) DO UPDATE SET after = excluded.after, agent_state = excluded.agent_state,
  state_hash = excluded.state_hash, lease_epoch = excluded.lease_epoch, worker = excluded.worker, written_at = now(),
  input_tokens = excluded.input_tokens, output_tokens = excluded.output_tokens, read_cache_tokens = excluded.read_cache_tokens,
  write_cache_tokens = excluded.write_cache_tokens, useful_cost = excluded.useful_cost, lost_cost = excluded.lost_cost`,
		lease.WaveID, attempt, step.Checkpoint.Number, step.Checkpoint.After, lease.Owner, []byte(step.Checkpoint.State), step.StateHash)
	if err != nil {
		return totals, fmt.Errorf("write checkpoint: %w", classify(err))
	}
	return totals, nil
}

func (l Ledger) totals(ctx context.Context, waveID int64) (runtime.Totals, error) {
	var t runtime.Totals
	var useful, lost string
	err := conn(ctx, l.pool).QueryRow(WithOperation(ctx, "waves.totals"), `
SELECT input_tokens, output_tokens, read_cache_tokens, write_cache_tokens, useful_cost::text, lost_cost::text, currency
  FROM easycode.waves WHERE id = $1`, waveID).Scan(&t.Usage.Input, &t.Usage.Output, &t.Usage.CacheRead, &t.Usage.CacheWrite, &useful, &lost, &t.Currency)
	t.UsefulCost, t.LostCost = pricing.NormalizeDecimal(useful), pricing.NormalizeDecimal(lost)
	if err != nil {
		return t, fmt.Errorf("wave totals: %w", classify(err))
	}
	return t, nil
}

// SetInputsDigest implements runtime.Ledger (RNF-15).
func (l Ledger) SetInputsDigest(ctx context.Context, lease runtime.Lease, digest string) error {
	row := l.fenced(ctx, "waves.inputs_digest", `UPDATE easycode.waves SET inputs_digest = $4`+fence+` RETURNING id`, lease, digest)
	var id int64
	return leaseLost(row.Scan(&id))
}

// OpenQuestion implements runtime.Ledger: PERG-n with numbered items; the wave waits without lease.
func (l Ledger) OpenQuestion(ctx context.Context, lease runtime.Lease, items []runtime.QuestionItem) (string, error) {
	q := conn(ctx, l.pool)
	var projectID int64
	row := l.fenced(ctx, "waves.ask_user", `UPDATE easycode.waves SET state = 'awaiting_user', lease_owner = NULL, lease_until = NULL`+fence+` RETURNING project_id`, lease)
	if err := row.Scan(&projectID); err != nil {
		return "", leaseLost(err)
	}
	var number int64
	err := q.QueryRow(WithOperation(ctx, "questions.next_number"), `
INSERT INTO easycode.project_counters (project_id, name, value) VALUES ($1, 'question', 1)
ON CONFLICT (project_id, name) DO UPDATE SET value = project_counters.value + 1 RETURNING value`, projectID).Scan(&number)
	if err != nil {
		return "", fmt.Errorf("next question number: %w", classify(err))
	}
	code := fmt.Sprintf("PERG-%d", number)
	var questionID int64
	err = q.QueryRow(WithOperation(ctx, "wave_questions.insert"),
		"INSERT INTO easycode.wave_questions (project_id, wave_id, code) VALUES ($1, $2, $3) RETURNING id", projectID, lease.WaveID, code).Scan(&questionID)
	if err != nil {
		return "", fmt.Errorf("open question: %w", classify(err))
	}
	for i, item := range items {
		excerpt := item.Excerpt
		if len([]rune(excerpt)) > 1000 {
			excerpt = string([]rune(excerpt)[:1000])
		}
		_, err := q.Exec(WithOperation(ctx, "question_items.insert"), `
INSERT INTO easycode.question_items (question_id, number, text, suggestion, origin_source, origin_excerpt)
VALUES ($1, $2, $3, nullif($4, ''), nullif($5, ''), CASE WHEN $5 = '' THEN NULL ELSE nullif($6, '') END)`,
			questionID, i+1, item.Text, item.Suggestion, item.Source, excerpt)
		if err != nil {
			return "", fmt.Errorf("question item: %w", classify(err))
		}
	}
	return code, nil
}

// Complete implements runtime.Ledger.
func (l Ledger) Complete(ctx context.Context, lease runtime.Lease) (runtime.Totals, int64, int64, error) {
	var execution, cycle int64
	row := l.fenced(ctx, "waves.complete", `
UPDATE easycode.waves SET state = 'completed', finished_at = now(), lease_owner = NULL, lease_until = NULL`+fence+`
RETURNING execution_duration_ms, (extract(epoch FROM now() - coalesce(started_at, created_at)) * 1000)::bigint`, lease)
	if err := row.Scan(&execution, &cycle); err != nil {
		return runtime.Totals{}, 0, 0, leaseLost(err)
	}
	totals, err := l.totals(ctx, lease.WaveID)
	return totals, execution, cycle, err
}

// Fail implements runtime.Ledger.
func (l Ledger) Fail(ctx context.Context, lease runtime.Lease, cause string) (runtime.Totals, error) {
	row := l.fenced(ctx, "waves.fail", `
UPDATE easycode.waves SET state = 'failed', failure_cause = $4, lease_owner = NULL, lease_until = NULL`+fence+` RETURNING id`, lease, cause)
	var id int64
	if err := row.Scan(&id); err != nil {
		return runtime.Totals{}, leaseLost(err)
	}
	return l.totals(ctx, lease.WaveID)
}

// ScheduleRetry implements runtime.Ledger.
func (l Ledger) ScheduleRetry(ctx context.Context, lease runtime.Lease) (int, int, error) {
	var used, maximum int
	row := l.fenced(ctx, "waves.retry", `UPDATE easycode.waves SET retries_used = retries_used + 1`+fence+` RETURNING retries_used, max_retries`, lease)
	if err := row.Scan(&used, &maximum); err != nil {
		return 0, 0, leaseLost(err)
	}
	return used, maximum, nil
}

// RecordWaveCheck implements runtime.Ledger (guard of wave: complete; failed evidence = artifact_errors).
func (l Ledger) RecordWaveCheck(ctx context.Context, lease runtime.Lease, check string, passed bool, evidence json.RawMessage) error {
	result := "failed"
	if passed {
		result = "passed"
	}
	var raw any
	if len(evidence) > 0 {
		raw = string(evidence)
	}
	_, err := conn(ctx, l.pool).Exec(WithOperation(ctx, "check_executions.wave"), `
INSERT INTO easycode.check_executions (project_id, wave_id, check_id, type, severity, result, evidence)
SELECT project_id, id, $2, 'automatic', 'blocks', $3, $4::jsonb FROM easycode.waves WHERE id = $1`, lease.WaveID, check, result, raw)
	if err != nil {
		return fmt.Errorf("record wave check: %w", classify(err))
	}
	return nil
}

// Release implements runtime.Ledger: the lease ends now, so the next claim resumes the wave.
func (l Ledger) Release(ctx context.Context, lease runtime.Lease) error {
	_, err := conn(ctx, l.pool).Exec(WithOperation(ctx, "waves.release"),
		`UPDATE easycode.waves SET lease_until = now() - interval '1 second'`+fence, lease.WaveID, lease.Owner, lease.Epoch)
	return classify(err)
}

// SetStageState implements runtime.Ledger.
func (l Ledger) SetStageState(ctx context.Context, projectID, stage, state string) error {
	return Workflow{pool: l.pool}.SetStageState(ctx, projectID, stage, state)
}

// OpenCall implements runtime.CallLog in its own transaction (outside the step).
func (l Ledger) OpenCall(ctx context.Context, waveID string, attempt, step int, modelID, priceID int64, role, inputHash string, estimatedInput int) (int64, error) {
	var id int64
	err := l.pool.QueryRow(WithOperation(ctx, "model_calls.open"), `
INSERT INTO easycode.model_calls (wave_id, attempt, step, model_id, role, price_id, input_tokens, currency, input_hash)
SELECT w.id, $2, $3, $4, $5, $6, $7, w.currency, $8 FROM easycode.waves w WHERE w.public_id = $1::uuid RETURNING id`,
		waveID, attempt, step, modelID, role, priceID, estimatedInput, inputHash).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("open model call: %w", classify(err))
	}
	return id, nil
}

// CurrentPriceID implements runtime.PriceLookup.
func (l Ledger) CurrentPriceID(ctx context.Context, projectID string, modelID int64, at time.Time) (int64, error) {
	var id int64
	err := conn(ctx, l.pool).QueryRow(WithOperation(ctx, "model_prices.current_id"), `
SELECT mp.id FROM easycode.model_prices mp JOIN easycode.projects p ON p.organization_id = mp.organization_id
 WHERE p.public_id = $1::uuid AND mp.model_id = $2 AND mp.effective_since <= $3 ORDER BY mp.effective_since DESC LIMIT 1`,
		projectID, modelID, at).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, failure.New(failure.KindConflict, pricing.CodeModelWithoutCurrentPrice, nil)
	}
	if err != nil {
		return 0, fmt.Errorf("current price: %w", classify(err))
	}
	return id, nil
}
