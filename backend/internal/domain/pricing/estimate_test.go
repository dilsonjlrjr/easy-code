package pricing

import "testing"

// US-40 CA-2: without history the estimate is input size × price in force, marked no_history.
func TestEstimateWithoutHistoryIsInputTimesPrice(t *testing.T) {
	value, basis := Estimate(InputTokens(4001), "20", "7", nil)
	if value != "0.02002" || !basis.NoHistory || basis.InputSizeTokens != 1001 || basis.PriceTableVersion != "7" ||
		basis.Method != MethodInputTimesPrice || basis.HistoricalAverage != "" || basis.ConsideredWaves != 0 {
		t.Fatalf("estimate %s %+v", value, basis)
	}
}

// US-40 CA-1: with history the raw cost is scaled by what the past waves really cost against their own
// raw cost; waves without a recorded size do not count.
func TestEstimateWithHistoryScalesByThePastWaves(t *testing.T) {
	history := []PastWave{{Cost: "0.2", InputSizeTokens: 1000}, {Cost: "0.4", InputSizeTokens: 3000}, {Cost: "9", InputSizeTokens: 0}}
	value, basis := Estimate(2000, "20", "7", history)
	// raw past = (1000 + 3000) × 20/M = 0.08; realized 0.6; factor 7.5; raw now 0.04 → 0.3
	if value != "0.3" || basis.NoHistory || basis.ConsideredWaves != 2 || basis.HistoricalAverage != "0.3" || basis.Factor != "7.5" ||
		basis.Method != MethodInputPriceAndHistory {
		t.Fatalf("estimate %s %+v", value, basis)
	}
	free, basis := Estimate(2000, "0", "8", history)
	if free != "0.3" || basis.Factor != "1" {
		t.Fatalf("free model: %s %+v", free, basis)
	}
	var many []PastWave
	for i := 0; i < HistoryLimit+5; i++ {
		many = append(many, PastWave{Cost: "1", InputSizeTokens: 10})
	}
	if _, basis := Estimate(10, "1", "1", many); basis.ConsideredWaves != HistoryLimit {
		t.Fatalf("history limit: %+v", basis)
	}
}

// policy.project_budget stop_by_estimate: spend + estimate above the stop limit.
func TestExceedsBalance(t *testing.T) {
	for _, c := range []struct {
		spend, estimate, limit string
		want                   bool
	}{{"0.04", "0.01", "0.05", false}, {"0.04", "0.0100001", "0.05", true}, {"0", "1", "", false}} {
		if got := ExceedsBalance(c.spend, c.estimate, c.limit); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
	if Balance("0.07", "0.05") != "0" || Balance("0.01", "0.05") != "0.04" {
		t.Fatal("balance")
	}
	if InputTokens(0) != 0 || InputTokens(1) != 1 || InputTokens(8) != 2 {
		t.Fatal("input tokens")
	}
}
