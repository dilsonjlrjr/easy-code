package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appgate "easycode/internal/application/gate"
	"easycode/internal/domain/identity"
)

// GateAnnotations keeps the prototype annotations of the gate.design rejection (US-57, migration 0018).
type GateAnnotations struct {
	pool *pgxpool.Pool
}

var _ appgate.AnnotationStore = GateAnnotations{}

// NewGateAnnotations builds the adapter.
func NewGateAnnotations(pool *pgxpool.Pool) GateAnnotations { return GateAnnotations{pool: pool} }

// RecordAnnotations implements appgate.AnnotationStore, inside the transaction of the decision.
func (g GateAnnotations) RecordAnnotations(ctx context.Context, p identity.Principal, projectID, stage string, round int, artifact string, version int,
	annotations []appgate.Annotation) error {
	q := conn(ctx, g.pool)
	for i, a := range annotations {
		var state *string
		if a.State != "" {
			state = &a.State
		}
		_, err := q.Exec(WithOperation(ctx, "gate_round_annotations.insert"), `
INSERT INTO easycode.gate_round_annotations (round_id, number, artifact_code, artifact_version, screen, screen_state, width, x, y, comment, created_by)
SELECT r.id, $4, $5, $6, $7, $8, $9, $10, $11, $12, u.id
  FROM easycode.gate_rounds r JOIN easycode.projects pr ON pr.id = r.project_id,
       easycode.users u JOIN easycode.organizations o ON o.id = u.organization_id
 WHERE pr.public_id = $1::uuid AND r.stage = $2 AND r.number = $3 AND o.public_id = $13::uuid AND u.sub = $14`,
			projectID, stage, round, i+1, artifact, version, a.Screen, state, a.Width, a.X, a.Y, a.Comment, p.OrganizationID, p.Subject)
		if err != nil {
			return fmt.Errorf("record annotation: %w", classify(err))
		}
	}
	return nil
}

// StageAnnotations implements appgate.AnnotationStore.
func (g GateAnnotations) StageAnnotations(ctx context.Context, projectID, stage string) (map[int][]appgate.Annotation, error) {
	rows, err := conn(ctx, g.pool).Query(WithOperation(ctx, "gate_round_annotations.of_stage"), `
SELECT r.number, a.number, a.artifact_code, a.artifact_version, a.screen, coalesce(a.screen_state, ''), a.width, a.x, a.y, a.comment
  FROM easycode.gate_round_annotations a JOIN easycode.gate_rounds r ON r.id = a.round_id JOIN easycode.projects pr ON pr.id = r.project_id
 WHERE pr.public_id = $1::uuid AND r.stage = $2
 ORDER BY r.number, a.number`, projectID, stage)
	if err != nil {
		return nil, fmt.Errorf("stage annotations: %w", classify(err))
	}
	out := map[int][]appgate.Annotation{}
	var round int
	var a appgate.Annotation
	_, err = pgx.ForEachRow(rows, []any{&round, &a.Number, &a.Artifact, &a.Version, &a.Screen, &a.State, &a.Width, &a.X, &a.Y, &a.Comment}, func() error {
		item := a
		if a.X != nil {
			x, y := *a.X, *a.Y
			item.X, item.Y = &x, &y
		}
		out[round] = append(out[round], item)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("stage annotations: %w", classify(err))
	}
	return out, nil
}

// reworkAnnotations renders the annotations of the last rejected round of the stage of a wave (US-57 CA-2),
// "" when it has none.
func reworkAnnotations(ctx context.Context, q querier, waveID int64) (string, error) {
	rows, err := q.Query(WithOperation(ctx, "gate_round_annotations.of_rework"), `
SELECT a.number, a.artifact_code, a.artifact_version, a.screen, coalesce(a.screen_state, ''), a.width, a.x, a.y, a.comment
  FROM easycode.waves w
  JOIN LATERAL (SELECT g.id FROM easycode.gate_rounds g WHERE g.project_id = w.project_id AND g.stage = w.stage AND g.state = 'rejected'
                 ORDER BY g.number DESC LIMIT 1) r ON true
  JOIN easycode.gate_round_annotations a ON a.round_id = r.id
 WHERE w.id = $1
 ORDER BY a.number`, waveID)
	if err != nil {
		return "", fmt.Errorf("rework annotations: %w", classify(err))
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appgate.Annotation, error) {
		var a appgate.Annotation
		return a, row.Scan(&a.Number, &a.Artifact, &a.Version, &a.Screen, &a.State, &a.Width, &a.X, &a.Y, &a.Comment)
	})
	if err != nil {
		return "", fmt.Errorf("rework annotations: %w", classify(err))
	}
	return appgate.RenderAnnotations(list), nil
}
