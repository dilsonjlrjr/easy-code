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

	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
	domainworkflow "easycode/internal/domain/workflow"
)

// ChannelWaves wakes the agent runtime when the orchestrator queues a wave.
const ChannelWaves = "easycode_waves"

// Wave limits of policy.wave_execution defaults (politicas.yaml): retries, execution and call timeouts.
const (
	defaultMaxRetries       = 3
	defaultExecutionTimeout = "1 hour"
	defaultCallTimeout      = "10 minutes"
)

// Workflow is the workflow, stage, wave and project command adapter.
type Workflow struct {
	pool    *pgxpool.Pool
	inForce domainworkflow.Definition
}

var (
	_ appworkflow.Readiness     = Workflow{}
	_ appworkflow.Commands      = Workflow{}
	_ appworkflow.Orchestration = Workflow{}
	_ appworkflow.Views         = Workflow{}
)

// NewWorkflow builds the adapter; inForce is shown for projects not started yet.
func NewWorkflow(pool *pgxpool.Pool, inForce domainworkflow.Definition) Workflow {
	return Workflow{pool: pool, inForce: inForce}
}

// Missing implements appworkflow.Readiness (US-05 CA-2, US-37 CA-2, US-39 CA-4).
func (w Workflow) Missing(ctx context.Context, projectID string, at time.Time) ([]string, error) {
	var hasPair, keysOK, pricesOK, hasBudget, hasNeed bool
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "workflow.readiness"), `
WITH pr AS (SELECT id, organization_id, budget_stop_limit, business_need FROM easycode.projects WHERE public_id = $1::uuid),
used AS (
  SELECT m.id, m.provider FROM easycode.model_pairs mp JOIN pr ON pr.id = mp.project_id
    JOIN easycode.models m ON m.id IN (mp.reasoning_model_id, mp.execution_model_id)
)
SELECT EXISTS (SELECT 1 FROM easycode.model_pairs mp JOIN pr ON pr.id = mp.project_id WHERE mp.stage IS NULL),
       CASE WHEN EXISTS (SELECT 1 FROM used)
            THEN NOT EXISTS (SELECT 1 FROM used u WHERE NOT EXISTS (
                   SELECT 1 FROM easycode.provider_keys k WHERE k.organization_id = (SELECT organization_id FROM pr)
                      AND k.provider = u.provider AND k.state = 'active'))
            ELSE EXISTS (SELECT 1 FROM easycode.provider_keys k WHERE k.organization_id = (SELECT organization_id FROM pr) AND k.state = 'active') END,
       NOT EXISTS (SELECT 1 FROM used u WHERE NOT EXISTS (
         SELECT 1 FROM easycode.model_prices p WHERE p.organization_id = (SELECT organization_id FROM pr)
            AND p.model_id = u.id AND p.effective_since <= $2)),
       (SELECT budget_stop_limit IS NOT NULL FROM pr),
       (SELECT btrim(business_need) <> '' FROM pr)`, projectID, at).Scan(&hasPair, &keysOK, &pricesOK, &hasBudget, &hasNeed)
	if err != nil {
		return nil, fmt.Errorf("start readiness: %w", classify(err))
	}
	missing := []string{}
	if !keysOK {
		missing = append(missing, appworkflow.MissingProviderKey)
	}
	if !hasPair {
		missing = append(missing, appworkflow.MissingModelPair)
	} else if !pricesOK {
		missing = append(missing, appworkflow.MissingCurrentPrice)
	}
	if !hasBudget {
		missing = append(missing, appworkflow.MissingBudget)
	}
	if !hasNeed {
		missing = append(missing, appworkflow.MissingBusinessNeed)
	}
	return missing, nil
}

// Enqueue implements appworkflow.Commands.
func (w Workflow) Enqueue(ctx context.Context, p identity.Principal, projectID, commandType, key, bodyHash string) (appworkflow.CommandView, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	q := conn(ctx, w.pool)
	_, err = q.Exec(WithOperation(ctx, "commands.insert_project"), `
INSERT INTO easycode.commands (public_id, organization_id, project_id, target_type, type, requested_by, origin, idempotency_key, body_hash)
SELECT $4::uuid, pr.organization_id, pr.id, 'project', $5, `+principalUserIDSQL+`, 'web', $6::uuid, $7
  FROM easycode.projects pr WHERE pr.public_id = $3::uuid`, p.OrganizationID, p.Subject, projectID, id.String(), commandType, key, bodyHash)
	if err != nil {
		return appworkflow.CommandView{}, fmt.Errorf("queue command: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "commands.notify"), "SELECT pg_notify($1, $2)", ChannelCommands, projectID); err != nil {
		return appworkflow.CommandView{}, fmt.Errorf("notify commands: %w", err)
	}
	return w.Get(ctx, projectID, id.String())
}

// Get implements appworkflow.Commands.
func (w Workflow) Get(ctx context.Context, projectID, commandID string) (appworkflow.CommandView, error) {
	view := appworkflow.CommandView{ID: commandID}
	var refusal *string
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "commands.get"), `
SELECT c.type, c.target_type, coalesce(wv.code, pr.public_id::text), c.status, u.sub, u.display_name, c.requested_at, c.processed_at, c.refusal_reason
  FROM easycode.commands c JOIN easycode.projects pr ON pr.id = c.project_id JOIN easycode.users u ON u.id = c.requested_by
  LEFT JOIN easycode.waves wv ON wv.id = c.wave_id
 WHERE c.public_id = $1::uuid AND pr.public_id = $2::uuid`, commandID, projectID).Scan(&view.Type, &view.TargetType, &view.TargetID, &view.State,
		&view.RequestedBy.Subject, &view.RequestedBy.Name, &view.RequestedAt, &view.CompletedAt, &refusal)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return view, fmt.Errorf("read command: %w", classify(err))
	}
	if refusal != nil {
		view.Refusal = *refusal
	}
	return view, nil
}

// LockProject implements appworkflow.Orchestration.
func (w Workflow) LockProject(ctx context.Context, projectID string) (string, error) {
	var state string
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "projects.lock"),
		"SELECT state FROM easycode.projects WHERE public_id = $1::uuid FOR UPDATE", projectID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return "", fmt.Errorf("lock project: %w", classify(err))
	}
	return state, nil
}

// StartProject implements appworkflow.Orchestration. The repository address is recorded now
// (projects_repository_ck); the git repository itself is created with US-42 (MARCO-3).
func (w Workflow) StartProject(ctx context.Context, projectID string, definition domainworkflow.Definition, definitionJSON []byte, hash string) error {
	q := conn(ctx, w.pool)
	var internalID int64
	err := q.QueryRow(WithOperation(ctx, "projects.start"), `
UPDATE easycode.projects SET state = 'in_progress', started_at = now(), repository = 'repos/' || public_id::text || '.git'
 WHERE public_id = $1::uuid AND state = 'created' RETURNING id`, projectID).Scan(&internalID)
	if err != nil {
		return fmt.Errorf("start project: %w", classify(err))
	}
	_, err = q.Exec(WithOperation(ctx, "workflow_configurations.pin"), `
INSERT INTO easycode.workflow_configurations (project_id, workflow_id, workflow_version, definition, definition_hash)
VALUES ($1, $2, $3, $4::jsonb, $5)`, internalID, definition.ID, definition.Version, string(definitionJSON), hash)
	if err != nil {
		return fmt.Errorf("pin workflow: %w", classify(err))
	}
	for _, stage := range definition.Stages {
		_, err := q.Exec(WithOperation(ctx, "stages.insert"),
			"INSERT INTO easycode.stages (project_id, stage, type, state) VALUES ($1, $2, 'stage', 'blocked')", internalID, stage.ID)
		if err != nil {
			return fmt.Errorf("insert stage %s: %w", stage.ID, classify(err))
		}
	}
	return nil
}

// SetStageState implements appworkflow.Orchestration.
func (w Workflow) SetStageState(ctx context.Context, projectID, stage, state string) error {
	_, err := conn(ctx, w.pool).Exec(WithOperation(ctx, "stages.set_state"), `
UPDATE easycode.stages s SET state = $3, updated_at = now() FROM easycode.projects pr
 WHERE pr.id = s.project_id AND pr.public_id = $1::uuid AND s.stage = $2`, projectID, stage, state)
	if err != nil {
		return fmt.Errorf("stage %s to %s: %w", stage, state, classify(err))
	}
	return nil
}

// ResolvePair implements appworkflow.Orchestration: the stage pair wins over the project pair.
func (w Workflow) ResolvePair(ctx context.Context, projectID, stage string) (appworkflow.ResolvedPair, error) {
	var pair appworkflow.ResolvedPair
	err := conn(ctx, w.pool).QueryRow(WithOperation(ctx, "model_pairs.resolve"), `
SELECT rm.id, rm.provider, rm.name, em.id, em.provider, em.name, mp.stage IS NOT NULL
  FROM easycode.model_pairs mp JOIN easycode.projects pr ON pr.id = mp.project_id
  JOIN easycode.models rm ON rm.id = mp.reasoning_model_id JOIN easycode.models em ON em.id = mp.execution_model_id
 WHERE pr.public_id = $1::uuid AND (mp.stage = $2 OR mp.stage IS NULL)
 ORDER BY mp.stage IS NULL LIMIT 1`, projectID, stage).Scan(&pair.Reasoning.ID, &pair.Reasoning.Provider, &pair.Reasoning.Name,
		&pair.Execution.ID, &pair.Execution.Provider, &pair.Execution.Name, &pair.FromStage)
	if errors.Is(err, pgx.ErrNoRows) {
		return pair, failure.New(failure.KindConflict, project.CodeIncompleteConfig, nil)
	}
	if err != nil {
		return pair, fmt.Errorf("resolve pair: %w", classify(err))
	}
	return pair, nil
}

// QueueWave implements appworkflow.Orchestration.
func (w Workflow) QueueWave(ctx context.Context, wave appworkflow.NewWave) (appworkflow.QueuedWave, error) {
	q := conn(ctx, w.pool)
	var projectID, stageID int64
	var currency string
	err := q.QueryRow(WithOperation(ctx, "waves.resolve_stage"), `
SELECT pr.id, s.id, o.currency FROM easycode.projects pr JOIN easycode.stages s ON s.project_id = pr.id AND s.stage = $2
  JOIN easycode.organizations o ON o.id = pr.organization_id WHERE pr.public_id = $1::uuid`, wave.ProjectID, wave.Stage).Scan(&projectID, &stageID, &currency)
	if err != nil {
		return appworkflow.QueuedWave{}, fmt.Errorf("resolve stage: %w", classify(err))
	}
	configurationID, err := w.agentConfiguration(ctx, q, projectID, wave.Configuration)
	if err != nil {
		return appworkflow.QueuedWave{}, err
	}
	var number int64
	err = q.QueryRow(WithOperation(ctx, "waves.next_number"), `
INSERT INTO easycode.project_counters (project_id, name, value) VALUES ($1, $2, 1)
ON CONFLICT (project_id, name) DO UPDATE SET value = project_counters.value + 1 RETURNING value`,
		projectID, "wave:"+domainworkflow.WavePrefix(wave.Stage)).Scan(&number)
	if err != nil {
		return appworkflow.QueuedWave{}, fmt.Errorf("next wave number: %w", classify(err))
	}
	queued := appworkflow.QueuedWave{Code: domainworkflow.WaveCode(wave.Stage, number)}
	var waveID int64
	err = q.QueryRow(WithOperation(ctx, "waves.insert"), `
INSERT INTO easycode.waves (project_id, stage, stage_id, code, agent, agent_configuration_id, max_retries, execution_timeout, call_timeout, currency, task)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::interval, $9::interval, $10, nullif($11, '')) RETURNING id, public_id::text`,
		projectID, wave.Stage, stageID, queued.Code, wave.Agent, configurationID, defaultMaxRetries, defaultExecutionTimeout, defaultCallTimeout, currency, wave.Task).
		Scan(&waveID, &queued.ID)
	if err != nil {
		return appworkflow.QueuedWave{}, fmt.Errorf("insert wave: %w", classify(err))
	}
	if err := waveSkills(ctx, q, projectID, waveID, wave.Agent); err != nil {
		return appworkflow.QueuedWave{}, err
	}
	_, err = q.Exec(WithOperation(ctx, "wave_models.insert"), `
INSERT INTO easycode.wave_models (wave_id, role, model_id, mode, reason)
VALUES ($1, 'reasoning', $2, 'manual', 'project_configuration'), ($1, 'execution', $3, 'manual', 'project_configuration')`,
		waveID, wave.Pair.Reasoning.ID, wave.Pair.Execution.ID)
	if err != nil {
		return appworkflow.QueuedWave{}, fmt.Errorf("insert wave models: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "waves.notify"), "SELECT pg_notify($1, $2)", ChannelWaves, wave.ProjectID); err != nil {
		return appworkflow.QueuedWave{}, fmt.Errorf("notify waves: %w", err)
	}
	return queued, nil
}

// agentConfiguration returns the latest configuration of the agent in the project, creating the
// platform default (version 1) the first time (US-25 makes it editable later).
func (w Workflow) agentConfiguration(ctx context.Context, q querier, projectID int64, defaults appworkflow.AgentConfiguration) (int64, error) {
	var id int64
	err := q.QueryRow(WithOperation(ctx, "agent_configurations.latest"),
		"SELECT id FROM easycode.agent_configurations WHERE project_id = $1 AND agent = $2 ORDER BY version DESC LIMIT 1", projectID, defaults.Agent).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("agent configuration: %w", classify(err))
	}
	err = q.QueryRow(WithOperation(ctx, "agent_configurations.default"), `
INSERT INTO easycode.agent_configurations (project_id, agent, version, instruction_version, instruction_hash, tools, limits)
VALUES ($1, $2, 1, $3, $4, $5::jsonb, $6::jsonb) RETURNING id`,
		projectID, defaults.Agent, defaults.InstructionVersion, defaults.InstructionHash, string(defaults.Tools), string(defaults.Limits)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("default agent configuration: %w", classify(err))
	}
	return id, nil
}

// Workflow implements appworkflow.Views.
func (w Workflow) Workflow(ctx context.Context, projectID string) (appworkflow.View, error) {
	q := conn(ctx, w.pool)
	var internalID int64
	var pinned []byte
	var currency string
	err := q.QueryRow(WithOperation(ctx, "workflow.definition"), `
SELECT pr.id, wc.definition, o.currency FROM easycode.projects pr JOIN easycode.organizations o ON o.id = pr.organization_id
  LEFT JOIN easycode.workflow_configurations wc ON wc.project_id = pr.id WHERE pr.public_id = $1::uuid`, projectID).Scan(&internalID, &pinned, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return appworkflow.View{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return appworkflow.View{}, fmt.Errorf("read workflow: %w", classify(err))
	}
	definition := w.inForce
	if pinned != nil {
		if err := json.Unmarshal(pinned, &definition); err != nil {
			return appworkflow.View{}, fmt.Errorf("pinned workflow: %w", err)
		}
	}
	type stageRow struct {
		state, gateState string
		round, active    int
		reevaluation     bool
		cost             string
	}
	rows, err := q.Query(WithOperation(ctx, "workflow.stages"), `
SELECT s.stage, s.state, s.reevaluation_pending,
       coalesce((SELECT max(g.number) FROM easycode.gate_rounds g WHERE g.stage_id = s.id), 0),
       coalesce((SELECT g.state FROM easycode.gate_rounds g WHERE g.stage_id = s.id ORDER BY g.number DESC LIMIT 1), ''),
       (SELECT count(*) FROM easycode.waves v WHERE v.stage_id = s.id AND v.state IN ('queued','running','awaiting_user','pausing','paused')),
       (SELECT coalesce(sum(v.useful_cost + v.lost_cost), 0)::text FROM easycode.waves v WHERE v.stage_id = s.id)
  FROM easycode.stages s WHERE s.project_id = $1`, internalID)
	if err != nil {
		return appworkflow.View{}, fmt.Errorf("read stages: %w", classify(err))
	}
	states := map[string]stageRow{}
	for rows.Next() {
		var id string
		var row stageRow
		if err := rows.Scan(&id, &row.state, &row.reevaluation, &row.round, &row.gateState, &row.active, &row.cost); err != nil {
			rows.Close()
			return appworkflow.View{}, err
		}
		states[id] = row
	}
	rows.Close()
	requirement := domainworkflow.ModelRequirement()
	capabilities := make([]string, 0, len(requirement.Capacities))
	for _, capacity := range requirement.Capacities {
		capabilities = append(capabilities, string(capacity))
	}
	view := appworkflow.View{ID: definition.ID, Version: definition.Version, Mode: "fixed"}
	for _, stage := range definition.Stages {
		row := states[stage.ID]
		cost := "0"
		if row.cost != "" {
			cost = project.TrimDecimal(row.cost)
		}
		dependsOn := stage.DependsOn
		if dependsOn == nil {
			dependsOn = []string{}
		}
		view.Stages = append(view.Stages, appworkflow.StageView{
			ID: stage.ID, Name: stage.Name, ShortName: stage.ShortName, Agent: stage.Agent, DependsOn: dependsOn,
			RequiredCapabilities: capabilities, State: row.state, Gate: stage.Gate, CurrentRound: row.round, GateState: row.gateState,
			ReevaluationPending: row.reevaluation, ActiveWaves: row.active, Cost: project.Money{Value: cost, Currency: currency},
		})
	}
	return view, nil
}

// StageStates implements appworkflow.Orchestration.
func (w Workflow) StageStates(ctx context.Context, projectID string) (map[string]string, error) {
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "stages.states"), `
SELECT s.stage, s.state FROM easycode.stages s JOIN easycode.projects pr ON pr.id = s.project_id WHERE pr.public_id = $1::uuid`, projectID)
	if err != nil {
		return nil, fmt.Errorf("stage states: %w", classify(err))
	}
	defer rows.Close()
	states := map[string]string{}
	for rows.Next() {
		var stage, state string
		if err := rows.Scan(&stage, &state); err != nil {
			return nil, err
		}
		states[stage] = state
	}
	return states, rows.Err()
}

// ApprovedTasks implements appworkflow.Orchestration: the approved T of the project in number order.
func (w Workflow) ApprovedTasks(ctx context.Context, projectID string) ([]string, error) {
	rows, err := conn(ctx, w.pool).Query(WithOperation(ctx, "artifacts.approved_tasks"), `
SELECT a.code FROM easycode.artifacts a JOIN easycode.projects pr ON pr.id = a.project_id
 WHERE pr.public_id = $1::uuid AND a.type = 'T' AND a.state = 'approved'
 ORDER BY length(a.code), a.code`, projectID)
	if err != nil {
		return nil, fmt.Errorf("approved tasks: %w", classify(err))
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
