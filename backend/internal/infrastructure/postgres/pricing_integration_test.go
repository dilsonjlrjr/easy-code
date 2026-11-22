package postgres_test

import (
	"context"
	"testing"
	"time"

	apppricing "easycode/internal/application/pricing"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
)

type pricingFixture struct {
	db      *postgrestest.Database
	dev     identity.Principal
	service *apppricing.Service
	catalog postgres.Catalog
}

func newPricingFixture(t *testing.T) pricingFixture {
	t.Helper()
	db := postgrestest.New(t)
	api := db.PoolAs("easycode_api")
	dev := seedDev(t, db)
	catalog := postgres.NewCatalog(api)
	return pricingFixture{db: db, dev: dev, catalog: catalog, service: apppricing.NewService(postgres.NewTxRunner(api), catalog, postgres.NewAuditTrail(api))}
}

func (f pricingFixture) model(t *testing.T, name string, caps pricing.Capabilities) {
	t.Helper()
	if _, err := f.service.RecordModel(context.Background(), f.dev, pricing.Model{Provider: "anthropic", Name: name, DisplayName: "Model " + name, Capabilities: caps, Active: true}); err != nil {
		t.Fatal(err)
	}
}

func newPrice(since time.Time, input string) pricing.Price {
	return pricing.Price{EffectiveSince: since, Currency: "BRL", Source: "official price page, consulted 2026-11-22",
		PerMillion: pricing.PerMillion{Input: input, Output: "100", ReadCache: "2", WriteCache: "25"}}
}

// US-37 CA-1 and CA-2: calls use the period in force at their moment; no period, no price.
func TestPriceInForceFollowsEffectiveDates(t *testing.T) {
	f := newPricingFixture(t)
	ctx := context.Background()
	f.model(t, "model-a", pricing.Capabilities{ToolUse: true, ContextTokens: 200_000})
	project := devProjectID(t, f.db)
	if _, err := f.catalog.CurrentPrice(ctx, project, "anthropic", "model-a", time.Now()); failure.From(err).Code != pricing.CodeModelWithoutCurrentPrice {
		t.Fatalf("CA-2: want model-without-current-price, got %v", err)
	}
	now := time.Now()
	first, err := f.service.RecordPrice(ctx, f.dev, "anthropic", "model-a", newPrice(now, "20"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.RecordPrice(ctx, f.dev, "anthropic", "model-a", newPrice(now.Add(time.Hour), "18.5"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.catalog.CurrentPrice(ctx, project, "anthropic", "model-a", now.Add(time.Second))
	if err != nil || before.ID != first.ID || before.PerMillion.Input != "20" {
		t.Fatalf("CA-1: before the new period the old price holds: %+v %v", before, err)
	}
	after, err := f.catalog.CurrentPrice(ctx, project, "anthropic", "model-a", now.Add(2*time.Hour))
	if err != nil || after.ID != second.ID || after.PerMillion.Input != "18.5" {
		t.Fatalf("CA-1: from the new period on it is used: %+v %v", after, err)
	}
	prices, err := f.service.ListPrices(ctx, f.dev, "anthropic", "model-a")
	if err != nil || len(prices) != 2 || prices[0].ID != second.ID {
		t.Fatalf("list descending: %+v %v", prices, err)
	}
	views, err := f.service.ListModels(ctx, f.dev, apppricing.ModelFilter{Provider: "anthropic"})
	if err != nil || len(views) != 1 || views[0].CurrentPrice == nil || views[0].CurrentPrice.ID != first.ID {
		t.Fatalf("listModels shows the current price: %+v %v", views, err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'price.record'"); n != 2 {
		t.Fatalf("RNF-13: price.record audited, got %d", n)
	}
}

// US-37 CA-3: a period already in force (or used) is never changed; a future unused one is.
func TestOnlyFutureUnusedPeriodsAreCorrected(t *testing.T) {
	f := newPricingFixture(t)
	ctx := context.Background()
	f.model(t, "model-b", pricing.Capabilities{ToolUse: true, ContextTokens: 200_000})
	now := time.Now()
	inForce, err := f.service.RecordPrice(ctx, f.dev, "anthropic", "model-b", newPrice(now, "20"))
	if err != nil {
		t.Fatal(err)
	}
	future, err := f.service.RecordPrice(ctx, f.dev, "anthropic", "model-b", newPrice(now.Add(24*time.Hour), "30"))
	if err != nil {
		t.Fatal(err)
	}
	input := pricing.PerMillion{Input: "25", Output: "100", ReadCache: "2", WriteCache: "25"}
	time.Sleep(10 * time.Millisecond)
	if _, err := f.service.CorrectPrice(ctx, f.dev, "anthropic", "model-b", inForce.ID, inForce.Version(), apppricing.PriceChange{PerMillion: &input}); failure.From(err).Code != pricing.CodeEffectivePeriodInUse {
		t.Fatalf("CA-3: period in force must be refused, got %v", err)
	}
	if _, err := f.service.CorrectPrice(ctx, f.dev, "anthropic", "model-b", future.ID, "stale", apppricing.PriceChange{PerMillion: &input}); failure.From(err).Code != apppricing.CodeFailedPrecondition {
		t.Fatalf("stale If-Match must fail the precondition, got %v", err)
	}
	corrected, err := f.service.CorrectPrice(ctx, f.dev, "anthropic", "model-b", future.ID, future.Version(), apppricing.PriceChange{PerMillion: &input})
	if err != nil || corrected.PerMillion.Input != "25" || corrected.Version() == future.Version() {
		t.Fatalf("future period is corrected with a new version: %+v %v", corrected, err)
	}
	// The database refuses it too, whatever the application checks (RNF-16).
	_, err = f.db.PoolAs("easycode_api").Exec(ctx, "UPDATE easycode.model_prices SET input_price = 1 WHERE id = $1", inForce.ID)
	if err == nil {
		t.Fatal("the trigger must refuse changing a period in force")
	}
	past := newPrice(now.Add(-time.Hour), "1")
	if _, err := f.service.RecordPrice(ctx, f.dev, "anthropic", "model-b", past); failure.From(err).Code != failure.CodeValidation {
		t.Fatalf("a period never starts in the past, got %v", err)
	}
}

func TestRecordModelIsAnIdempotentUpsertForAdministrators(t *testing.T) {
	f := newPricingFixture(t)
	ctx := context.Background()
	f.model(t, "model-c", pricing.Capabilities{ToolUse: false, ContextTokens: 100_000})
	f.model(t, "model-c", pricing.Capabilities{ToolUse: true, ContextTokens: 200_000, ImageInput: true})
	views, err := f.service.ListModels(ctx, f.dev, apppricing.ModelFilter{})
	if err != nil || len(views) != 1 || !views[0].Model.Capabilities.ToolUse || views[0].Model.DisplayName != "Model model-c" {
		t.Fatalf("upsert: %+v %v", views, err)
	}
	member := f.dev
	member.Administrator = false
	if _, err := f.service.RecordModel(ctx, member, views[0].Model); failure.From(err).Kind != failure.KindForbidden {
		t.Fatalf("only administrators record models, got %v", err)
	}
	if _, err := f.service.ListPrices(ctx, f.dev, "anthropic", "unknown-model"); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("unknown model is not found, got %v", err)
	}
}
