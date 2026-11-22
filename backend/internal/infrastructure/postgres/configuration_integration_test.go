package postgres_test

import (
	"context"
	"testing"

	apppricing "easycode/internal/application/pricing"
	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/pricing"
	"easycode/internal/domain/project"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/specs"
)

func configurationService(t *testing.T, f projectFixture) *appproject.ConfigurationService {
	t.Helper()
	wf, err := specs.BasicWorkflow()
	if err != nil {
		t.Fatal(err)
	}
	api := f.db.PoolAs("easycode_api")
	return appproject.NewConfigurationService(postgres.NewTxRunner(api), f.repo, f.repo, postgres.NewCatalog(api), postgres.NewAuditTrail(api), wf.Definition)
}

func registerModels(t *testing.T, f projectFixture) {
	t.Helper()
	api := f.db.PoolAs("easycode_api")
	prices := apppricing.NewService(postgres.NewTxRunner(api), postgres.NewCatalog(api), postgres.NewAuditTrail(api))
	for name, caps := range map[string]pricing.Capabilities{
		"reasoning-model": {ToolUse: true, ContextTokens: 1_000_000, PromptCache: true},
		"execution-model": {ToolUse: true, ContextTokens: 200_000},
		"no-tools-model":  {ToolUse: false, ContextTokens: 200_000},
	} {
		if _, err := prices.RecordModel(context.Background(), f.dev, pricing.Model{Provider: "anthropic", Name: name, DisplayName: name, Capabilities: caps, Active: true}); err != nil {
			t.Fatal(err)
		}
	}
}

func pair(execution string) *appproject.Pair {
	return &appproject.Pair{
		Reasoning: appproject.ModelRef{Provider: "anthropic", Model: "reasoning-model"},
		Execution: appproject.ModelRef{Provider: "anthropic", Model: execution},
	}
}

// US-16 CA-1 (choose the pair), CA-2 (capability refused by name), CA-3 (stage adjustment),
// If-Match and the Phase 1 human-only gates.
func TestModelPairConfiguration(t *testing.T) {
	f := newProjectFixture(t)
	registerModels(t, f)
	ctx := context.Background()
	view, err := f.service.Create(ctx, f.dev, roomBooking())
	if err != nil {
		t.Fatal(err)
	}
	service := configurationService(t, f)
	config, err := service.Get(ctx, f.dev, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if config.ModelPair != nil || config.Version != 1 || len(config.GateApproval) != 3 || config.GateApproval["gate.requirements"].Mode != "human" {
		t.Fatalf("initial configuration: %+v", config)
	}
	_, err = service.Modify(ctx, f.dev, view.ID, 1, appproject.Change{ModelPair: pair("no-tools-model")})
	if classified := failure.From(err); classified.Code != pricing.CodeModelWithoutCapability || classified.Extensions["missing_capacity"] != "tool_use" {
		t.Fatalf("CA-2: want model-without-capability tool_use, got %v", err)
	}
	config, err = service.Modify(ctx, f.dev, view.ID, 1, appproject.Change{
		ModelPair:   pair("execution-model"),
		PairByStage: map[string]*appproject.Pair{"requirements": pair("reasoning-model")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.Version != 2 || config.ModelPair.Execution.Model != "execution-model" || config.PairByStage["requirements"].Execution.Model != "reasoning-model" {
		t.Fatalf("CA-1/CA-3: %+v", config)
	}
	if _, err := service.Modify(ctx, f.dev, view.ID, 1, appproject.Change{ArtifactLanguage: ptr("en")}); failure.From(err).Code != appproject.CodeFailedPrecondition {
		t.Fatalf("stale If-Match must fail, got %v", err)
	}
	config, err = service.Modify(ctx, f.dev, view.ID, 2, appproject.Change{PairByStage: map[string]*appproject.Pair{"requirements": nil}, ArtifactLanguage: ptr("en")})
	if err != nil || len(config.PairByStage) != 0 || config.ArtifactLanguage != "en" {
		t.Fatalf("null removes the stage pair: %+v %v", config, err)
	}
	if _, err := service.Modify(ctx, f.dev, view.ID, 3, appproject.Change{GateApproval: map[string]string{"gate.implementation": "automatic"}}); failure.From(err).Code != project.CodeApprovalNotAllowed {
		t.Fatalf("Phase 1 gates are human only, got %v", err)
	}
	items := []project.Constraint{{ID: "RESTR-01", Category: "database", Description: "PostgreSQL 17"}, {Category: "integration", Description: "Single sign-on"}}
	config, err = service.Modify(ctx, f.dev, view.ID, 3, appproject.Change{Constraints: &items})
	if err != nil || config.Constraints.Version != 2 || config.Constraints.Items[1].ID != "RESTR-02" {
		t.Fatalf("constraints new version: %+v %v", config.Constraints, err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.audit_records WHERE action IN ('configuration.modify','constraints.modify')"); n != 4 {
		t.Fatalf("RNF-13: configuration changes audited, got %d", n)
	}
}

func ptr[T any](v T) *T { return &v }
