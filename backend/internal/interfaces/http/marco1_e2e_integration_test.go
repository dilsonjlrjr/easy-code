package http_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	"easycode/internal/application/runtime"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/sealing"
	"easycode/internal/infrastructure/specs"
	httpapi "easycode/internal/interfaces/http"
)

// scriptedProvider answers the requirements agent with a fixed script (no network). The call at
// blockAt waits until its context ends the first time: the worker "dies" in the middle of it.
type scriptedProvider struct {
	mu       sync.Mutex
	script   []provider.Message
	next     int
	calls    int
	blockAt  int
	blocked  chan struct{}
	didBlock bool
	requests []provider.Request
}

func (p *scriptedProvider) Name() string { return "anthropic" }

func (p *scriptedProvider) Capabilities(context.Context, string) (provider.Capabilities, error) {
	return provider.Capabilities{ToolUse: true, ContextTokens: 200_000}, nil
}

func (p *scriptedProvider) TestKey(context.Context, provider.Secret) error { return nil }

func (p *scriptedProvider) Complete(ctx context.Context, req provider.Request, _ func(provider.Chunk)) (provider.Response, error) {
	if key, ok := provider.KeyFromContext(ctx); !ok || string(key.Reveal()) != "sk-ant-e2e-0000000000" {
		return provider.Response{}, &provider.Error{Class: provider.ErrorAuthentication, Message: "bad key"}
	}
	p.mu.Lock()
	p.calls++
	p.requests = append(p.requests, req)
	if p.next == p.blockAt && !p.didBlock {
		p.didBlock = true
		p.mu.Unlock()
		close(p.blocked)
		<-ctx.Done()
		return provider.Response{}, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted"}
	}
	answer := p.script[min(p.next, len(p.script)-1)]
	p.next++
	p.mu.Unlock()
	return provider.Response{Message: answer, Usage: provider.Usage{Input: 1200, Output: 300, CacheRead: 100}, Stop: provider.StopTool, ProviderID: "req"}, nil
}

// localGateway runs the gateway use case in process, with the real platform key opener.
type localGateway struct{ service *appgateway.Service }

func (g localGateway) OpenSession(ctx context.Context, scope appgateway.Scope, sealed []byte) (string, error) {
	return g.service.Open(ctx, scope, sealed)
}

func (g localGateway) Call(ctx context.Context, id string, req provider.Request, onChunk func(provider.Chunk)) (provider.Response, error) {
	return g.service.Call(ctx, id, req, onChunk)
}

func (g localGateway) CloseSession(_ context.Context, id string) error { return g.service.Close(id) }

func call(id, name string, args any) provider.ToolCall {
	raw, _ := json.Marshal(args)
	return provider.ToolCall{ID: id, Name: name, Args: raw}
}

func referenceFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	entries, err := os.ReadDir("testdata/reserva")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join("testdata/reserva", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files["docs/requisitos/"+entry.Name()] = string(raw)
	}
	return files
}

func script(t *testing.T) []provider.Message {
	files := referenceFiles(t)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	ask := provider.Message{Calls: []provider.ToolCall{call("t1", runtime.ToolAskUser, map[string]any{"items": []map[string]string{
		{"text": "Reservas recorrentes entram no escopo?", "suggestion": "Não no MVP"},
		{"text": "Qual a duração mínima de uma reserva?", "suggestion": "30 minutos"},
	}})}}
	first := provider.Message{}
	for i, path := range paths {
		content := files[path]
		if strings.HasSuffix(path, "VISAO.md") { // US-29 CA-1: first delivery out of schema
			content = strings.Replace(content, "objetivo: Reservar salas pelo navegador sem conflito de horário.\n", "", 1)
		}
		first.Calls = append(first.Calls, call(fmt.Sprintf("w%d", i), runtime.ToolWriteFile, map[string]string{"path": path, "content": content}))
	}
	first.Calls = append(first.Calls, call("f1", runtime.ToolFinish, map[string]any{}))
	fix := provider.Message{Calls: []provider.ToolCall{
		call("w9", runtime.ToolWriteFile, map[string]string{"path": "docs/requisitos/VISAO.md", "content": files["docs/requisitos/VISAO.md"]}),
		call("w10", runtime.ToolWriteFile, map[string]string{"path": "../escape.md", "content": "x"}), // refused (RN-1)
		call("f2", runtime.ToolFinish, map[string]any{}),
	}}
	return []provider.Message{ask, first, fix}
}

type worker struct {
	dispatcher *command.Dispatcher
	progress   *appgate.Progress
	executor   func(name string) *runtime.Executor
}

func newWorker(t *testing.T, db *postgrestest.Database, gateway runtime.Gateway) worker {
	t.Helper()
	pool := db.PoolAs("easycode_worker")
	tx := postgres.NewTxRunner(pool)
	outbox := postgres.NewOutbox(pool)
	wf, err := specs.BasicWorkflow()
	if err != nil {
		t.Fatal(err)
	}
	workflows := postgres.NewWorkflow(pool, wf.Definition)
	agents := func(agent string) (appworkflow.AgentConfiguration, error) {
		d, err := specs.DefaultAgent(agent)
		return appworkflow.AgentConfiguration{Agent: agent, InstructionVersion: d.InstructionVersion, InstructionHash: d.InstructionHash, Tools: d.Tools, Limits: d.Limits}, err
	}
	orchestrator := appworkflow.NewOrchestrator(workflows, workflows, outbox, agents, wf.Definition, wf.JSON, wf.Hash)
	dispatcher := command.NewDispatcher(tx, postgres.NewCommands(pool))
	dispatcher.Register("start", orchestrator.Start)
	dispatcher.Register("answer", appwave.NewAnswerHandler(postgres.NewWaves(pool, wf.Definition), outbox).Handle)
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		t.Fatal(err)
	}
	store := appartifact.NewStore(tx, specs.HeaderParser{}, validator, postgres.NewArtifacts(pool), outbox)
	progress := appgate.NewProgress(tx, postgres.NewGates(pool), store, validator, flow{workflows, orchestrator}, outbox, wf.Definition)
	ledger := postgres.NewLedger(pool)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	executor := func(name string) *runtime.Executor {
		models := runtime.NewGatewayModels(gateway, postgres.NewProviderKeys(pool), ledger, ledger)
		return runtime.NewExecutor(tx, ledger, map[string]runtime.Agent{
			"requirements-agent": {Steps: runtime.NewFileAgent(models, 4000), WritablePaths: []string{"docs/requisitos"}},
		}, func(agent string) (string, string, error) {
			d, err := specs.DefaultAgent(agent)
			return string(d.Instruction), d.InstructionHash, err
		}, outbox, postgres.NewTelemetryNotifier(pool), store, progress, func(string) []string { return []string{"VISAO", "US", "RNF", "BACKLOG"} },
			runtime.Settings{Worker: name, Lease: 2 * time.Second, MaxCorrections: 3, RetryBackoff: 10 * time.Millisecond, Poll: 50 * time.Millisecond}, logger)
	}
	return worker{dispatcher: dispatcher, progress: progress, executor: executor}
}

type flow struct {
	postgres.Workflow
	*appworkflow.Orchestrator
}

func (f flow) SetStageState(ctx context.Context, projectID, stage, state string) error {
	return f.Workflow.SetStageState(ctx, projectID, stage, state)
}

func (w worker) drain(t *testing.T) {
	t.Helper()
	if _, err := w.dispatcher.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func liveRuntimeDeps(broker *appevent.Broker, shutdown chan struct{}) func(*httpapi.Dependencies, *postgrestest.Database) {
	return func(d *httpapi.Dependencies, db *postgrestest.Database) {
		pool := db.PoolAs("easycode_api")
		wf, _ := specs.BasicWorkflow()
		tx := postgres.NewTxRunner(pool)
		trail := postgres.NewAuditTrail(pool)
		projects := postgres.NewProjects(pool, wf.Definition.ID, 3)
		workflows := postgres.NewWorkflow(pool, wf.Definition)
		waves := appwave.NewService(tx, projects, postgres.NewWaves(pool, wf.Definition), postgres.NewWaves(pool, wf.Definition), trail)
		d.Workflow = appworkflow.NewService(tx, projects, workflows, workflows, workflows, trail)
		d.Artifacts = appartifact.NewQueries(projects, postgres.NewArtifacts(pool))
		d.Waves, d.Access = waves, waves
		d.Gates = appgate.NewDecisions(tx, projects, postgres.NewGates(pool), postgres.NewOutbox(pool), trail)
		d.Events, d.History, d.Shutdown = broker, postgres.NewOutbox(pool), shutdown
	}
}

func (l liveAPI) json(t *testing.T, method, path, body string, headers map[string]string, want int) map[string]any {
	t.Helper()
	rc := l.send(method, path, body, headers)
	if rc.Response.StatusCode() != want {
		t.Fatalf("%s %s: %d %s", method, path, rc.Response.StatusCode(), rc.Response.Body())
	}
	out := map[string]any{}
	if len(rc.Response.Body()) > 0 {
		if err := json.Unmarshal(rc.Response.Body(), &out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return out
}

var keyCounter int

func key() map[string]string {
	keyCounter++
	return map[string]string{"Idempotency-Key": fmt.Sprintf("0193a5c0-44aa-7e01-b2c9-%012d", keyCounter)}
}

// TestMarco1RequirementsWaveEndToEnd is the MARCO-1 done criterion with the reference project
// (BACKLOG, PLANO): web API → worker → gateway → scripted provider; a question answered, a delivery
// out of schema corrected (US-29), a forced worker restart resumed from the checkpoint (US-11),
// the cost recorded per call (US-36), the artifacts valid, versioned and readable (US-17, US-26),
// gate.requirements approved by a human and the next stage unblocked (US-05 CA-4), and the SSE
// replay in order without gaps (US-09).
func TestMarco1RequirementsWaveEndToEnd(t *testing.T) {
	broker := appevent.NewBroker()
	shutdown := make(chan struct{})
	live := newLiveAPI(t, liveRuntimeDeps(broker, shutdown))
	db := live.db
	ctx := context.Background()

	// Platform key pair and the organization's sealed key (US-15).
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
	provider := &scriptedProvider{script: script(t), blockAt: 2, blocked: make(chan struct{})}
	gateway := localGateway{service: appgateway.NewService(opener, time.Minute, provider)}
	work := newWorker(t, db, gateway)

	// Models, prices, project, pair, start (US-37, US-03, US-16, US-05).
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
		"budget": map[string]any{"stop_limit": map[string]string{"value": "200.00", "currency": "BRL"}}})
	projectID := live.json(t, "POST", "/api/v1/projects", string(body), key(), 201)["id"].(string)
	base := "/api/v1/projects/" + projectID
	live.json(t, "PATCH", base+"/configuration", `{"model_pair":{"reasoning":{"provider":"anthropic","model":"reasoning-model"},"execution":{"provider":"anthropic","model":"execution-model"}}}`,
		map[string]string{"If-Match": `"1"`}, 200)
	live.json(t, "POST", base+"/start", "", key(), 202)
	work.drain(t)

	// First run: the agent asks (US-17 CA-2, US-10).
	if ran, err := work.executor("worker-1").RunOnce(ctx); err != nil || !ran {
		t.Fatalf("first run: %v %v", ran, err)
	}
	wave := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200)
	if wave["state"] != "awaiting_user" || wave["question_open"] != "PERG-1" {
		t.Fatalf("US-17 CA-2: %v", wave)
	}
	questions := live.json(t, "GET", base+"/waves/REQ-01/questions", "", nil, 200)["items"].([]any)
	items := questions[0].(map[string]any)["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["suggestion"] != "Não no MVP" {
		t.Fatalf("numbered items with suggestion: %v", questions)
	}
	live.json(t, "POST", base+"/waves/REQ-01/questions/PERG-1/answer", `{"answers":[{"number":1,"text":"Não"},{"number":2,"text":"30 minutos"}]}`, key(), 202)
	work.drain(t)
	if state := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200)["state"]; state != "running" {
		t.Fatalf("US-10 CA-1: answered wave runs again, got %v", state)
	}

	// Second run dies in the middle of the third model call (forced restart, US-11).
	runCtx, kill := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, err := work.executor("worker-1").RunOnce(runCtx); done <- err }()
	select {
	case <-provider.blocked:
	case <-time.After(30 * time.Second):
		t.Fatal("the third call never started")
	}
	kill()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("forced stop: %v", err)
	}
	callsBefore := count(t, db, "SELECT count(*) FROM easycode.model_calls WHERE result = 'success'")
	answersInPrompt := strings.Contains(fmt.Sprint(provider.requests[1].Messages), "30 minutos")
	if !answersInPrompt {
		t.Fatal("US-10 CA-1: the answers go into the agent context")
	}

	// Another worker resumes from the checkpoint and completes the wave.
	if ran, err := work.executor("worker-2").RunOnce(ctx); err != nil || !ran {
		t.Fatalf("resume: %v %v", ran, err)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.wave.resumed_after_crash.v1'"); n != 1 {
		t.Fatal("US-11: resumed_after_crash")
	}
	if callsAfter := count(t, db, "SELECT count(*) FROM easycode.model_calls WHERE result = 'success'"); callsAfter != callsBefore+1 {
		t.Fatalf("US-11 CA-1: earlier calls not repeated (%d → %d)", callsBefore, callsAfter)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.model_calls WHERE result = 'aborted' AND lost AND estimated_cost AND cost > 0"); n != 1 {
		t.Fatal("US-11 CA-2: the call in flight is lost cost, estimated")
	}
	checkpoints := count(t, db, "SELECT count(*) FROM easycode.wave_checkpoints")
	steps := count(t, db, "SELECT count(*) FROM easycode.model_calls WHERE result = 'success'") + count(t, db, "SELECT count(*) FROM easycode.tool_executions")
	if checkpoints != steps {
		t.Fatalf("US-11 CA-3: one checkpoint per model call and tool execution (%d checkpoints, %d steps)", checkpoints, steps)
	}

	// Wave, cost and artifacts (US-17 CA-1, CA-3; US-36 CA-1; US-29; US-26).
	wave = live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200)
	cost := wave["cost"].(map[string]any)
	if wave["state"] != "completed" || wave["result"] != "success" || cost["useful"].(map[string]any)["value"] == "0" ||
		cost["lost"].(map[string]any)["value"] == "0" || cost["estimated_lost"].(map[string]any)["value"] == "0" {
		t.Fatalf("US-36 CA-1: %v", wave)
	}
	if wave["models"].(map[string]any)["execution"].(map[string]any)["model"] != "execution-model" || wave["versions"].(map[string]any)["inputs_summary_sha256"] == nil {
		t.Fatalf("models and inputs digest: %v", wave)
	}
	calls := live.json(t, "GET", base+"/waves/REQ-01/calls?type=model", "", nil, 200)["items"].([]any)
	if len(calls) != 4 || calls[0].(map[string]any)["role"] != "reasoning" {
		t.Fatalf("US-36: every call recorded, first by the reasoning model: %v", calls)
	}
	artifacts := live.json(t, "GET", base+"/artifacts?limit=50", "", nil, 200)["items"].([]any)
	types := map[string]int{}
	for _, item := range artifacts {
		a := item.(map[string]any)
		if a["state"] != "in_review" {
			t.Fatalf("US-17 CA-1: artifacts in review, got %v", a)
		}
		types[a["type"].(string)]++
	}
	if types["VISAO"] != 1 || types["US"] < 1 || types["RNF"] < 4 || types["BACKLOG"] != 1 {
		t.Fatalf("US-17 CA-1: outputs %v", types)
	}
	if visao := live.json(t, "GET", base+"/artifacts/VISAO", "", nil, 200); !strings.Contains(fmt.Sprint(visao["header"]), "Reservar salas pelo navegador") {
		t.Fatalf("the corrected VISAO is stored: %v", visao["header"])
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.tool_executions WHERE permission = 'refused' AND refusal_reason = 'path_not_allowed'"); n != 1 {
		t.Fatal("US-17 RN-1: writes outside docs/requisitos are refused")
	}

	// gate.requirements: human approval, only artifact.schema_valid (marco criterion).
	workflow := live.json(t, "GET", base+"/workflow", "", nil, 200)["stages"].([]any)
	if workflow[0].(map[string]any)["state"] != "awaiting_gate" {
		t.Fatalf("US-17 CA-3: %v", workflow[0])
	}
	round := live.json(t, "GET", base+"/gates/gate.requirements/rounds/1", "", nil, 200)
	checks := round["checks"].([]any)
	if round["state"] != "awaiting_human" || len(checks) != 1 || checks[0].(map[string]any)["check"] != "artifact.schema_valid" || checks[0].(map[string]any)["status"] != "passed" {
		t.Fatalf("gate round: %v", round)
	}
	me := live.json(t, "GET", "/api/v1/me", "", nil, 200)
	live.json(t, "POST", base+"/gates/gate.requirements/rounds/1/approval", `{"confirmed_human_checks":[]}`, mergeHeaders(key(), nil), 403)
	approved := live.json(t, "POST", base+"/gates/gate.requirements/rounds/1/approval", `{"confirmed_human_checks":[]}`,
		mergeHeaders(key(), map[string]string{"X-CSRF-Token": me["csrf_token"].(string)}), 200)
	if approved["state"] != "approved" {
		t.Fatalf("approval: %v", approved)
	}
	if _, err := work.progress.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workflow = live.json(t, "GET", base+"/workflow", "", nil, 200)["stages"].([]any)
	if workflow[0].(map[string]any)["state"] != "approved" || workflow[1].(map[string]any)["state"] != "ready" {
		t.Fatalf("US-05 CA-4: next stage unblocked without human action: %v", workflow)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.waves WHERE code = 'ARQ-01' AND state = 'queued'"); n != 1 {
		t.Fatal("US-05 CA-4: first wave of the next stage queued")
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.artifacts WHERE state = 'approved'"); n != 7 {
		t.Fatalf("artifacts approved with the gate, got %d", n)
	}

	// SSE replay from the start: domain events in sequence, no gap, no duplicate (US-09 CA-3, CA-4).
	last := count(t, db, "SELECT max(sequence) FROM easycode.events")
	ids := readStream(t, live, base+"/events?since=0", int64(last), shutdown)
	for i, id := range ids {
		if id != int64(i+1) {
			t.Fatalf("US-09 CA-3: ids must be 1..n in order, got %v", ids[:min(len(ids), 20)])
		}
	}
	if int64(len(ids)) != int64(last) {
		t.Fatalf("US-09 CA-4: full history (%d of %d)", len(ids), last)
	}
}

func mergeHeaders(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// readStream opens the SSE stream and collects the ids up to the stop id, then ends the stream.
func readStream(t *testing.T, live liveAPI, path string, stop int64, shutdown chan struct{}) []int64 {
	t.Helper()
	rc := live.send(fasthttp.MethodGet, path, "", nil)
	if rc.Response.StatusCode() != fasthttp.StatusOK || !strings.HasPrefix(string(rc.Response.Header.ContentType()), "text/event-stream") {
		t.Fatalf("stream: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	stream := rc.Response.BodyStream()
	reader := bufio.NewReader(stream)
	var ids []int64
	found := make(chan struct{})
	go func() {
		defer close(found)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if id, ok := strings.CutPrefix(strings.TrimSpace(line), "id: "); ok {
				n, _ := strconv.ParseInt(id, 10, 64)
				ids = append(ids, n)
				if n >= stop {
					return
				}
			}
		}
	}()
	select {
	case <-found:
	case <-time.After(20 * time.Second):
		t.Fatal("stream did not deliver the history")
	}
	close(shutdown)
	return ids
}

// US-29 CA-2: an artifact still invalid after the correction limit fails the wave with the
// reason "artefato inválido" and the schema errors visible in getWave.
func TestWaveFailsAfterTheCorrectionLimit(t *testing.T) {
	live := newLiveAPI(t, liveRuntimeDeps(appevent.NewBroker(), make(chan struct{})))
	db, ctx := live.db, context.Background()
	dir := t.TempDir()
	_, _ = sealing.GenerateKeyPair(filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem"))
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
	steps := script(t)
	provider := &scriptedProvider{script: []provider.Message{steps[1]}, blockAt: -1, blocked: make(chan struct{})}
	work := newWorker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, provider)})
	for _, model := range []string{"reasoning-model", "execution-model"} {
		live.json(t, "PUT", "/api/v1/providers/anthropic/models/"+model,
			`{"display_name":"m","capabilities":{"tool_use":true,"context_tokens":200000,"prompt_cache":true,"image_input":false},"active":true}`, nil, 200)
		live.json(t, "POST", "/api/v1/providers/anthropic/models/"+model+"/prices", `{"effective_since":"`+time.Now().UTC().Format(time.RFC3339)+
			`","currency":"BRL","per_million_tokens":{"input":"20","output":"100","read_cache":"2","write_cache":"25"},"source":"e2e"}`, key(), 201)
	}
	projectID := live.json(t, "POST", "/api/v1/projects", newProjectBody, key(), 201)["id"].(string)
	base := "/api/v1/projects/" + projectID
	live.json(t, "PATCH", base+"/configuration", `{"model_pair":{"reasoning":{"provider":"anthropic","model":"reasoning-model"},"execution":{"provider":"anthropic","model":"execution-model"}}}`,
		map[string]string{"If-Match": `"1"`}, 200)
	live.json(t, "POST", base+"/start", "", key(), 202)
	work.drain(t)
	if _, err := work.executor("worker-1").RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	wave := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200)
	errorsList, _ := wave["artifact_errors"].([]any)
	if wave["state"] != "failed" || wave["failure_cause"] != "invalid_artifact" || len(errorsList) == 0 {
		t.Fatalf("US-29 CA-2: %v", wave)
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.artifacts"); n != 0 {
		t.Fatal("nothing is stored from an invalid delivery")
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.model_calls"); n != 4 {
		t.Fatalf("the first delivery and 3 corrections, got %d calls", n)
	}
}
