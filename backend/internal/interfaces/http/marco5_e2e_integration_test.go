package http_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/application/command"
	appevent "easycode/internal/application/event"
	appfinding "easycode/internal/application/finding"
	appgate "easycode/internal/application/gate"
	appgateway "easycode/internal/application/gateway"
	apppending "easycode/internal/application/pending"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/runtime"
	apptraceability "easycode/internal/application/traceability"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/provider"
	"easycode/internal/domain/sandbox"
	domainworkflow "easycode/internal/domain/workflow"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/specs"
	httpapi "easycode/internal/interfaces/http"
)

// qualityDeps wires the api over the workflow in force of MARCO-5 (the seven agents, the parallel
// activities), with the findings and the traceability.
func qualityDeps(t *testing.T) func(*httpapi.Dependencies, *postgrestest.Database) {
	return func(d *httpapi.Dependencies, db *postgrestest.Database) {
		teamDepsOf(t, specs.StandardAgents)(d, db)
		pool := db.PoolAs("easycode_api")
		wf, _ := specs.StandardWorkflow(specs.StandardAgents)
		tx := postgres.NewTxRunner(pool)
		projects := postgres.NewProjects(pool, wf.Definition.ID, len(wf.Definition.Stages))
		d.Findings = appfinding.NewDecisions(tx, projects, postgres.NewFindings(pool), postgres.NewOutbox(pool), postgres.NewAuditTrail(pool))
		d.Traceability = apptraceability.NewService(projects, postgres.NewTraceability(pool))
	}
}

// teamDepsOf is teamDeps over the workflow of the given agents.
func teamDepsOf(t *testing.T, agents []string) func(*httpapi.Dependencies, *postgrestest.Database) {
	return func(d *httpapi.Dependencies, db *postgrestest.Database) {
		pool := db.PoolAs("easycode_api")
		wf, err := specs.StandardWorkflow(agents)
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

// qualityWorker is the worker of cmd/easycode-worker in MARCO-5: the seven agents with their policies and
// modes (stages and parallel activities), the governance engine over the repository and the finding machine.
type qualityWorker struct {
	teamWorker
	findings *appfinding.Service
	store    *appartifact.Store
}

func newQualityWorker(t *testing.T, db *postgrestest.Database, gateway runtime.Gateway, runner runtime.CommandRunner, repos git.Repositories) qualityWorker {
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
	waves := postgres.NewWaves(pool, wf.Definition)
	dispatcher.Register("cancel", appwave.NewCancelHandler(waves, outbox).Handle)
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		t.Fatal(err)
	}
	store := appartifact.NewStore(tx, specs.HeaderParser{}, validator, postgres.NewArtifacts(pool), outbox)
	progress := newProgress(t, tx, pool, store, validator, flow{workflows, orchestrator}, outbox, wf.Definition)
	progress.WithRepository(repos, specs.HeaderParser{})
	findings := appfinding.NewService(tx, postgres.NewFindings(pool), store, orchestrator, progress, waves, outbox, wf.Definition)
	progress.WithHook(findings)
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
				modes[stage.ID] = domainworkflow.ModeReview
			} else if stage.Agent == name {
				modes[stage.ID] = stage.AgentMode
			}
		}
		for _, activity := range wf.Definition.Activities {
			if activity.Agent == name {
				modes[activity.ID] = activity.AgentMode
			}
		}
		agents[name] = runtime.Agent{Steps: runtime.NewToolAgent(models, 4000, runner), Policy: &policy, Modes: modes}
	}
	outputsOf := func(stage string) []domainworkflow.Output {
		if s, ok := wf.Definition.Stage(stage); ok {
			return s.Outputs
		}
		a, _ := wf.Definition.Activity(stage)
		return a.Outputs
	}
	outputs := func(stage string) []string {
		var types []string
		seen := map[string]bool{}
		for _, o := range outputsOf(stage) {
			if o.Required && o.Type != "" && !o.Attachment && !seen[o.Type] {
				seen[o.Type] = true
				types = append(types, o.Type)
			}
		}
		return types
	}
	attachments := func(stage string) []string {
		var paths []string
		for _, o := range outputsOf(stage) {
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
		runtime.Settings{Worker: "worker-1", Lease: 30 * time.Second, MaxCorrections: 3, RetryBackoff: 10 * time.Millisecond, Poll: 50 * time.Millisecond,
			Image: "go", ProjectLimits: sandbox.Limits{CPU: 1, MemoryMiB: 2048, DiskMiB: 512, Time: 3 * time.Minute}},
		slog.New(slog.NewJSONHandler(io.Discard, nil))).WithRepository(repos, attachments).WithFindings(findings)
	return qualityWorker{teamWorker: teamWorker{dispatcher: dispatcher, progress: progress, executor: executor}, findings: findings, store: store}
}

// settle applies the gate and finding effects and runs the queued waves until nothing changes.
func (w qualityWorker) settle(t *testing.T) {
	t.Helper()
	for i := 0; i < 40; i++ {
		w.drain(t)
		decided, err := w.progress.Reconcile(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.findings.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		ran, err := w.executor.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ran && decided == 0 {
			if again, _ := w.executor.RunOnce(context.Background()); !again {
				return
			}
		}
	}
	t.Fatal("the workflow did not settle")
}

// qualityScript answers each agent of the reference project by agent and purpose, read from the
// system prompt the harness builds; entries are built when called (they read the project state).
type qualityScript struct {
	mu       sync.Mutex
	answers  map[string][]func() provider.Message
	block    map[string]chan struct{} // role → released before the first answer
	started  map[string]chan struct{}
	requests map[string]int
	prompts  map[string][]string // role → last message of each request (debug of rejected deliveries)
}

func (s *qualityScript) Name() string { return "anthropic" }

func (s *qualityScript) Capabilities(context.Context, string) (provider.Capabilities, error) {
	return provider.Capabilities{ToolUse: true, ContextTokens: 200_000}, nil
}

func (s *qualityScript) TestKey(context.Context, provider.Secret) error { return nil }

func roleOf(system string) string {
	role := ""
	for _, agent := range specs.StandardAgents {
		if strings.Contains(system, "`autor` é "+agent) {
			role = agent
		}
	}
	switch {
	case strings.Contains(system, "`finish(resultado, evidencia)`"):
		return role + "/revalidation"
	case strings.Contains(system, "`finish(parecer, ressalvas)`"):
		return role + "/review"
	case strings.Contains(system, "**VALIDACAO**"):
		return role + "/validation"
	}
	for _, mode := range []string{"PLANO", "EXECUCAO", "AMEACAS", "AUDITORIA"} {
		if strings.Contains(system, "**"+mode+"**") {
			return role + "/" + mode
		}
	}
	return role
}

func (s *qualityScript) Complete(ctx context.Context, req provider.Request, _ func(provider.Chunk)) (provider.Response, error) {
	role := roleOf(req.System)
	s.mu.Lock()
	gate, blocked := s.block[role]
	if blocked {
		delete(s.block, role)
		close(s.started[role])
	}
	s.mu.Unlock()
	if blocked {
		select {
		case <-gate:
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
	}
	s.mu.Lock()
	queue := s.answers[role]
	s.requests[role]++
	if s.prompts == nil {
		s.prompts = map[string][]string{}
	}
	if n := len(req.Messages); n > 0 {
		last := req.Messages[n-1]
		text := last.Text
		for _, r := range last.Results {
			text += " | " + r.Content
		}
		s.prompts[role] = append(s.prompts[role], text)
	}
	if len(queue) == 0 {
		s.mu.Unlock()
		return provider.Response{}, &provider.Error{Class: provider.ErrorRequest, Message: "no script for " + role}
	}
	s.answers[role] = queue[1:]
	s.mu.Unlock()
	return provider.Response{Message: queue[0](), Usage: provider.Usage{Input: 900, Output: 200}, Stop: provider.StopTool, ProviderID: "req"}, nil
}

func fixed(m provider.Message) func() provider.Message { return func() provider.Message { return m } }

var (
	topVersion = regexp.MustCompile(`(?m)^versao: \d+$`)
	topWave    = regexp.MustCompile(`(?m)^onda: .*$`)
)

// bump writes the next version of an artifact file: versao, onda and a history entry (with the finding
// of a rework).
func bump(content string, version int, wave, description, finding string) string {
	content = topVersion.ReplaceAllString(content, fmt.Sprintf("versao: %d", version))
	content = topWave.ReplaceAllString(content, "onda: "+wave)
	entry := fmt.Sprintf("- {versao: %d, data: '2026-11-22', onda: %s, descricao: %s", version, wave, description)
	if finding != "" {
		entry += ", achado: " + finding
	}
	entry += "}\n"
	start := strings.Index(content, "historico:\n") + len("historico:\n")
	lines := strings.SplitAfter(content[start:], "\n")
	offset := start
	for _, line := range lines {
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, " ") {
			break
		}
		offset += len(line)
	}
	if strings.HasPrefix(content[start:], "  -") {
		entry = "  " + entry
	}
	return content[:offset] + entry + content[offset:]
}

// withHeaderField adds lines at the end of the YAML header.
func withHeaderField(content, lines string) string {
	end := strings.Index(content[4:], "\n---\n") + 4
	return content[:end] + "\n" + lines + content[end:]
}

// scannerRunner runs build, tests and lint in the real sandbox and answers the scanners of the
// Segurança with a recorded output: the image with opengrep, trivy and gitleaks and their offline
// databases is not built in CI yet (pending item of MARCO-5).
type scannerRunner struct {
	real runtime.CommandRunner
}

func (r scannerRunner) Run(ctx context.Context, request sandbox.ExecutionRequest, files map[string]string) (runtime.RunOutcome, error) {
	if request.Category != "scanner" {
		return r.real.Run(ctx, request, files)
	}
	output := []byte(request.Argv[0] + ": scan of app finished\n")
	if request.Argv[0] == "trivy" {
		output = []byte("example.org/calendar v1.2.0  CVE-2026-0001  HIGH  (no fixed version)\n")
	}
	sum := sha256.Sum256(output)
	zero := 0
	now := time.Now()
	return runtime.RunOutcome{Output: output, Files: files, Result: sandbox.ExecutionResult{ExecutionID: request.ExecutionID, Status: "finished", ExitCode: &zero,
		EndReason: sandbox.EndCompleted, Start: now, End: now.Add(time.Second), Duration: time.Second, OutputStart: string(output),
		OutputBytes: int64(len(output)), OutputSHA256: hex.EncodeToString(sum[:])}}, nil
}

func roundOf(t *testing.T, live liveAPI, base, gate string, round int) map[string]any {
	t.Helper()
	return live.json(t, "GET", fmt.Sprintf("%s/gates/%s/rounds/%d", base, gate, round), "", nil, 200)
}

func stageStates(t *testing.T, live liveAPI, base string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, item := range live.json(t, "GET", base+"/workflow", "", nil, 200)["stages"].([]any) {
		s := item.(map[string]any)
		out[s["id"].(string)] = s
	}
	return out
}

func failedChecks(round map[string]any) []string {
	var out []string
	for _, item := range round["checks"].([]any) {
		c := item.(map[string]any)
		if c["status"] == "failed" && c["failure_severity"] == "blocks" {
			out = append(out, c["check"].(string))
		}
	}
	return out
}

// TestMarco5QualityAndFindings is the MARCO-5 done criterion (PLANO): a failing blocking check blocks a gate
// (US-31, US-28 CA-3); a finding produces rework waves, is revalidated by its author, escalates after three
// cycles and closes after an extra round authorized by a human (US-32); QA plans and runs the tests in the
// sandbox (US-22) and Segurança models the threats and audits with the scanners (US-23); a risk is accepted
// with the separation of duties (US-33); a finding that changes the approved ARQ reopens the Arquitetura,
// blocks the Modelagem de Dados and pauses its running wave (US-34); and the traceability goes from the US to
// the commit and the tests (US-28). Scripted fake provider only; build, tests and lint in the real sandbox.
func TestMarco5QualityAndFindings(t *testing.T) {
	sandboxRunner := startRunner(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repos := git.New(t.TempDir())
	live := newLiveAPI(t, qualityDeps(t))
	db := live.db
	opener := sealKeys(t, db, map[string]string{"anthropic": anthropicKey})

	requirements := referenceFiles(t)
	arch := folderFiles(t, "testdata/reserva-arquitetura")
	team := folderFiles(t, "testdata/reserva-equipe")
	quality := folderFiles(t, "testdata/reserva-qualidade")
	archFiles := func(version int, wave string) map[string]string {
		files := map[string]string{"docs/arquitetura/ARQ.md": arch["ARQ.md"], "docs/arquitetura/adr/ADR-01.md": arch["ADR-01.md"],
			"docs/arquitetura/tarefas/T-01.md": arch["T-01.md"]}
		for p, content := range files {
			if version > 1 {
				files[p] = bump(content, version, wave, "vínculo corrigido", "")
			}
		}
		if version == 1 {
			// US-28 CA-3: the task points to a US the project does not have; the link check fails and names it.
			files["docs/arquitetura/tarefas/T-01.md"] = strings.Replace(arch["T-01.md"], "vinculos: [US-01, ADR-01]", "vinculos: [US-01, ADR-01, US-99]", 1)
		}
		files["docs/arquitetura/estrutura.md"], files["docs/arquitetura/api/openapi.yaml"] = arch["estrutura.md"], arch["openapi.yaml"]
		return files
	}
	arq2 := archFiles(2, "ARQ-02")
	arq3 := strings.Replace(bump(arq2["docs/arquitetura/ARQ.md"], 3, "ARQ-03", "autorização por dono da reserva", "ACH-01"),
		"Regras de reserva e conflito de horário", "Regras de reserva e conflito de horário com autorização por dono da reserva", 1)
	task := arq2["docs/arquitetura/tarefas/T-01.md"]
	devTask := strings.Replace(task, "situacao: planejada", "situacao: concluida\nregistro_dev:\n  testes: [app/reserva_test.go]", 1)
	buggy := strings.Replace(reservaCode, "n.Start < e.End && e.Start < n.End", "n.Start >= e.Start && n.Start < e.End", 1)
	devTest := strings.Replace(reservaTest, "WANT", "ErrConflict", 1)
	ct := func(name, execution string) string { return withHeaderField(quality[name], execution) }
	ctRun := func(result1, result2, wave string) map[string]string {
		achado := ""
		if result2 == "falhou" {
			achado = ", achado: ACH-02, evidencia: 'want conflict, got <nil>'"
		}
		return map[string]string{
			"docs/qa/cenarios/CT-01.md": ct("CT-01.md", "execucao: {teste: 'app/reserva_test.go::TestReserveFreeRoom', resultado: "+result1+", onda: "+wave+"}"),
			"docs/qa/cenarios/CT-02.md": ct("CT-02.md", "execucao: {teste: 'app/e2e/reserva_e2e_test.go::TestConflictStartingBefore', resultado: "+result2+achado+", onda: "+wave+"}"),
			"docs/qa/cenarios/CT-03.md": ct("CT-03.md", "execucao: {resultado: passou, evidencia: 'sem sessão: 401 em todas as rotas', onda: "+wave+"}"),
			"docs/qa/cenarios/CT-04.md": ct("CT-04.md", "execucao: {resultado: passou, evidencia: 'p95 de 40 ms com 100 reservas', onda: "+wave+"}"),
		}
	}
	report2 := strings.NewReplacer("REL-QA-01", "REL-QA-02", "onda: QA-02", "onda: QA-07", "execucao: 1", "execucao: 2",
		"passaram: 1, falharam: 1", "passaram: 2, falharam: 0", "ca_must_cobertos: 1", "ca_must_cobertos: 2",
		"resultado: falhou, achado: ACH-02", "resultado: passou", "achados_abertos: [ACH-02]", "achados_abertos: []",
		"veredito: reprovado", "veredito: aprovado", "Relatório de QA 01", "Relatório de QA 02").Replace(quality["REL-QA-01.md"])
	security2 := strings.NewReplacer("REL-SEG-01", "REL-SEG-02", "onda: SEG-03", "onda: SEG-04", "execucao: 1", "execucao: 2",
		"alto: {abertos: 1, fechados: 1, aceitos: 0}", "alto: {abertos: 1, fechados: 1, aceitos: 1}", "veredito: reprovado", "veredito: aprovado",
		"Relatório de segurança 01", "Relatório de segurança 02").Replace(quality["REL-SEG-01.md"])
	security2 = strings.Replace(security2, "abertos: 1, fechados: 1, aceitos: 1", "abertos: 0, fechados: 1, aceitos: 1", 1)
	validation := strings.Replace(team["VALIDACAO-01.md"], `  - {id: ARQ, versao: 1}
  - {id: ADR-01, versao: 1}
  - {id: T-01, versao: 1}
  - {id: DIC, versao: 1}
  - {id: PERSIST, versao: 1}
  - {id: TELAS, versao: 2}`, `  - {id: ARQ, versao: 3}
  - {id: ADR-01, versao: 2}
  - {id: T-01, versao: 2}
  - {id: DIC, versao: 1}
  - {id: PERSIST, versao: 1}
  - {id: TELAS, versao: 1}
  - {id: AMEACAS, versao: 1}
  - {id: CT-01, versao: 1}
  - {id: CT-02, versao: 1}
  - {id: CT-03, versao: 1}
  - {id: CT-04, versao: 1}`, 1)
	app := map[string]any{"directory": "app"}
	exec := func(id, category string, argv ...string) provider.ToolCall {
		args := map[string]any{"category": category, "argv": argv}
		for k, v := range app {
			args[k] = v
		}
		return call(id, runtime.ToolExecuteCommand, args)
	}
	write := func(id, p, content string) provider.ToolCall {
		return call(id, runtime.ToolWriteFile, map[string]string{"path": p, "content": content})
	}
	finish := func(id string) provider.ToolCall { return call(id, runtime.ToolFinish, map[string]any{}) }
	revalidation := func(id, result, evidence string) provider.ToolCall {
		return call(id, runtime.ToolFinish, map[string]any{"resultado": result, "evidencia": evidence})
	}
	devRework := func(n int, fix bool) func() provider.Message {
		tests := exec(fmt.Sprintf("x%dt", n), "tests", "go", "test", "-count=1", "-v", ".")
		calls := []provider.ToolCall{}
		if fix {
			calls = append(calls, write(fmt.Sprintf("x%dw", n), "app/reserva.go", reservaCode))
			tests = exec(fmt.Sprintf("x%dt", n), "tests", "go", "test", "-count=1", "-v", "./...")
		}
		calls = append(calls, exec(fmt.Sprintf("x%db", n), "build", "go", "build", "./..."), tests,
			exec(fmt.Sprintf("x%dl", n), "lint", "go", "vet", "./..."), finish(fmt.Sprintf("x%df", n)))
		return fixed(provider.Message{Calls: calls})
	}
	qaTests := func(id string) provider.ToolCall { return exec(id, "tests", "go", "test", "-count=1", "-v", "./...") }
	scans := func(prefix string) []provider.ToolCall {
		return []provider.ToolCall{
			exec(prefix+"1", "scanner", "opengrep", "scan", "--config", "auto", "app"),
			exec(prefix+"2", "scanner", "trivy", "fs", "--offline-scan", "--skip-db-update", "app"),
			exec(prefix+"3", "scanner", "gitleaks", "dir", "app"),
		}
	}
	approve := map[string]any{"parecer": "aprovado", "ressalvas": []string{}}
	dataFiles := map[string]string{"docs/dados/DIC.md": team["DIC.md"], "docs/dados/PERSIST.md": team["PERSIST.md"], "docs/dados/esquema.sql": team["esquema.sql"]}
	designFiles := map[string]string{"docs/design/TELAS.md": team["TELAS.md"], "docs/design/fluxo.md": team["fluxo.md"], "docs/design/wireframes.md": team["wireframes.md"],
		"docs/design/estilo.md": team["estilo.md"], "docs/design/prototipo/reservar.html": team["reservar.html"]}
	dbaGate := make(chan struct{})
	script := &qualityScript{requests: map[string]int{}, block: map[string]chan struct{}{"dba-agent": dbaGate},
		started: map[string]chan struct{}{"dba-agent": make(chan struct{})}, answers: map[string][]func() provider.Message{
			"requirements-agent": {fixed(provider.Message{Calls: append(writeAll(requirements, "r"), finish("rf"))})},
			"architect-agent": {
				fixed(provider.Message{Calls: append(writeAll(archFiles(1, "ARQ-01"), "a"), finish("af"))}),
				fixed(provider.Message{Calls: append(writeAll(arq2, "b"), finish("bf"))}),
				fixed(provider.Message{Calls: []provider.ToolCall{write("c1", "docs/arquitetura/ARQ.md", arq3), finish("cf")}}),
			},
			"qa-agent/PLANO": {fixed(provider.Message{Calls: []provider.ToolCall{
				write("p1", "docs/qa/cenarios/CT-01.md", quality["CT-01.md"]), write("p2", "docs/qa/cenarios/CT-02.md", quality["CT-02.md"]),
				write("p4", "docs/qa/cenarios/CT-03.md", quality["CT-03.md"]), write("p5", "docs/qa/cenarios/CT-04.md", quality["CT-04.md"]),
				write("p3", "docs/qa/plano.md", quality["plano.md"]), finish("pf")}})},
			"security-agent/AMEACAS": {fixed(provider.Message{Calls: []provider.ToolCall{
				write("s1", "docs/seguranca/ameacas.md", quality["ameacas.md"]), write("s2", "docs/achados/ACH-01.md", quality["ACH-01.md"]), finish("sf")}})},
			"security-agent/revalidation": {fixed(provider.Message{Calls: []provider.ToolCall{
				call("sr0", runtime.ToolReadFile, map[string]string{"path": "docs/arquitetura/ARQ.md"}),
				revalidation("sr1", "corrigido", "ARQ v3: o componente api autoriza pelo dono da reserva (AMEACAS/AM-1 mitigada no desenho).")}})},
			"dba-agent": {
				fixed(provider.Message{Calls: []provider.ToolCall{call("d0", runtime.ToolReadFile, map[string]string{"path": "docs/arquitetura/ARQ.md"})}}),
				fixed(provider.Message{Calls: append(writeAll(dataFiles, "dw"), finish("df"))}),
			},
			"designer-agent": {
				fixed(provider.Message{Calls: append(writeAll(designFiles, "gw"), finish("gf"))}),
				fixed(provider.Message{Calls: []provider.ToolCall{finish("gr")}}), // reevaluation without impact (US-34 CA-2)
			},
			"dba-agent/review":      {fixed(provider.Message{Calls: []provider.ToolCall{call("vd", runtime.ToolFinish, approve)}})},
			"designer-agent/review": {fixed(provider.Message{Calls: []provider.ToolCall{call("vg", runtime.ToolFinish, approve)}})},
			"architect-agent/validation": {fixed(provider.Message{Calls: []provider.ToolCall{
				write("va", "docs/validacao/VALIDACAO-01.md", validation), finish("vf")}})},
			"developer-agent": {
				fixed(provider.Message{Calls: []provider.ToolCall{
					write("e1", "app/go.mod", "module example.com/reserva\n\ngo 1.26\n"), write("e2", "app/reserva.go", buggy),
					write("e3", "app/reserva_test.go", devTest), write("e4", "app/README.md", "# Reserva\n\nTestes: `go test ./...` em app/.\n"),
					write("e5", "docs/arquitetura/tarefas/T-01.md", devTask),
					exec("e6", "build", "go", "build", "./..."), exec("e7", "tests", "go", "test", "-count=1", "-v", "./..."),
					exec("e8", "lint", "go", "vet", "./..."), finish("ef")}}),
				devRework(1, false), devRework(2, false), devRework(3, false), devRework(4, true),
			},
			"qa-agent/EXECUCAO": {
				func() provider.Message {
					calls := []provider.ToolCall{write("q1", "app/e2e/reserva_e2e_test.go", quality["reserva_e2e_test.go"]), qaTests("q2")}
					for p, content := range ctRun("passou", "falhou", "QA-02") {
						calls = append(calls, write("q3"+p, p, content))
					}
					return provider.Message{Calls: append(calls, write("q4", "docs/qa/REL-QA-01.md", quality["REL-QA-01.md"]),
						write("q5", "docs/achados/ACH-02.md", quality["ACH-02.md"]), finish("qf"))}
				},
				func() provider.Message {
					calls := []provider.ToolCall{qaTests("k1")}
					for p, content := range ctRun("passou", "passou", "QA-07") {
						calls = append(calls, write("k2"+p, p, content))
					}
					return provider.Message{Calls: append(calls, write("k3", "docs/qa/REL-QA-02.md", report2), finish("kf"))}
				},
			},
			"qa-agent/revalidation": {
				fixed(provider.Message{Calls: []provider.ToolCall{qaTests("r1"), revalidation("r1f", "nao_corrigido", "TestConflictStartingBefore ainda falha")}}),
				fixed(provider.Message{Calls: []provider.ToolCall{qaTests("r2"), revalidation("r2f", "nao_corrigido", "TestConflictStartingBefore ainda falha")}}),
				fixed(provider.Message{Calls: []provider.ToolCall{qaTests("r3"), revalidation("r3f", "nao_corrigido", "TestConflictStartingBefore ainda falha")}}),
				fixed(provider.Message{Calls: []provider.ToolCall{qaTests("r4"), revalidation("r4f", "corrigido", "go test ./...: ok, TestConflictStartingBefore passa")}}),
			},
			"security-agent/AUDITORIA": {
				fixed(provider.Message{Calls: append(scans("u"), write("u4", "docs/seguranca/REL-SEG-01.md", quality["REL-SEG-01.md"]),
					write("u5", "docs/achados/ACH-03.md", quality["ACH-03.md"]), write("u6", "docs/achados/ACH-04.md", quality["ACH-04.md"]), finish("uf"))}),
				fixed(provider.Message{Calls: append(scans("w"), write("w4", "docs/seguranca/REL-SEG-02.md", security2), finish("wf"))}),
			},
		}}
	runner := scannerRunner{real: sandboxRunner}
	gateway := localGateway{service: appgateway.NewService(opener, time.Minute, script)}
	work := newQualityWorker(t, db, gateway, runner, repos)
	registerModels(t, live, "anthropic")
	base := newProject(t, live, work.teamWorker, "Reserva com qualidade", "anthropic", "anthropic")

	ctx := context.Background()

	// Requisitos, then the QA plan (US-22 CA-1) beside the Arquitetura.
	work.settle(t)
	live.approve(t, base, "gate.requirements", 1)
	work.settle(t)
	if w := live.json(t, "GET", base+"/waves/QA-01", "", nil, 200); w["state"] != "completed" || w["stage"] != "qa-plan" || w["agent"] != "qa-agent" {
		t.Fatalf("US-22 CA-1: QA plan wave in parallel with the architecture: %v", w)
	}
	if cts := live.json(t, "GET", base+"/artifacts?type=CT", "", nil, 200)["items"].([]any); len(cts) != 4 {
		t.Fatalf("US-22 CA-1: CT tied to the criteria: %v", cts)
	}
	// US-31 CA-2, US-28 CA-3: a failing blocking check rejects the round without a human, with the check as
	// the reason; the link to an unknown id is pointed out.
	r1 := roundOf(t, live, base, "gate.architecture", 1)
	if r1["state"] != "rejected" || fmt.Sprint(failedChecks(r1)) != "[traceability.existing_references]" || !strings.Contains(fmt.Sprint(r1["checks"]), "unknown id US-99") ||
		r1["decision"].(map[string]any)["record"].(map[string]any)["per"].(map[string]any)["type"] != "orchestrator" {
		t.Fatalf("US-31 CA-2: %v", failedChecks(r1))
	}
	r2 := roundOf(t, live, base, "gate.architecture", 2)
	if r2["state"] != "awaiting_human" {
		t.Fatalf("gate.architecture round 2: %v", failedChecks(r2))
	}
	approval := live.send("POST", base+"/gates/gate.architecture/rounds/1/approval", confirmedChecks(t, r1),
		mergeHeaders(key(), map[string]string{"X-CSRF-Token": live.json(t, "GET", "/api/v1/me", "", nil, 200)["csrf_token"].(string)}))
	if approval.Response.StatusCode() != 409 {
		t.Fatalf("US-31 RN-1: no 'approve anyway' of a rejected round: %d", approval.Response.StatusCode())
	}
	live.approve(t, base, "gate.architecture", 2)
	work.drain(t)
	if _, err := work.progress.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	// US-34 CA-1: the DBA is working (its wave runs a model call) when the threat model of Segurança finds
	// that the approved ARQ lacks the access control: the Arquitetura reopens, ARQ goes back to draft in a
	// new version, Dados is blocked and its running wave pauses; the Design, approved, waits for a reevaluation.
	dba := make(chan error, 1)
	go func() { _, err := work.executor.RunOnce(ctx); dba <- err }()
	<-script.started["dba-agent"]
	if _, err := work.executor.RunOnce(ctx); err != nil { // DSG-01
		t.Fatal(err)
	}
	live.approve(t, base, "gate.design", 1)
	if _, err := work.progress.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := work.executor.RunOnce(ctx); err != nil { // SEG-01: AMEACAS and ACH-01
		t.Fatal(err)
	}
	close(dbaGate)
	if err := <-dba; err != nil {
		t.Fatal(err)
	}
	stages := stageStates(t, live, base)
	arq := live.json(t, "GET", base+"/artifacts/ARQ", "", nil, 200)
	if stages["architecture"]["state"] != "reopened" || stages["architecture"]["reopened_by_finding"] != "ACH-01" || arq["state"] != "draft" || arq["version"] != float64(3) ||
		stages["data"]["state"] != "blocked" || stages["design"]["state"] != "approved" || stages["design"]["reevaluation_pending"] != true {
		t.Fatalf("US-34 CA-1: %v %v %v %v", stages["architecture"], arq["state"], stages["data"], stages["design"])
	}
	if w := live.json(t, "GET", base+"/waves/DBA-01", "", nil, 200); w["state"] != "paused" {
		t.Fatalf("US-34 CA-1: the running DBA wave pauses: %v", w["state"])
	}
	// US-34 CA-3: the threat model that emitted the finding is not blocked; it revalidates.
	if stages["security-threats"]["state"] == "blocked" || stages["security-threats"]["type"] != "parallel_activity" {
		t.Fatalf("US-34 CA-3: %v", stages["security-threats"])
	}
	if round := roundOf(t, live, base, "gate.architecture", 2); round["state"] != "invalidated" {
		t.Fatalf("US-34: the approved round is invalidated: %v", round["state"])
	}
	work.settle(t) // ARQ-03 reworks the ARQ; the Segurança revalidates (SEG-02)
	finding := live.json(t, "GET", base+"/findings/ACH-01", "", nil, 200)
	if finding["status"] != "closed" || finding["resolution"] != "fixed" || finding["artifact"] != "ACH-01" || len(finding["cycle_history"].([]any)) != 1 {
		t.Fatalf("US-32 CA-2: ACH-01 revalidated by its author: %v\nwaves %v\nstages %v", finding, waveStates(t, live, base), live.json(t, "GET", base+"/waves/ARQ-03/calls?limit=50", "", nil, 200))
	}
	live.approve(t, base, "gate.architecture", 3)
	work.settle(t)
	// US-34 CA-2: with the Arquitetura approved again, Dados goes back (its wave resumes) and the Design,
	// approved, gets a reevaluation wave.
	if w := live.json(t, "GET", base+"/waves/DBA-01", "", nil, 200); w["state"] != "completed" {
		t.Fatalf("US-34 CA-2: the paused wave resumes: %v", w["state"])
	}
	stages = stageStates(t, live, base)
	if stages["data"]["state"] != "awaiting_gate" || stages["design"]["reevaluation_pending"] != false ||
		count(t, db, "SELECT count(*) FROM easycode.waves WHERE stage = 'design' AND purpose = 'reevaluation' AND state = 'completed'") != 1 {
		t.Fatalf("US-34 CA-2: %v %v", stages["data"], stages["design"])
	}
	live.approve(t, base, "gate.data", 1)
	work.settle(t) // Validação técnica (automatic): CT and AMEACAS approved in gate.validation
	validationRound := roundOf(t, live, base, "gate.validation", 1)
	if validationRound["state"] != "approved" || len(failedChecks(validationRound)) != 0 {
		t.Fatalf("gate.validation: %v", failedChecks(validationRound))
	}
	if threats := live.json(t, "GET", base+"/artifacts/AMEACAS", "", nil, 200); threats["state"] != "approved" {
		t.Fatalf("US-23 CA-1: AMEACAS valid and approved: %v", threats["state"])
	}

	// Implementação in the sandbox, then QA executes and finds a defect (US-22 CA-3).
	live.approve(t, base, "gate.implementation", 1)
	work.settle(t)
	qaRound := roundOf(t, live, base, "gate.qa", 1)
	if qaRound["state"] != "rejected" || !strings.Contains(fmt.Sprint(qaRound["blocking_findings"]), "ACH-02") {
		t.Fatalf("US-31: the high finding blocks gate.qa: %v", failedChecks(qaRound))
	}
	escalated := live.json(t, "GET", base+"/findings/ACH-02", "", nil, 200)
	if escalated["status"] != "escalated" || len(escalated["cycle_history"].([]any)) != 3 || escalated["destination_stage"] != "implementation" ||
		len(escalated["escalations"].([]any)) != 1 || !strings.Contains(fmt.Sprint(escalated["available_actions"]), "authorize_new_rework") {
		t.Fatalf("US-32 CA-1, CA-4: three cycles then escalated: %v", escalated)
	}
	// US-32 CA-5: the rework of the implementation is an internal cycle: its gate stays approved.
	if stages = stageStates(t, live, base); stages["implementation"]["state"] != "approved" || stages["qa"]["state"] != "returned" ||
		count(t, db, "SELECT count(*) FROM easycode.waves WHERE stage = 'implementation' AND purpose = 'rework'") != 3 {
		t.Fatalf("US-32 CA-5: %v", stages["implementation"])
	}
	if pending := live.json(t, "GET", "/api/v1/pending-items", "", nil, 200); !strings.Contains(fmt.Sprint(pending), "ACH-02") {
		t.Logf("pending items do not list the escalated finding yet: %v", pending)
	}
	// US-32 CA-6: the extra round needs a recent SSO authentication.
	stale := live
	stale.cookie = login(t, live.harness, devUser, time.Now().Add(-11*time.Minute))
	staleCSRF := stale.json(t, "GET", "/api/v1/me", "", nil, 200)["csrf_token"].(string)
	rc := stale.send("POST", base+"/findings/ACH-02/extra-round", `{"justification":"Mais uma rodada com a correção dos extremos"}`,
		mergeHeaders(key(), map[string]string{"X-CSRF-Token": staleCSRF}))
	if rc.Response.StatusCode() != 401 || decode(t, rc)["type"] != "urn:easycode:problem:reauthentication-required" {
		t.Fatalf("US-32 CA-6: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	csrf := live.json(t, "GET", "/api/v1/me", "", nil, 200)["csrf_token"].(string)
	extra := live.json(t, "POST", base+"/findings/ACH-02/extra-round", `{"justification":"Mais uma rodada com a correção dos extremos"}`,
		mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf}), 200)
	if extra["status"] != "in_rework" {
		t.Fatalf("US-32 CA-6: %v", extra)
	}
	work.settle(t) // DEV fixes (cycle 4), QA revalidates, the QA stage is redone (REL-QA-02)
	closed := live.json(t, "GET", base+"/findings/ACH-02", "", nil, 200)
	if closed["status"] != "closed" || len(closed["cycle_history"].([]any)) != 4 {
		t.Fatalf("US-32 CA-2: closed after the authorized round: %v", closed)
	}
	qa2 := roundOf(t, live, base, "gate.qa", 2)
	if qa2["state"] != "awaiting_human" {
		t.Fatalf("US-22 CA-2: gate.qa round 2: %v", failedChecks(qa2))
	}
	coverage := live.json(t, "GET", base+"/coverage", "", nil, 200)["items"].([]any)
	if len(coverage) != 1 || coverage[0].(map[string]any)["status"] != "covered" {
		t.Fatalf("US-22 CA-2, US-28 CA-2: every Must criterion with a passing CT: %v", coverage)
	}
	live.approve(t, base, "gate.qa", 2)
	work.settle(t)

	// Segurança audits with the scanners (US-23 CA-2, CA-3); a finding without evidence is discarded (US-32 CA-3).
	discarded := live.json(t, "GET", base+"/findings/ACH-03", "", nil, 200)
	if discarded["status"] != "discarded" || count(t, db, "SELECT count(*) FROM easycode.waves w JOIN easycode.artifacts a ON a.id = w.finding_id WHERE a.code = 'ACH-03'") != 0 {
		t.Fatalf("US-32 CA-3: %v", discarded)
	}
	vulnerability := live.json(t, "GET", base+"/findings/ACH-04", "", nil, 200)
	if vulnerability["status"] != "in_rework" || vulnerability["severity"] != "high" || vulnerability["category"] != "vulnerability" {
		t.Fatalf("US-23 CA-3: %v", vulnerability)
	}
	if round := roundOf(t, live, base, "gate.security", 1); round["state"] != "rejected" {
		t.Fatalf("gate.security round 1: %v", failedChecks(round))
	}
	// US-33: the approver accepts the risk with justification (CA-1, CA-2, CA-3, CA-5, CA-6).
	carla := live.as(t, oidctest.User{Subject: "carla", Name: "Carla Dias"})
	live.json(t, "PUT", base+"/members/carla", `{"roles":["member"]}`, nil, 200)
	carlaCSRF := carla.json(t, "GET", "/api/v1/me", "", nil, 200)["csrf_token"].(string)
	accept := base + "/findings/ACH-04/risk-acceptance"
	body := `{"justification":"Sem versão corrigida; cálculo isolado até a próxima release","valid_until":"` + time.Now().AddDate(0, 3, 0).Format("2006-01-02") + `"}`
	carla.json(t, "POST", accept, body, mergeHeaders(key(), map[string]string{"X-CSRF-Token": carlaCSRF}), 403)
	if rc := stale.send("POST", accept, body, mergeHeaders(key(), map[string]string{"X-CSRF-Token": staleCSRF})); rc.Response.StatusCode() != 401 {
		t.Fatalf("US-33 CA-5: %d", rc.Response.StatusCode())
	}
	live.json(t, "POST", accept, `{"justification":""}`, mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf}), 400)
	past := live.send("POST", accept, `{"justification":"Sem versão corrigida ainda","valid_until":"2020-01-01"}`, mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf}))
	if past.Response.StatusCode() != 400 || !strings.Contains(string(past.Response.Body()), "valid_until") {
		t.Fatalf("US-33 CA-6: %d %s", past.Response.StatusCode(), past.Response.Body())
	}
	accepted := live.json(t, "POST", accept, body, mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf}), 200)
	acceptance, _ := accepted["acceptance"].(map[string]any)
	if accepted["status"] != "accepted" || accepted["blocking"] != false || acceptance == nil || acceptance["per"].(map[string]any)["id"] != "dev" ||
		acceptance["justification"] == "" || acceptance["valid_until"] == nil {
		t.Fatalf("US-33 CA-1: %v", accepted)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'finding.accept_risk'"); n != 1 {
		t.Fatalf("RNF-13: acceptance in the audit trail (%d)", n)
	}
	work.settle(t) // the rework of ACH-04 is canceled; Segurança audits again (REL-SEG-02)
	if n := count(t, db, "SELECT count(*) FROM easycode.waves w JOIN easycode.artifacts a ON a.id = w.finding_id WHERE a.code = 'ACH-04' AND w.state = 'canceled'"); n != 1 {
		t.Fatalf("estados.yaml accept_risk: the rework wave of the accepted finding is canceled (%d)", n)
	}
	sec2 := roundOf(t, live, base, "gate.security", 2)
	if sec2["state"] != "awaiting_human" || roundChecks(sec2)["rel_seg.scans_executed"] != "passed" || roundChecks(sec2)["security.no_critical_risk"] != "passed" {
		t.Fatalf("US-23 CA-2: REL-SEG with the scanners run in the sandbox: %v", failedChecks(sec2))
	}
	live.approve(t, base, "gate.security", 2)
	work.settle(t)
	// US-33 RN-2 (risk_acceptance_same_cycle): who accepted a risk does not approve the Release of the cycle.
	// The Release stage is MARCO-6: its row and round are set up directly to reach the database rule.
	for _, sql := range []string{
		"INSERT INTO easycode.stages (project_id, stage, type, state) SELECT id, 'release', 'stage', 'blocked' FROM easycode.projects LIMIT 1",
		"UPDATE easycode.stages SET state = 'ready' WHERE stage = 'release'", "UPDATE easycode.stages SET state = 'running' WHERE stage = 'release'",
		"UPDATE easycode.stages SET state = 'awaiting_gate' WHERE stage = 'release'",
		"INSERT INTO easycode.gate_rounds (project_id, stage, stage_id, number, approval_mode) SELECT project_id, 'release', id, 1, 'human' FROM easycode.stages WHERE stage = 'release'",
		"UPDATE easycode.gate_rounds SET state = 'awaiting_human' WHERE stage = 'release'",
	} {
		if _, err := db.Admin.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	release := live.send("POST", base+"/gates/gate.release/rounds/1/approval", `{"confirmed_human_checks":[]}`, mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf}))
	if release.Response.StatusCode() != 409 || decode(t, release)["reason"] != "risk_acceptance_same_cycle" {
		t.Fatalf("US-33 RN-2: %d %s", release.Response.StatusCode(), release.Response.Body())
	}

	// US-28 CA-1: from the US to the decisions, the tasks, the screens, the CT, the commits, the tests and the findings.
	graph := live.json(t, "GET", base+"/artifacts/US-01/links?depth=3", "", nil, 200)
	types := map[string]bool{}
	for _, item := range graph["nodes"].([]any) {
		n := item.(map[string]any)
		types[n["type"].(string)] = true
		if n["exists"] != true {
			t.Fatalf("US-28: every link of the US exists: %v", n)
		}
	}
	for _, want := range []string{"T", "ADR", "TELAS", "CT", "commit", "test", "ACH", "ARQ"} {
		if !types[want] {
			t.Fatalf("US-28 CA-1: %s in the traceability of US-01: %v", want, graph)
		}
	}
	edge := graph["edges"].([]any)[0].(map[string]any)
	if edge["from"] == nil || edge["to"] == nil || edge["rule"] == nil {
		t.Fatalf("Traceability edges: %v", edge)
	}
	project := live.json(t, "GET", base, "", nil, 200)
	if open := project["summary"].(map[string]any)["open_findings"].(map[string]any); open["high"] != float64(0) {
		t.Fatalf("summary.open_findings: %v", open)
	}
	if list := live.json(t, "GET", base+"/findings?status=closed", "", nil, 200)["items"].([]any); len(list) != 2 {
		t.Fatalf("listFindings by status: %v", list)
	}
}

func roundChecks(round map[string]any) map[string]string {
	out := map[string]string{}
	for _, item := range round["checks"].([]any) {
		c := item.(map[string]any)
		out[c["check"].(string)] = c["status"].(string)
	}
	return out
}

// TestMarco5BlockingFindingReevaluatesTheRound is US-31 CA-3: with the round of a stage waiting for the
// human decision, a critical or high finding opened in the stage makes the round evaluate again; the
// approval is no longer available and the round shows the finding as the cause (gate.reevaluation_started).
func TestMarco5BlockingFindingReevaluatesTheRound(t *testing.T) {
	live := newLiveAPI(t, qualityDeps(t))
	db := live.db
	opener := sealKeys(t, db, map[string]string{"anthropic": anthropicKey})
	script := &qualityScript{requests: map[string]int{}, answers: map[string][]func() provider.Message{
		"requirements-agent": {fixed(provider.Message{Calls: append(writeAll(referenceFiles(t), "r"), call("rf", runtime.ToolFinish, map[string]any{}))})},
	}}
	work := newQualityWorker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, script)}, nil, git.New(t.TempDir()))
	registerModels(t, live, "anthropic")
	base := newProject(t, live, work.teamWorker, "Reserva", "anthropic", "anthropic")
	work.settle(t)
	round := roundOf(t, live, base, "gate.requirements", 1)
	if round["state"] != "awaiting_human" || round["reevaluation"] != nil {
		t.Fatalf("round 1: %v", failedChecks(round))
	}
	projectID := strings.TrimPrefix(base, "/api/v1/projects/")
	finding := strings.NewReplacer("onda: QA-02", "onda: REQ-01", "etapa_destino: implementacao", "etapa_destino: requisitos",
		"categoria: defeito", "categoria: requisito_ambiguo", "artefatos_afetados: [US-01/CA-2, app/reserva.go]", "artefatos_afetados: [US-01/CA-2]").
		Replace(folderFiles(t, "testdata/reserva-qualidade")["ACH-02.md"])
	ctx := context.Background()
	tx := postgres.NewTxRunner(db.PoolAs("easycode_worker"))
	if err := tx.InTx(ctx, func(ctx context.Context) error {
		if _, err := work.store.StoreVersion(ctx, appartifact.Input{ProjectID: projectID, WaveCode: "REQ-01", Agent: "requirements-agent",
			Path: "docs/achados/ACH-02.md", Content: []byte(finding)}); err != nil {
			return err
		}
		return work.findings.WaveCompleted(ctx, runtime.WaveOutcome{ProjectID: projectID, Wave: "REQ-01", Stage: "requirements", Agent: "requirements-agent",
			Purpose: runtime.PurposeExecution, Correlation: "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b40", Findings: []string{"ACH-02"}})
	}); err != nil {
		t.Fatal(err)
	}
	round = roundOf(t, live, base, "gate.requirements", 1)
	reevaluation, _ := round["reevaluation"].(map[string]any)
	if round["state"] != "rejected" || reevaluation["finding"] != "ACH-02" || reevaluation["evaluation"] != float64(2) ||
		!strings.Contains(fmt.Sprint(round["blocking_findings"]), "ACH-02") || len(round["available_actions"].([]any)) != 0 {
		t.Fatalf("US-31 CA-3: %v %v", round["state"], round["reevaluation"])
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.gate.reevaluation_started.v1'"); n != 1 {
		t.Fatalf("US-31 CA-3: gate.reevaluation_started (%d)", n)
	}
	csrf := live.json(t, "GET", "/api/v1/me", "", nil, 200)["csrf_token"].(string)
	if rc := live.send("POST", base+"/gates/gate.requirements/rounds/1/approval", confirmedChecks(t, round),
		mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf})); rc.Response.StatusCode() != 409 {
		t.Fatalf("US-31 CA-3: approval unavailable: %d", rc.Response.StatusCode())
	}
	// discardFinding needs the anti-CSRF token like the other decisions; a duplicate names its original.
	live.json(t, "POST", base+"/findings/ACH-02/discard", `{"reason":"no_evidence","justification":"evidência não reproduz"}`, key(), 403)
	discarded := live.json(t, "POST", base+"/findings/ACH-02/discard", `{"reason":"no_evidence","justification":"evidência não reproduz"}`,
		mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf}), 200)
	if discarded["status"] != "discarded" || discarded["discard_reason"] == nil {
		t.Fatalf("discard: %v", discarded)
	}
}
