package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/artifact"
	appgate "easycode/internal/application/gate"
	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Gates stores gate rounds, their checks and decisions.
type Gates struct {
	pool *pgxpool.Pool
}

var (
	_ appgate.Store         = Gates{}
	_ appgate.DecisionStore = Gates{}
)

// NewGates builds the adapter.
func NewGates(pool *pgxpool.Pool) Gates { return Gates{pool: pool} }

// ActiveWaves implements appgate.Store.
func (g Gates) ActiveWaves(ctx context.Context, projectID, stage string) (int, error) {
	var n int
	err := conn(ctx, g.pool).QueryRow(WithOperation(ctx, "gates.active_waves"), `
SELECT count(*) FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.public_id = $1::uuid AND w.stage = $2 AND w.state NOT IN ('completed','canceled')`, projectID, stage).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("active waves: %w", classify(err))
	}
	return n, nil
}

// OpenRound implements appgate.Store.
func (g Gates) OpenRound(ctx context.Context, projectID, stage string, outputs []artifact.Stored) (int, error) {
	q := conn(ctx, g.pool)
	var roundID int64
	var number int
	err := q.QueryRow(WithOperation(ctx, "gate_rounds.open"), `
INSERT INTO easycode.gate_rounds (project_id, stage, stage_id, number, approval_mode)
SELECT pr.id, s.stage, s.id, coalesce((SELECT max(r.number) FROM easycode.gate_rounds r WHERE r.stage_id = s.id), 0) + 1, 'human'
  FROM easycode.projects pr JOIN easycode.stages s ON s.project_id = pr.id AND s.stage = $2 WHERE pr.public_id = $1::uuid
RETURNING id, number`, projectID, stage).Scan(&roundID, &number)
	if err != nil {
		return 0, fmt.Errorf("open gate round: %w", classify(err))
	}
	for _, output := range outputs {
		_, err := q.Exec(WithOperation(ctx, "gate_round_artifacts.insert"), `
INSERT INTO easycode.gate_round_artifacts (round_id, artifact_version_id)
SELECT $1, v.id FROM easycode.artifact_versions v JOIN easycode.artifacts ar ON ar.id = v.artifact_id
  JOIN easycode.projects pr ON pr.id = ar.project_id WHERE pr.public_id = $2::uuid AND ar.code = $3 AND v.version = $4`,
			roundID, projectID, output.Code, output.Version)
		if err != nil {
			return 0, fmt.Errorf("round output: %w", classify(err))
		}
	}
	return number, nil
}

const roundByKey = `(SELECT r.id FROM easycode.gate_rounds r JOIN easycode.projects pr ON pr.id = r.project_id
 WHERE pr.public_id = $1::uuid AND r.stage = $2 AND r.number = $3)`

// RecordRoundCheck implements appgate.Store.
func (g Gates) RecordRoundCheck(ctx context.Context, projectID, stage string, round int, check string, passed bool, evidence json.RawMessage) error {
	result := "failed"
	if passed {
		result = "passed"
	}
	var raw any
	if len(evidence) > 0 {
		raw = string(evidence)
	}
	_, err := conn(ctx, g.pool).Exec(WithOperation(ctx, "check_executions.round"), `
INSERT INTO easycode.check_executions (project_id, round_id, evaluation, check_id, type, severity, result, evidence)
SELECT r.project_id, r.id, r.current_evaluation, $4, 'automatic', 'blocks', $5, $6::jsonb FROM easycode.gate_rounds r WHERE r.id = `+roundByKey,
		projectID, stage, round, check, result, raw)
	if err != nil {
		return fmt.Errorf("record round check: %w", classify(err))
	}
	return nil
}

// SetRoundState implements appgate.Store (orchestrator transitions).
func (g Gates) SetRoundState(ctx context.Context, projectID, stage string, round int, state string) error {
	_, err := conn(ctx, g.pool).Exec(WithOperation(ctx, "gate_rounds.set_state"), `
UPDATE easycode.gate_rounds SET state = $4,
       decided_by_type = CASE WHEN $4 IN ('approved','rejected') THEN 'orchestrator' ELSE decided_by_type END,
       decided_at = CASE WHEN $4 IN ('approved','rejected') THEN now() ELSE decided_at END
 WHERE id = `+roundByKey, projectID, stage, round, state)
	if err != nil {
		return fmt.Errorf("gate round to %s: %w", state, classify(err))
	}
	return nil
}

// Decided implements appgate.Store.
func (g Gates) Decided(ctx context.Context) ([]appgate.Decided, error) {
	rows, err := conn(ctx, g.pool).Query(WithOperation(ctx, "gate_rounds.decided"), `
SELECT pr.public_id::text, s.stage, r.number, r.state
  FROM easycode.stages s JOIN easycode.projects pr ON pr.id = s.project_id
  JOIN LATERAL (SELECT number, state FROM easycode.gate_rounds x WHERE x.stage_id = s.id ORDER BY x.number DESC LIMIT 1) r ON true
 WHERE s.state = 'awaiting_gate' AND r.state IN ('approved','rejected')`)
	if err != nil {
		return nil, fmt.Errorf("decided rounds: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appgate.Decided, error) {
		var d appgate.Decided
		return d, row.Scan(&d.ProjectID, &d.Stage, &d.Round, &d.State)
	})
}

// Rounds implements appgate.DecisionStore.
func (g Gates) Rounds(ctx context.Context, projectID, stage string) ([]appgate.RoundView, error) {
	rows, err := conn(ctx, g.pool).Query(WithOperation(ctx, "gate_rounds.list"), `
SELECT r.number FROM easycode.gate_rounds r JOIN easycode.projects pr ON pr.id = r.project_id
 WHERE pr.public_id = $1::uuid AND r.stage = $2 ORDER BY r.number DESC`, projectID, stage)
	if err != nil {
		return nil, fmt.Errorf("list rounds: %w", classify(err))
	}
	numbers, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, err
	}
	views := make([]appgate.RoundView, 0, len(numbers))
	for _, number := range numbers {
		view, err := g.Round(ctx, projectID, stage, number)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// Round implements appgate.DecisionStore.
func (g Gates) Round(ctx context.Context, projectID, stage string, round int) (appgate.RoundView, error) {
	q := conn(ctx, g.pool)
	view := appgate.RoundView{Gate: "gate." + stage, Stage: stage, Round: round}
	var roundID int64
	var byType, reason *string
	var decision appgate.DecisionView
	var sub, name *string
	var decidedAt *time.Time
	err := q.QueryRow(WithOperation(ctx, "gate_rounds.get"), `
SELECT r.id, r.state, r.approval_mode, r.opened_at, r.decided_by_type, r.decided_at, r.reason, u.sub, u.display_name
  FROM easycode.gate_rounds r JOIN easycode.projects pr ON pr.id = r.project_id
  LEFT JOIN LATERAL (SELECT gc.user_id FROM easycode.gate_confirmations gc WHERE gc.round_id = r.id ORDER BY gc.decided_at DESC LIMIT 1) c ON true
  LEFT JOIN easycode.users u ON u.id = c.user_id
 WHERE pr.public_id = $1::uuid AND r.stage = $2 AND r.number = $3`, projectID, stage, round).Scan(&roundID, &view.State, &view.ApprovalMode,
		&view.OpenedAt, &byType, &decidedAt, &reason, &sub, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return view, fmt.Errorf("read round: %w", classify(err))
	}
	rows, err := q.Query(WithOperation(ctx, "gate_round_artifacts.list"), `
SELECT ar.code, ar.type, v.version, ar.state FROM easycode.gate_round_artifacts ga JOIN easycode.artifact_versions v ON v.id = ga.artifact_version_id
  JOIN easycode.artifacts ar ON ar.id = v.artifact_id WHERE ga.round_id = $1 ORDER BY ar.type, ar.code`, roundID)
	if err != nil {
		return view, fmt.Errorf("round outputs: %w", classify(err))
	}
	view.Outputs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (appgate.OutputRef, error) {
		var o appgate.OutputRef
		return o, row.Scan(&o.Code, &o.Type, &o.Version, &o.State)
	})
	if err != nil {
		return view, err
	}
	rows, err = q.Query(WithOperation(ctx, "check_executions.round_list"), `
SELECT c.check_id, c.type, c.severity, c.result, c.evidence FROM easycode.check_executions c JOIN easycode.gate_rounds r ON r.id = c.round_id
 WHERE c.round_id = $1 AND c.evaluation = r.current_evaluation ORDER BY c.check_id`, roundID)
	if err != nil {
		return view, fmt.Errorf("round checks: %w", classify(err))
	}
	view.Checks, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (appgate.CheckView, error) {
		var c appgate.CheckView
		return c, row.Scan(&c.Check, &c.Type, &c.Severity, &c.Status, &c.Evidence)
	})
	if err != nil {
		return view, err
	}
	if byType != nil && decidedAt != nil {
		decision.At = *decidedAt
		decision.Result, decision.ByType = view.State, *byType
		if reason != nil {
			decision.Reason = *reason
		}
		if sub != nil && *byType == "human" {
			decision.By = appproject.UserRef{Subject: *sub, Name: *name}
		}
		for _, check := range view.Checks {
			if check.Status == "failed" {
				decision.FailedChecks = append(decision.FailedChecks, check.Check)
			}
		}
		view.Decision = &decision
	}
	return view, nil
}

// Decide implements appgate.DecisionStore (api role, synchronous decision; triggers check the
// approver role, the blocking checks and the confirmations).
func (g Gates) Decide(ctx context.Context, p identity.Principal, projectID, stage string, round int, approve bool, reason string) error {
	q := conn(ctx, g.pool)
	var roundID int64
	err := q.QueryRow(WithOperation(ctx, "gate_rounds.lock"), roundByKey+` FOR UPDATE`, projectID, stage, round).Scan(&roundID)
	if errors.Is(err, pgx.ErrNoRows) {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return fmt.Errorf("lock round: %w", classify(err))
	}
	decision, state := "approve", "approved"
	if !approve {
		decision, state = "reject", "rejected"
	}
	_, err = q.Exec(WithOperation(ctx, "gate_confirmations.insert"), `
INSERT INTO easycode.gate_confirmations (round_id, evaluation, user_id, decision, reason, origin)
SELECT $3, r.current_evaluation, u.id, $4, nullif($5, ''), 'web' FROM easycode.gate_rounds r,
  (SELECT uu.id FROM easycode.users uu JOIN easycode.organizations oo ON oo.id = uu.organization_id WHERE oo.public_id = $1::uuid AND uu.sub = $2) u
 WHERE r.id = $3`, p.OrganizationID, p.Subject, roundID, decision, reason)
	if err != nil {
		return fmt.Errorf("record decision: %w", classify(err))
	}
	_, err = q.Exec(WithOperation(ctx, "gate_rounds.decide"), `
UPDATE easycode.gate_rounds SET state = $2, decided_by_type = 'human', decided_at = now(), reason = nullif($3, '') WHERE id = $1`,
		roundID, state, reason)
	if err != nil {
		return fmt.Errorf("decide round: %w", classify(err))
	}
	return nil
}
