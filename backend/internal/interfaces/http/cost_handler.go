package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

	appcost "easycode/internal/application/cost"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// CostUseCases are the cost and time reads (US-38, US-40).
type CostUseCases interface {
	ProjectCosts(ctx context.Context, principal identity.Principal, projectID, groupBy string) (appcost.ProjectCosts, error)
	UserCosts(ctx context.Context, principal identity.Principal, from, until *time.Time) (appcost.UserCosts, error)
	StageEstimate(ctx context.Context, principal identity.Principal, projectID, stage string) (appcost.Estimate, error)
}

type aggregatedCostJSON struct {
	Useful        moneyJSON          `json:"useful"`
	Lost          moneyJSON          `json:"lost"`
	Total         moneyJSON          `json:"total"`
	Rework        moneyJSON          `json:"rework"`
	EstimatedLost moneyJSON          `json:"estimated_lost"`
	PerProvider   []providerCostJSON `json:"per_provider"`
}

func aggregatedCostOf(d appcost.Detailed, currency string) aggregatedCostJSON {
	out := aggregatedCostJSON{Useful: moneyJSON{d.Useful, currency}, Lost: moneyJSON{d.Lost, currency}, Total: moneyJSON{d.Total, currency},
		Rework: moneyJSON{d.Rework, currency}, EstimatedLost: moneyJSON{d.EstimatedLost, currency}, PerProvider: []providerCostJSON{}}
	for _, p := range d.PerProvider {
		out.PerProvider = append(out.PerProvider, providerCostJSON{Provider: p.Provider, Total: moneyJSON{p.Total, currency}})
	}
	return out
}

type timeJSON struct {
	ExecutionMs int64 `json:"execution_ms"`
	CycleMs     int64 `json:"cycle_ms"`
}

type costGroupJSON struct {
	Key    string             `json:"key"`
	Cost   aggregatedCostJSON `json:"cost"`
	Tokens tokensJSON         `json:"tokens"`
	Time   timeJSON           `json:"time"`
	Waves  int                `json:"waves"`
}

type costBudgetJSON struct {
	StopLimit           moneyJSON `json:"stop_limit"`
	Balance             moneyJSON `json:"balance"`
	UsedPercentage      float64   `json:"used_percentage"`
	AlertPercentages    []int     `json:"percentage_alert_limits"`
	LastAlertPercentage *int      `json:"last_alert_percentage"`
}

type reworkJSON struct {
	Waves    int      `json:"waves"`
	Findings []string `json:"findings"`
}

type projectCostsJSON struct {
	GroupedBy string             `json:"grouped_by"`
	Total     aggregatedCostJSON `json:"total"`
	Tokens    tokensJSON         `json:"tokens"`
	Time      timeJSON           `json:"time"`
	Budget    *costBudgetJSON    `json:"budget"`
	Rework    reworkJSON         `json:"rework"`
	Groups    []costGroupJSON    `json:"groups"`
}

func tokensOfCost(t appcost.Tokens) tokensJSON {
	return tokensJSON{Input: t.Input, Output: t.Output, ReadCache: t.ReadCache, WriteCache: t.WriteCache}
}

// getProjectCosts is the aggregated cost and time of the project (US-38 CA-1..CA-3, US-24 CA-3, US-39).
func (a *api) getProjectCosts(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	costs, err := a.deps.Costs.ProjectCosts(RequestContext(rc), principal, projectID, string(rc.QueryArgs().Peek("group_by")))
	if err != nil {
		return err
	}
	c := costs.Currency
	out := projectCostsJSON{GroupedBy: costs.GroupedBy, Total: aggregatedCostOf(costs.Total, c), Tokens: tokensOfCost(costs.Tokens),
		Time: timeJSON(costs.Time), Rework: reworkJSON(costs.Rework), Groups: []costGroupJSON{}}
	if b := costs.Budget; b != nil {
		out.Budget = &costBudgetJSON{StopLimit: moneyJSON{b.StopLimit, c}, Balance: moneyJSON{b.Balance, c}, UsedPercentage: b.UsedPercentage,
			AlertPercentages: b.AlertPercentages, LastAlertPercentage: b.LastAlertPercentage}
	}
	for _, g := range costs.Groups {
		out.Groups = append(out.Groups, costGroupJSON{Key: g.Key, Cost: aggregatedCostOf(g.Cost, c), Tokens: tokensOfCost(g.Tokens),
			Time: timeJSON(g.Time), Waves: g.Waves})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

type largestSpendJSON struct {
	Project string    `json:"project"`
	Name    string    `json:"name"`
	Total   moneyJSON `json:"total"`
}

type userCostsJSON struct {
	From             string             `json:"from"`
	Until            string             `json:"until"`
	Total            aggregatedCostJSON `json:"total"`
	ReworkPercentage float64            `json:"rework_percentage"`
	LargestSpend     []largestSpendJSON `json:"largest_spend"`
}

func queryDate(rc *fasthttp.RequestCtx, name string) (*time.Time, error) {
	raw := string(rc.QueryArgs().Peek(name))
	if raw == "" {
		return nil, nil
	}
	day, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return nil, failure.Invalid(failure.FieldError{Field: name, Message: "date"})
	}
	return &day, nil
}

// getUserCosts is the cost of the projects of the person in a period (home card, PL-02).
func (a *api) getUserCosts(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	from, err := queryDate(rc, "from")
	if err != nil {
		return err
	}
	until, err := queryDate(rc, "until")
	if err != nil {
		return err
	}
	costs, err := a.deps.Costs.UserCosts(RequestContext(rc), principal, from, until)
	if err != nil {
		return err
	}
	out := userCostsJSON{From: costs.From.Format(time.DateOnly), Until: costs.Until.Format(time.DateOnly), Total: aggregatedCostOf(costs.Total, costs.Currency),
		ReworkPercentage: costs.ReworkPercentage, LargestSpend: []largestSpendJSON{}}
	for _, p := range costs.LargestSpend {
		out.LargestSpend = append(out.LargestSpend, largestSpendJSON{Project: p.ProjectID, Name: p.Name, Total: moneyJSON{p.Total, costs.Currency}})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

type estimateBasisJSON struct {
	InputSizeTokens   int64      `json:"input_size_tokens"`
	PriceTableVersion string     `json:"price_table_version"`
	InputPrice        moneyJSON  `json:"input_price_per_million"`
	HistoricalAverage *moneyJSON `json:"historical_average"`
	Factor            *string    `json:"factor"`
	ConsideredWaves   int        `json:"considered_waves"`
	Method            string     `json:"method"`
}

type estimateJSON struct {
	Stage     string            `json:"stage"`
	Estimated moneyJSON         `json:"estimated"`
	NoHistory bool              `json:"no_history"`
	Basis     estimateBasisJSON `json:"basis"`
	Performed *moneyJSON        `json:"performed"`
	Wave      *string           `json:"wave"`
}

// getStageEstimate is the estimate of the stage with the base of the calculation (US-40 CA-1..CA-3).
func (a *api) getStageEstimate(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	e, err := a.deps.Costs.StageEstimate(RequestContext(rc), principal, projectID, PathParam(rc, "stage"))
	if err != nil {
		return err
	}
	c := e.Currency
	price := e.Basis.InputPrice
	if price == "" {
		price = "0"
	}
	out := estimateJSON{Stage: e.Stage, Estimated: moneyJSON{e.Value, c}, NoHistory: e.Basis.NoHistory, Wave: optional(e.Wave),
		Basis: estimateBasisJSON{InputSizeTokens: e.Basis.InputSizeTokens, PriceTableVersion: e.Basis.PriceTableVersion,
			InputPrice: moneyJSON{price, c}, Factor: optional(e.Basis.Factor), ConsideredWaves: e.Basis.ConsideredWaves, Method: e.Basis.Method}}
	if e.Basis.HistoricalAverage != "" {
		out.Basis.HistoricalAverage = &moneyJSON{e.Basis.HistoricalAverage, c}
	}
	if e.Performed != nil {
		out.Performed = &moneyJSON{*e.Performed, c}
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}
