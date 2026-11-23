// Package cost holds the cost and time reads of MARCO-7 (PROJETO 10): the aggregated cost and time of a
// project by stage, agent, model or provider with rework apart (US-38), the cost of the projects of a person
// in a period (US-38, US-50 home card) and the estimate of a stage before it runs (US-40). Values are exact
// decimals in text, summed with big.Rat, never floating point (RNF-16).
package cost

import (
	"context"
	"math/big"
	"slices"
	"sort"
	"time"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
	domainproject "easycode/internal/domain/project"
)

// Groupings of getProjectCosts.
const (
	GroupStage    = "stage"
	GroupAgent    = "agent"
	GroupModel    = "model"
	GroupProvider = "provider"
)

// PurposeRework is the purpose of the waves whose cost is the rework of a finding (US-38 CA-2), the same rule
// as the rework count of the RELEASE (RELEASE.custos).
const PurposeRework = "rework"

// MaxPeriodDays bounds getUserCosts.
const MaxPeriodDays = 366

// Tokens are the tokens of a wave or a group.
type Tokens struct{ Input, Output, ReadCache, WriteCache int64 }

func (t *Tokens) add(o Tokens) {
	t.Input, t.Output, t.ReadCache, t.WriteCache = t.Input+o.Input, t.Output+o.Output, t.ReadCache+o.ReadCache, t.WriteCache+o.WriteCache
}

// Time is the execution time and the cycle time (with human wait, US-10 CA-3) in milliseconds.
type Time struct{ ExecutionMs, CycleMs int64 }

// WaveCost is one wave as the aggregation reads it.
type WaveCost struct {
	Code, Stage, Agent, Purpose string
	Finding                     string // ACH of a rework wave
	Useful, Lost                string
	EstimatedLost               bool
	Tokens                      Tokens
	Time                        Time
}

// CallCost is the cost of the calls of one wave to one model (grouping by model and provider).
type CallCost struct {
	Wave, Purpose   string
	Provider, Model string
	Useful, Lost    string
	Estimated       string // lost cost of calls marked estimated
	Tokens          Tokens
	DurationMs      int64
}

// Detailed is DetailedCost: useful, lost, total, rework, estimated lost and the total per provider.
type Detailed struct {
	Useful, Lost, Rework, EstimatedLost, Total string
	PerProvider                                []ProviderTotal
}

// ProviderTotal is the total of one provider (US-24 CA-3).
type ProviderTotal struct{ Provider, Total string }

// Group is one row of getProjectCosts.
type Group struct {
	Key    string
	Cost   Detailed
	Tokens Tokens
	Time   Time
	Waves  int
}

// Budget is the budget of the project in the costs view (US-39).
type Budget struct {
	StopLimit, Balance  string
	UsedPercentage      float64
	AlertPercentages    []int
	LastAlertPercentage *int
}

// Rework summarizes the rework of the project: how many waves and the findings behind them (ESP-06 3.6).
type Rework struct {
	Waves    int
	Findings []string
}

// ProjectCosts is getProjectCosts.
type ProjectCosts struct {
	GroupedBy string
	Currency  string
	Total     Detailed
	Tokens    Tokens
	Time      Time
	Budget    *Budget
	Groups    []Group
	Rework    Rework
}

// ProjectFacts is what the reader returns for one project.
type ProjectFacts struct {
	Currency         string
	StopLimit        string // "" when none
	AlertPercentages []int
	LastAlert        *int
	Waves            []WaveCost
	Calls            []CallCost
}

// PeriodCall is a call of a project of the person in the period (getUserCosts).
type PeriodCall struct {
	ProjectID, ProjectName string
	Purpose                string
	Cost                   string
	Lost, Estimated        bool
}

// ProjectTotal is the spend of one project in the period.
type ProjectTotal struct{ ProjectID, Name, Total string }

// UserCosts is getUserCosts.
type UserCosts struct {
	From, Until      time.Time
	Currency         string
	Total            Detailed
	ReworkPercentage float64
	LargestSpend     []ProjectTotal
}

// EstimateFacts are the inputs of the estimate of a stage.
type EstimateFacts struct {
	Currency       string
	Orchestrator   bool // the stage of the orchestrator has no model (release): estimate zero
	InputBytes     int64
	PriceID        string
	InputPrice     string
	History        []pricing.PastWave
	LastCompleted  *RecordedEstimate // latest completed wave of the stage (estimated × performed, CA-3)
	PriceMissing   bool              // the reasoning model has no price in force
	PairMissing    bool
	ReasoningModel int64
}

// RecordedEstimate is the estimate recorded at the dispatch of a wave and what it really cost.
type RecordedEstimate struct {
	Wave      string
	Estimated string
	Basis     pricing.EstimateBasis
	Performed string
}

// Estimate is getStageEstimate.
type Estimate struct {
	Stage     string
	Value     string
	Currency  string
	Basis     pricing.EstimateBasis
	Performed *string
	Wave      string // wave of the recorded estimate, "" for a live estimate
}

// Reader reads the facts of the cost views (api role).
type Reader interface {
	ProjectFacts(ctx context.Context, projectID string) (ProjectFacts, error)
	PeriodCalls(ctx context.Context, principal identity.Principal, from, until time.Time) (string, []PeriodCall, error)
	EstimateFacts(ctx context.Context, projectID, stage string) (EstimateFacts, error)
}

// Service implements the cost reads.
type Service struct {
	projects appproject.Repository
	reader   Reader
	stages   []string // stage and activity ids of the workflow in force
	now      func() time.Time
}

// NewService builds the service; stages are the ids of the workflow in force (StageId).
func NewService(projects appproject.Repository, reader Reader, stages []string) *Service {
	return &Service{projects: projects, reader: reader, stages: stages, now: time.Now}
}

func rat(value string) *big.Rat {
	if value == "" {
		return new(big.Rat)
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return new(big.Rat)
	}
	return r
}

func text(r *big.Rat) string { return pricing.NormalizeDecimal(r.FloatString(8)) }

// accumulator sums a DetailedCost exactly.
type accumulator struct {
	useful, lost, rework, estimated *big.Rat
	providers                       map[string]*big.Rat
}

func newAccumulator() *accumulator {
	return &accumulator{useful: new(big.Rat), lost: new(big.Rat), rework: new(big.Rat), estimated: new(big.Rat), providers: map[string]*big.Rat{}}
}

func (a *accumulator) add(useful, lost, estimated string, rework bool) {
	u, l := rat(useful), rat(lost)
	a.useful.Add(a.useful, u)
	a.lost.Add(a.lost, l)
	a.estimated.Add(a.estimated, rat(estimated))
	if rework {
		a.rework.Add(a.rework, new(big.Rat).Add(u, l))
	}
}

func (a *accumulator) provider(name, useful, lost string) {
	total, ok := a.providers[name]
	if !ok {
		total = new(big.Rat)
		a.providers[name] = total
	}
	total.Add(total, rat(useful))
	total.Add(total, rat(lost))
}

func (a *accumulator) detailed() Detailed {
	d := Detailed{Useful: text(a.useful), Lost: text(a.lost), Rework: text(a.rework), EstimatedLost: text(a.estimated),
		Total: text(new(big.Rat).Add(a.useful, a.lost)), PerProvider: []ProviderTotal{}}
	names := make([]string, 0, len(a.providers))
	for name := range a.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		d.PerProvider = append(d.PerProvider, ProviderTotal{Provider: name, Total: text(a.providers[name])})
	}
	return d
}

// ProjectCosts is getProjectCosts (US-38 CA-1..CA-3, US-24 CA-3, US-39): the totals come from the waves and
// every grouping partitions them, so the sum of the groups is the total (RNF-16). Grouping by model or
// provider reads the calls of each wave: each call is of one model; the time of such a group is the time of
// its calls (the cycle time of a wave is not split between its models).
func (s *Service) ProjectCosts(ctx context.Context, principal identity.Principal, projectID, groupBy string) (ProjectCosts, error) {
	if _, err := appproject.Authorize(ctx, s.projects, principal, projectID); err != nil {
		return ProjectCosts{}, err
	}
	if groupBy == "" {
		groupBy = GroupStage
	}
	if !slices.Contains([]string{GroupStage, GroupAgent, GroupModel, GroupProvider}, groupBy) {
		return ProjectCosts{}, failure.Invalid(failure.FieldError{Field: "group_by", Message: "enum"})
	}
	facts, err := s.reader.ProjectFacts(ctx, projectID)
	if err != nil {
		return ProjectCosts{}, err
	}
	return Aggregate(facts, groupBy), nil
}

// Aggregate builds getProjectCosts from the facts of a project.
func Aggregate(facts ProjectFacts, groupBy string) ProjectCosts {
	out := ProjectCosts{GroupedBy: groupBy, Currency: facts.Currency, Groups: []Group{}, Rework: Rework{Findings: []string{}}}
	total := newAccumulator()
	findings := map[string]bool{}
	for _, w := range facts.Waves {
		rework := w.Purpose == PurposeRework
		estimated := ""
		if w.EstimatedLost {
			estimated = w.Lost
		}
		total.add(w.Useful, w.Lost, estimated, rework)
		out.Tokens.add(w.Tokens)
		out.Time.ExecutionMs += w.Time.ExecutionMs
		out.Time.CycleMs += w.Time.CycleMs
		if rework {
			out.Rework.Waves++
			if w.Finding != "" && !findings[w.Finding] {
				findings[w.Finding] = true
				out.Rework.Findings = append(out.Rework.Findings, w.Finding)
			}
		}
	}
	for _, c := range facts.Calls {
		total.provider(c.Provider, c.Useful, c.Lost)
	}
	sort.Strings(out.Rework.Findings)
	out.Total = total.detailed()
	type bucket struct {
		cost  *accumulator
		group Group
		waves map[string]bool
	}
	buckets := map[string]*bucket{}
	order := []string{}
	get := func(key string) *bucket {
		b, ok := buckets[key]
		if !ok {
			b = &bucket{cost: newAccumulator(), group: Group{Key: key}, waves: map[string]bool{}}
			buckets[key] = b
			order = append(order, key)
		}
		return b
	}
	switch groupBy {
	case GroupStage, GroupAgent:
		for _, w := range facts.Waves {
			key := w.Stage
			if groupBy == GroupAgent {
				key = w.Agent
			}
			b := get(key)
			estimated := ""
			if w.EstimatedLost {
				estimated = w.Lost
			}
			b.cost.add(w.Useful, w.Lost, estimated, w.Purpose == PurposeRework)
			b.group.Tokens.add(w.Tokens)
			b.group.Time.ExecutionMs += w.Time.ExecutionMs
			b.group.Time.CycleMs += w.Time.CycleMs
			b.waves[w.Code] = true
		}
		keyOf := make(map[string]string, len(facts.Waves))
		for _, w := range facts.Waves {
			keyOf[w.Code] = w.Stage
			if groupBy == GroupAgent {
				keyOf[w.Code] = w.Agent
			}
		}
		for _, c := range facts.Calls {
			if b, ok := buckets[keyOf[c.Wave]]; ok {
				b.cost.provider(c.Provider, c.Useful, c.Lost)
			}
		}
	default:
		for _, c := range facts.Calls {
			key := c.Provider
			if groupBy == GroupModel {
				key = c.Provider + "/" + c.Model
			}
			b := get(key)
			b.cost.add(c.Useful, c.Lost, c.Estimated, c.Purpose == PurposeRework)
			b.cost.provider(c.Provider, c.Useful, c.Lost)
			b.group.Tokens.add(c.Tokens)
			b.group.Time.ExecutionMs += c.DurationMs
			b.group.Time.CycleMs += c.DurationMs
			b.waves[c.Wave] = true
		}
	}
	for _, key := range order {
		b := buckets[key]
		b.group.Cost, b.group.Waves = b.cost.detailed(), len(b.waves)
		out.Groups = append(out.Groups, b.group)
	}
	if facts.StopLimit != "" {
		spend := out.Total.Total
		used, _ := new(big.Rat).Quo(new(big.Rat).Mul(rat(spend), big.NewRat(100, 1)), rat(facts.StopLimit)).Float64()
		alerts := facts.AlertPercentages
		if alerts == nil {
			alerts = []int{}
		}
		out.Budget = &Budget{StopLimit: facts.StopLimit, Balance: pricing.Balance(spend, facts.StopLimit),
			UsedPercentage: float64(int64(used*100+0.5)) / 100, AlertPercentages: alerts, LastAlertPercentage: facts.LastAlert}
	}
	return out
}

// UserCosts is getUserCosts (PL-02): the calls of the projects where the person has a role, in [from, until)
// by the date of each call; without a period, the current month (UTC: the organization has no time zone in
// the MVP). A period longer than 366 days, or with until before from, is refused.
func (s *Service) UserCosts(ctx context.Context, principal identity.Principal, from, until *time.Time) (UserCosts, error) {
	now := s.now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	if from != nil {
		start = *from
	}
	if until != nil {
		end = *until
	} else if from != nil {
		end = start.AddDate(0, 1, 0)
	}
	if !end.After(start) {
		return UserCosts{}, failure.Invalid(failure.FieldError{Field: "until", Message: "range"})
	}
	if end.Sub(start) > MaxPeriodDays*24*time.Hour {
		return UserCosts{}, failure.Invalid(failure.FieldError{Field: "until", Message: "period"})
	}
	currency, calls, err := s.reader.PeriodCalls(ctx, principal, start, end)
	if err != nil {
		return UserCosts{}, err
	}
	total := newAccumulator()
	perProject := map[string]*big.Rat{}
	names := map[string]string{}
	for _, c := range calls {
		useful, lost, estimated := c.Cost, "", ""
		if c.Lost {
			useful, lost = "", c.Cost
			if c.Estimated {
				estimated = c.Cost
			}
		}
		total.add(useful, lost, estimated, c.Purpose == PurposeRework)
		sum, ok := perProject[c.ProjectID]
		if !ok {
			sum = new(big.Rat)
			perProject[c.ProjectID] = sum
		}
		sum.Add(sum, rat(c.Cost))
		names[c.ProjectID] = c.ProjectName
	}
	out := UserCosts{From: start, Until: end, Currency: currency, Total: total.detailed(), LargestSpend: []ProjectTotal{}}
	out.Total.PerProvider = []ProviderTotal{}
	if sum := new(big.Rat).Add(total.useful, total.lost); sum.Sign() > 0 {
		share, _ := new(big.Rat).Quo(new(big.Rat).Mul(total.rework, big.NewRat(100, 1)), sum).Float64()
		out.ReworkPercentage = float64(int64(share*100+0.5)) / 100
	}
	ids := make([]string, 0, len(perProject))
	for id := range perProject {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if c := perProject[ids[i]].Cmp(perProject[ids[j]]); c != 0 {
			return c > 0
		}
		return ids[i] < ids[j]
	})
	for i, id := range ids {
		if i == 5 {
			break
		}
		out.LargestSpend = append(out.LargestSpend, ProjectTotal{ProjectID: id, Name: names[id], Total: text(perProject[id])})
	}
	return out, nil
}

// StageEstimate is getStageEstimate (US-40): input size × price in force × historical average of the
// completed waves of the same stage in the organization (CA-1); without history only size × price, marked
// no_history (CA-2); once a wave of the stage completed, the estimate recorded at its dispatch beside what it
// really cost (CA-3). A reasoning model without a price in force is 409 model-without-current-price.
func (s *Service) StageEstimate(ctx context.Context, principal identity.Principal, projectID, stage string) (Estimate, error) {
	if _, err := appproject.Authorize(ctx, s.projects, principal, projectID); err != nil {
		return Estimate{}, err
	}
	if !slices.Contains(s.stages, stage) {
		return Estimate{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	facts, err := s.reader.EstimateFacts(ctx, projectID, stage)
	if err != nil {
		return Estimate{}, err
	}
	out := Estimate{Stage: stage, Currency: facts.Currency}
	if last := facts.LastCompleted; last != nil {
		performed := last.Performed
		out.Value, out.Basis, out.Performed, out.Wave = last.Estimated, last.Basis, &performed, last.Wave
		return out, nil
	}
	if facts.Orchestrator {
		out.Value, out.Basis = "0", pricing.EstimateBasis{NoHistory: true, Method: pricing.MethodInputTimesPrice, InputPrice: "0"}
		return out, nil
	}
	if facts.PairMissing {
		return Estimate{}, failure.New(failure.KindConflict, domainproject.CodeIncompleteConfig, nil)
	}
	if facts.PriceMissing {
		return Estimate{}, failure.New(failure.KindConflict, pricing.CodeModelWithoutCurrentPrice, nil)
	}
	out.Value, out.Basis = pricing.Estimate(pricing.InputTokens(facts.InputBytes), facts.InputPrice, facts.PriceID, facts.History)
	return out, nil
}
