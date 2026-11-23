package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/retention"
)

// Retention implements retention.Store (US-04, RNF-20).
type Retention struct {
	pool *pgxpool.Pool
}

var _ retention.Store = Retention{}

// NewRetention builds the adapter. The login of the pool must be able to SET ROLE easycode_retention (the
// worker login is a member of it): the anonymization runs with that role only, inside its transaction.
func NewRetention(pool *pgxpool.Pool) Retention { return Retention{pool: pool} }

// AnonymizedText replaces a free human text of a deleted project.
const AnonymizedText = "[anonymized]"

// PendingDeletions implements retention.Store.
func (r Retention) PendingDeletions(ctx context.Context, limit int) ([]string, error) {
	rows, err := conn(ctx, r.pool).Query(WithOperation(ctx, "projects.pending_anonymization"), `
SELECT public_id::text FROM easycode.projects
 WHERE deletion_requested_at IS NOT NULL AND data_anonymized_at IS NULL
 ORDER BY deletion_requested_at LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending deletions: %w", classify(err))
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// anonymizations clear the personal data of the project $1 (internal id) with $2 as the replacement text:
// the columns marked as personal data in DIC.md that belong to a project.
var anonymizations = []string{
	`UPDATE easycode.findings SET acceptance_justification = CASE WHEN acceptance_justification IS NULL THEN NULL ELSE $2 END,
        discard_reason = CASE WHEN discard_reason IS NULL THEN NULL ELSE $2 END
  WHERE project_id = $1 AND (acceptance_justification IS NOT NULL OR discard_reason IS NOT NULL)`,
	`UPDATE easycode.finding_escalations e SET justification = $2 FROM easycode.findings f
  WHERE f.artifact_id = e.finding_id AND f.project_id = $1 AND e.justification IS NOT NULL`,
	`UPDATE easycode.gate_rounds SET reason = $2 WHERE project_id = $1 AND reason IS NOT NULL`,
	`UPDATE easycode.gate_confirmations c SET reason = $2 FROM easycode.gate_rounds r
  WHERE r.id = c.round_id AND r.project_id = $1 AND c.reason IS NOT NULL`,
	`UPDATE easycode.question_items i SET answer = $2 FROM easycode.wave_questions q
  WHERE q.id = i.question_id AND q.project_id = $1 AND i.answer IS NOT NULL`,
	`UPDATE easycode.audit_records SET justification = $2 WHERE project_id = $1 AND justification IS NOT NULL`,
	`UPDATE easycode.model_calls c SET content = NULL, content_purged_at = now() FROM easycode.waves w
  WHERE w.id = c.wave_id AND w.project_id = $1 AND c.content IS NOT NULL AND $2::text IS NOT NULL`,
	`UPDATE easycode.wave_checkpoints c SET agent_state = NULL, content_purged_at = now() FROM easycode.waves w
  WHERE w.id = c.wave_id AND w.project_id = $1 AND c.agent_state IS NOT NULL AND $2::text IS NOT NULL`,
}

// Anonymize implements retention.Store.
func (r Retention) Anonymize(ctx context.Context, projectID string) (int, error) {
	q := conn(ctx, r.pool)
	if _, err := q.Exec(ctx, "SET LOCAL ROLE easycode_retention"); err != nil {
		return 0, fmt.Errorf("retention role: %w", classify(err))
	}
	var internalID int64
	if err := q.QueryRow(WithOperation(ctx, "projects.lock_for_anonymization"),
		"SELECT id FROM easycode.projects WHERE public_id = $1::uuid AND deletion_requested_at IS NOT NULL AND data_anonymized_at IS NULL FOR UPDATE",
		projectID).Scan(&internalID); err != nil {
		return 0, fmt.Errorf("project to anonymize: %w", classify(err))
	}
	total := 0
	for i, statement := range anonymizations {
		tag, err := q.Exec(WithOperation(ctx, fmt.Sprintf("anonymize.%d", i)), statement, internalID, AnonymizedText)
		if err != nil {
			return total, fmt.Errorf("anonymize project data: %w", classify(err))
		}
		total += int(tag.RowsAffected())
	}
	if _, err := q.Exec(WithOperation(ctx, "projects.anonymized"),
		"UPDATE easycode.projects SET data_anonymized_at = now() WHERE id = $1", internalID); err != nil {
		return total, fmt.Errorf("mark project anonymized: %w", classify(err))
	}
	return total, nil
}
