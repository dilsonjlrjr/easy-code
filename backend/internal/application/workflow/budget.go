package workflow

import (
	"context"
	"math/big"

	"github.com/google/uuid"

	"easycode/internal/domain/pricing"
	"easycode/internal/domain/project"
)

// CheckBudget applies policy.project_budget to a project in progress (US-39, RNF-16): when the spend
// reaches the stop limit the project is paused by the budget (exhaust_budget) and its running waves are
// asked to pause at their next checkpoint, cooperatively (the call in flight is never aborted); when the
// spend crosses an alert limit not emitted yet, the alert is recorded and emitted (alert_budget), and the
// state does not change. Call it in its own transaction (it locks the project row).
func (o *Orchestrator) CheckBudget(ctx context.Context, projectID, correlation string) error {
	if o.lifecycle == nil {
		return nil
	}
	state, err := o.store.LockProject(ctx, projectID)
	if err != nil || state != project.StateInProgress {
		return err
	}
	status, err := o.lifecycle.Status(ctx, projectID)
	if err != nil || status.StopLimit == "" {
		return err
	}
	if correlation == "" {
		correlation = newCorrelation()
	}
	emit := o.emitter(ctx, projectID, correlation)
	if !status.Balance {
		if err := emit("br.easycode.project.budget_exhausted.v1", "project", "", map[string]any{
			"project": projectID, "actor": orchestratorActor, "cause": "spend_reached_limit",
			"spend": money(status.Spend, status.Currency), "stop_limit": money(status.StopLimit, status.Currency),
			"transition": map[string]any{"machine": "project", "from_state": project.StateInProgress, "to_state": project.StatePaused},
		}); err != nil {
			return err
		}
		return o.pause(ctx, projectID, correlation, PauseBudget, WaveCauseBudgetExhausted, orchestratorActor)
	}
	percentages, last, err := o.lifecycle.Alerts(ctx, projectID)
	if err != nil {
		return err
	}
	crossed := 0
	for _, p := range percentages {
		if p > last && p > crossed && reached(status.Spend, status.StopLimit, p) {
			crossed = p
		}
	}
	if crossed == 0 {
		return nil
	}
	if err := o.lifecycle.RecordAlert(ctx, projectID, crossed); err != nil {
		return err
	}
	return emit("br.easycode.project.budget_alerted.v1", "project", "", map[string]any{
		"project": projectID, "actor": orchestratorActor, "spend": money(status.Spend, status.Currency),
		"alert_limit": money(percentOf(status.StopLimit, crossed), status.Currency), "stop_limit": money(status.StopLimit, status.Currency),
		"transition": map[string]any{"machine": "project", "from_state": project.StateInProgress, "to_state": project.StateInProgress},
	})
}

// StopByEstimate applies stop_by_estimate of policy.project_budget at the dispatch of a wave (US-40): when the
// spend plus the estimate of the next wave exceeds the stop limit, the project is paused by the budget
// (exhaust_budget, cause estimate_exceeds_balance) before the wave runs; it stays queued, and only a human
// resumes the project after raising the limit (PD-02). Call it in its own transaction (it locks the project).
func (o *Orchestrator) StopByEstimate(ctx context.Context, projectID, estimate, correlation string) error {
	if o.lifecycle == nil {
		return nil
	}
	state, err := o.store.LockProject(ctx, projectID)
	if err != nil || state != project.StateInProgress {
		return err
	}
	status, err := o.lifecycle.Status(ctx, projectID)
	if err != nil || status.StopLimit == "" {
		return err
	}
	if !pricing.ExceedsBalance(status.Spend, estimate, status.StopLimit) {
		return nil
	}
	if correlation == "" {
		correlation = newCorrelation()
	}
	emit := o.emitter(ctx, projectID, correlation)
	if err := emit("br.easycode.project.budget_exhausted.v1", "project", "", map[string]any{
		"project": projectID, "actor": orchestratorActor, "cause": "estimate_exceeds_balance",
		"spend": money(status.Spend, status.Currency), "stop_limit": money(status.StopLimit, status.Currency),
		"next_wave_estimate": money(estimate, status.Currency),
		"transition":         map[string]any{"machine": "project", "from_state": project.StateInProgress, "to_state": project.StatePaused},
	}); err != nil {
		return err
	}
	return o.pause(ctx, projectID, correlation, PauseBudget, WaveCauseBudgetExhausted, orchestratorActor)
}

// CheckBudgets runs CheckBudget on every project whose spend reached a limit (reconcile of the worker: the
// lead lowered the limit below the spend, PL-67, or a wave ended right at the limit).
func (o *Orchestrator) CheckBudgets(ctx context.Context, inTx func(context.Context, func(context.Context) error) error) (int, error) {
	if o.lifecycle == nil {
		return 0, nil
	}
	candidates, err := o.lifecycle.BudgetCandidates(ctx)
	if err != nil {
		return 0, err
	}
	for _, projectID := range candidates {
		if err := inTx(ctx, func(ctx context.Context) error { return o.CheckBudget(ctx, projectID, "") }); err != nil {
			return 0, err
		}
	}
	return len(candidates), nil
}

func rat(value string) *big.Rat {
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return new(big.Rat)
	}
	return r
}

// reached reports whether spend ≥ limit × percentage / 100 (exact decimals, RNF-16).
func reached(spend, limit string, percentage int) bool {
	threshold := new(big.Rat).Mul(rat(limit), big.NewRat(int64(percentage), 100))
	return rat(spend).Cmp(threshold) >= 0
}

func percentOf(limit string, percentage int) string {
	return new(big.Rat).Mul(rat(limit), big.NewRat(int64(percentage), 100)).FloatString(4)
}

func addDecimal(a, b string) string {
	return new(big.Rat).Add(rat(a), rat(b)).FloatString(8)
}

func newCorrelation() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}
