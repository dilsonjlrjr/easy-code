package cost

import (
	"context"
	"testing"
	"time"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
)

// projects gives a role in every project except "other" (RNF-04: no role is not found).
type projects struct{ appproject.Repository }

func (projects) Access(_ context.Context, _ identity.Principal, projectID string) (appproject.Access, error) {
	if projectID == "other" {
		return appproject.Access{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return appproject.Access{ProjectID: projectID, Roles: []identity.Role{identity.RoleMember}}, nil
}

type reader struct {
	facts    ProjectFacts
	calls    []PeriodCall
	estimate EstimateFacts
	from     time.Time
	until    time.Time
}

func (r *reader) ProjectFacts(context.Context, string) (ProjectFacts, error) { return r.facts, nil }
func (r *reader) PeriodCalls(_ context.Context, _ identity.Principal, from, until time.Time) (string, []PeriodCall, error) {
	r.from, r.until = from, until
	return "BRL", r.calls, nil
}
func (r *reader) EstimateFacts(context.Context, string, string) (EstimateFacts, error) {
	return r.estimate, nil
}

func referenceFacts() ProjectFacts {
	return ProjectFacts{Currency: "BRL", StopLimit: "1", AlertPercentages: []int{50, 80},
		Waves: []WaveCost{
			{Stage: "requirements", Agent: "requirements-agent", Purpose: "execution", Count: 1, Useful: "0.076", Lost: "0",
				Tokens: Tokens{Input: 1800, Output: 400}, Time: Time{ExecutionMs: 1000, CycleMs: 61000}},
			{Stage: "requirements", Agent: "requirements-agent", Purpose: "rework", Finding: "ACH-01", Count: 1, Useful: "0.038", Lost: "0.01",
				Estimated: "0.01", Tokens: Tokens{Input: 900, Output: 200}, Time: Time{ExecutionMs: 500, CycleMs: 900}},
			{Stage: "architecture", Agent: "architect-agent", Purpose: "execution", Count: 1, Useful: "0.2", Lost: "0",
				Tokens: Tokens{Input: 5000}, Time: Time{ExecutionMs: 2000, CycleMs: 3000}},
		},
		Calls: []CallCost{
			{Stage: "requirements", Agent: "requirements-agent", Purpose: "execution", Provider: "anthropic", Model: "r", Waves: 1, Useful: "0.076",
				Tokens: Tokens{Input: 1800, Output: 400}, DurationMs: 900},
			{Stage: "requirements", Agent: "requirements-agent", Purpose: "rework", Provider: "anthropic", Model: "r", Waves: 1, Useful: "0.038",
				Lost: "0.01", Estimated: "0.01", Tokens: Tokens{Input: 900, Output: 200}, DurationMs: 400},
			{Stage: "architecture", Agent: "architect-agent", Purpose: "execution", Provider: "openai", Model: "e", Waves: 1, Useful: "0.2",
				Tokens: Tokens{Input: 5000}, DurationMs: 1500},
			{Stage: "requirements", Agent: "requirements-agent", Purpose: "execution", Provider: "anthropic", Waves: 1},
			{Stage: "requirements", Agent: "requirements-agent", Purpose: "rework", Provider: "anthropic", Waves: 1},
			{Stage: "architecture", Agent: "architect-agent", Purpose: "execution", Provider: "openai", Waves: 1},
		}}
}

// US-38 CA-1: in every grouping the groups add up to the total of the waves; CA-2: rework apart;
// CA-3: cycle and execution time per stage; US-24 CA-3: per provider; US-39: the budget.
func TestProjectCostsGroupsAddUpToTheTotal(t *testing.T) {
	s := NewService(projects{}, &reader{facts: referenceFacts()}, []string{"requirements"})
	for _, groupBy := range []string{"", GroupAgent, GroupModel, GroupProvider} {
		out, err := s.ProjectCosts(context.Background(), identity.Principal{}, "p", groupBy)
		if err != nil {
			t.Fatal(err)
		}
		if out.Total.Total != "0.324" || out.Total.Useful != "0.314" || out.Total.Lost != "0.01" || out.Total.Rework != "0.048" ||
			out.Total.EstimatedLost != "0.01" || len(out.Total.PerProvider) != 2 || out.Total.PerProvider[1].Total != "0.2" {
			t.Fatalf("%s total: %+v", groupBy, out.Total)
		}
		sum := new(rationals)
		for _, g := range out.Groups {
			sum.add(g.Cost.Total)
		}
		if sum.String() != out.Total.Total {
			t.Fatalf("%s: groups add up to %s, total %s", groupBy, sum, out.Total.Total)
		}
	}
	stage, _ := s.ProjectCosts(context.Background(), identity.Principal{}, "p", GroupStage)
	req := stage.Groups[0]
	if stage.GroupedBy != GroupStage || len(stage.Groups) != 2 || req.Key != "requirements" || req.Waves != 2 || req.Cost.Rework != "0.048" ||
		req.Time.CycleMs != 61900 || req.Time.ExecutionMs != 1500 || req.Tokens.Input != 2700 {
		t.Fatalf("by stage: %+v", stage.Groups)
	}
	if stage.Rework.Waves != 1 || len(stage.Rework.Findings) != 1 || stage.Rework.Findings[0] != "ACH-01" {
		t.Fatalf("rework: %+v", stage.Rework)
	}
	if b := stage.Budget; b == nil || b.Balance != "0.676" || b.UsedPercentage != 32.4 || len(b.AlertPercentages) != 2 {
		t.Fatalf("budget: %+v", stage.Budget)
	}
	model, _ := s.ProjectCosts(context.Background(), identity.Principal{}, "p", GroupModel)
	if model.Groups[0].Key != "anthropic/r" || model.Groups[0].Waves != 2 || model.Groups[0].Time.ExecutionMs != 1300 {
		t.Fatalf("by model: %+v", model.Groups)
	}
	provider, _ := s.ProjectCosts(context.Background(), identity.Principal{}, "p", GroupProvider)
	if provider.Groups[0].Key != "anthropic" || provider.Groups[0].Waves != 2 || provider.Groups[1].Waves != 1 {
		t.Fatalf("by provider: %+v", provider.Groups)
	}
	if _, err := s.ProjectCosts(context.Background(), identity.Principal{}, "p", "week"); failure.From(err).Code != failure.CodeValidation {
		t.Fatalf("bad grouping: %v", err)
	}
	if _, err := s.ProjectCosts(context.Background(), identity.Principal{}, "other", ""); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("RNF-04: %v", err)
	}
	empty := Aggregate(ProjectFacts{Currency: "BRL"}, GroupStage)
	if empty.Total.Total != "0" || empty.Budget != nil || len(empty.Groups) != 0 {
		t.Fatalf("no waves: %+v", empty)
	}
}

type rationals struct{ values []string }

func (r *rationals) add(v string) { r.values = append(r.values, v) }
func (r *rationals) String() string {
	a := newAccumulator()
	for _, v := range r.values {
		a.add(v, "", "", false)
	}
	return a.detailed().Total
}

// PL-02: the month by default, the rework share and the projects of largest spend; periods are bounded.
func TestUserCostsOfThePeriod(t *testing.T) {
	r := &reader{calls: []PeriodCall{
		{ProjectID: "a", ProjectName: "A", Purpose: "execution", Cost: "1"},
		{ProjectID: "b", ProjectName: "B", Purpose: "rework", Cost: "3"},
		{ProjectID: "b", ProjectName: "B", Purpose: "execution", Cost: "1", Lost: true, Estimated: true},
	}}
	s := NewService(projects{}, r, nil)
	s.now = func() time.Time { return time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC) }
	out, err := s.UserCosts(context.Background(), identity.Principal{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.from.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || !r.until.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("period %s %s", r.from, r.until)
	}
	if out.Total.Total != "5" || out.Total.Lost != "1" || out.Total.EstimatedLost != "1" || out.ReworkPercentage != 60 ||
		len(out.LargestSpend) != 2 || out.LargestSpend[0].ProjectID != "b" || out.LargestSpend[0].Total != "4" {
		t.Fatalf("user costs: %+v", out)
	}
	from, until := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC)
	if _, err := s.UserCosts(context.Background(), identity.Principal{}, &from, &until); failure.From(err).Code != failure.CodeValidation {
		t.Fatalf("period over 366 days: %v", err)
	}
	if _, err := s.UserCosts(context.Background(), identity.Principal{}, &until, &from); failure.From(err).Code != failure.CodeValidation {
		t.Fatalf("until before from: %v", err)
	}
}

// US-40: the live estimate, the recorded one beside what it cost, and the refusals.
func TestStageEstimate(t *testing.T) {
	r := &reader{estimate: EstimateFacts{Currency: "BRL", InputBytes: 4000, PriceID: "3", InputPrice: "20"}}
	s := NewService(projects{}, r, []string{"requirements", "release"})
	ctx := context.Background()
	live, err := s.StageEstimate(ctx, identity.Principal{}, "p", "requirements")
	if err != nil || live.Value != "0.02" || !live.Basis.NoHistory || live.Performed != nil || live.Basis.PriceTableVersion != "3" {
		t.Fatalf("CA-2: %+v %v", live, err)
	}
	r.estimate.History = []pricing.PastWave{{Cost: "0.2", InputSizeTokens: 1000}}
	if h, _ := s.StageEstimate(ctx, identity.Principal{}, "p", "requirements"); h.Basis.NoHistory || h.Value != "0.2" || h.Basis.ConsideredWaves != 1 {
		t.Fatalf("CA-1: %+v", h)
	}
	r.estimate.LastCompleted = &RecordedEstimate{Wave: "REQ-01", Estimated: "0.02", Performed: "0.076", Basis: pricing.EstimateBasis{InputSizeTokens: 1000}}
	if done, _ := s.StageEstimate(ctx, identity.Principal{}, "p", "requirements"); done.Performed == nil || *done.Performed != "0.076" || done.Value != "0.02" || done.Wave != "REQ-01" {
		t.Fatalf("CA-3: %+v", done)
	}
	r.estimate.LastCompleted = nil
	r.estimate.PriceMissing = true
	if _, err := s.StageEstimate(ctx, identity.Principal{}, "p", "requirements"); failure.From(err).Code != pricing.CodeModelWithoutCurrentPrice {
		t.Fatalf("no price: %v", err)
	}
	if _, err := s.StageEstimate(ctx, identity.Principal{}, "p", "nowhere"); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("unknown stage: %v", err)
	}
	r.estimate = EstimateFacts{Orchestrator: true, Currency: "BRL"}
	if rel, err := s.StageEstimate(ctx, identity.Principal{}, "p", "release"); err != nil || rel.Value != "0" {
		t.Fatalf("release: %+v %v", rel, err)
	}
	if _, err := s.StageEstimate(ctx, identity.Principal{}, "other", "requirements"); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("RNF-04: %v", err)
	}
}
