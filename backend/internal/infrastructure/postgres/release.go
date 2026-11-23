package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	appgate "easycode/internal/application/gate"
	"easycode/internal/domain/pricing"
)

var _ appgate.ReleaseStore = Gates{}

// StageApprovals implements appgate.ReleaseStore: the last round of each stage when approved, with the
// approver (display name), or the template value of the automatic mode when the orchestrator approved it.
func (g Gates) StageApprovals(ctx context.Context, projectID string) (map[string]appgate.Approval, error) {
	rows, err := conn(ctx, g.pool).Query(WithOperation(ctx, "gate_rounds.approvals"), `
SELECT r.stage, r.number, coalesce(r.decided_at, r.opened_at),
       CASE WHEN r.decided_by_type = 'human' THEN coalesce((SELECT coalesce(u.display_name, u.sub) FROM easycode.gate_confirmations c
                    JOIN easycode.users u ON u.id = c.user_id
                   WHERE c.round_id = r.id AND c.decision = 'approve' AND c.discarded_at IS NULL ORDER BY c.id DESC LIMIT 1), 'humano')
            ELSE 'automatico' END
  FROM easycode.gate_rounds r JOIN easycode.projects pr ON pr.id = r.project_id
 WHERE pr.public_id = $1::uuid AND r.state = 'approved'
   AND r.number = (SELECT max(x.number) FROM easycode.gate_rounds x WHERE x.stage_id = r.stage_id)`, projectID)
	if err != nil {
		return nil, fmt.Errorf("stage approvals: %w", classify(err))
	}
	defer rows.Close()
	out := map[string]appgate.Approval{}
	for rows.Next() {
		var stage string
		var a appgate.Approval
		if err := rows.Scan(&stage, &a.Round, &a.At, &a.By); err != nil {
			return nil, err
		}
		out[stage] = a
	}
	return out, rows.Err()
}

// StageCosts implements appgate.ReleaseStore: waves started in the stage and its activities, rework
// waves, tokens, cost (useful and lost) and times in minutes (RELEASE.custos, release.costs_consistent).
func (g Gates) StageCosts(ctx context.Context, projectID string, stages map[string][]string) ([]appgate.StageCost, error) {
	out := make([]appgate.StageCost, 0, len(stages))
	for _, stage := range []string{"requirements", "architecture", "data", "design", "validation", "implementation", "qa", "security"} {
		ids, ok := stages[stage]
		if !ok {
			continue
		}
		c := appgate.StageCost{Stage: stage}
		err := conn(ctx, g.pool).QueryRow(WithOperation(ctx, "waves.stage_costs"), `
SELECT count(*) FILTER (WHERE w.started_at IS NOT NULL)::int, count(*) FILTER (WHERE w.started_at IS NOT NULL AND w.purpose = 'rework')::int,
       coalesce(sum(w.input_tokens + w.output_tokens + w.read_cache_tokens + w.write_cache_tokens), 0)::bigint,
       coalesce(sum(w.useful_cost + w.lost_cost), 0)::text,
       coalesce(sum(w.execution_duration_ms), 0)::float8 / 60000,
       coalesce(sum(extract(epoch FROM coalesce(w.finished_at, now()) - w.created_at)) FILTER (WHERE w.started_at IS NOT NULL), 0)::float8 / 60
  FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.public_id = $1::uuid AND w.stage = ANY($2::text[])`, projectID, ids).
			Scan(&c.Waves, &c.Rework, &c.Tokens, &c.Cost, &c.ExecutionMin, &c.CycleMin)
		if err != nil {
			return nil, fmt.Errorf("stage costs: %w", classify(err))
		}
		c.Cost = pricing.NormalizeDecimal(c.Cost)
		out = append(out, c)
	}
	return out, nil
}

// ProjectActiveWaves implements appgate.ReleaseStore (release.no_active_wave).
func (g Gates) ProjectActiveWaves(ctx context.Context, projectID string) ([]string, error) {
	rows, err := conn(ctx, g.pool).Query(WithOperation(ctx, "waves.active_codes"), `
SELECT w.code FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.public_id = $1::uuid AND w.state IN ('queued','running','awaiting_user','pausing','paused') ORDER BY w.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("active waves: %w", classify(err))
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Delivery implements appgate.ReleaseStore: the project name, its repository address and accumulated cost.
func (g Gates) Delivery(ctx context.Context, projectID string) (appgate.Delivery, error) {
	var d appgate.Delivery
	err := conn(ctx, g.pool).QueryRow(WithOperation(ctx, "projects.delivery"), `
SELECT pr.name, coalesce(pr.repository, ''), coalesce(sum(w.useful_cost + w.lost_cost), 0)::text, coalesce(sum(w.lost_cost), 0)::text, o.currency
  FROM easycode.projects pr JOIN easycode.organizations o ON o.id = pr.organization_id LEFT JOIN easycode.waves w ON w.project_id = pr.id
 WHERE pr.public_id = $1::uuid GROUP BY pr.id, o.currency`, projectID).Scan(&d.Name, &d.Repository, &d.Cost, &d.Lost, &d.Currency)
	if err != nil {
		return d, fmt.Errorf("project delivery: %w", classify(err))
	}
	d.Cost, d.Lost = pricing.NormalizeDecimal(d.Cost), pricing.NormalizeDecimal(d.Lost)
	return d, nil
}

// ReleaseRounds returns the state of the last round of each stage (getReleaseSummary).
func (g Gates) ReleaseRounds(ctx context.Context, projectID string) (map[string]int, error) {
	rows, err := conn(ctx, g.pool).Query(WithOperation(ctx, "gate_rounds.last_of_stages"), `
SELECT s.stage, coalesce((SELECT max(x.number) FROM easycode.gate_rounds x WHERE x.stage_id = s.id), 0)
  FROM easycode.stages s JOIN easycode.projects pr ON pr.id = s.project_id WHERE pr.public_id = $1::uuid AND s.type = 'stage'`, projectID)
	if err != nil {
		return nil, fmt.Errorf("last rounds: %w", classify(err))
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var stage string
		var n int
		if err := rows.Scan(&stage, &n); err != nil {
			return nil, err
		}
		out[stage] = n
	}
	return out, rows.Err()
}

// ReleaseArtifact implements appgate.ReleaseStore: the RELEASE of the project and the tag of its current header.
func (g Gates) ReleaseArtifact(ctx context.Context, projectID string) (string, string, error) {
	var code, tag string
	err := conn(ctx, g.pool).QueryRow(WithOperation(ctx, "artifacts.release"), `
SELECT a.code, coalesce(v.header->'entrega'->>'tag', '')
  FROM easycode.artifacts a JOIN easycode.projects pr ON pr.id = a.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version
 WHERE pr.public_id = $1::uuid AND a.type = 'RELEASE' ORDER BY a.code LIMIT 1`, projectID).Scan(&code, &tag)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("release artifact: %w", classify(err))
	}
	return code, tag, nil
}
