package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/pending"
	"easycode/internal/domain/identity"
	domainworkflow "easycode/internal/domain/workflow"
)

// Pending implements pending.Store (US-48): gates awaiting a human for approvers and leads, open
// agent questions for every role, in the projects of the person (project_members_user_idx,
// gate_rounds_awaiting_idx, wave_questions_open_idx).
type Pending struct {
	pool   *pgxpool.Pool
	agents map[string]string // stage → agent of the workflow in force
}

var _ pending.Store = Pending{}

// NewPending builds the adapter.
func NewPending(pool *pgxpool.Pool, definition domainworkflow.Definition) Pending {
	agents := map[string]string{}
	for _, stage := range definition.Stages {
		agents[stage.ID] = stage.Agent
	}
	for _, activity := range definition.Activities {
		agents[activity.ID] = activity.Agent
	}
	return Pending{pool: pool, agents: agents}
}

const pendingItemsSQL = `
WITH me AS (` + principalUserIDSQL + `),
mine AS (
  SELECT pr.id, pr.public_id, pr.name, bool_or(m.role IN ('approver','lead')) AS decides, bool_or(m.role = 'approver') AS approver
    FROM easycode.projects pr JOIN easycode.project_members m ON m.project_id = pr.id
   WHERE m.user_id = (SELECT * FROM me) AND pr.deletion_requested_at IS NULL
   GROUP BY pr.id, pr.public_id, pr.name
),
items AS (
  SELECT 'gate' AS type, p.public_id::text AS project_id, p.name AS project_name, r.stage, 'gate_round' AS target_type,
         r.gate || '/' || r.number AS target_id, r.opened_at, r.gate, r.number,
         (SELECT count(*) FROM easycode.check_executions c
           WHERE c.round_id = r.id AND c.evaluation = r.current_evaluation AND c.result IN ('failed','error'))::int AS failed_checks,
         EXISTS (SELECT 1 FROM easycode.check_executions c WHERE c.round_id = r.id AND c.evaluation = r.current_evaluation
                    AND c.result IN ('failed','error') AND c.severity = 'blocks') AS blocks,
         '' AS wave, 0 AS questions, '' AS finding, '' AS severity, 0 AS cycles, p.approver AS you_decide, 0 AS budget_percentage
    FROM easycode.gate_rounds r JOIN mine p ON p.id = r.project_id
   WHERE r.state = 'awaiting_human' AND p.decides AND 'gate' = ANY ($3::text[])
  UNION ALL
  SELECT 'question', p.public_id::text, p.name, w.stage, 'question', w.code || '/' || q.code, q.opened_at, '', 0, 0, false,
         w.code, (SELECT count(*) FROM easycode.question_items i WHERE i.question_id = q.id)::int, '', '', 0, true, 0
    FROM easycode.wave_questions q JOIN mine p ON p.id = q.project_id JOIN easycode.waves w ON w.id = q.wave_id
   WHERE q.answered_at IS NULL AND w.state = 'awaiting_user' AND 'question' = ANY ($3::text[])
  UNION ALL
  -- escalated finding waiting for the approver decision (US-32 CA-4, ESP-19), for whoever decides
  SELECT 'finding_escalated', p.public_id::text, p.name, f.origin_stage, 'finding', a.code,
         coalesce((SELECT max(e.escalated_at) FROM easycode.finding_escalations e WHERE e.finding_id = f.artifact_id), f.updated_at),
         '', 0, 0, f.severity IN ('critical','high'), '', 0, a.code, f.severity,
         (SELECT count(*) FROM easycode.finding_cycles c WHERE c.finding_id = f.artifact_id)::int, p.approver, 0
    FROM easycode.findings f JOIN mine p ON p.id = f.project_id JOIN easycode.artifacts a ON a.id = f.artifact_id
   WHERE f.status = 'escalated' AND p.decides AND 'finding_escalated' = ANY ($3::text[])
  UNION ALL
  -- budget alert or stop (US-39 CA-1, CA-2, RN-5) for the lead and the approver, until the lead changes the
  -- limit or the next alert replaces it (RN-4)
  SELECT 'budget', p.public_id::text, p.name, '', 'project', p.public_id::text,
         coalesce((SELECT max(e.occurred_at) FROM easycode.events e WHERE e.project_id = pr.id
                     AND e.type IN ('br.easycode.project.budget_alerted.v1', 'br.easycode.project.budget_exhausted.v1')), pr.created_at),
         '', 0, 0, pr.state = 'paused', '', 0, '', '', 0, true,
         floor((SELECT coalesce(sum(x.useful_cost + x.lost_cost), 0) FROM easycode.waves x WHERE x.project_id = pr.id) * 100 / pr.budget_stop_limit)::int
    FROM mine p JOIN easycode.projects pr ON pr.id = p.id
   WHERE p.decides AND pr.budget_stop_limit IS NOT NULL AND 'budget' = ANY ($3::text[])
     AND ((pr.state = 'in_progress' AND pr.last_alert_percentage IS NOT NULL) OR (pr.state = 'paused' AND pr.pause_reason = 'budget'))
)
SELECT type, project_id, project_name, stage, target_type, target_id, opened_at, gate, number, failed_checks, blocks,
       wave, questions, finding, severity, cycles, you_decide, budget_percentage, count(*) OVER ()
  FROM items
 ORDER BY opened_at, target_id
 LIMIT $4 OFFSET $5`

// Items implements pending.Store.
func (p Pending) Items(ctx context.Context, principal identity.Principal, types []string, limit, offset int) ([]pending.Item, int, error) {
	rows, err := conn(ctx, p.pool).Query(WithOperation(ctx, "pending_items.list"), pendingItemsSQL,
		principal.OrganizationID, principal.Subject, types, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("pending items: %w", classify(err))
	}
	total := 0
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending.Item, error) {
		var item pending.Item
		err := row.Scan(&item.Type, &item.ProjectID, &item.ProjectName, &item.Stage, &item.TargetType, &item.TargetID, &item.OpenedAt,
			&item.Gate, &item.Round, &item.FailedChecks, &item.Blocks, &item.Wave, &item.Questions, &item.Finding, &item.Severity, &item.Cycles,
			&item.YouDecide, &item.BudgetPercentage, &total)
		item.Agent = p.agents[item.Stage]
		return item, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("scan pending items: %w", err)
	}
	if len(items) == 0 && offset > 0 {
		var n int
		// total of the filter when the page is past the end
		if err := conn(ctx, p.pool).QueryRow(WithOperation(ctx, "pending_items.count"),
			"SELECT count(*) FROM ("+pendingItemsSQL[:len(pendingItemsSQL)-len(" LIMIT $4 OFFSET $5")]+") x",
			principal.OrganizationID, principal.Subject, types).Scan(&n); err != nil {
			return nil, 0, fmt.Errorf("count pending items: %w", classify(err))
		}
		total = n
	}
	return items, total, nil
}
