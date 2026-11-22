package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/application/command"
	appevent "easycode/internal/application/event"
	appgate "easycode/internal/application/gate"
	appgateway "easycode/internal/application/gateway"
	apprepository "easycode/internal/application/repository"
	"easycode/internal/application/runtime"
	appsandbox "easycode/internal/application/sandbox"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/provider"
	"easycode/internal/domain/sandbox"
	"easycode/internal/infrastructure/docker"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/sealing"
	"easycode/internal/infrastructure/specs"
	httpapi "easycode/internal/interfaces/http"
	sandboxapi "easycode/internal/interfaces/sandbox"
)

// agentScripts answers each agent with its own fixed script (no network, never the real API): the
// agent is recognized by the "`autor` é <agent>" line of the system prompt the harness writes.
type agentScripts struct {
	mu       sync.Mutex
	scripts  map[string][]provider.Message
	requests map[string][]provider.Request
}

func (p *agentScripts) Name() string { return "anthropic" }

func (p *agentScripts) Capabilities(context.Context, string) (provider.Capabilities, error) {
	return provider.Capabilities{ToolUse: true, ContextTokens: 200_000}, nil
}

func (p *agentScripts) TestKey(context.Context, provider.Secret) error { return nil }

func (p *agentScripts) Complete(_ context.Context, req provider.Request, _ func(provider.Chunk)) (provider.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	agent := ""
	for name := range p.scripts {
		if strings.Contains(req.System, "`autor` é "+name) {
			agent = name
		}
	}
	p.requests[agent] = append(p.requests[agent], req)
	script := p.scripts[agent]
	if len(script) == 0 {
		return provider.Response{}, &provider.Error{Class: provider.ErrorRequest, Message: "no script for " + agent}
	}
	p.scripts[agent] = script[1:]
	return provider.Response{Message: script[0], Usage: provider.Usage{Input: 900, Output: 200}, Stop: provider.StopTool, ProviderID: "req"}, nil
}

func folderFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(raw)
	}
	return files
}

func writeAll(files map[string]string, idPrefix string) []provider.ToolCall {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var calls []provider.ToolCall
	for i, p := range paths {
		calls = append(calls, call(fmt.Sprintf("%s%d", idPrefix, i), runtime.ToolWriteFile, map[string]string{"path": p, "content": files[p]}))
	}
	return calls
}

const reservaCode = `package reserva

import "errors"

// Booking is a reservation of a room between two instants (minutes of the day).
type Booking struct {
	Room       string
	Start, End int
}

// ErrConflict refuses a booking of the same room at the same time (US-01 RN-1).
var ErrConflict = errors.New("conflito de horário")

// Book keeps the bookings.
type Book struct{ items []Booking }

// Reserve records the booking unless it overlaps another one of the same room.
func (b *Book) Reserve(n Booking) error {
	for _, e := range b.items {
		if e.Room == n.Room && n.Start < e.End && e.Start < n.End {
			return ErrConflict
		}
	}
	b.items = append(b.items, n)
	return nil
}

// Upcoming lists the bookings.
func (b *Book) Upcoming() []Booking { return b.items }
`

const reservaTest = `package reserva

import "testing"

func TestReserveFreeRoom(t *testing.T) { // US-01 CA-1
	var b Book
	if err := b.Reserve(Booking{Room: "A", Start: 600, End: 660}); err != nil || len(b.Upcoming()) != 1 {
		t.Fatal(err)
	}
}

func TestReserveConflict(t *testing.T) { // US-01 CA-2
	var b Book
	_ = b.Reserve(Booking{Room: "A", Start: 600, End: 660})
	if err := b.Reserve(Booking{Room: "A", Start: 630, End: 690}); err != WANT {
		t.Fatal(err)
	}
}
`

func phase1Scripts(t *testing.T) map[string][]provider.Message {
	requirements := provider.Message{Calls: append(writeAll(referenceFiles(t), "r"), call("rf", runtime.ToolFinish, map[string]any{}))}
	architecture := folderFiles(t, "testdata/reserva-arquitetura")
	archFiles := map[string]string{
		"docs/arquitetura/ARQ.md": architecture["ARQ.md"], "docs/arquitetura/adr/ADR-01.md": architecture["ADR-01.md"],
		"docs/arquitetura/tarefas/T-01.md": architecture["T-01.md"], "docs/arquitetura/estrutura.md": architecture["estrutura.md"],
		"docs/arquitetura/api/openapi.yaml": architecture["openapi.yaml"],
	}
	architect := []provider.Message{
		{Calls: []provider.ToolCall{
			call("a1", runtime.ToolReadFile, map[string]string{"path": "docs/requisitos/US-01-reservar-sala.md"}),
			// US-30 CA-2: an approved artifact of another agent is never written.
			call("a2", runtime.ToolWriteFile, map[string]string{"path": "docs/requisitos/US-01-reservar-sala.md", "content": "x"}),
			// RNF-07: the architect only validates the contract; building code is outside its list.
			call("a3", runtime.ToolExecuteCommand, map[string]any{"category": "build", "argv": []string{"go", "build", "./..."}}),
		}},
		{Calls: append(writeAll(archFiles, "aw"), call("af", runtime.ToolFinish, map[string]any{}))},
	}
	build := map[string]any{"category": "build", "argv": []string{"go", "build", "./..."}, "directory": "app"}
	tests := map[string]any{"category": "tests", "argv": []string{"go", "test", "-count=1", "-v", "./..."}, "directory": "app"}
	lint := map[string]any{"category": "lint", "argv": []string{"go", "vet", "./..."}, "directory": "app"} // code.lint_clean (US-31)
	developer := []provider.Message{
		{Calls: []provider.ToolCall{
			call("d1", runtime.ToolReadFile, map[string]string{"path": "docs/arquitetura/tarefas/T-01.md"}),
			// US-19 CA-3: the approved ADR is not changed by the developer.
			call("d2", runtime.ToolWriteFile, map[string]string{"path": "docs/arquitetura/adr/ADR-01.md", "content": "x"}),
			// RNF-07: curl is not an executable of the categories of the developer.
			call("d3", runtime.ToolExecuteCommand, map[string]any{"category": "tests", "argv": []string{"curl", "https://proxy.golang.org"}}),
			call("d4", runtime.ToolWriteFile, map[string]string{"path": "app/go.mod", "content": "module example.com/reserva\n\ngo 1.26\n"}),
			call("d5", runtime.ToolWriteFile, map[string]string{"path": "app/reserva.go", "content": reservaCode}),
			call("d6", runtime.ToolWriteFile, map[string]string{"path": "app/reserva_test.go", "content": strings.Replace(reservaTest, "WANT", "nil", 1)}),
			call("d7", runtime.ToolExecuteCommand, build),
			call("d8", runtime.ToolExecuteCommand, tests), // fails: CA-2 the wave does not finish with red tests
			call("d9", runtime.ToolFinish, map[string]any{}),
		}},
		{Calls: []provider.ToolCall{
			call("d10", runtime.ToolWriteFile, map[string]string{"path": "app/reserva_test.go", "content": strings.Replace(reservaTest, "WANT", "ErrConflict", 1)}),
			call("d11", runtime.ToolWriteFile, map[string]string{"path": "app/README.md", "content": "# Reserva\n\nTestes: `go test ./...` em app/.\n"}),
			call("d12", runtime.ToolExecuteCommand, build),
			call("d13", runtime.ToolExecuteCommand, tests),
			call("d13l", runtime.ToolExecuteCommand, lint),
			call("d14", runtime.ToolFinish, map[string]any{}),
		}},
	}
	return map[string][]provider.Message{"requirements-agent": {requirements}, "architect-agent": architect, "developer-agent": developer}
}

// startRunner serves an in-process easycode-sandbox on a Unix socket over the real Docker daemon.
func startRunner(t *testing.T) *sandboxapi.Client {
	t.Helper()
	socket, image := os.Getenv("EASYCODE_TEST_DOCKER_SOCKET"), os.Getenv("EASYCODE_TEST_SANDBOX_IMAGE")
	if socket == "" || image == "" {
		t.Skip("EASYCODE_TEST_DOCKER_SOCKET and EASYCODE_TEST_SANDBOX_IMAGE not set: the MARCO-3 flow needs the sandbox")
	}
	runner := appsandbox.NewRunner(docker.New(socket), specs.AgentPolicy, appsandbox.Config{Images: map[string]string{"go": image}, Runtime: "runc",
		Defaults: sandbox.Limits{CPU: 2, MemoryMiB: 4096, DiskMiB: 1024, Time: 5 * time.Minute, Processes: 256, MaxOutputBytes: 1 << 20}})
	path := filepath.Join(t.TempDir(), "runner.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: sandboxapi.NewServer(runner, slog.New(slog.NewJSONHandler(io.Discard, nil))).Handler(), ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return sandboxapi.NewClient(path)
}

type phase1Worker struct {
	dispatcher *command.Dispatcher
	progress   *appgate.Progress
	executor   *runtime.Executor
}

func newPhase1Worker(t *testing.T, db *postgrestest.Database, gateway runtime.Gateway, runner runtime.CommandRunner, repos git.Repositories) phase1Worker {
	t.Helper()
	pool := db.PoolAs("easycode_worker")
	tx := postgres.NewTxRunner(pool)
	outbox := postgres.NewOutbox(pool)
	wf, err := specs.BasicWorkflow()
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
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		t.Fatal(err)
	}
	store := appartifact.NewStore(tx, specs.HeaderParser{}, validator, postgres.NewArtifacts(pool), outbox)
	progress := newProgress(t, tx, pool, store, validator, flow{workflows, orchestrator}, outbox, wf.Definition)
	ledger := postgres.NewLedger(pool)
	models := runtime.NewGatewayModels(gateway, postgres.NewProviderKeys(pool), ledger, ledger)
	agents := map[string]runtime.Agent{}
	for _, name := range []string{"requirements-agent", "architect-agent", "developer-agent"} {
		policy, err := specs.AgentPolicy(name)
		if err != nil {
			t.Fatal(err)
		}
		agents[name] = runtime.Agent{Steps: runtime.NewToolAgent(models, 4000, runner), Policy: &policy}
	}
	outputs := func(stage string) []string {
		definition, _ := wf.Definition.Stage(stage)
		var types []string
		seen := map[string]bool{}
		for _, o := range definition.Outputs {
			if o.Required && o.Type != "" && !o.Attachment && !seen[o.Type] {
				seen[o.Type] = true
				types = append(types, o.Type)
			}
		}
		return types
	}
	attachments := func(stage string) []string {
		if stage == "architecture" {
			return []string{"docs/arquitetura/estrutura.md", "docs/arquitetura/api/"}
		}
		return nil
	}
	executor := runtime.NewExecutor(tx, ledger, agents, func(agent string) (string, string, error) {
		d, err := specs.DefaultAgent(agent)
		return string(d.Instruction), d.InstructionHash, err
	}, outbox, postgres.NewTelemetryNotifier(pool), store, progress, outputs,
		runtime.Settings{Worker: "worker-1", Lease: 15 * time.Second, MaxCorrections: 3, RetryBackoff: 10 * time.Millisecond, Poll: 50 * time.Millisecond,
			Image: "go", ProjectLimits: sandbox.Limits{CPU: 1, MemoryMiB: 2048, DiskMiB: 512, Time: 3 * time.Minute}},
		slog.New(slog.NewJSONHandler(io.Discard, nil))).WithRepository(repos, attachments)
	return phase1Worker{dispatcher: dispatcher, progress: progress, executor: executor}
}

func (w phase1Worker) runWave(t *testing.T, live liveAPI, base, wave, want string) map[string]any {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		ran, err := w.executor.RunOnce(ctx)
		if err != nil {
			t.Fatalf("%s: %v", wave, err)
		}
		if !ran {
			break
		}
	}
	got := live.json(t, "GET", base+"/waves/"+wave, "", nil, 200)
	if got["state"] != want {
		t.Fatalf("%s: want %s, got %v (%v)", wave, want, got["state"], got["failure_cause"])
	}
	return got
}

func (l liveAPI) approve(t *testing.T, base, gate string, round int) {
	t.Helper()
	csrf := l.json(t, "GET", "/api/v1/me", "", nil, 200)["csrf_token"].(string)
	current := l.json(t, "GET", fmt.Sprintf("%s/gates/%s/rounds/%d", base, gate, round), "", nil, 200)
	decided := l.json(t, "POST", fmt.Sprintf("%s/gates/%s/rounds/%d/approval", base, gate, round), confirmedChecks(t, current),
		mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf}), 200)
	if decided["state"] != "approved" {
		t.Fatalf("%s approval: %v", gate, decided)
	}
}

// confirmedChecks is the approval body that confirms every human check of the round (US-06 RN-3).
func confirmedChecks(t *testing.T, round map[string]any) string {
	t.Helper()
	ids := []string{}
	for _, item := range round["checks"].([]any) {
		if c := item.(map[string]any); c["type"] == "human" {
			ids = append(ids, c["check"].(string))
		}
	}
	body, _ := json.Marshal(map[string]any{"confirmed_human_checks": ids})
	return string(body)
}

func checksOf(t *testing.T, round map[string]any) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, item := range round["checks"].([]any) {
		c := item.(map[string]any)
		out[c["check"].(string)] = c["status"].(string)
	}
	return out
}

// TestMarco3RequirementsToCodeEndToEnd is the MARCO-3 done criterion (PLANO, closes Phase 1): the
// reference project goes Requisitos → Arquitetura → Implementação with human approvals, through the
// scripted provider (never the real API); every agent uses only its tools (US-30, RNF-07); the developer
// builds and tests in the real sandbox, without network and with limits (US-19, RNF-02, RNF-03); the
// evidence is in the API (US-41) and the project repository is cloned with code, tests and docs (US-42).
func TestMarco3RequirementsToCodeEndToEnd(t *testing.T) {
	runner := startRunner(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repos := git.New(t.TempDir())
	broker := appevent.NewBroker()
	live := newLiveAPI(t, liveRuntimeDeps(broker, make(chan struct{})), func(d *httpapi.Dependencies, db *postgrestest.Database) {
		wf, _ := specs.BasicWorkflow()
		projects := postgres.NewProjects(db.PoolAs("easycode_api"), wf.Definition.ID, len(wf.Definition.Stages))
		d.Repository = apprepository.NewService(projects, repos, "https://easycode.test", git.MainBranch)
	})
	db, ctx := live.db, context.Background()

	dir := t.TempDir()
	if _, err := sealing.GenerateKeyPair(filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")); err != nil {
		t.Fatal(err)
	}
	sealer, _ := sealing.LoadSealer(filepath.Join(dir, "public.pem"))
	opener, _ := sealing.LoadOpener(filepath.Join(dir, "private.pem"))
	sealed, ref, _ := sealer.Seal([]byte("sk-ant-e2e-0000000000"))
	var keyID int64
	if err := db.Admin.QueryRow(ctx, `INSERT INTO easycode.provider_keys (organization_id, provider, encrypted_value, encryption_key_ref, last4, created_by)
SELECT organization_id, 'anthropic', $1, $2, '0000', id FROM easycode.users WHERE sub = 'dev' RETURNING id`, sealed, ref).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Admin.Exec(ctx, "UPDATE easycode.provider_keys SET state = 'active', tested_at = now(), test_result = 'valid' WHERE id = $1", keyID); err != nil {
		t.Fatal(err)
	}
	scripts := &agentScripts{scripts: phase1Scripts(t), requests: map[string][]provider.Request{}}
	work := newPhase1Worker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, scripts)}, runner, repos)

	for _, model := range []string{"reasoning-model", "execution-model"} {
		live.json(t, "PUT", "/api/v1/providers/anthropic/models/"+model,
			`{"display_name":"`+model+`","capabilities":{"tool_use":true,"context_tokens":200000,"prompt_cache":true,"image_input":false},"active":true}`, nil, 200)
		live.json(t, "POST", "/api/v1/providers/anthropic/models/"+model+"/prices", `{"effective_since":"`+time.Now().UTC().Format(time.RFC3339)+
			`","currency":"BRL","per_million_tokens":{"input":"20","output":"100","read_cache":"2","write_cache":"25"},"source":"e2e"}`, key(), 201)
	}
	need, err := specs.ReferenceNeed()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"name": "Reserva de salas", "business_need": need, "provider_submission_acknowledgment": true,
		"budget":      map[string]any{"stop_limit": map[string]string{"value": "200.00", "currency": "BRL"}},
		"constraints": map[string]any{"items": []map[string]string{{"category": "language", "description": "Backend em Go"}}}})
	projectID := live.json(t, "POST", "/api/v1/projects", string(body), key(), 201)["id"].(string)
	base := "/api/v1/projects/" + projectID
	live.json(t, "PATCH", base+"/configuration", `{"model_pair":{"reasoning":{"provider":"anthropic","model":"reasoning-model"},"execution":{"provider":"anthropic","model":"execution-model"}}}`,
		map[string]string{"If-Match": `"1"`}, 200)
	live.json(t, "POST", base+"/start", "", key(), 202)
	if _, err := work.dispatcher.Drain(ctx); err != nil {
		t.Fatal(err)
	}

	// Requisitos: the wave commits its artifacts to the project repository (D-17).
	if empty := live.json(t, "GET", base+"/repository", "", nil, 200); empty["status"] != "not_created" || empty["last_commit"] != nil {
		t.Fatalf("getRepository before the first commit: %v", empty)
	}
	req := work.runWave(t, live, base, "REQ-01", "completed")
	if req["git_commit"] == nil {
		t.Fatalf("REQ-01 commit: %v", req)
	}
	// US-18 CA-2: without approved requirements no architecture wave exists.
	if n := count(t, db, "SELECT count(*) FROM easycode.waves WHERE stage = 'architecture'"); n != 0 {
		t.Fatalf("US-18 CA-2: architecture wave before the approval (%d)", n)
	}
	live.approve(t, base, "gate.requirements", 1)
	if _, err := work.progress.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	// Arquitetura (US-18): reads the approved requirements, cannot write them nor build code.
	work.runWave(t, live, base, "ARQ-01", "completed")
	if system := scripts.requests["architect-agent"][0].System; !strings.Contains(system, "RESTR-01 (language): Backend em Go") {
		t.Fatal("US-18 CA-4: the constraints are an input of the architecture")
	}
	if prompt := fmt.Sprint(scripts.requests["architect-agent"][0].Messages); !strings.Contains(prompt, "docs/requisitos/US-01-reservar-sala.md") {
		t.Fatal("US-18: the approved requirements are the input of the architect")
	}
	if read := fmt.Sprint(scripts.requests["architect-agent"][1].Messages); !strings.Contains(read, "reservar uma sala disponível") {
		t.Fatal("US-18: read_file returns the approved US")
	}
	// US-30 CA-2: the write on the approved US of the requirements agent is refused and recorded.
	if n := count(t, db, "SELECT count(*) FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id WHERE w.code = 'ARQ-01' AND t.refusal_reason = 'foreign_artifact_write'"); n != 1 {
		t.Fatalf("US-30 CA-2: foreign approved artifact (%d)", n)
	}
	tools := map[string]bool{}
	for _, definition := range scripts.requests["architect-agent"][0].Tools {
		tools[definition.Name] = true
	}
	if !tools["read_file"] || !tools["write_file"] || !tools["execute_command"] || tools["git"] {
		t.Fatalf("RNF-07: the model only receives the allowed tools: %v", tools)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id WHERE w.code = 'ARQ-01' AND t.refusal_reason = 'command_not_allowed'"); n != 1 {
		t.Fatalf("RNF-07: build refused to the architect, recorded with the wave (%d)", n)
	}
	types := map[string]int{}
	for _, item := range live.json(t, "GET", base+"/artifacts?limit=50&stage=architecture", "", nil, 200)["items"].([]any) {
		types[item.(map[string]any)["type"].(string)]++
	}
	if types["ARQ"] != 1 || types["ADR"] != 1 || types["T"] != 1 {
		t.Fatalf("US-18 CA-1: ARQ, at least one ADR and the T: %v", types)
	}
	round := live.json(t, "GET", base+"/gates/gate.architecture/rounds/1", "", nil, 200)
	if checks := checksOf(t, round); round["state"] != "awaiting_human" || checks["artifact.schema_valid"] != "passed" || checks["traceability.t_points_to_us"] != "passed" {
		t.Fatalf("gate.architecture: %v", round)
	}
	live.approve(t, base, "gate.architecture", 1)
	if _, err := work.progress.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	tasks := live.json(t, "GET", base+"/artifacts?type=T", "", nil, 200)["items"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["situation"] != "planejada" {
		t.Fatalf("ArtifactSummary.situation of the T: %v", tasks)
	}
	waves := live.json(t, "GET", base+"/waves?stage=implementation", "", nil, 200)["items"].([]any)
	if len(waves) != 1 || waves[0].(map[string]any)["task"] != "T-01" {
		t.Fatalf("PL-20: one implementation wave per approved task: %v", waves)
	}

	// Implementação (US-19): code and tests in the sandbox, red tests refused, then green.
	dev := work.runWave(t, live, base, "DEV-01", "completed")
	sha, _ := dev["git_commit"].(string)
	if len(sha) != 40 {
		t.Fatalf("US-19 CA-1: commit of the wave: %v", dev)
	}
	executions := live.json(t, "GET", base+"/waves/DEV-01/sandbox-executions", "", nil, 200)["items"].([]any)
	if len(executions) != 5 {
		t.Fatalf("US-41 CA-1: every command of the wave: %v", executions)
	}
	for _, item := range executions {
		e := item.(map[string]any)
		if e["exit_code"] == nil || e["duration_ms"] == nil || e["end_reason"] != "completed" || e["output_start"] == nil || e["command"] == "" {
			t.Fatalf("US-41 CA-1: command, exit code, duration and output: %v", e)
		}
	}
	red, green := executions[1].(map[string]any), executions[3].(map[string]any)
	redTests, _ := red["tests"].(map[string]any)
	greenTests, _ := green["tests"].(map[string]any)
	if red["category"] != "tests" || red["exit_code"] == float64(0) || redTests["failed"] != float64(1) ||
		greenTests["total"] != float64(2) || greenTests["passed"] != float64(2) || greenTests["failed"] != float64(0) {
		t.Fatalf("US-41 CA-2, US-19 CA-2: test counts: %v %v", red, green)
	}
	rc := live.send("GET", base+"/waves/DEV-01/sandbox-executions/"+green["id"].(string)+"/output", "", nil)
	if rc.Response.StatusCode() != 200 || !strings.Contains(string(rc.Response.Body()), "--- PASS: TestReserveConflict") ||
		!strings.HasPrefix(string(rc.Response.Header.Peek("Content-Disposition")), "attachment;") || string(rc.Response.Header.Peek("X-Content-Type-Options")) != "nosniff" {
		t.Fatalf("US-41 CA-1: full output to download: %d %s", rc.Response.StatusCode(), rc.Response.Header.String())
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.wave.task_failed.v1' AND envelope->'data'->>'task' = 'T-01'"); n != 1 {
		t.Fatalf("US-19 CA-2: the red tests of the task are recorded (%d)", n)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id WHERE w.code = 'DEV-01' AND t.tool = 'finish' AND t.status = 'error'"); n != 1 {
		t.Fatalf("US-19 CA-2: finish refused with red tests (%d)", n)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.tool_executions t JOIN easycode.waves w ON w.id = t.wave_id WHERE w.code = 'DEV-01' AND t.refusal_reason = 'foreign_artifact_write'"); n != 1 {
		t.Fatalf("US-19 CA-3: the approved ADR is not changed (%d)", n)
	}
	round = live.json(t, "GET", base+"/gates/gate.implementation/rounds/1", "", nil, 200)
	if checks := checksOf(t, round); round["state"] != "awaiting_human" || checks["code.build_green"] != "passed" || checks["tests.executed_green"] != "passed" {
		t.Fatalf("gate.implementation: %v", round)
	}
	live.approve(t, base, "gate.implementation", 1)
	if _, err := work.progress.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	stages := live.json(t, "GET", base+"/workflow", "", nil, 200)["stages"].([]any)
	for _, item := range stages {
		if s := item.(map[string]any); s["state"] != "approved" {
			t.Fatalf("Phase 1 done: every stage approved: %v", stages)
		}
	}

	// US-42: getRepository and the clone with the session credential.
	repository := live.json(t, "GET", base+"/repository", "", nil, 200)
	last, _ := repository["last_commit"].(map[string]any)
	if repository["status"] != "available" || repository["clone_url"] != "https://easycode.test/git/"+projectID+".git" || repository["main_branch"] != "main" || last["sha"] != sha || last["wave"] != "DEV-01" {
		t.Fatalf("getRepository: %v", repository)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &fasthttp.Server{Handler: live.handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown() })
	clone := func(who liveAPI, target string) ([]byte, error) {
		token := strings.TrimPrefix(who.cookie, "ec_session=")
		cmd := exec.Command("git", "clone", "-q", fmt.Sprintf("http://easycode:%s@%s/git/%s.git", token, listener.Addr(), projectID), target)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1"}
		return cmd.CombinedOutput()
	}
	target := filepath.Join(t.TempDir(), "clone")
	if out, err := clone(live, target); err != nil {
		t.Fatalf("US-42 CA-1: clone: %v %s", err, out)
	}
	for _, p := range []string{"app/reserva.go", "app/reserva_test.go", "docs/arquitetura/ARQ.md", "docs/arquitetura/tarefas/T-01.md", "docs/requisitos/US-01-reservar-sala.md"} {
		if _, err := os.Stat(filepath.Join(target, p)); err != nil {
			t.Fatalf("US-42 CA-1: %s in the clone: %v", p, err)
		}
	}
	story, _ := os.ReadFile(filepath.Join(target, "docs/requisitos/US-01-reservar-sala.md"))
	approved := live.json(t, "GET", base+"/artifacts/US-01", "", nil, 200)
	if approved["state"] != "approved" || !strings.Contains(string(story), fmt.Sprintf("versao: %v\n", approved["version"])) {
		t.Fatalf("US-42 CA-2: the file has the approved version: %v", approved["version"])
	}
	log, _ := exec.Command("git", "-C", target, "log", "--format=%s").Output()
	if !strings.Contains(string(log), "T-01: implementação da tarefa (DEV-01)") {
		t.Fatalf("US-19 CA-1: the commit cites the task: %s", log)
	}
	outsider := live.as(t, oidctest.User{Subject: "carla", Name: "Carla Dias"})
	if out, err := clone(outsider, filepath.Join(t.TempDir(), "denied")); err == nil || !strings.Contains(string(out), "not found") {
		t.Fatalf("US-42 CA-3: a user without role cannot clone: %v %s", err, out)
	}
	outsider.json(t, "GET", base+"/repository", "", nil, 404)
}
