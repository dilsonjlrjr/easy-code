package postgres_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	apppricing "easycode/internal/application/pricing"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/pricing"
	"easycode/internal/infrastructure/postgres"
)

// ADR-23 (0020): a claude-code model without an own price uses the price in force of the anthropic model of
// the same name, in the listing, in the start readiness, in the worker lookup and in the cost of the call; an
// own price of the claude-code model is the exception and wins.
func TestClaudeCodeInheritsTheAnthropicPrice(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := context.Background()
	api := f.db.PoolAs("easycode_api")
	prices := apppricing.NewService(postgres.NewTxRunner(api), postgres.NewCatalog(api), postgres.NewAuditTrail(api))
	caps := pricing.Capabilities{ToolUse: true, ContextTokens: 200_000}
	for _, m := range []pricing.Model{{Provider: "anthropic", Name: "model-z"}, {Provider: "claude-code", Name: "model-z"}, {Provider: "claude-code", Name: "model-solo"}} {
		m.DisplayName, m.Capabilities, m.Active = m.Name, caps, true
		if _, err := prices.RecordModel(ctx, f.dev, m); err != nil {
			t.Fatal(err)
		}
	}
	anthropic, err := prices.RecordPrice(ctx, f.dev, "anthropic", "model-z", newPrice(time.Now(), "3"))
	if err != nil {
		t.Fatal(err)
	}
	modelID := func(provider, name string) int64 {
		var id int64
		if err := f.db.Admin.QueryRow(ctx, "SELECT id FROM easycode.models WHERE provider = $1 AND name = $2", provider, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	inherits, solo := modelID("claude-code", "model-z"), modelID("claude-code", "model-solo")
	ledger := postgres.NewLedger(f.db.PoolAs("easycode_worker"))
	later := time.Now().Add(time.Second)

	got, err := ledger.CurrentPriceID(ctx, f.projectID, inherits, later)
	if err != nil || got != anthropic.ID {
		t.Fatalf("worker lookup inherits the anthropic price %d: %d %v", anthropic.ID, got, err)
	}
	if _, err := ledger.CurrentPriceID(ctx, f.projectID, solo, later); failure.From(err).Code != pricing.CodeModelWithoutCurrentPrice {
		t.Fatalf("no anthropic twin, no price: %v", err)
	}
	views, err := prices.ListModels(ctx, f.dev, apppricing.ModelFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		switch v.Model.Provider + "/" + v.Model.Name {
		case "claude-code/model-z":
			if v.CurrentPrice == nil || v.CurrentPrice.ID != anthropic.ID || !v.PriceInherited {
				t.Fatalf("listing shows the inherited price: %+v", v)
			}
		case "anthropic/model-z":
			if v.CurrentPrice == nil || v.PriceInherited {
				t.Fatalf("anthropic keeps its own price: %+v", v)
			}
		case "claude-code/model-solo":
			if v.CurrentPrice != nil {
				t.Fatalf("no twin, no price: %+v", v)
			}
		}
	}

	// The start readiness takes the inherited price for a claude-code pair; a model without price still blocks.
	setPair := func(model int64) {
		if _, err := f.db.Admin.Exec(ctx, `UPDATE easycode.model_pairs SET reasoning_model_id = $2, execution_model_id = $2
  WHERE project_id = (SELECT id FROM easycode.projects WHERE public_id = $1::uuid)`, f.projectID, model); err != nil {
			t.Fatal(err)
		}
	}
	setPair(inherits)
	if missing, err := f.views.Missing(ctx, f.projectID, later); err != nil || slices.Contains(missing, appworkflow.MissingCurrentPrice) {
		t.Fatalf("inherited price is a price in force: %v %v", missing, err)
	}
	setPair(solo)
	if missing, err := f.views.Missing(ctx, f.projectID, later); err != nil || !slices.Contains(missing, appworkflow.MissingCurrentPrice) {
		t.Fatalf("a model without price blocks the start: %v %v", missing, err)
	}

	// The database computes the cost of a claude-code call with the inherited price, and refuses a price of
	// an unrelated model (RNF-16).
	var wave string
	if err := f.db.Admin.QueryRow(ctx, "SELECT public_id::text FROM easycode.waves WHERE code = 'REQ-01'").Scan(&wave); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	call, err := ledger.OpenCall(ctx, wave, 1, 1, inherits, anthropic.ID, "execution", hash, 1_000_000)
	if err != nil {
		t.Fatalf("a claude-code call with the inherited price opens: %v", err)
	}
	var cost string
	if err := f.db.Admin.QueryRow(ctx, "SELECT cost::text FROM easycode.model_calls WHERE id = $1", call).Scan(&cost); err != nil || !strings.HasPrefix(cost, "3.") {
		t.Fatalf("cost by the inherited price (1M input tokens × 3): %q %v", cost, err)
	}
	if _, err := ledger.OpenCall(ctx, wave, 1, 2, solo, anthropic.ID, "execution", hash, 10); err == nil {
		t.Fatal("the price of another model is refused by the database")
	}

	// An own price of the claude-code model is the exception and wins over the inherited one.
	own, err := prices.RecordPrice(ctx, f.dev, "claude-code", "model-z", newPrice(time.Now(), "1"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ledger.CurrentPriceID(ctx, f.projectID, inherits, time.Now().Add(time.Second)); err != nil || got != own.ID {
		t.Fatalf("own price wins: %d %v (own %d)", got, err, own.ID)
	}
}
