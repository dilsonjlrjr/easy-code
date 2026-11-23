package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
)

var _ appproject.ConfigurationRepository = Projects{}

// Configuration implements appproject.ConfigurationRepository.
func (r Projects) Configuration(ctx context.Context, projectID string) (appproject.StoredConfiguration, error) {
	q := conn(ctx, r.pool)
	stored := appproject.StoredConfiguration{GateModes: map[string]string{}}
	config := &stored.Configuration
	config.PairByStage = map[string]appproject.Pair{}
	var internalID int64
	var stopLimit, ceiling *string
	var alerts []int
	err := q.QueryRow(WithOperation(ctx, "configuration.project"), `
SELECT pr.id, pr.row_version, pr.artifact_language, pr.budget_stop_limit::text, pr.budget_alert_limits, o.currency, o.stop_limit_ceiling::text
  FROM easycode.projects pr JOIN easycode.organizations o ON o.id = pr.organization_id
 WHERE pr.public_id = $1::uuid`, projectID).Scan(&internalID, &config.Version, &config.ArtifactLanguage, &stopLimit, &alerts, &stored.Currency, &ceiling)
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return stored, fmt.Errorf("read configuration: %w", classify(err))
	}
	if stopLimit != nil {
		config.Budget = &project.Budget{StopLimit: project.Money{Value: project.TrimDecimal(*stopLimit), Currency: stored.Currency}, AlertPercentages: alerts}
	}
	if ceiling != nil {
		config.StopLimitCeiling = &project.Money{Value: project.TrimDecimal(*ceiling), Currency: stored.Currency}
	}
	rows, err := q.Query(WithOperation(ctx, "configuration.pairs"), `
SELECT mp.stage, rm.provider, rm.name, em.provider, em.name
  FROM easycode.model_pairs mp
  JOIN easycode.models rm ON rm.id = mp.reasoning_model_id
  JOIN easycode.models em ON em.id = mp.execution_model_id
 WHERE mp.project_id = $1`, internalID)
	if err != nil {
		return stored, fmt.Errorf("read pairs: %w", classify(err))
	}
	for rows.Next() {
		var stage *string
		var pair appproject.Pair
		if err := rows.Scan(&stage, &pair.Reasoning.Provider, &pair.Reasoning.Model, &pair.Execution.Provider, &pair.Execution.Model); err != nil {
			rows.Close()
			return stored, err
		}
		if stage == nil {
			projectPair := pair
			config.ModelPair = &projectPair
		} else {
			config.PairByStage[*stage] = pair
		}
	}
	rows.Close()
	rows, err = q.Query(WithOperation(ctx, "configuration.gates"), "SELECT gate, approval_mode FROM easycode.gate_configurations WHERE project_id = $1", internalID)
	if err != nil {
		return stored, fmt.Errorf("read gates: %w", classify(err))
	}
	for rows.Next() {
		var gate, mode string
		if err := rows.Scan(&gate, &mode); err != nil {
			rows.Close()
			return stored, err
		}
		stored.GateModes[gate] = mode
	}
	rows.Close()
	var items []byte
	view := appproject.ConstraintsView{}
	err = q.QueryRow(WithOperation(ctx, "configuration.constraints"), `
SELECT c.version, c.items, u.sub, u.display_name, c.defined_at
  FROM easycode.project_constraints c JOIN easycode.users u ON u.id = c.defined_by
 WHERE c.project_id = $1 ORDER BY c.version DESC LIMIT 1`, internalID).Scan(&view.Version, &items, &view.DefinedBy.Subject, &view.DefinedBy.Name, &view.DefinedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return stored, fmt.Errorf("read constraints: %w", classify(err))
	default:
		var decoded []constraintJSON
		if err := json.Unmarshal(items, &decoded); err != nil {
			return stored, err
		}
		for _, item := range decoded {
			view.Items = append(view.Items, project.Constraint(item))
		}
		config.Constraints = &view
	}
	return stored, nil
}

// Save implements appproject.ConfigurationRepository.
func (r Projects) Save(ctx context.Context, p identity.Principal, projectID string, version int, change appproject.Change) error {
	q := conn(ctx, r.pool)
	var internalID, userID int64
	var stopLimit *string
	var alerts []int
	if change.Budget != nil {
		stopLimit = &change.Budget.StopLimit.Value
		alerts = change.Budget.AlertPercentages
	}
	err := q.QueryRow(WithOperation(ctx, "configuration.bump"), `
UPDATE easycode.projects SET row_version = row_version + 1,
       budget_stop_limit = coalesce($5::numeric, budget_stop_limit),
       budget_alert_limits = coalesce($6::smallint[], budget_alert_limits),
       -- US-39 RN-4: a new limit takes the budget alert out of the inbox; the next check emits again what applies.
       last_alert_percentage = CASE WHEN $5::numeric IS NULL THEN last_alert_percentage END,
       artifact_language = coalesce($7, artifact_language)
 WHERE public_id = $3::uuid AND row_version = $4
RETURNING id, `+principalUserIDSQL, p.OrganizationID, p.Subject, projectID, version, stopLimit, alerts, change.ArtifactLanguage).Scan(&internalID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return failure.New(failure.KindConflict, appproject.CodeFailedPrecondition, nil)
	}
	if err != nil {
		return fmt.Errorf("update configuration: %w", classify(err))
	}
	if change.ModelPair != nil {
		if err := savePair(ctx, q, internalID, userID, nil, *change.ModelPair); err != nil {
			return err
		}
	}
	for stage, pair := range change.PairByStage {
		if pair == nil {
			if _, err := q.Exec(WithOperation(ctx, "model_pairs.delete_stage"), "DELETE FROM easycode.model_pairs WHERE project_id = $1 AND stage = $2", internalID, stage); err != nil {
				return fmt.Errorf("remove stage pair: %w", classify(err))
			}
			continue
		}
		if err := savePair(ctx, q, internalID, userID, &stage, *pair); err != nil {
			return err
		}
	}
	for gate, mode := range change.GateApproval {
		_, err := q.Exec(WithOperation(ctx, "gate_configurations.save"), `
INSERT INTO easycode.gate_configurations (project_id, gate, approval_mode, defined_by) VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, gate) DO UPDATE SET approval_mode = excluded.approval_mode, defined_by = excluded.defined_by, defined_at = now()`,
			internalID, gate, mode, userID)
		if err != nil {
			return fmt.Errorf("save gate mode: %w", classify(err))
		}
	}
	if change.Constraints != nil {
		var next int
		if err := q.QueryRow(WithOperation(ctx, "project_constraints.next_version"),
			"SELECT coalesce(max(version), 0) + 1 FROM easycode.project_constraints WHERE project_id = $1", internalID).Scan(&next); err != nil {
			return fmt.Errorf("next constraints version: %w", classify(err))
		}
		if err := insertConstraints(ctx, q, internalID, userID, next, *change.Constraints); err != nil {
			return err
		}
	}
	return nil
}

func savePair(ctx context.Context, q querier, projectID, userID int64, stage *string, pair appproject.Pair) error {
	tag, err := q.Exec(WithOperation(ctx, "model_pairs.save"), `
INSERT INTO easycode.model_pairs (project_id, stage, reasoning_model_id, execution_model_id, defined_by)
SELECT $1, $2, rm.id, em.id, $7
  FROM easycode.models rm, easycode.models em
 WHERE rm.provider = $3 AND rm.name = $4 AND em.provider = $5 AND em.name = $6
ON CONFLICT (project_id, stage) DO UPDATE SET reasoning_model_id = excluded.reasoning_model_id,
  execution_model_id = excluded.execution_model_id, defined_by = excluded.defined_by, defined_at = now()`,
		projectID, stage, pair.Reasoning.Provider, pair.Reasoning.Model, pair.Execution.Provider, pair.Execution.Model, userID)
	if err != nil {
		return fmt.Errorf("save pair: %w", classify(err))
	}
	if tag.RowsAffected() == 0 {
		return failure.Invalid(failure.FieldError{Field: "model_pair", Message: "invalid"})
	}
	return nil
}
