package postgres_test

import (
	"context"
	"testing"
	"time"

	"easycode/internal/application/command"
	apppricing "easycode/internal/application/pricing"
	appproject "easycode/internal/application/project"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/project"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/specs"
)

type startFixture struct {
	projectFixture
	projectID  string
	workflow   *appworkflow.Service
	config     *appproject.ConfigurationService
	dispatcher *command.Dispatcher
	views      postgres.Workflow
}

func newStartFixture(t *testing.T) startFixture {
	t.Helper()
	f := newProjectFixture(t)
	registerModels(t, f)
	ctx := context.Background()
	view, err := f.service.Create(ctx, f.dev, roomBooking())
	if err != nil {
		t.Fatal(err)
	}
	wf, err := specs.BasicWorkflow()
	if err != nil {
		t.Fatal(err)
	}
	api := f.db.PoolAs("easycode_api")
	worker := f.db.PoolAs("easycode_worker")
	apiWorkflow := postgres.NewWorkflow(api, wf.Definition)
	workerWorkflow := postgres.NewWorkflow(worker, wf.Definition)
	dispatcher := command.NewDispatcher(postgres.NewTxRunner(worker), postgres.NewCommands(worker))
	agents := func(agent string) (appworkflow.AgentConfiguration, error) {
		defaults, err := specs.DefaultAgent(agent)
		return appworkflow.AgentConfiguration{Agent: agent, InstructionVersion: defaults.InstructionVersion,
			InstructionHash: defaults.InstructionHash, Tools: defaults.Tools, Limits: defaults.Limits}, err
	}
	orchestrator := appworkflow.NewOrchestrator(workerWorkflow, workerWorkflow, postgres.NewOutbox(worker), agents, wf.Definition, wf.JSON, wf.Hash)
	dispatcher.Register("start", orchestrator.Start)
	return startFixture{
		projectFixture: f, projectID: view.ID, dispatcher: dispatcher, views: apiWorkflow, config: configurationService(t, f),
		workflow: appworkflow.NewService(postgres.NewTxRunner(api), f.repo, apiWorkflow, apiWorkflow, apiWorkflow, postgres.NewAuditTrail(api)),
	}
}

func (f startFixture) start(t *testing.T, key string) (appworkflow.CommandView, error) {
	t.Helper()
	return f.workflow.Start(context.Background(), f.dev, f.projectID, key, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
}

func (f startFixture) activeKey(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var id int64
	err := f.db.Admin.QueryRow(ctx, `
INSERT INTO easycode.provider_keys (organization_id, provider, encrypted_value, encryption_key_ref, last4, created_by)
SELECT u.organization_id, 'anthropic', '\x01'::bytea, 'x25519:test', 'abcd', u.id FROM easycode.users u WHERE u.sub = 'dev' RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Admin.Exec(ctx, "UPDATE easycode.provider_keys SET state = 'active', tested_at = now(), test_result = 'valid' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
}

func (f startFixture) price(t *testing.T, models ...string) {
	t.Helper()
	api := f.db.PoolAs("easycode_api")
	prices := apppricing.NewService(postgres.NewTxRunner(api), postgres.NewCatalog(api), postgres.NewAuditTrail(api))
	for _, model := range models {
		if _, err := prices.RecordPrice(context.Background(), f.dev, "anthropic", model, newPrice(time.Now(), "20")); err != nil {
			t.Fatal(err)
		}
	}
}

func missing(err error) []string {
	values, _ := failure.From(err).Extensions["missing"].([]string)
	return values
}

// US-05 CA-2, US-37 CA-2: the start lists what is missing; with everything set it is accepted.
func TestStartRefusedWithTheListOfWhatIsMissing(t *testing.T) {
	f := newStartFixture(t)
	ctx := context.Background()
	_, err := f.start(t, "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9c01")
	if got := missing(err); failure.From(err).Code != project.CodeIncompleteConfig || len(got) != 2 || got[0] != "provider_key" || got[1] != "model_pair" {
		t.Fatalf("CA-2: want provider_key and model_pair, got %v %v", got, err)
	}
	if _, err := f.config.Modify(ctx, f.dev, f.projectID, 1, appproject.Change{ModelPair: pair("execution-model")}); err != nil {
		t.Fatal(err)
	}
	f.activeKey(t)
	_, err = f.start(t, "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9c02")
	if got := missing(err); len(got) != 1 || got[0] != "current_price" {
		t.Fatalf("US-37 CA-2: model without current price, got %v %v", got, err)
	}
	f.price(t, "reasoning-model", "execution-model")
	cmd, err := f.start(t, "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9c03")
	if err != nil || cmd.State != command.StatePending || cmd.Type != "start" {
		t.Fatalf("accepted start: %+v %v", cmd, err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'project.start'"); n != 1 {
		t.Fatal("RNF-13: project.start audited")
	}
}

// US-05 CA-1, CA-3, CA-5; US-16 CA-1, CA-3, CA-4: the orchestrator instantiates the stages, queues
// REQ-01 with the stage pair and keeps it when the project pair changes later.
func TestOrchestratorStartsTheWorkflowAndQueuesTheRequirementsWave(t *testing.T) {
	f := newStartFixture(t)
	ctx := context.Background()
	before, err := f.workflow.Workflow(ctx, f.dev, f.projectID)
	if err != nil || len(before.Stages) != 3 || before.Stages[0].State != "" {
		t.Fatalf("CA-5: three stages with no state before the start: %+v %v", before, err)
	}
	_, err = f.config.Modify(ctx, f.dev, f.projectID, 1, appproject.Change{
		ModelPair:   pair("execution-model"),
		PairByStage: map[string]*appproject.Pair{"requirements": pair("reasoning-model")},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.activeKey(t)
	f.price(t, "reasoning-model", "execution-model")
	cmd, err := f.start(t, "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9c04")
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := f.dispatcher.Drain(ctx); err != nil || processed != 1 {
		t.Fatalf("drain: %d %v", processed, err)
	}
	applied, err := f.workflow.Command(ctx, f.dev, f.projectID, cmd.ID)
	if err != nil || applied.State != command.StateApplied || applied.CompletedAt == nil {
		t.Fatalf("command applied: %+v %v", applied, err)
	}
	after, err := f.workflow.Workflow(ctx, f.dev, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, stage := range after.Stages {
		states[stage.ID] = stage.State
	}
	if states["requirements"] != "ready" || states["architecture"] != "blocked" || states["implementation"] != "blocked" || after.Stages[0].ActiveWaves != 1 {
		t.Fatalf("CA-1/CA-3: %v", states)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.waves WHERE code = 'REQ-01' AND state = 'queued' AND agent = 'requirements-agent' AND agent_configuration_id IS NOT NULL"); n != 1 {
		t.Fatal("CA-1: REQ-01 queued for the requirements agent")
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.wave_models wm JOIN easycode.models m ON m.id = wm.model_id WHERE wm.role = 'execution' AND m.name = 'reasoning-model'"); n != 1 {
		t.Fatal("US-16 CA-3: the stage pair wins over the project pair")
	}
	for _, eventType := range []string{"project.started", "stage.unblocked", "wave.created"} {
		if n := count(t, f.db, "SELECT count(*) FROM easycode.events WHERE type = 'br.easycode."+eventType+".v1' AND correlationid::text = '"+cmd.ID+"'"); n != 1 {
			t.Fatalf("event %s with the command as correlation", eventType)
		}
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.wave.model_selected.v1'"); n != 2 {
		t.Fatal("US-16 CA-1: the wave records both models")
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.projects WHERE state = 'in_progress' AND repository IS NOT NULL"); n != 1 {
		t.Fatal("project in progress")
	}
	// US-16 CA-4: changing the pair now does not change the queued wave.
	if _, err := f.config.Modify(ctx, f.dev, f.projectID, 2, appproject.Change{PairByStage: map[string]*appproject.Pair{"requirements": nil}}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.wave_models wm JOIN easycode.models m ON m.id = wm.model_id WHERE wm.role = 'execution' AND m.name = 'reasoning-model'"); n != 1 {
		t.Fatal("US-16 CA-4: the wave keeps the pair it started with")
	}
	if _, err := f.start(t, "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9c05"); failure.From(err).Code != failure.CodeStateConflict {
		t.Fatalf("starting again is a state conflict, got %v", err)
	}
}

// The worker rechecks the guard: a key removed after the request refuses the command.
func TestWorkerRefusesTheStartWhenTheGuardFails(t *testing.T) {
	f := newStartFixture(t)
	ctx := context.Background()
	if _, err := f.config.Modify(ctx, f.dev, f.projectID, 1, appproject.Change{ModelPair: pair("execution-model")}); err != nil {
		t.Fatal(err)
	}
	f.activeKey(t)
	f.price(t, "reasoning-model", "execution-model")
	cmd, err := f.start(t, "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9c06")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Admin.Exec(ctx, "UPDATE easycode.provider_keys SET state = 'removed', removed_at = now(), encrypted_value = NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.dispatcher.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	refused, _ := f.workflow.Command(ctx, f.dev, f.projectID, cmd.ID)
	if refused.State != command.StateRefused || refused.Refusal != project.CodeIncompleteConfig {
		t.Fatalf("want refused incomplete-configuration, got %+v", refused)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.stages"); n != 0 {
		t.Fatal("nothing applied")
	}
}
