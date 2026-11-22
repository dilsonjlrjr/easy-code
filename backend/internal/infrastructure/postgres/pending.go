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
         '' AS wave, 0 AS questions, p.approver AS you_decide
    FROM easycode.gate_rounds r JOIN mine p ON p.id = r.project_id
   WHERE r.state = 'awaiting_human' AND p.decides AND 'gate' = ANY ($3::text[])
  UNION ALL
  SELECT 'question', p.public_id::text, p.name, w.stage, 'question', w.code || '/' || q.code, q.opened_at, '', 0, 0, false,
         w.code, (SELECT count(*) FROM easycode.question_items i WHERE i.question_id = q.id)::int, true
    FROM easycode.wave_questions q JOIN mine p ON p.id = q.project_id JOIN easycode.waves w ON w.id = q.wave_id
   WHERE q.answered_at IS NULL AND w.state = 'awaiting_user' AND 'question' = ANY ($3::text[])
)
SELECT type, project_id, project_name, stage, target_type, target_id, opened_at, gate, number, failed_checks, blocks,
       wave, questions, you_decide, count(*) OVER ()
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
			&item.Gate, &item.Round, &item.FailedChecks, &item.Blocks, &item.Wave, &item.Questions, &item.YouDecide, &total)
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
