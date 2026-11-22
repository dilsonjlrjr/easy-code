package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/application/command"
	appevent "easycode/internal/application/event"
	appgate "easycode/internal/application/gate"
	appgateway "easycode/internal/application/gateway"
	apppending "easycode/internal/application/pending"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/runtime"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/provider/anthropic"
	"easycode/internal/infrastructure/provider/openai"
	"easycode/internal/infrastructure/provider/providertest"
	"easycode/internal/infrastructure/sealing"
	"easycode/internal/infrastructure/specs"
	httpapi "easycode/internal/interfaces/http"
)

const (
	anthropicKey = "sk-ant-e2e-0000000000"
	openaiKey    = "sk-proj-e2e-0000000000"
)

// teamDeps wires the api over the workflow in force of Phase 2 (workflow-padrao with the stages of the
// agents that exist, MARCO-4).
func teamDeps(t *testing.T) func(*httpapi.Dependencies, *postgrestest.Database) {
	return func(d *httpapi.Dependencies, db *postgrestest.Database) {
		pool := db.PoolAs("easycode_api")
		wf, err := specs.StandardWorkflow(specs.StandardAgents)
		if err != nil {
			t.Fatal(err)
		}
		tx := postgres.NewTxRunner(pool)
		trail := postgres.NewAuditTrail(pool)
		projects := postgres.NewProjects(pool, wf.Definition.ID, len(wf.Definition.Stages))
		workflows := postgres.NewWorkflow(pool, wf.Definition)
		waveStore := postgres.NewWaves(pool, wf.Definition)
		waves := appwave.NewService(tx, projects, waveStore, waveStore, trail)
		d.Projects = appproject.NewService(tx, projects, postgres.NewOutbox(pool), trail, wf.Definition.ID, wf.Definition.Version)
		d.Configuration = appproject.NewConfigurationService(tx, projects, projects, postgres.NewCatalog(pool), trail, wf.Definition)
		d.Workflow = appworkflow.NewService(tx, projects, workflows, workflows, workflows, trail)
		d.Artifacts = appartifact.NewQueries(projects, postgres.NewArtifacts(pool))
		d.Waves, d.Access = waves, waves
		d.Gates = appgate.NewDecisions(tx, projects, postgres.NewGates(pool), postgres.NewOutbox(pool), trail)
		d.Events, d.History, d.Shutdown = appevent.NewBroker(), postgres.NewOutbox(pool), make(chan struct{})
		d.Pending = apppending.NewService(postgres.NewPending(pool, wf.Definition))
		d.Notices = postgres.NewOutbox(pool)
	}
}

type teamWorker struct {
	dispatcher *command.Dispatcher
	progress   *appgate.Progress
	executor   *runtime.Executor
}

// newTeamWorker is the worker of cmd/easycode-worker over the Phase 2 workflow: every agent with a
// harness, its policy and its mode per stage, and the start, answer and cancel commands.
func newTeamWorker(t *testing.T, db *postgrestest.Database, gateway runtime.Gateway, repos *git.Repositories) teamWorker {
	t.Helper()
	pool := db.PoolAs("easycode_worker")
	tx := postgres.NewTxRunner(pool)
	outbox := postgres.NewOutbox(pool)
	wf, err := specs.StandardWorkflow(specs.StandardAgents)
	if err != nil {
		t.Fatal(err)
	}
	workflows := postgres.NewWorkflow(pool, wf.Definition)
	defaults := func(agent string) (appworkflow.AgentConfiguration, error) {
		d, err := specs.DefaultAgent(agent)
		return appworkflow.AgentConfiguration{Agent: agent, InstructionVersion: d.InstructionVersion, InstructionHash: d.InstructionHash, Tools: d.Tools, Limits: d.Limits}, err
	}
	orchestrator := appworkflow.NewOrchestrator(workflows, workflows, outbox, defaults, wf.Definition, wf.JSON, wf.Hash)
	dispatcher := command.NewDispatcher(tx, postgres.NewCommands(pool))
	dispatcher.Register("start", orchestrator.Start)
	dispatcher.Register("answer", appwave.NewAnswerHandler(postgres.NewWaves(pool, wf.Definition), outbox).Handle)
	dispatcher.Register("cancel", appwave.NewCancelHandler(postgres.NewWaves(pool, wf.Definition), outbox).Handle)
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		t.Fatal(err)
	}
	store := appartifact.NewStore(tx, specs.HeaderParser{}, validator, postgres.NewArtifacts(pool), outbox)
	progress := appgate.NewProgress(tx, postgres.NewGates(pool), store, validator, flow{workflows, orchestrator}, outbox, wf.Definition)
	ledger := postgres.NewLedger(pool)
	models := runtime.NewGatewayModels(gateway, postgres.NewProviderKeys(pool), ledger, ledger)
	agents := map[string]runtime.Agent{}
	for _, name := range specs.StandardAgents {
		policy, err := specs.AgentPolicy(name)
		if err != nil {
			t.Fatal(err)
		}
		modes := map[string]string{}
		for _, stage := range wf.Definition.Stages {
			if stage.IsReviewer(name) {
				modes[stage.ID] = "PARECER"
			} else if stage.Agent == name {
				modes[stage.ID] = stage.AgentMode
			}
		}
		agents[name] = runtime.Agent{Steps: runtime.NewToolAgent(models, 4000, nil), Policy: &policy, Modes: modes}
	}
	outputs := func(stage string) []string {
		definition, _ := wf.Definition.Stage(stage)
		var types []string
		seen := map[string]bool{}
		for _, o := range definition.Outputs {
			if o.Required && o.Type != "" && !seen[o.Type] {
				seen[o.Type] = true
				types = append(types, o.Type)
			}
		}
		return types
	}
	attachments := func(stage string) []string {
		definition, _ := wf.Definition.Stage(stage)
		var paths []string
		for _, o := range definition.Outputs {
			if o.Attachment {
				paths = append(paths, o.Path)
			}
		}
		if stage == "architecture" {
			paths = append(paths, "docs/arquitetura/api/")
		}
		return paths
	}
	executor := runtime.NewExecutor(tx, ledger, agents, func(agent string) (string, string, error) {
		d, err := specs.DefaultAgent(agent)
		return string(d.Instruction), d.InstructionHash, err
	}, outbox, postgres.NewTelemetryNotifier(pool), store, progress, outputs,
		runtime.Settings{Worker: "worker-1", Lease: 15 * time.Second, MaxCorrections: 3, RetryBackoff: 10 * time.Millisecond, Poll: 50 * time.Millisecond},
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if repos != nil {
		executor = executor.WithRepository(*repos, attachments)
	}
	return teamWorker{dispatcher: dispatcher, progress: progress, executor: executor}
}

func (w teamWorker) drain(t *testing.T) {
	t.Helper()
	if _, err := w.dispatcher.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (w teamWorker) reconcile(t *testing.T) {
	t.Helper()
	if _, err := w.progress.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// runAll runs the queued waves until none is left.
func (w teamWorker) runAll(t *testing.T) {
	t.Helper()
	for i := 0; i < 10; i++ {
		ran, err := w.executor.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ran {
			return
		}
	}
}

// sealKeys records an active sealed key of each provider for the organization (US-15) and returns the
// opener of the gateway.
func sealKeys(t *testing.T, db *postgrestest.Database, keys map[string]string) sealing.Opener {
	t.Helper()
	dir := t.TempDir()
	if _, err := sealing.GenerateKeyPair(filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")); err != nil {
		t.Fatal(err)
	}
	sealer, _ := sealing.LoadSealer(filepath.Join(dir, "public.pem"))
	opener, _ := sealing.LoadOpener(filepath.Join(dir, "private.pem"))
	for name, key := range keys {
		sealed, ref, _ := sealer.Seal([]byte(key))
		var id int64
		if err := db.Admin.QueryRow(context.Background(), `INSERT INTO easycode.provider_keys (organization_id, provider, encrypted_value, encryption_key_ref, last4, created_by)
SELECT organization_id, $1, $2, $3, '0000', id FROM easycode.users WHERE sub = 'dev' RETURNING id`, name, sealed, ref).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Admin.Exec(context.Background(), "UPDATE easycode.provider_keys SET state = 'active', tested_at = now(), test_result = 'valid' WHERE id = $1", id); err != nil {
			t.Fatal(err)
		}
	}
	return opener
}

// models registers a reasoning and an execution model of a provider with the same prices (US-24 CA-4:
// the matrix and the price per token type, the same for both providers so costs compare).
func registerModels(t *testing.T, live liveAPI, providerName string) {
	t.Helper()
	for _, model := range []string{"reasoning-model", "execution-model"} {
		live.json(t, "PUT", "/api/v1/providers/"+providerName+"/models/"+model,
			`{"display_name":"`+model+`","capabilities":{"tool_use":true,"context_tokens":200000,"prompt_cache":true,"image_input":false},"active":true}`, nil, 200)
		live.json(t, "POST", "/api/v1/providers/"+providerName+"/models/"+model+"/prices", `{"effective_since":"`+time.Now().UTC().Format(time.RFC3339)+
			`","currency":"BRL","per_million_tokens":{"input":"20","output":"100","read_cache":"2","write_cache":"25"},"source":"e2e"}`, key(), 201)
	}
	models := live.json(t, "GET", "/api/v1/models", "", nil, 200)["items"].([]any)
	found := 0
	for _, item := range models {
		m := item.(map[string]any)
		if m["provider"] == providerName {
			found++
			if m["capabilities"] == nil {
				t.Fatalf("US-24 CA-4: capability matrix of %v", m)
			}
		}
	}
	if found < 2 {
		t.Fatalf("US-24 CA-4: models of %s: %v", providerName, models)
	}
}

// newProject creates and configures a reference project with the pair and starts it.
func newProject(t *testing.T, live liveAPI, work teamWorker, name, reasoning, execution string) string {
	t.Helper()
	need, err := specs.ReferenceNeed()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"name": name, "business_need": need, "provider_submission_acknowledgment": true,
		"budget":      map[string]any{"stop_limit": map[string]string{"value": "200.00", "currency": "BRL"}},
		"constraints": map[string]any{"items": []map[string]string{{"category": "language", "description": "Backend em Go"}}}})
	projectID := live.json(t, "POST", "/api/v1/projects", string(body), key(), 201)["id"].(string)
	base := "/api/v1/projects/" + projectID
	live.json(t, "PATCH", base+"/configuration", fmt.Sprintf(`{"model_pair":{"reasoning":{"provider":%q,"model":"reasoning-model"},"execution":{"provider":%q,"model":"execution-model"}}}`,
		reasoning, execution), map[string]string{"If-Match": `"1"`}, 200)
	live.json(t, "POST", base+"/start", "", key(), 202)
	work.drain(t)
	return base
}

// teamScript answers every agent of the reference project by the agent and mode the harness writes in
// the system prompt; the same instance serves one provider (it pops the answers).
type teamScript struct {
	mu      sync.Mutex
	answers map[string][]provider.Message
}

func (s *teamScript) next(system string) provider.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	role := ""
	for _, agent := range specs.StandardAgents {
		if strings.Contains(system, "`autor` é "+agent) {
			role = agent
		}
	}
	switch {
	case strings.Contains(system, "`finish(parecer, ressalvas)`"):
		role += "/review"
	case strings.Contains(system, "**VALIDACAO**"):
		role += "/validation"
	}
	answers := s.answers[role]
	if len(answers) == 0 {
		return provider.Message{Text: "sem roteiro para " + role}
	}
	s.answers[role] = answers[1:]
	return answers[0]
}

func newTeamScript(t *testing.T) *teamScript {
	team := folderFiles(t, "testdata/reserva-equipe", "")
	architecture := folderFiles(t, "testdata/reserva-arquitetura", "")
	archFiles := map[string]string{
		"docs/arquitetura/ARQ.md": architecture["ARQ.md"], "docs/arquitetura/adr/ADR-01.md": architecture["ADR-01.md"],
		"docs/arquitetura/tarefas/T-01.md": architecture["T-01.md"], "docs/arquitetura/estrutura.md": architecture["estrutura.md"],
		"docs/arquitetura/api/openapi.yaml": architecture["openapi.yaml"],
	}
	dataFiles := map[string]string{"docs/dados/DIC.md": team["DIC.md"], "docs/dados/PERSIST.md": team["PERSIST.md"],
		"docs/dados/esquema.sql": team["esquema.sql"], "docs/achados/ACH-01.md": team["ACH-01.md"]}
	// US-21 CA-3: the first delivery has a screen of a US the project does not have (orphan screen).
	orphan := strings.Replace(team["TELAS.md"], "acessibilidade:", `  - id: TL-2
    nome: Relatórios
    arquivo: docs/design/prototipo/relatorios.html
    us: [US-99]
    criterios: [US-99/CA-1]
    estados: [vazio, sucesso]
acessibilidade:`, 1)
	designFiles := map[string]string{"docs/design/TELAS.md": orphan, "docs/design/fluxo.md": team["fluxo.md"],
		"docs/design/wireframes.md": team["wireframes.md"], "docs/design/estilo.md": team["estilo.md"], "docs/design/prototipo/reservar.html": team["reservar.html"]}
	fixed := strings.NewReplacer("onda: DSG-01\nversao: 1\n", "onda: DSG-02\nversao: 2\n",
		"descricao: criação}\n", "descricao: criação}\n  - {versao: 2, data: \"2026-11-22\", onda: DSG-02, descricao: tela órfã removida}\n").Replace(team["TELAS.md"])
	approve := map[string]any{"parecer": "aprovado", "ressalvas": []string{}}
	return &teamScript{answers: map[string][]provider.Message{
		"requirements-agent": {
			{Calls: []provider.ToolCall{call("r0", runtime.ToolListFiles, map[string]string{"directory": "docs"})}},
			{Calls: append(writeAll(referenceFiles(t), "r"), call("rf", runtime.ToolFinish, map[string]any{}))},
		},
		"architect-agent": {{Calls: append(writeAll(archFiles, "a"), call("af", runtime.ToolFinish, map[string]any{}))}},
		"dba-agent": {
			{Calls: []provider.ToolCall{
				call("d0", runtime.ToolReadFile, map[string]string{"path": "docs/arquitetura/ARQ.md"}),
				// RNF-07, US-30 CA-2: the approved ARQ of the architect is not the DBA's to change.
				call("d1", runtime.ToolWriteFile, map[string]string{"path": "docs/arquitetura/ARQ.md", "content": "x"}),
			}},
			{Calls: append(writeAll(dataFiles, "dw"), call("df", runtime.ToolFinish, map[string]any{}))},
		},
		"designer-agent": {
			// RNF-07: the designer reads requirements, architecture and design, not the data model.
			{Calls: []provider.ToolCall{call("g0", runtime.ToolReadFile, map[string]string{"path": "docs/dados/DIC.md"})}},
			{Calls: append(writeAll(designFiles, "gw"), call("gf", runtime.ToolFinish, map[string]any{}))},
			{Calls: []provider.ToolCall{
				call("gx", runtime.ToolWriteFile, map[string]string{"path": "docs/design/TELAS.md", "content": fixed}),
				call("gy", runtime.ToolFinish, map[string]any{}),
			}},
		},
		"dba-agent/review":      {{Calls: []provider.ToolCall{call("vd", runtime.ToolFinish, approve)}}},
		"designer-agent/review": {{Calls: []provider.ToolCall{call("vg", runtime.ToolFinish, approve)}}},
		"architect-agent/validation": {{Calls: []provider.ToolCall{
			call("va", runtime.ToolWriteFile, map[string]string{"path": "docs/validacao/VALIDACAO-01.md", "content": team["VALIDACAO-01.md"]}),
			call("vf", runtime.ToolFinish, map[string]any{}),
		}}},
	}}
}

// teamOutcome is what a run of the workflow produced, to compare providers.
type teamOutcome struct {
	artifacts []string          // type code version state
	checks    map[string]string // gate/check → status
	costs     []string          // wave useful cost
}

func waveStates(t *testing.T, live liveAPI, base string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, item := range live.json(t, "GET", base+"/waves?limit=100", "", nil, 200)["items"].([]any) {
		w := item.(map[string]any)
		out[w["id"].(string)] = w["state"].(string)
	}
	return out
}

// runTeam takes a started reference project from Requisitos to the Implementação unblocked, with human
// approvals, asserting the MARCO-4 criteria on the way.
func runTeam(t *testing.T, live liveAPI, work teamWorker, base string) teamOutcome {
	t.Helper()
	outcome := teamOutcome{checks: map[string]string{}}
	record := func(gate string, round map[string]any) {
		for check, status := range checksOf(t, round) {
			outcome.checks[gate+"/"+check] = status
		}
	}
	work.runAll(t)
	live.approve(t, base, "gate.requirements", 1)
	work.reconcile(t)
	work.runAll(t)
	live.approve(t, base, "gate.architecture", 1)
	work.reconcile(t)

	// US-20 CA-3: with the architecture approved, Dados and Design are ready and queued together.
	states := waveStates(t, live, base)
	if states["DBA-01"] != "queued" || states["DSG-01"] != "queued" {
		t.Fatalf("US-20 CA-3: data and design in parallel: %v", states)
	}
	stages := map[string]string{}
	for _, item := range live.json(t, "GET", base+"/workflow", "", nil, 200)["stages"].([]any) {
		s := item.(map[string]any)
		stages[s["id"].(string)] = s["state"].(string)
	}
	if stages["data"] != "ready" || stages["design"] != "ready" || stages["validation"] != "blocked" {
		t.Fatalf("US-20 CA-3: stages %v", stages)
	}
	work.runAll(t)
	states = waveStates(t, live, base)
	if states["DBA-01"] != "completed" || states["DSG-01"] != "completed" {
		t.Fatalf("DBA and Designer waves: %v", states)
	}

	// US-20 CA-1: DIC and PERSIST valid by the schemas; CA-2: the finding for the architecture.
	data := live.json(t, "GET", base+"/gates/gate.data/rounds/1", "", nil, 200)
	if checks := checksOf(t, data); data["state"] != "awaiting_human" || checks["artifact.schema_valid"] != "passed" || checks["persist.model_references"] != "passed" {
		t.Fatalf("US-20 CA-1: gate.data %v", data)
	}
	record("gate.data", data)
	finding := live.json(t, "GET", base+"/artifacts/ACH-01", "", nil, 200)
	header := finding["header"].(map[string]any)
	if header["categoria"] != "desvio_arquitetura" || header["etapa_destino"] != "arquitetura" || header["evidencia"] == "" || !strings.Contains(fmt.Sprint(finding["author"]), "dba-agent") {
		t.Fatalf("US-20 CA-2: finding for the architecture with evidence: %v", finding)
	}
	// US-21 CA-3: the link check points out the orphan screen and the round is rejected; the rework wave
	// of the designer fixes it.
	orphanRound := live.json(t, "GET", base+"/gates/gate.design/rounds/1", "", nil, 200)
	if checks := checksOf(t, orphanRound); orphanRound["state"] != "rejected" || checks["artifact.schema_valid"] != "passed" ||
		checks["traceability.interface_us_in_screen"] != "failed" || !strings.Contains(fmt.Sprint(orphanRound["checks"]), "TELAS/TL-2") {
		t.Fatalf("US-21 CA-3: orphan screen: %v", orphanRound)
	}
	work.reconcile(t)
	if states := waveStates(t, live, base); states["DSG-02"] != "queued" {
		t.Fatalf("rework of the design: %v", states)
	}
	work.runAll(t)
	// US-21 CA-1, CA-2: TELAS valid; every US with interface on a screen.
	design := live.json(t, "GET", base+"/gates/gate.design/rounds/2", "", nil, 200)
	if checks := checksOf(t, design); design["state"] != "awaiting_human" || checks["artifact.schema_valid"] != "passed" ||
		checks["traceability.interface_us_in_screen"] != "passed" || checks["telas.criteria_consistent"] != "passed" {
		t.Fatalf("US-21 CA-1: gate.design %v", design)
	}
	record("gate.design", design)
	live.approve(t, base, "gate.data", 1)
	live.approve(t, base, "gate.design", 2)
	work.reconcile(t)

	// Validação técnica: one opinion wave per participant, then the lead's (template VALIDACAO).
	states = waveStates(t, live, base)
	if states["VAL-01"] != "queued" || states["VAL-02"] != "queued" || states["VAL-03"] != "queued" {
		t.Fatalf("validation waves: %v", states)
	}
	work.runAll(t)
	for code, agent := range map[string]string{"VAL-01": "dba-agent", "VAL-02": "designer-agent", "VAL-03": "architect-agent"} {
		if w := live.json(t, "GET", base+"/waves/"+code, "", nil, 200); w["state"] != "completed" || w["agent"] != agent {
			t.Fatalf("%s: %v", code, w)
		}
	}
	// US-20 CA-4: VALIDACAO with the result before the Implementação.
	validation := live.json(t, "GET", base+"/gates/gate.validation/rounds/1", "", nil, 200)
	if checks := checksOf(t, validation); validation["state"] != "awaiting_human" || checks["artifact.schema_valid"] != "passed" ||
		checks["validacao.review_opinions_recorded"] != "passed" || checks["validacao.verdict_unblocked"] != "passed" {
		t.Fatalf("US-20 CA-4: gate.validation %v", validation)
	}
	record("gate.validation", validation)
	if states := waveStates(t, live, base); states["DEV-01"] != "" {
		t.Fatalf("US-20 CA-4: no implementation before the validation is approved: %v", states)
	}
	live.approve(t, base, "gate.validation", 1)
	work.reconcile(t)
	if states := waveStates(t, live, base); states["DEV-01"] != "queued" {
		t.Fatalf("implementation unblocked after the validation: %v", states)
	}

	for _, item := range live.json(t, "GET", base+"/artifacts?limit=100", "", nil, 200)["items"].([]any) {
		a := item.(map[string]any)
		outcome.artifacts = append(outcome.artifacts, fmt.Sprintf("%v %v %v %v", a["type"], a["id"], a["version"], a["state"]))
	}
	for _, item := range live.json(t, "GET", base+"/waves?limit=100", "", nil, 200)["items"].([]any) {
		w := item.(map[string]any)
		if w["state"] == "completed" {
			cost := w["cost_total"].(map[string]any)
			outcome.costs = append(outcome.costs, fmt.Sprintf("%v %v", w["id"], cost["value"]))
		}
	}
	sort.Strings(outcome.artifacts)
	sort.Strings(outcome.costs)
	// The implementation is out of this test (MARCO-3 runs it in the sandbox): its wave is canceled (US-14).
	live.json(t, "POST", base+"/waves/DEV-01/cancellation", `{"confirm":true}`, key(), 202)
	work.drain(t)
	return outcome
}

// TestMarco4ExtendedTeamWithTwoProviders is the MARCO-4 done criterion (PLANO): DBA and Designer run and
// deliver valid artifacts in the positions of workflow-padrao (US-20, US-21), and the same waves run with
// Anthropic and with OpenAI through the real adapters against scripted APIs (never the real ones) with
// equivalent results (US-24 CA-1, CA-2); a pair with one model of each provider records the calls and
// their cost per provider (US-24 CA-3).
func TestMarco4ExtendedTeamWithTwoProviders(t *testing.T) {
	live := newLiveAPI(t, teamDeps(t))
	db := live.db
	opener := sealKeys(t, db, map[string]string{"anthropic": anthropicKey, "openai": openaiKey})
	var mu sync.Mutex
	scripts := map[string]*teamScript{"anthropic": newTeamScript(t), "openai": newTeamScript(t)}
	scriptOf := func(name string) providertest.Script {
		return func(system string) provider.Message {
			mu.Lock()
			script := scripts[name]
			mu.Unlock()
			return script.next(system)
		}
	}
	anthropicAPI := providertest.NewAnthropic(anthropicKey, scriptOf("anthropic"))
	openaiAPI := providertest.NewOpenAI(openaiKey, scriptOf("openai"))
	t.Cleanup(anthropicAPI.Close)
	t.Cleanup(openaiAPI.Close)
	gateway := localGateway{service: appgateway.NewService(opener, time.Minute,
		anthropic.New(anthropicAPI.URL, anthropicAPI.Client(), 10*time.Second), openai.New(openaiAPI.URL, openaiAPI.Client(), 10*time.Second))}
	repos := git.New(t.TempDir())
	work := newTeamWorker(t, db, gateway, &repos)
	registerModels(t, live, "anthropic")
	registerModels(t, live, "openai")

	withAnthropic := runTeam(t, live, work, newProject(t, live, work, "Reserva com Anthropic", "anthropic", "anthropic"))
	withOpenAI := runTeam(t, live, work, newProject(t, live, work, "Reserva com OpenAI", "openai", "openai"))
	if anthropicAPI.Calls() == 0 || openaiAPI.Calls() == 0 || anthropicAPI.Calls() != openaiAPI.Calls() {
		t.Fatalf("each project calls only its provider, the same number of times: %d %d", anthropicAPI.Calls(), openaiAPI.Calls())
	}
	if fmt.Sprint(withAnthropic.artifacts) != fmt.Sprint(withOpenAI.artifacts) || fmt.Sprint(withAnthropic.checks) != fmt.Sprint(withOpenAI.checks) ||
		fmt.Sprint(withAnthropic.costs) != fmt.Sprint(withOpenAI.costs) {
		t.Fatalf("US-24 CA-2: equivalent results\nanthropic %v\n%v\n%v\nopenai %v\n%v\n%v", withAnthropic.artifacts, withAnthropic.checks, withAnthropic.costs,
			withOpenAI.artifacts, withOpenAI.checks, withOpenAI.costs)
	}
	// US-24 CA-1: the same instructions, tools and prompts reach both providers.
	first := func(body map[string]any, system bool) string {
		if system {
			if blocks, ok := body["system"].([]any); ok {
				return blocks[0].(map[string]any)["text"].(string)
			}
			return body["messages"].([]any)[0].(map[string]any)["content"].(string)
		}
		return fmt.Sprint(len(body["tools"].([]any)))
	}
	if first(anthropicAPI.Bodies[0], true) != first(openaiAPI.Bodies[0], true) || first(anthropicAPI.Bodies[0], false) != first(openaiAPI.Bodies[0], false) {
		t.Fatal("US-24 CA-1: the waves run without any change in instructions or tools")
	}
	// RNF-07: the refused writes and reads of DBA and Designer are recorded with the wave.
	if n := count(t, db, "SELECT count(*) FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id WHERE w.code = 'DBA-01' AND t.refusal_reason = 'foreign_artifact_write'"); n != 2 {
		t.Fatalf("RNF-07: DBA write on the approved ARQ (%d)", n)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id WHERE w.code = 'DSG-01' AND t.refusal_reason = 'path_not_allowed'"); n != 2 {
		t.Fatalf("RNF-07: designer read of the data model (%d)", n)
	}

	// US-24 CA-3: reasoning by one provider and execution by the other; the cost is per provider.
	mixed := newTeamScript(t)
	mu.Lock()
	scripts["anthropic"], scripts["openai"] = mixed, mixed
	mu.Unlock()
	base := newProject(t, live, work, "Reserva com os dois", "anthropic", "openai")
	before := anthropicAPI.Calls() + openaiAPI.Calls()
	work.runAll(t)
	if w := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200); w["state"] != "completed" {
		t.Fatalf("US-24 CA-3: the wave completes: %v", w)
	}
	calls := live.json(t, "GET", base+"/waves/REQ-01/calls?type=model", "", nil, 200)["items"].([]any)
	byProvider := map[string]int{}
	for _, item := range calls {
		c := item.(map[string]any)
		if c["cost"] == nil {
			t.Fatalf("US-24 CA-3: cost of each call: %v", c)
		}
		byProvider[fmt.Sprintf("%v/%v", c["provider"], c["role"])]++
	}
	if byProvider["anthropic/reasoning"] != 1 || byProvider["openai/execution"] != 1 || anthropicAPI.Calls()+openaiAPI.Calls() != before+2 {
		t.Fatalf("US-24 CA-3: calls per provider: %v", byProvider)
	}
}

// blockingProvider blocks every call until it is aborted, or fails it at once (cancel tests).
type blockingProvider struct {
	mu      sync.Mutex
	fail    bool
	started chan struct{}
}

func (p *blockingProvider) Name() string { return "anthropic" }

func (p *blockingProvider) Capabilities(context.Context, string) (provider.Capabilities, error) {
	return provider.Capabilities{ToolUse: true, ContextTokens: 200_000}, nil
}

func (p *blockingProvider) TestKey(context.Context, provider.Secret) error { return nil }

func (p *blockingProvider) Complete(ctx context.Context, _ provider.Request, _ func(provider.Chunk)) (provider.Response, error) {
	p.mu.Lock()
	fail, started := p.fail, p.started
	p.mu.Unlock()
	if fail {
		return provider.Response{}, &provider.Error{Class: provider.ErrorRequest, Message: "bad request"}
	}
	close(started)
	<-ctx.Done()
	return provider.Response{}, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted", PartialUsage: provider.Usage{Input: 50}}
}

// TestMarco4CancelWave covers US-14 end to end: canceling a running wave aborts the model call in flight
// and the wave is canceled within RNF-10 (5 s), with the lost cost, the event and the audit (CA-1); a
// queued wave (CA-2) and a failed wave (CA-4) are canceled; a canceled wave offers no cancel (CA-3).
func TestMarco4CancelWave(t *testing.T) {
	live := newLiveAPI(t, teamDeps(t))
	db, ctx := live.db, context.Background()
	opener := sealKeys(t, db, map[string]string{"anthropic": anthropicKey})
	blocking := &blockingProvider{started: make(chan struct{})}
	work := newTeamWorker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, blocking)}, nil)
	registerModels(t, live, "anthropic")
	confirm := `{"confirm":true}`

	// CA-2: a queued wave.
	queued := newProject(t, live, work, "Reserva na fila", "anthropic", "anthropic")
	live.json(t, "POST", queued+"/waves/REQ-01/cancellation", `{"confirm":false}`, key(), 400)
	live.json(t, "POST", queued+"/waves/REQ-01/cancellation", confirm, nil, 428)
	if actions := live.json(t, "GET", queued+"/waves/REQ-01", "", nil, 200)["available_actions"]; fmt.Sprint(actions) != "[cancel]" {
		t.Fatalf("cancel offered to a queued wave: %v", actions)
	}
	live.json(t, "POST", queued+"/waves/REQ-01/cancellation", confirm, key(), 202)
	work.drain(t)
	if w := live.json(t, "GET", queued+"/waves/REQ-01", "", nil, 200); w["state"] != "canceled" || w["result"] != "canceled" {
		t.Fatalf("US-14 CA-2: %v", w)
	}

	// CA-1: a running wave, with the call in flight aborted within RNF-10.
	running := newProject(t, live, work, "Reserva em execução", "anthropic", "anthropic")
	done := make(chan error, 1)
	go func() { _, err := work.executor.RunOnce(ctx); done <- err }()
	select {
	case <-blocking.started:
	case <-time.After(30 * time.Second):
		t.Fatal("the model call never started")
	}
	asked := time.Now()
	accepted := live.json(t, "POST", running+"/waves/REQ-01/cancellation", confirm, key(), 202)
	if accepted["type"] != "cancel" {
		t.Fatalf("AcceptedCommand: %v", accepted)
	}
	work.drain(t)
	select {
	case err := <-done:
		if !errors.Is(err, runtime.ErrLeaseLost) {
			t.Fatalf("the worker stops the wave: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RNF-10: the call in flight was not aborted")
	}
	wave := live.json(t, "GET", running+"/waves/REQ-01", "", nil, 200)
	if elapsed := time.Since(asked); elapsed > 5*time.Second || wave["state"] != "canceled" {
		t.Fatalf("RNF-10: canceled in %s: %v", elapsed, wave["state"])
	}
	if lost := wave["cost"].(map[string]any)["lost"].(map[string]any)["value"]; lost == "0" {
		t.Fatalf("US-14 CA-1: the lost cost is recorded: %v", wave["cost"])
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.model_calls c JOIN easycode.waves w ON w.id = c.wave_id JOIN easycode.projects p ON p.id = w.project_id WHERE p.name = 'Reserva em execução' AND c.result = 'aborted' AND c.lost"); n != 1 {
		t.Fatalf("US-14 CA-1: the call in flight is aborted and lost (%d)", n)
	}
	if n := count(t, db, `SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.wave.canceled.v1' AND envelope->'data'->>'cause' = 'user_request'
   AND envelope->'data'->'transition'->>'from_state' = 'running' AND envelope->'data'->'lost_cost'->>'value' <> '0'`); n != 1 {
		t.Fatalf("US-14 CA-1: wave.canceled with the lost cost (%d)", n)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'wave.cancel' AND actor_id = 'dev'"); n != 2 {
		t.Fatalf("RNF-13: cancel audited (%d)", n)
	}
	// CA-3: a canceled wave offers no cancel and refuses it.
	if actions := wave["available_actions"]; fmt.Sprint(actions) != "[]" {
		t.Fatalf("US-14 CA-3: %v", actions)
	}
	refused := live.json(t, "POST", running+"/waves/REQ-01/cancellation", confirm, key(), 409)
	if refused["code"] != "state-conflict" && !strings.Contains(fmt.Sprint(refused["type"]), "state-conflict") {
		t.Fatalf("US-14 CA-3: %v", refused)
	}

	// CA-4: a failed wave that will not be restarted.
	blocking.mu.Lock()
	blocking.fail = true
	blocking.mu.Unlock()
	failed := newProject(t, live, work, "Reserva com falha", "anthropic", "anthropic")
	work.runAll(t)
	if w := live.json(t, "GET", failed+"/waves/REQ-01", "", nil, 200); w["state"] != "failed" || fmt.Sprint(w["available_actions"]) != "[cancel]" {
		t.Fatalf("failed wave: %v", w)
	}
	live.json(t, "POST", failed+"/waves/REQ-01/cancellation", confirm, key(), 202)
	work.drain(t)
	if w := live.json(t, "GET", failed+"/waves/REQ-01", "", nil, 200); w["state"] != "canceled" || w["failure_cause"] != nil {
		t.Fatalf("US-14 CA-4: %v", w)
	}
}
