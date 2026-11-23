package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appcost "easycode/internal/application/cost"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
	"easycode/internal/domain/project"
	domainworkflow "easycode/internal/domain/workflow"
)

// Costs reads the facts of the cost views (US-38, US-40) for the api.
type Costs struct {
	pool    *pgxpool.Pool
	inForce domainworkflow.Definition
}

var _ appcost.Reader = Costs{}

// NewCosts builds the adapter over the workflow in force (to know the agent of a stage).
func NewCosts(pool *pgxpool.Pool, inForce domainworkflow.Definition) Costs {
	return Costs{pool: pool, inForce: inForce}
}

// cycleSQL is the cycle time of a wave in ms, the same rule as getWave (with the human wait, US-10 CA-3).
const cycleSQL = `(extract(epoch FROM coalesce(w.finished_at, now()) - coalesce(w.started_at, w.created_at)) * 1000)::bigint`

// ProjectFacts implements appcost.Reader.
func (c Costs) ProjectFacts(ctx context.Context, projectID string) (appcost.ProjectFacts, error) {
	q := conn(ctx, c.pool)
	var facts appcost.ProjectFacts
	var internalID int64
	var stopLimit *string
	var alerts []int16
	var last *int16
	err := q.QueryRow(WithOperation(ctx, "costs.project"), `
SELECT pr.id, o.currency, pr.budget_stop_limit::text, pr.budget_alert_limits, pr.last_alert_percentage
  FROM easycode.projects pr JOIN easycode.organizations o ON o.id = pr.organization_id WHERE pr.public_id = $1::uuid`, projectID).
		Scan(&internalID, &facts.Currency, &stopLimit, &alerts, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return facts, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return facts, fmt.Errorf("cost project: %w", classify(err))
	}
	if stopLimit != nil {
		facts.StopLimit = project.TrimDecimal(*stopLimit)
	}
	for _, a := range alerts {
		facts.AlertPercentages = append(facts.AlertPercentages, int(a))
	}
	if last != nil {
		n := int(*last)
		facts.LastAlert = &n
	}
	rows, err := q.Query(WithOperation(ctx, "costs.waves"), `
SELECT w.code, w.stage, w.agent, w.purpose, coalesce(f.code, ''), w.useful_cost::text, w.lost_cost::text, w.estimated_lost_cost,
       w.input_tokens, w.output_tokens, w.read_cache_tokens, w.write_cache_tokens, w.execution_duration_ms, `+cycleSQL+`
  FROM easycode.waves w LEFT JOIN easycode.artifacts f ON f.id = w.finding_id
 WHERE w.project_id = $1 ORDER BY w.id`, internalID)
	if err != nil {
		return facts, fmt.Errorf("cost waves: %w", classify(err))
	}
	facts.Waves, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (appcost.WaveCost, error) {
		var w appcost.WaveCost
		err := row.Scan(&w.Code, &w.Stage, &w.Agent, &w.Purpose, &w.Finding, &w.Useful, &w.Lost, &w.EstimatedLost,
			&w.Tokens.Input, &w.Tokens.Output, &w.Tokens.ReadCache, &w.Tokens.WriteCache, &w.Time.ExecutionMs, &w.Time.CycleMs)
		w.Useful, w.Lost = pricing.NormalizeDecimal(w.Useful), pricing.NormalizeDecimal(w.Lost)
		return w, err
	})
	if err != nil {
		return facts, fmt.Errorf("scan cost waves: %w", err)
	}
	rows, err = q.Query(WithOperation(ctx, "costs.calls"), `
SELECT w.code, w.purpose, m.provider, m.name,
       coalesce(sum(mc.cost) FILTER (WHERE NOT mc.lost), 0)::text, coalesce(sum(mc.cost) FILTER (WHERE mc.lost), 0)::text,
       coalesce(sum(mc.cost) FILTER (WHERE mc.lost AND mc.estimated_cost), 0)::text,
       coalesce(sum(mc.input_tokens), 0)::bigint, coalesce(sum(mc.output_tokens), 0)::bigint,
       coalesce(sum(mc.read_cache_tokens), 0)::bigint, coalesce(sum(mc.write_cache_tokens), 0)::bigint,
       coalesce(sum(extract(epoch FROM mc.finished_at - mc.started_at) * 1000), 0)::bigint
  FROM easycode.model_calls mc JOIN easycode.waves w ON w.id = mc.wave_id JOIN easycode.models m ON m.id = mc.model_id
 -- the waves of the project as an array: one scan of model_calls_wave_idx per partition, not a hash join that
 -- reads every call of the organization (RNF-09)
 WHERE mc.wave_id = ANY(ARRAY(SELECT x.id FROM easycode.waves x WHERE x.project_id = $1))
 GROUP BY w.id, w.code, w.purpose, m.provider, m.name ORDER BY w.id, m.provider, m.name`, internalID)
	if err != nil {
		return facts, fmt.Errorf("cost calls: %w", classify(err))
	}
	facts.Calls, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (appcost.CallCost, error) {
		var call appcost.CallCost
		err := row.Scan(&call.Wave, &call.Purpose, &call.Provider, &call.Model, &call.Useful, &call.Lost, &call.Estimated,
			&call.Tokens.Input, &call.Tokens.Output, &call.Tokens.ReadCache, &call.Tokens.WriteCache, &call.DurationMs)
		call.Useful, call.Lost, call.Estimated = pricing.NormalizeDecimal(call.Useful), pricing.NormalizeDecimal(call.Lost), pricing.NormalizeDecimal(call.Estimated)
		return call, err
	})
	if err != nil {
		return facts, fmt.Errorf("scan cost calls: %w", err)
	}
	return facts, nil
}

// PeriodCalls implements appcost.Reader: the calls started in [from, until) of the projects where the person
// has a role (RNF-04), summed by project, purpose and lost or estimated.
func (c Costs) PeriodCalls(ctx context.Context, p identity.Principal, from, until time.Time) (string, []appcost.PeriodCall, error) {
	q := conn(ctx, c.pool)
	var currency string
	if err := q.QueryRow(WithOperation(ctx, "organizations.currency"),
		"SELECT currency FROM easycode.organizations WHERE public_id = $1::uuid", p.OrganizationID).Scan(&currency); err != nil {
		return "", nil, fmt.Errorf("organization currency: %w", classify(err))
	}
	rows, err := q.Query(WithOperation(ctx, "costs.period_calls"), `
SELECT pr.public_id::text, pr.name, w.purpose, mc.lost, mc.estimated_cost, sum(mc.cost)::text
  FROM easycode.projects pr JOIN easycode.waves w ON w.project_id = pr.id JOIN easycode.model_calls mc ON mc.wave_id = w.id
 WHERE pr.deletion_requested_at IS NULL
   AND EXISTS (SELECT 1 FROM easycode.project_members m WHERE m.project_id = pr.id AND m.user_id = `+principalUserIDSQL+`)
   AND mc.started_at >= $3 AND mc.started_at < $4
 GROUP BY pr.public_id, pr.name, w.purpose, mc.lost, mc.estimated_cost`, p.OrganizationID, p.Subject, from, until)
	if err != nil {
		return "", nil, fmt.Errorf("period calls: %w", classify(err))
	}
	calls, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appcost.PeriodCall, error) {
		var call appcost.PeriodCall
		err := row.Scan(&call.ProjectID, &call.ProjectName, &call.Purpose, &call.Lost, &call.Estimated, &call.Cost)
		call.Cost = pricing.NormalizeDecimal(call.Cost)
		return call, err
	})
	if err != nil {
		return "", nil, fmt.Errorf("scan period calls: %w", err)
	}
	return currency, calls, nil
}

// EstimateFacts implements appcost.Reader for the next wave of a stage: the reasoning model of the pair in
// force for the stage (the stage pair wins, US-16), its price in force now, the size of the inputs and the
// history of the stage in the organization; and the latest completed wave of the stage, if any.
func (c Costs) EstimateFacts(ctx context.Context, projectID, stage string) (appcost.EstimateFacts, error) {
	q := conn(ctx, c.pool)
	var facts appcost.EstimateFacts
	var internalID int64
	var modelID *int64
	err := q.QueryRow(WithOperation(ctx, "costs.estimate_project"), `
SELECT pr.id, o.currency,
       (SELECT mp.reasoning_model_id FROM easycode.model_pairs mp WHERE mp.project_id = pr.id AND (mp.stage = $2 OR mp.stage IS NULL)
         ORDER BY mp.stage IS NULL LIMIT 1)
  FROM easycode.projects pr JOIN easycode.organizations o ON o.id = pr.organization_id WHERE pr.public_id = $1::uuid`, projectID, stage).
		Scan(&internalID, &facts.Currency, &modelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return facts, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return facts, fmt.Errorf("estimate project: %w", classify(err))
	}
	last, err := lastCompletedEstimate(ctx, q, internalID, stage)
	if err != nil {
		return facts, err
	}
	facts.LastCompleted = last
	if last != nil {
		return facts, nil // estimated × performed of the last completed wave (US-40 CA-3)
	}
	if c.inForce.AgentOf(stage) == "orchestrator" {
		facts.Orchestrator = true
		return facts, nil
	}
	if modelID == nil {
		facts.PairMissing = true
		return facts, nil
	}
	facts.ReasoningModel = *modelID
	in, err := estimateInputs(ctx, q, internalID, stage, *modelID, 0)
	if err != nil {
		return facts, err
	}
	facts.InputBytes, facts.PriceID, facts.InputPrice, facts.History, facts.PriceMissing = in.bytes, in.priceID, in.inputPrice, in.history, in.priceMissing
	return facts, nil
}

type estimateInput struct {
	bytes        int64
	priceID      string
	inputPrice   string
	priceMissing bool
	history      []pricing.PastWave
}

// estimateInputs reads the inputs of the estimate (US-40): the size of the business need, the constraints in
// force and the approved artifacts of the project (what the agent reads); the price in force of the model; and
// the completed execution waves of the same stage in the organization with a recorded input size, the most
// recent first (except the wave being dispatched).
func estimateInputs(ctx context.Context, q querier, projectID int64, stage string, modelID, exceptWave int64) (estimateInput, error) {
	var in estimateInput
	var priceID, inputPrice *string
	err := q.QueryRow(WithOperation(ctx, "costs.estimate_inputs"), `
SELECT octet_length(pr.business_need)
       + coalesce((SELECT octet_length(c.items::text) FROM easycode.project_constraints c WHERE c.project_id = pr.id ORDER BY c.version DESC LIMIT 1), 0)
       + coalesce((SELECT sum(octet_length(v.header::text) + octet_length(v.body)) FROM easycode.artifacts a
                     JOIN easycode.artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version
                    WHERE a.project_id = pr.id AND a.state = 'approved'), 0),
       p.id::text, p.input_price::text
  FROM easycode.projects pr
  LEFT JOIN LATERAL (SELECT mp.id, mp.input_price FROM easycode.model_prices mp
                      WHERE mp.organization_id = pr.organization_id AND mp.model_id = $2 AND mp.effective_since <= now()
                      ORDER BY mp.effective_since DESC LIMIT 1) p ON true
 WHERE pr.id = $1`, projectID, modelID).Scan(&in.bytes, &priceID, &inputPrice)
	if err != nil {
		return in, fmt.Errorf("estimate inputs: %w", classify(err))
	}
	if priceID == nil {
		in.priceMissing = true
		return in, nil
	}
	in.priceID, in.inputPrice = *priceID, pricing.NormalizeDecimal(*inputPrice)
	rows, err := q.Query(WithOperation(ctx, "costs.estimate_history"), `
SELECT (w.useful_cost + w.lost_cost)::text, (w.estimate_basis->>'input_size_tokens')::bigint
  FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id
 WHERE pr.organization_id = (SELECT organization_id FROM easycode.projects WHERE id = $1)
   AND w.stage = $2 AND w.state = 'completed' AND w.purpose = 'execution' AND w.id <> $4
   AND jsonb_typeof(w.estimate_basis->'input_size_tokens') = 'number'
 ORDER BY w.finished_at DESC LIMIT $3`, projectID, stage, pricing.HistoryLimit, exceptWave)
	if err != nil {
		return in, fmt.Errorf("estimate history: %w", classify(err))
	}
	in.history, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (pricing.PastWave, error) {
		var w pricing.PastWave
		err := row.Scan(&w.Cost, &w.InputSizeTokens)
		w.Cost = pricing.NormalizeDecimal(w.Cost)
		return w, err
	})
	if err != nil {
		return in, fmt.Errorf("scan estimate history: %w", err)
	}
	return in, nil
}

// lastCompletedEstimate is the estimate recorded at the dispatch of the latest completed wave of the stage and
// what it cost (US-40 CA-3); nil before any wave of the stage completes.
func lastCompletedEstimate(ctx context.Context, q querier, projectID int64, stage string) (*appcost.RecordedEstimate, error) {
	var r appcost.RecordedEstimate
	var basis []byte
	err := q.QueryRow(WithOperation(ctx, "costs.estimate_last_completed"), `
SELECT w.code, coalesce(w.cost_estimate, 0)::text, coalesce(w.estimate_basis, '{}'), (w.useful_cost + w.lost_cost)::text
  FROM easycode.waves w WHERE w.project_id = $1 AND w.stage = $2 AND w.state = 'completed'
 ORDER BY w.finished_at DESC LIMIT 1`, projectID, stage).Scan(&r.Wave, &r.Estimated, &basis, &r.Performed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last completed estimate: %w", classify(err))
	}
	_ = json.Unmarshal(basis, &r.Basis) // a wave dispatched before US-40 has {"method":"none"}
	if r.Basis.Method == "" || r.Basis.Method == "none" {
		r.Basis.Method, r.Basis.NoHistory = "none", true
	}
	r.Estimated, r.Performed = pricing.NormalizeDecimal(r.Estimated), pricing.NormalizeDecimal(r.Performed)
	return &r, nil
}

// waveEstimate is the estimate of a queued wave at its dispatch (policy.project_budget: stop_by_estimate),
// with the reasoning model of the wave; a wave without a model (the orchestrator) or without a price in force
// estimates zero (the call itself refuses a model without a price).
func waveEstimate(ctx context.Context, q querier, waveID int64) (string, pricing.EstimateBasis, error) {
	var projectID int64
	var stage string
	var modelID *int64
	err := q.QueryRow(WithOperation(ctx, "waves.estimate_model"), `
SELECT w.project_id, w.stage, (SELECT wm.model_id FROM easycode.wave_models wm WHERE wm.wave_id = w.id AND wm.role = 'reasoning')
  FROM easycode.waves w WHERE w.id = $1`, waveID).Scan(&projectID, &stage, &modelID)
	if err != nil {
		return "", pricing.EstimateBasis{}, fmt.Errorf("wave estimate model: %w", classify(err))
	}
	none := pricing.EstimateBasis{NoHistory: true, Method: "none", InputPrice: "0"}
	if modelID == nil {
		return "0", none, nil
	}
	in, err := estimateInputs(ctx, q, projectID, stage, *modelID, waveID)
	if err != nil {
		return "", pricing.EstimateBasis{}, err
	}
	if in.priceMissing {
		return "0", none, nil
	}
	value, basis := pricing.Estimate(pricing.InputTokens(in.bytes), in.inputPrice, in.priceID, in.history)
	return value, basis, nil
}
