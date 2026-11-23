package pricing

import (
	"math/big"
)

// BytesPerToken converts the size of a text into input tokens for the estimate (the same rule as the
// estimated input of a call in flight, RNF-16).
const BytesPerToken = 4

// HistoryLimit is how many past waves of the same stage the historical average considers (most recent first).
const HistoryLimit = 20

// PastWave is a completed wave of the same stage: what it cost and the input size recorded at its dispatch.
type PastWave struct {
	Cost            string // useful + lost, exact decimal
	InputSizeTokens int64
}

// EstimateBasis is the base of the calculation shown with the estimate (US-40 CA-1, CA-2) and recorded in
// waves.estimate_basis.
type EstimateBasis struct {
	InputSizeTokens   int64  `json:"input_size_tokens"`
	PriceTableVersion string `json:"price_table_version"`
	// InputPrice is the price per million input tokens in force (the reasoning model of the pair).
	InputPrice string `json:"input_price"`
	// HistoricalAverage is the mean cost of the considered waves; "" without history.
	HistoricalAverage string `json:"historical_average,omitempty"`
	// Factor is the historical ratio of real cost to input size × price; "" without history.
	Factor          string `json:"factor,omitempty"`
	ConsideredWaves int    `json:"considered_waves"`
	NoHistory       bool   `json:"no_history"`
	Method          string `json:"method"`
}

// Methods of the estimate.
const (
	MethodInputTimesPrice      = "input_times_price"
	MethodInputPriceAndHistory = "input_times_price_times_history"
)

// InputTokens is the size of the inputs in tokens (rounded up).
func InputTokens(bytes int64) int64 {
	if bytes <= 0 {
		return 0
	}
	return (bytes + BytesPerToken - 1) / BytesPerToken
}

func ratOf(value string) *big.Rat {
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return new(big.Rat)
	}
	return r
}

// decimal8 renders a rational with at most 8 decimals (numeric(18,8)), trailing zeros dropped.
func decimal8(r *big.Rat) string {
	return NormalizeDecimal(r.FloatString(8))
}

// Estimate is the simple estimate of PROJETO 10: input size × price in force × historical average. The raw
// cost is the input tokens at the input price per million; with history it is scaled by the ratio between
// what the past waves of the stage really cost and their own raw cost at the same price, so the estimate
// follows the stage (outputs, retries, several calls). Without history it is the raw cost alone, marked
// no_history (US-40 CA-2). Past waves without a recorded input size do not count.
func Estimate(inputTokens int64, inputPrice, priceVersion string, history []PastWave) (string, EstimateBasis) {
	basis := EstimateBasis{InputSizeTokens: inputTokens, PriceTableVersion: priceVersion, InputPrice: NormalizeDecimal(inputPrice),
		NoHistory: true, Method: MethodInputTimesPrice}
	perToken := new(big.Rat).Quo(ratOf(inputPrice), big.NewRat(1_000_000, 1))
	raw := new(big.Rat).Mul(big.NewRat(inputTokens, 1), perToken)
	realized, rawPast := new(big.Rat), new(big.Rat)
	considered := 0
	for _, w := range history {
		if w.InputSizeTokens <= 0 || considered == HistoryLimit {
			continue
		}
		considered++
		realized.Add(realized, ratOf(w.Cost))
		rawPast.Add(rawPast, new(big.Rat).Mul(big.NewRat(w.InputSizeTokens, 1), perToken))
	}
	if considered == 0 {
		return decimal8(raw), basis
	}
	basis.NoHistory, basis.Method, basis.ConsideredWaves = false, MethodInputPriceAndHistory, considered
	basis.HistoricalAverage = decimal8(new(big.Rat).Quo(realized, big.NewRat(int64(considered), 1)))
	if rawPast.Sign() == 0 {
		// A free model has no raw cost to scale: the history alone is the estimate.
		basis.Factor = "1"
		return basis.HistoricalAverage, basis
	}
	factor := new(big.Rat).Quo(realized, rawPast)
	basis.Factor = decimal8(factor)
	return decimal8(new(big.Rat).Mul(raw, factor)), basis
}

// ExceedsBalance reports whether spend + estimate > stop limit (policy.project_budget, stop_by_estimate).
func ExceedsBalance(spend, estimate, stopLimit string) bool {
	if stopLimit == "" {
		return false
	}
	total := new(big.Rat).Add(ratOf(spend), ratOf(estimate))
	return total.Cmp(ratOf(stopLimit)) > 0
}

// Balance is stop limit − spend, never negative.
func Balance(spend, stopLimit string) string {
	b := new(big.Rat).Sub(ratOf(stopLimit), ratOf(spend))
	if b.Sign() < 0 {
		return "0"
	}
	return decimal8(b)
}
