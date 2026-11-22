package pricing_test

import (
	"testing"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/pricing"
)

func validPrice(now time.Time) pricing.Price {
	return pricing.Price{
		EffectiveSince: now.Add(time.Hour), Currency: "BRL", Source: "official price page, 2026-11-22",
		PerMillion: pricing.PerMillion{Input: "20", Output: "100.5", ReadCache: "2", WriteCache: "25.12345678"},
	}
}

func TestValidatePriceAcceptsExactDecimalsInTheOrganizationCurrency(t *testing.T) {
	now := time.Now()
	if err := pricing.ValidatePrice(validPrice(now), "BRL", now); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePriceRefusesFloatsOtherCurrencyAndPast(t *testing.T) {
	now := time.Now()
	price := validPrice(now)
	price.PerMillion.Input = "1e3"
	price.PerMillion.Output = "0.123456789"
	price.Currency = "USD"
	price.EffectiveSince = now.Add(-time.Hour)
	err := failure.From(pricing.ValidatePrice(price, "BRL", now))
	if err.Code != failure.CodeValidation || len(err.Fields) != 4 {
		t.Fatalf("want 4 field errors, got %+v", err)
	}
}

func TestMissingReportsTheFirstLackingCapacity(t *testing.T) {
	model := pricing.Model{Capabilities: pricing.Capabilities{ToolUse: false, ContextTokens: 200_000}}
	req := pricing.Requirement{Capacities: []pricing.Capacity{pricing.CapacityToolUse}, MinContextTokens: 128_000}
	if got := model.Missing(req); got != pricing.CapacityToolUse {
		t.Fatalf("want tool_use, got %q", got)
	}
	model.Capabilities = pricing.Capabilities{ToolUse: true, ContextTokens: 64_000}
	if got := model.Missing(req); got != pricing.CapacityContextTokens {
		t.Fatalf("want context_tokens, got %q", got)
	}
	model.Capabilities.ContextTokens = 1_000_000
	if got := model.Missing(req); got != "" {
		t.Fatalf("want none, got %q", got)
	}
}

func TestNormalizeDecimal(t *testing.T) {
	for in, want := range map[string]string{"3.50000000": "3.5", "20.00000000": "20", "0.00000001": "0.00000001", "7": "7"} {
		if got := pricing.NormalizeDecimal(in); got != want {
			t.Errorf("%s: got %s", in, got)
		}
	}
}
