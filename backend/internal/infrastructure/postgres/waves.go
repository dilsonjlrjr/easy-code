package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/idempotency"
	appproject "easycode/internal/application/project"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
	domainworkflow "easycode/internal/domain/workflow"
)

// Waves reads waves for the api and applies the answer command in the worker.
type Waves struct {
	pool    *pgxpool.Pool
	inForce domainworkflow.Definition
}

var (
	_ appwave.Reader   = Waves{}
	_ appwave.Answers  = Waves{}
	_ appwave.Resumer  = Waves{}
	_ appwave.Canceler = Waves{}
)

// NewWaves builds the adapter.
func NewWaves(pool *pgxpool.Pool, inForce domainworkflow.Definition) Waves {
	return Waves{pool: pool, inForce: inForce}
}

const waveSummary = `w.code, w.stage, w.agent, w.state, w.purpose, w.attempt, coalesce(ac.version, 0), w.created_at, w.started_at,
  CASE WHEN w.state IN ('completed','canceled') THEN w.finished_at
       WHEN w.state = 'failed' THEN (SELECT max(e.occurred_at) FROM easycode.events e WHERE e.project_id = w.project_id AND e.waveid = w.code AND e.type = 'br.easycode.wave.failed.v1')
  END,
  w.useful_cost::text, w.lost_cost::text, w.currency, w.estimated_lost_cost, coalesce(w.task, ''),
  w.input_tokens, w.output_tokens, w.read_cache_tokens, w.write_cache_tokens`

func scanSummary(row pgx.Row, s *appwave.Summary, extra ...any) error {
	args := append([]any{&s.Code, &s.Stage, &s.Agent, &s.State, &s.Purpose, &s.Attempt, &s.ConfigVersion, &s.CreatedAt, &s.StartedAt, &s.EndedAt,
		&s.Cost.Useful, &s.Cost.Lost, &s.Cost.Currency, &s.Cost.EstimatedLost, &s.Task,
		&s.Tokens.Input, &s.Tokens.Output, &s.Tokens.CacheRead, &s.Tokens.CacheWrite}, extra...)
	if err := row.Scan(args...); err != nil {
		return err
	}
	s.Cost.Useful, s.Cost.Lost = pricing.NormalizeDecimal(s.Cost.Useful), pricing.NormalizeDecimal(s.Cost.Lost)
	return nil
}

// List implements appwave.Reader.
func (w Waves) List(ctx context.Context, projectID, stage, agent string, states []string, limit int) ([]appwave.Summary, error) {
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "waves.list"), `
SELECT `+waveSummary+` FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
  LEFT JOIN easycode.agent_configurations ac ON ac.id = w.agent_configuration_id
 WHERE pr.public_id = $1::uuid AND ($2 = '' OR w.stage = $2) AND ($3 = '' OR w.agent = $3)
   AND (coalesce(cardinality($4::text[]), 0) = 0 OR w.state = ANY($4::text[]))
 ORDER BY w.created_at DESC LIMIT $5`, projectID, stage, agent, states, limit)
	if err != nil {
		return nil, fmt.Errorf("list waves: %w", classify(err))
	}
	defer rows.Close()
	var list []appwave.Summary
	for rows.Next() {
		var s appwave.Summary
		if err := scanSummary(rows, &s); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// Get implements appwave.Reader.
func (w Waves) Get(ctx context.Context, projectID, code string) (appwave.View, error) {
	q := conn(ctx, w.pool)
	var v appwave.View
	var waveID int64
	var digest, cause, question, instruction *string
	var step int
	var buildOrTestFailed bool
	err := scanSummary(q.QueryRow(WithOperation(ctx, "waves.get"), `
SELECT `+waveSummary+`, w.id, w.inputs_digest, w.failure_cause,
       w.execution_duration_ms, (extract(epoch FROM coalesce(w.finished_at, now()) - coalesce(w.started_at, w.created_at)) * 1000)::bigint,
       w.current_step, ac.instruction_version,
       (SELECT q.code FROM easycode.wave_questions q WHERE q.wave_id = w.id AND q.answered_at IS NULL ORDER BY q.opened_at DESC LIMIT 1),
       coalesce(w.git_commit, ''),
       EXISTS (SELECT 1 FROM easycode.tool_executions t WHERE t.wave_id = w.id AND t.category IN ('build','tests') AND t.status <> 'success')
  FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
  LEFT JOIN easycode.agent_configurations ac ON ac.id = w.agent_configuration_id
 WHERE pr.public_id = $1::uuid AND w.code = $2`, projectID, code), &v.Summary, &waveID, &digest, &cause,
		&v.ExecutionMs, &v.CycleMs, &step, &instruction, &question,
		&v.GitCommit, &buildOrTestFailed)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return v, fmt.Errorf("read wave: %w", classify(err))
	}
	v.InputsDigest, v.InstructionVersion, v.QuestionOpen = deref(digest), deref(instruction), deref(question)
	if cause != nil {
		v.FailureCause = *cause
		if *cause == "unrecoverable_error" && buildOrTestFailed {
			v.FailureCause = "build_or_test_failed" // US-19 CA-2: the failure keeps the command output
		}
	}
	if step > 0 || v.State != "queued" {
		v.LastCheckpoint = &step
	}
	// A wave failed by the correction limit shows the schema errors (US-29 CA-2).
	var evidence []byte
	err = q.QueryRow(WithOperation(ctx, "waves.schema_failure"), `
SELECT evidence FROM easycode.check_executions WHERE wave_id = $1 AND check_id = 'artifact.schema_valid' AND result = 'failed'
 ORDER BY executed_at DESC LIMIT 1`, waveID).Scan(&evidence)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return v, fmt.Errorf("wave schema check: %w", classify(err))
	default:
		v.ArtifactErrors = evidence
		if v.State == "failed" {
			v.FailureCause = "invalid_artifact"
		}
	}
	rows, err := q.Query(WithOperation(ctx, "waves.get_models"), `
SELECT wm.role, m.provider, m.name FROM easycode.wave_models wm JOIN easycode.models m ON m.id = wm.model_id WHERE wm.wave_id = $1`, waveID)
	if err != nil {
		return v, fmt.Errorf("wave models: %w", classify(err))
	}
	for rows.Next() {
		var role string
		var ref appproject.ModelRef
		if err := rows.Scan(&role, &ref.Provider, &ref.Model); err != nil {
			rows.Close()
			return v, err
		}
		if role == "reasoning" {
			v.Reasoning = ref
		} else {
			v.Execution = ref
		}
	}
	rows.Close()
	_ = q.QueryRow(WithOperation(ctx, "waves.price_table"),
		"SELECT coalesce(max(price_id)::text, '') FROM easycode.model_calls WHERE wave_id = $1", waveID).Scan(&v.PriceTable)
	if v.Skills, err = waveSkillRefs(ctx, q, waveID); err != nil {
		return v, err
	}
	// US-24 CA-3: the cost of the calls per provider (useful and lost).
	costs, err := q.Query(WithOperation(ctx, "model_calls.cost_per_provider"), `
SELECT m.provider, sum(c.cost)::text FROM easycode.model_calls c JOIN easycode.models m ON m.id = c.model_id
 WHERE c.wave_id = $1 AND c.result IS NOT NULL GROUP BY m.provider ORDER BY m.provider`, waveID)
	if err != nil {
		return v, fmt.Errorf("cost per provider: %w", classify(err))
	}
	v.PerProvider, err = pgx.CollectRows(costs, func(row pgx.CollectableRow) (appwave.ProviderCost, error) {
		var c appwave.ProviderCost
		err := row.Scan(&c.Provider, &c.Total)
		c.Total = pricing.NormalizeDecimal(c.Total)
		return c, err
	})
	if err != nil {
		return v, err
	}
	if v.Outputs, err = w.outputs(ctx, waveID, v); err != nil {
		return v, err
	}
	return v, w.control(ctx, waveID, &v)
}

func (w Waves) outputs(ctx context.Context, waveID int64, v appwave.View) ([]appwave.ExpectedOutput, error) {
	stage, ok := w.inForce.Stage(v.Stage)
	if !ok {
		return nil, nil
	}
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "waves.outputs"), `
SELECT ar.type, ar.code, v.version FROM easycode.artifact_versions v JOIN easycode.artifacts ar ON ar.id = v.artifact_id
 WHERE v.wave_id = $1 ORDER BY ar.type, ar.code`, waveID)
	if err != nil {
		return nil, fmt.Errorf("wave outputs: %w", classify(err))
	}
	defer rows.Close()
	written := map[string][]appwave.ExpectedOutput{}
	for rows.Next() {
		var o appwave.ExpectedOutput
		if err := rows.Scan(&o.Type, &o.Artifact, &o.Version); err != nil {
			return nil, err
		}
		o.Status = "written"
		written[o.Type] = append(written[o.Type], o)
	}
	var out []appwave.ExpectedOutput
	seen := map[string]bool{}
	for _, output := range stage.Outputs {
		if output.Type == "" || seen[output.Type] {
			continue
		}
		seen[output.Type] = true
		if list, ok := written[output.Type]; ok {
			out = append(out, list...)
			continue
		}
		out = append(out, appwave.ExpectedOutput{Type: output.Type, Status: "pending"})
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Questions implements appwave.Reader.
func (w Waves) Questions(ctx context.Context, projectID, code string) ([]appwave.QuestionView, error) {
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "wave_questions.list"), `
SELECT q.code, q.opened_at, q.answered_at, u.sub, u.display_name, i.number, i.text, coalesce(i.suggestion, ''),
       coalesce(i.origin_source, ''), coalesce(i.origin_excerpt, ''), coalesce(i.answer, '')
  FROM easycode.wave_questions q JOIN easycode.waves w ON w.id = q.wave_id JOIN easycode.projects pr ON pr.id = w.project_id
  JOIN easycode.question_items i ON i.question_id = q.id LEFT JOIN easycode.users u ON u.id = q.answered_by
 WHERE pr.public_id = $1::uuid AND w.code = $2 ORDER BY q.opened_at, i.number`, projectID, code)
	if err != nil {
		return nil, fmt.Errorf("list questions: %w", classify(err))
	}
	defer rows.Close()
	var list []appwave.QuestionView
	for rows.Next() {
		var codeQ string
		var opened time.Time
		var answered *time.Time
		var sub, name *string
		var item appwave.QuestionItem
		if err := rows.Scan(&codeQ, &opened, &answered, &sub, &name, &item.Number, &item.Text, &item.Suggestion, &item.Source, &item.Excerpt, &item.Answer); err != nil {
			return nil, err
		}
		if len(list) == 0 || list[len(list)-1].Code != codeQ {
			view := appwave.QuestionView{Code: codeQ, OpenedAt: opened, AnsweredAt: answered}
			if sub != nil {
				view.AnsweredBy = &appproject.UserRef{Subject: *sub, Name: deref(name)}
			}
			list = append(list, view)
		}
		list[len(list)-1].Items = append(list[len(list)-1].Items, item)
	}
	if len(list) == 0 {
		if _, err := w.Get(ctx, projectID, code); err != nil {
			return nil, err
		}
	}
	return list, rows.Err()
}

// Checkpoints implements appwave.Reader.
func (w Waves) Checkpoints(ctx context.Context, projectID, code string) ([]appwave.CheckpointView, error) {
	if _, err := w.Get(ctx, projectID, code); err != nil {
		return nil, err
	}
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "wave_checkpoints.list"), `
SELECT c.step, c.attempt, c.after, c.written_at, c.input_tokens, c.output_tokens, c.read_cache_tokens, c.write_cache_tokens,
       c.useful_cost::text, c.lost_cost::text, w.currency
  FROM easycode.wave_checkpoints c JOIN easycode.waves w ON w.id = c.wave_id JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.public_id = $1::uuid AND w.code = $2 ORDER BY c.attempt, c.step`, projectID, code)
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appwave.CheckpointView, error) {
		var c appwave.CheckpointView
		err := row.Scan(&c.Number, &c.Attempt, &c.After, &c.WrittenAt, &c.Tokens.Input, &c.Tokens.Output, &c.Tokens.CacheRead, &c.Tokens.CacheWrite,
			&c.Cost.Useful, &c.Cost.Lost, &c.Cost.Currency)
		c.Cost.Useful, c.Cost.Lost = pricing.NormalizeDecimal(c.Cost.Useful), pricing.NormalizeDecimal(c.Cost.Lost)
		return c, err
	})
}

// Calls implements appwave.Reader: model calls and tool executions in time order.
func (w Waves) Calls(ctx context.Context, projectID, code, kind string) ([]appwave.CallView, error) {
	if _, err := w.Get(ctx, projectID, code); err != nil {
		return nil, err
	}
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "waves.calls"), `
SELECT * FROM (
  SELECT 'model' AS kind, c.started_at, coalesce((extract(epoch FROM c.finished_at - c.started_at) * 1000)::bigint, 0), coalesce(c.result, 'aborted'),
         c.lost, c.estimated_cost, c.attempt, m.provider, m.name, c.role, c.input_tokens, c.output_tokens, c.read_cache_tokens, c.write_cache_tokens,
         c.cost::text, c.currency, c.price_id::text, '' AS tool, coalesce(c.summary, ''), '' AS refusal, coalesce(c.error_summary, '')
    FROM easycode.model_calls c JOIN easycode.waves w ON w.id = c.wave_id JOIN easycode.projects pr ON pr.id = w.project_id
    JOIN easycode.models m ON m.id = c.model_id
   WHERE pr.public_id = $1::uuid AND w.code = $2
  UNION ALL
  SELECT 'tool', t.started_at, coalesce(t.duration_ms, 0), CASE WHEN t.permission = 'refused' THEN 'refused' WHEN t.status = 'success' THEN 'success' ELSE 'error' END,
         false, false, t.attempt, '', '', '', 0, 0, 0, 0, '0', '', '', t.tool, coalesce(t.summary, ''), coalesce(t.refusal_reason, ''), ''
    FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id JOIN easycode.projects pr ON pr.id = w.project_id
   WHERE pr.public_id = $1::uuid AND w.code = $2) x
 WHERE $3 = '' OR x.kind = $3
 ORDER BY 2`, projectID, code, kind)
	if err != nil {
		return nil, fmt.Errorf("list calls: %w", classify(err))
	}
	calls, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appwave.CallView, error) {
		var c appwave.CallView
		err := row.Scan(&c.Type, &c.Start, &c.DurationMs, &c.Result, &c.Lost, &c.Estimated, &c.Attempt, &c.Provider, &c.Model, &c.Role,
			&c.Tokens.Input, &c.Tokens.Output, &c.Tokens.CacheRead, &c.Tokens.CacheWrite, &c.Cost, &c.Currency, &c.PriceVersion, &c.Tool, &c.Summary, &c.Refusal, &c.Error)
		c.Cost = pricing.NormalizeDecimal(c.Cost)
		return c, err
	})
	for i := range calls {
		calls[i].Number = i + 1
	}
	return calls, err
}

// Record implements appwave.Answers (api role).
func (w Waves) Record(ctx context.Context, p identity.Principal, projectID, waveCode, questionCode string, answers []string) error {
	q := conn(ctx, w.pool)
	var questionID int64
	err := q.QueryRow(WithOperation(ctx, "wave_questions.answer"), `
UPDATE easycode.wave_questions wq SET answered_by = `+principalUserIDSQL+`, answered_at = now(), origin = 'web'
  FROM easycode.waves w, easycode.projects pr
 WHERE w.id = wq.wave_id AND pr.id = w.project_id AND pr.public_id = $3::uuid AND w.code = $4 AND wq.code = $5 AND wq.answered_at IS NULL
RETURNING wq.id`, p.OrganizationID, p.Subject, projectID, waveCode, questionCode).Scan(&questionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	if err != nil {
		return fmt.Errorf("answer question: %w", classify(err))
	}
	for i, answer := range answers {
		if _, err := q.Exec(WithOperation(ctx, "question_items.answer"),
			"UPDATE easycode.question_items SET answer = $3 WHERE question_id = $1 AND number = $2", questionID, i+1, answer); err != nil {
			return fmt.Errorf("answer item: %w", classify(err))
		}
	}
	return nil
}

// EnqueueAnswer implements appwave.Answers.
func (w Waves) EnqueueAnswer(ctx context.Context, p identity.Principal, projectID, waveCode, questionCode, key, bodyHash string) (appworkflow.CommandView, error) {
	payload, _ := json.Marshal(map[string]string{"wave": waveCode, "question": questionCode})
	return w.enqueueWaveCommand(ctx, p, projectID, waveCode, "answer", payload, key, bodyHash)
}

// EnqueueCancel implements appwave.Answers (US-14).
func (w Waves) EnqueueCancel(ctx context.Context, p identity.Principal, projectID, waveCode, key, bodyHash string) (appworkflow.CommandView, error) {
	payload, _ := json.Marshal(map[string]string{"wave": waveCode})
	return w.enqueueWaveCommand(ctx, p, projectID, waveCode, "cancel", payload, key, bodyHash)
}

func (w Waves) enqueueWaveCommand(ctx context.Context, p identity.Principal, projectID, waveCode, commandType string, payload []byte, key, bodyHash string) (appworkflow.CommandView, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	if bodyHash == "" {
		bodyHash = idempotency.HashBody(payload)
	}
	q := conn(ctx, w.pool)
	tag, err := q.Exec(WithOperation(ctx, "commands.insert_"+commandType), `
INSERT INTO easycode.commands (public_id, organization_id, project_id, target_type, wave_id, type, payload, requested_by, origin, idempotency_key, body_hash, confirmed)
SELECT $4::uuid, pr.organization_id, pr.id, 'wave', wv.id, $9, $6::jsonb, `+principalUserIDSQL+`, 'web', $7::uuid, $8, $10
  FROM easycode.projects pr JOIN easycode.waves wv ON wv.project_id = pr.id AND wv.code = $5 WHERE pr.public_id = $3::uuid`,
		p.OrganizationID, p.Subject, projectID, id.String(), waveCode, string(payload), key, bodyHash, commandType,
		commandType == "cancel" || commandType == "pause_now" || commandType == "restart")
	if err != nil {
		return appworkflow.CommandView{}, fmt.Errorf("queue %s: %w", commandType, classify(err))
	}
	if tag.RowsAffected() == 0 {
		return appworkflow.CommandView{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if _, err := q.Exec(WithOperation(ctx, "commands.notify"), "SELECT pg_notify($1, $2)", ChannelCommands, projectID); err != nil {
		return appworkflow.CommandView{}, err
	}
	return Workflow{pool: w.pool}.Get(ctx, projectID, id.String())
}

// Cancel implements appwave.Canceler (worker role, US-14): under the wave lock it rechecks the guard,
// closes the call in flight as aborted and lost with its estimated cost (RNF-16), and moves the wave to
// canceled without lease, so the fenced writes of the worker that runs it fail and it stops (RNF-10).
func (w Waves) Cancel(ctx context.Context, projectID, waveCode string) (appwave.Canceled, error) {
	var out appwave.Canceled
	var id int64
	var task *string
	q := conn(ctx, w.pool)
	err := q.QueryRow(WithOperation(ctx, "waves.lock_for_cancel"), `
SELECT w.id, w.code, w.stage, w.agent, w.purpose, w.state, w.task, w.currency FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.public_id = $1::uuid AND w.code = $2 FOR UPDATE OF w`, projectID, waveCode).
		Scan(&id, &out.Code, &out.Stage, &out.Agent, &out.Purpose, &out.From, &task, &out.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return out, fmt.Errorf("lock wave: %w", classify(err))
	}
	if !appwave.CanCancel(out.From) {
		return out, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	out.Task = deref(task)
	var lost string
	var aborted int
	err = q.QueryRow(WithOperation(ctx, "model_calls.abort_for_cancel"), `
WITH aborted AS (
  UPDATE easycode.model_calls SET result = 'aborted', lost = true, estimated_cost = true, finished_at = now(), error_summary = 'wave canceled'
   WHERE wave_id = $1 AND result IS NULL RETURNING cost)
SELECT coalesce(sum(cost), 0)::text, count(*) FROM aborted`, id).Scan(&lost, &aborted)
	if err != nil {
		return out, fmt.Errorf("abort calls of canceled wave: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "waves.cancel"), `
UPDATE easycode.waves SET state = 'canceled', finished_at = now(), lease_owner = NULL, lease_until = NULL, failure_cause = NULL,
       cost_estimate = coalesce(cost_estimate, 0), estimate_basis = coalesce(estimate_basis, '{"method":"none"}'),
       pause_cause = NULL, pause_mode = NULL, lost_cost = lost_cost + $2::numeric, estimated_lost_cost = estimated_lost_cost OR $3
 WHERE id = $1`, id, lost, aborted > 0); err != nil {
		return out, fmt.Errorf("cancel wave: %w", classify(err))
	}
	if aborted > 0 {
		out.LostCost = pricing.NormalizeDecimal(lost)
	}
	return out, nil
}

// Resume implements appwave.Resumer (worker role).
func (w Waves) Resume(ctx context.Context, projectID, waveCode string) (appwave.Ref, error) {
	var ref appwave.Ref
	var state string
	q := conn(ctx, w.pool)
	err := q.QueryRow(WithOperation(ctx, "waves.lock_for_answer"), `
SELECT w.code, w.stage, w.agent, w.purpose, w.state FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.public_id = $1::uuid AND w.code = $2 FOR UPDATE OF w`, projectID, waveCode).Scan(&ref.Code, &ref.Stage, &ref.Agent, &ref.Purpose, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ref, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return ref, fmt.Errorf("lock wave: %w", classify(err))
	}
	if state != "awaiting_user" {
		return ref, failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	if _, err := q.Exec(WithOperation(ctx, "waves.answered"), `
UPDATE easycode.waves w SET state = 'running' FROM easycode.projects pr WHERE pr.id = w.project_id AND pr.public_id = $1::uuid AND w.code = $2`,
		projectID, waveCode); err != nil {
		return ref, fmt.Errorf("resume wave: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "waves.notify"), "SELECT pg_notify($1, $2)", ChannelWaves, projectID); err != nil {
		return ref, err
	}
	return ref, nil
}

// SandboxExecutions implements appwave.Reader (US-41): the commands of the wave in order, after the
// cursor (execution id).
func (w Waves) SandboxExecutions(ctx context.Context, projectID, code string, limit int, after string) ([]appwave.SandboxExecution, error) {
	if _, err := w.Get(ctx, projectID, code); err != nil {
		return nil, err
	}
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "tool_executions.sandbox_list"), `
SELECT t.public_id::text, coalesce(t.command, ''), t.category, coalesce(t.profile, ''), t.started_at, coalesce(t.duration_ms, 0), t.exit_code,
       coalesce(t.end_reason, 'executor_error'), t.tests_total, t.tests_passed, t.tests_failed, t.tests_skipped, coalesce(t.output_start, ''),
       coalesce(t.output_bytes, 0), coalesce(t.output_sha256, ''), coalesce(t.image_digest, '')
  FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.public_id = $1::uuid AND w.code = $2 AND t.category IS NOT NULL
   AND ($3 = '' OR (t.started_at, t.id) > (SELECT c.started_at, c.id FROM easycode.tool_executions c WHERE c.public_id::text = $3 AND c.wave_id = w.id))
 ORDER BY t.started_at, t.id LIMIT $4`, projectID, code, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list sandbox executions: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appwave.SandboxExecution, error) {
		var e appwave.SandboxExecution
		err := row.Scan(&e.ID, &e.Command, &e.Category, &e.Profile, &e.Start, &e.DurationMs, &e.ExitCode, &e.EndReason, &e.TestsTotal,
			&e.TestsPassed, &e.TestsFailed, &e.TestsSkipped, &e.OutputStart, &e.OutputBytes, &e.OutputSHA256, &e.ImageDigest)
		return e, err
	})
}

// ExecutionOutput implements appwave.Reader: the stored output (gzip) of one execution of the wave.
func (w Waves) ExecutionOutput(ctx context.Context, projectID, code, executionID string) (appwave.ExecutionOutput, error) {
	var out appwave.ExecutionOutput
	var number int
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "tool_executions.output"), `
SELECT t.output, (SELECT count(*) FROM easycode.tool_executions o WHERE o.wave_id = t.wave_id AND o.category IS NOT NULL
                   AND (o.started_at, o.id) <= (t.started_at, t.id))
  FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.public_id = $1::uuid AND w.code = $2 AND t.public_id::text = $3 AND t.category IS NOT NULL`, projectID, code, executionID).
		Scan(&out.Compressed, &number)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return out, fmt.Errorf("execution output: %w", classify(err))
	}
	out.FileName = fmt.Sprintf("%s-exec-%d.txt", code, number)
	return out, nil
}
