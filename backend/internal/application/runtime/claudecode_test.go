package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/application/runtime"
	"easycode/internal/domain/provider"
	"easycode/internal/domain/sandbox"
)

// fakeForwarder is easycode-gateway for the model proxy: every forward gets the next canned answer.
type fakeForwarder struct {
	mu       sync.Mutex
	answers  []canned
	paths    []string
	sessions []appgateway.Scope
	closed   int
}

func (f *fakeForwarder) OpenSession(_ context.Context, scope appgateway.Scope, sealed []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if string(sealed) != "sealed" {
		return "", errors.New("unexpected key")
	}
	f.sessions = append(f.sessions, scope)
	return fmt.Sprintf("s%d", len(f.sessions)), nil
}

func (f *fakeForwarder) CloseSession(context.Context, string) error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	return nil
}

func (f *fakeForwarder) Forward(_ context.Context, _, path, rawQuery string, _ http.Header, _ []byte) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, path+"?"+rawQuery)
	if len(f.answers) == 0 {
		return answer(http.StatusOK, "application/json", `{"input_tokens":3}`), nil
	}
	next := f.answers[0]
	f.answers = f.answers[1:]
	return answer(next.status, next.contentType, next.body), nil
}

// canned is one answer of the fake gateway.
type canned struct {
	status            int
	contentType, body string
}

func (f *fakeForwarder) queue(answers ...canned) {
	f.mu.Lock()
	f.answers = append(f.answers, answers...)
	f.mu.Unlock()
}

func answer(status int, contentType, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func stream(body string) canned {
	return canned{http.StatusOK, "text/event-stream", body}
}

func sseEvent(data map[string]any) string {
	encoded, _ := json.Marshal(data)
	return "event: " + data["type"].(string) + "\ndata: " + string(encoded) + "\n\n"
}

// sseAnswer is a streamed answer with the usage split as the Messages API sends it and an optional tool_use.
func sseAnswer(toolID, toolName, input string) string {
	var out strings.Builder
	out.WriteString(sseEvent(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]int{
		"input_tokens": 10, "cache_read_input_tokens": 5, "cache_creation_input_tokens": 7, "output_tokens": 1}}}))
	if toolID != "" {
		out.WriteString(sseEvent(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": toolID, "name": toolName, "input": map[string]any{}}}))
		half := len(input) / 2
		out.WriteString(sseEvent(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": input[:half]}}))
		out.WriteString(sseEvent(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": input[half:]}}))
		out.WriteString(sseEvent(map[string]any{"type": "content_block_stop", "index": 0}))
	}
	out.WriteString(sseEvent(map[string]any{"type": "message_delta", "usage": map[string]int{"output_tokens": 20}}))
	out.WriteString(sseEvent(map[string]any{"type": "message_stop"}))
	return out.String()
}

type fakeLedger struct {
	mu     sync.Mutex
	next   int64
	steps  []int
	roles  []string
	holdAt int // the n-th call (1-based) is refused by the budget
}

func (l *fakeLedger) OpenCall(_ context.Context, _ string, _, step int, _, _ int64, role, _ string, _ int) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holdAt > 0 && len(l.steps)+1 == l.holdAt {
		return 0, runtime.ErrDispatchHeld
	}
	l.next++
	l.steps, l.roles = append(l.steps, step), append(l.roles, role)
	return 100 + l.next, nil
}

func (l *fakeLedger) CurrentPriceID(context.Context, string, int64, time.Time) (int64, error) {
	return 7, nil
}

type sealedKeys struct{}

func (sealedKeys) ActiveSealedKey(context.Context, string, string) ([]byte, error) {
	return []byte("sealed"), nil
}

// claudeClient is the side of Claude Code: HTTP over the socket of the run, as agent-bridge forwards it.
type claudeClient struct {
	ctx    context.Context
	http   *http.Client
	token  string
	status []int
	bodies []string
}

func (c *claudeClient) post(path string, body map[string]any) (int, string) {
	encoded, _ := json.Marshal(body)
	request, _ := http.NewRequestWithContext(c.ctx, http.MethodPost, "http://claude"+path, bytes.NewReader(encoded))
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.http.Do(request)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	c.status, c.bodies = append(c.status, resp.StatusCode), append(c.bodies, string(raw))
	return resp.StatusCode, string(raw)
}

func (c *claudeClient) messages(model string, messages ...map[string]any) (int, string) {
	return c.post("/v1/messages?beta=true", map[string]any{"model": model, "stream": true, "messages": messages})
}

func user(text string) map[string]any { return map[string]any{"role": "user", "content": text} }

func toolResult(id, text string, isError bool) map[string]any {
	return map[string]any{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": id, "is_error": isError, "content": text}}}
}

// claudeRun simulates one Claude Code run: it talks to the proxy and returns its stream-json and the files it left.
type claudeRun func(c *claudeClient) (output string, files map[string]string)

// fakeClaude is the sandbox runner of agent sessions, with a scripted Claude Code per run.
type fakeClaude struct {
	dir      string
	mu       sync.Mutex
	runs     []claudeRun
	requests []sandbox.ExecutionRequest
	uploads  []map[string]string
	aborted  int
}

func (f *fakeClaude) Run(ctx context.Context, req sandbox.ExecutionRequest, files map[string]string) (runtime.RunOutcome, error) {
	f.mu.Lock()
	index := len(f.requests)
	f.requests, f.uploads = append(f.requests, req), append(f.uploads, files)
	script := f.runs[index]
	f.mu.Unlock()
	socket := filepath.Join(f.dir, req.AgentSession.SocketDir, "model.sock")
	client := &claudeClient{ctx: ctx, token: req.AgentSession.Token, http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}}
	output, written := script(client)
	if ctx.Err() != nil {
		f.mu.Lock()
		f.aborted++
		f.mu.Unlock()
		return runtime.RunOutcome{}, ctx.Err()
	}
	collected := map[string]string{}
	for p, content := range files {
		collected[p] = content
	}
	for p, content := range written {
		collected[p] = content
	}
	code := 0
	return runtime.RunOutcome{Result: sandbox.ExecutionResult{EndReason: sandbox.EndCompleted, ExitCode: &code, AgentVersion: "2.1.332"},
		Output: []byte(output), Files: collected}, nil
}

func resultLine(structured map[string]any) string {
	init, _ := json.Marshal(map[string]any{"type": "system", "subtype": "init", "claude_code_version": "2.1.332", "tools": []string{"Read", "Write"}})
	result, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "structured_output": structured})
	return string(init) + "\n" + `{"type":"assistant","message":{"content":[]}}` + "\n" + string(result) + "\n"
}

type claudeFixture struct {
	agent   *runtime.ClaudeCodeAgent
	gateway *fakeForwarder
	ledger  *fakeLedger
	runner  *fakeClaude
	dir     string
}

func newClaudeFixture(t *testing.T, runs ...claudeRun) *claudeFixture {
	t.Helper()
	dir, err := os.MkdirTemp("", "ccp") // short: a Unix socket path has at most 108 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	f := &claudeFixture{gateway: &fakeForwarder{}, ledger: &fakeLedger{}, runner: &fakeClaude{dir: dir, runs: runs}, dir: dir}
	f.agent = runtime.NewClaudeCodeAgent(runtime.NewModelProxy(dir, f.gateway, sealedKeys{}, f.ledger, f.ledger), f.runner)
	return f
}

func ccWave(t *testing.T) runtime.WaveContext {
	w := wave()
	w.ProjectID, w.WaveID, w.Attempt, w.Image = "p1", "0b9c7a52-1f0e-4c7e-9a51-3f7c2d1e9b10", 1, "go"
	w.Policy = policy(t, "requirements-agent")
	w.Models = map[string]runtime.ModelRef{"reasoning": {ID: 1, Provider: provider.ClaudeCode, Name: "claude-opus"},
		"execution": {ID: 2, Provider: provider.ClaudeCode, Name: "claude-sonnet"}}
	w.Base = map[string]string{"docs/requisitos/VISAO.md": "antiga", "app/main.go": "package main"}
	w.SkillFiles = map[string]string{"skills/req/SKILL.md": "skill"}
	return w
}

// run drives the harness until a step other than continue, or an error.
func (f *claudeFixture) run(t *testing.T, w runtime.WaveContext, cp runtime.Checkpoint) ([]runtime.StepResult, runtime.Checkpoint, error) {
	t.Helper()
	var results []runtime.StepResult
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, err := f.agent.Next(ctx, w, cp)
		cancel()
		if err != nil {
			return results, cp, err
		}
		if result.New.Number != cp.Number+1 {
			t.Fatalf("checkpoint %d after %d", result.New.Number, cp.Number)
		}
		results = append(results, result)
		cp = result.New
		if result.Type != runtime.StepContinue {
			return results, cp, nil
		}
	}
	t.Fatal("the harness did not stop")
	return nil, cp, nil
}

const visao = "docs/requisitos/VISAO.md"

// writeAndDeliver writes the VISAO with Write and delivers.
func writeAndDeliver(content string) claudeRun {
	return func(c *claudeClient) (string, map[string]string) {
		c.messages("claude-sonnet", user("task"))
		c.messages("claude-sonnet", user("task"), toolResult("tu1", "ok", false))
		return resultLine(map[string]any{"status": "complete", "summary": "pronto"}),
			map[string]string{visao: content, "docs/outra-pasta/x.md": "fora", ".easycode/home/.claude.json": "{}"}
	}
}

// US-58 CA-5: the run goes through the proxy, every call is a step with its usage and role, the tool is a
// step, and only the files written under the policy are delivered.
func TestClaudeCodeRunDeliversTheWrittenFiles(t *testing.T) {
	f := newClaudeFixture(t, writeAndDeliver("nova"))
	f.gateway.queue(stream(sseAnswer("tu1", "Write", `{"file_path":"/workspace/docs/requisitos/VISAO.md","content":"nova"}`)), stream(sseAnswer("", "", "")))
	w := ccWave(t)
	cp, digest, err := f.agent.Start(context.Background(), w)
	if err != nil || len(digest) != 64 {
		t.Fatalf("start: %v", err)
	}
	results, _, err := f.run(t, w, cp)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("call, tool, call, delivery: %+v", results)
	}
	call := results[0].Call
	if call == nil || call.ID != 101 || call.Result != "success" || call.Role != "execution" || call.Provider != provider.ClaudeCode || call.Model != "claude-sonnet" ||
		call.Usage != (provider.Usage{Input: 10, Output: 20, CacheRead: 5, CacheWrite: 7}) || results[0].New.After != runtime.AfterModelCall {
		t.Fatalf("call record: %+v", call)
	}
	if tool := results[1].Tool; tool == nil || tool.Tool != runtime.ToolWriteFile || !tool.Allowed || !tool.Success || tool.Summary != "gravou VISAO.md" {
		t.Fatalf("tool record: %+v", results[1].Tool)
	}
	if f.ledger.steps[0] != 1 || f.ledger.steps[1] != 3 {
		t.Fatalf("calls are opened with the number of their checkpoint: %v", f.ledger.steps)
	}
	last := results[3]
	if last.Type != runtime.StepComplete || len(last.Outputs) != 1 || last.Outputs[0].Path != visao || string(last.Outputs[0].Content) != "nova" {
		t.Fatalf("delivery: %+v", last)
	}
	if !strings.Contains(last.Tool.Summary, "2.1.332") {
		t.Fatalf("RNF-15: the version of Claude Code is recorded: %q", last.Tool.Summary)
	}
	req := f.runner.requests[0]
	argv := strings.Join(req.Argv, " ")
	if req.Category != sandbox.CategoryAgentSession || req.Image != "go" || req.AgentSession == nil || req.AgentSession.Token == "" ||
		strings.Join(req.AgentSession.Writable, ",") != "docs/requisitos" || req.Environment["ANTHROPIC_SMALL_FAST_MODEL"] != "claude-sonnet" {
		t.Fatalf("request: %+v", req)
	}
	for _, want := range []string{"--model claude-sonnet", "--tools Read,Glob,Grep,Write,Edit", "--max-turns 60", "--output-format stream-json",
		"--append-system-prompt-file /workspace/.easycode/system.md", "agent-bridge guard", "--json-schema"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv lacks %q: %s", want, argv)
		}
	}
	if strings.Contains(argv, "WebFetch") || strings.Contains(argv, "Bash") {
		t.Fatalf("RNF-07: only the tools of the agent: %s", argv)
	}
	upload := f.runner.uploads[0]
	if _, ok := upload["app/main.go"]; ok {
		t.Fatal("a file the agent cannot read is not in the workspace")
	}
	if upload[visao] != "antiga" || upload[".easycode/skills/req/SKILL.md"] != "skill" || !strings.Contains(upload[".easycode/task.md"], "entrada_nao_confiavel") ||
		!strings.Contains(upload[".easycode/system.md"], "`Write` e `Edit`") {
		t.Fatalf("workspace: %v", upload)
	}
	// The run goroutine closes the proxy session after the last step is handed over: wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		entries, _ := os.ReadDir(f.dir)
		if len(entries) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the socket directory is removed after the run: %v", entries)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// US-58 CA-7 (US-10): the question waits for the user; the answer starts a new run with it.
func TestClaudeCodeAsksAndContinuesWithTheAnswer(t *testing.T) {
	ask := func(c *claudeClient) (string, map[string]string) {
		c.messages("claude-sonnet", user("task"))
		return resultLine(map[string]any{"status": "ask", "summary": "dúvida", "questions": []map[string]string{{"text": "Escopo?", "suggestion": "Sim"}}}), nil
	}
	f := newClaudeFixture(t, ask, writeAndDeliver("com resposta"))
	f.gateway.queue(stream(sseAnswer("", "", "")), stream(sseAnswer("tu1", "Write", `{"file_path":"docs/requisitos/VISAO.md"}`)), stream(sseAnswer("", "", "")))
	w := ccWave(t)
	cp, _, _ := f.agent.Start(context.Background(), w)
	results, cp, err := f.run(t, w, cp)
	if err != nil {
		t.Fatal(err)
	}
	last := results[len(results)-1]
	if last.Type != runtime.StepAsk || len(last.Question) != 1 || last.Question[0].Text != "Escopo?" || !f.agent.Awaiting(cp) {
		t.Fatalf("question: %+v", last)
	}
	if _, err := f.agent.Next(context.Background(), w, cp); err == nil {
		t.Fatal("no step while the wave waits for the answer")
	}
	answered, err := f.agent.Answer(cp, []string{"Só o módulo de vendas"})
	if err != nil || f.agent.Awaiting(answered) {
		t.Fatalf("answer: %v", err)
	}
	results, _, err = f.run(t, w, answered)
	if err != nil || results[len(results)-1].Type != runtime.StepComplete {
		t.Fatalf("second run: %v %+v", err, results)
	}
	task := f.runner.uploads[1][".easycode/task.md"]
	if !strings.Contains(task, "Escopo?") || !strings.Contains(task, "Só o módulo de vendas") {
		t.Fatalf("the next run gets the question and the answer: %s", task)
	}
	if f.runner.requests[0].ExecutionID == f.runner.requests[1].ExecutionID {
		t.Fatal("each run is a new execution")
	}
}

// US-58 CA-7 (US-29): a rejected delivery is counted and the problems go to the next run.
func TestClaudeCodeRejectionStartsACorrectionRun(t *testing.T) {
	f := newClaudeFixture(t, writeAndDeliver("v1"), writeAndDeliver("v2"))
	for i := 0; i < 2; i++ {
		f.gateway.queue(stream(sseAnswer("tu1", "Write", `{"file_path":"docs/requisitos/VISAO.md"}`)), stream(sseAnswer("", "", "")))
	}
	w := ccWave(t)
	cp, _, _ := f.agent.Start(context.Background(), w)
	_, cp, err := f.run(t, w, cp)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := f.agent.Reject(cp, "- VISAO: falta objetivo")
	if err != nil || f.agent.Corrections(rejected) != 1 {
		t.Fatalf("US-29 CA-1: rejection counted, %v", err)
	}
	results, _, err := f.run(t, w, rejected)
	if err != nil {
		t.Fatal(err)
	}
	last := results[len(results)-1]
	if last.Type != runtime.StepComplete || string(last.Outputs[0].Content) != "v2" {
		t.Fatalf("correction: %+v", last)
	}
	if f.runner.uploads[1][visao] != "v1" || !strings.Contains(f.runner.uploads[1][".easycode/task.md"], "falta objetivo") {
		t.Fatal("the correction run starts from the files written and the problems")
	}
}

// US-58 CA-6 (US-30): a write the hook refused comes back to the agent and is a refused tool step.
func TestClaudeCodeHookRefusalIsARefusedTool(t *testing.T) {
	run := func(c *claudeClient) (string, map[string]string) {
		c.messages("claude-sonnet", user("task"))
		c.messages("claude-sonnet", user("task"), toolResult("tu1", "PreToolUse:Write hook error: easycode: write to /etc/passwd is not allowed for this agent", true))
		return resultLine(map[string]any{"status": "complete", "summary": "nada"}), map[string]string{"etc/passwd": "x"}
	}
	f := newClaudeFixture(t, run)
	f.gateway.queue(stream(sseAnswer("tu1", "Write", `{"file_path":"/etc/passwd","content":"x"}`)), stream(sseAnswer("", "", "")))
	w := ccWave(t)
	cp, _, _ := f.agent.Start(context.Background(), w)
	results, _, err := f.run(t, w, cp)
	if err != nil {
		t.Fatal(err)
	}
	refused := results[1].Tool
	if refused == nil || refused.Allowed || refused.RefusalReason != "path_not_allowed" || refused.Tool != runtime.ToolWriteFile {
		t.Fatalf("refusal: %+v", refused)
	}
	if last := results[len(results)-1]; last.Type != runtime.StepComplete || len(last.Outputs) != 0 {
		t.Fatalf("nothing outside the policy is delivered: %+v", last)
	}
}

// US-58 CA-7 (RNF-16): a call refused by the budget aborts the run and the wave holds; no call is forwarded.
func TestClaudeCodeHeldDispatchAbortsTheRun(t *testing.T) {
	var held string
	run := func(c *claudeClient) (string, map[string]string) {
		c.messages("claude-sonnet", user("task"))
		_, held = c.messages("claude-sonnet", user("task"), toolResult("tu1", "ok", false))
		<-c.ctx.Done() // Claude Code would end on the 400; the harness aborts it first
		return "", nil
	}
	f := newClaudeFixture(t, run)
	f.ledger.holdAt = 2
	f.gateway.queue(stream(sseAnswer("tu1", "Read", `{"file_path":"docs/requisitos/VISAO.md"}`)))
	w := ccWave(t)
	cp, _, _ := f.agent.Start(context.Background(), w)
	results, _, err := f.run(t, w, cp)
	if !errors.Is(err, runtime.ErrDispatchHeld) {
		t.Fatalf("want the held dispatch, got %v", err)
	}
	if len(results) != 2 || results[0].Call == nil || results[1].Tool == nil || results[1].Tool.Tool != runtime.ToolReadFile {
		t.Fatalf("the steps before the refusal are kept: %+v", results)
	}
	// the 400 may not reach the client before the abort: what matters is that nothing was forwarded
	if (held != "" && !strings.Contains(held, "dispatch held") && !strings.Contains(held, "context canceled")) || len(f.gateway.paths) != 1 {
		t.Fatalf("the refused call is not forwarded: %q %v", held, f.gateway.paths)
	}
	waitFor(t, func() bool { f.runner.mu.Lock(); defer f.runner.mu.Unlock(); return f.runner.aborted == 1 })
}

// US-58 CA-7 (US-11): a worker restart with the run in flight starts the run again from the checkpoint.
func TestClaudeCodeRestartRedoesTheRun(t *testing.T) {
	first := func(c *claudeClient) (string, map[string]string) {
		c.messages("claude-sonnet", user("task"))
		<-c.ctx.Done() // the worker falls here
		return "", nil
	}
	f := newClaudeFixture(t, first)
	f.gateway.queue(stream(sseAnswer("", "", "")))
	w := ccWave(t)
	cp, _, _ := f.agent.Start(context.Background(), w)
	result, err := f.agent.Next(context.Background(), w, cp)
	if err != nil || result.Call == nil {
		t.Fatalf("first call: %v", err)
	}
	f.agent.Release(w) // the old process is gone with its run
	restarted := newClaudeFixture(t, writeAndDeliver("depois da queda"))
	restarted.gateway.queue(stream(sseAnswer("tu1", "Write", `{"file_path":"docs/requisitos/VISAO.md"}`)), stream(sseAnswer("", "", "")))
	results, _, err := restarted.run(t, w, result.New)
	if err != nil {
		t.Fatal(err)
	}
	if last := results[len(results)-1]; last.Type != runtime.StepComplete || string(last.Outputs[0].Content) != "depois da queda" {
		t.Fatalf("resumed: %+v", last)
	}
	if restarted.ledger.steps[0] != result.New.Number+1 {
		t.Fatalf("the new run numbers its calls after the checkpoint: %v", restarted.ledger.steps)
	}
}

// A run that ends without the structured result is retried with the problem, three in a row fail.
func TestClaudeCodeRunWithoutResultIsRetried(t *testing.T) {
	empty := func(*claudeClient) (string, map[string]string) {
		return `{"type":"result","subtype":"error_during_execution","is_error":true}` + "\n", nil
	}
	f := newClaudeFixture(t, empty, empty, empty)
	w := ccWave(t)
	cp, _, _ := f.agent.Start(context.Background(), w)
	results, _, err := f.run(t, w, cp)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].Type != runtime.StepContinue || results[2].Type != runtime.StepFail || results[2].Failure != runtime.CauseUnrecoverable {
		t.Fatalf("retries: %+v", results)
	}
}

// An invalid credential seen by the proxy fails the step with an authentication error.
func TestClaudeCodeInvalidCredentialFails(t *testing.T) {
	run := func(c *claudeClient) (string, map[string]string) {
		c.messages("claude-sonnet", user("task"))
		return `{"type":"result","subtype":"success","is_error":true}` + "\n", nil
	}
	f := newClaudeFixture(t, run)
	f.gateway.queue(canned{http.StatusUnauthorized, "application/json", `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`})
	w := ccWave(t)
	cp, _, _ := f.agent.Start(context.Background(), w)
	results, _, err := f.run(t, w, cp)
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Class != provider.ErrorAuthentication {
		t.Fatalf("want an authentication error, got %v", err)
	}
	if len(results) != 1 || results[0].Call.Result != "error" || results[0].Call.Error != "authentication" {
		t.Fatalf("the failed call is recorded: %+v", results)
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The participant of a validation stage delivers its opinion in the structured result.
func TestClaudeCodeReviewWaveDeliversTheOpinion(t *testing.T) {
	run := func(c *claudeClient) (string, map[string]string) {
		c.messages("claude-sonnet", user("task"))
		return resultLine(map[string]any{"status": "complete", "summary": "ok", "parecer": "reprovado", "ressalvas": []string{"falta índice"}}), nil
	}
	f := newClaudeFixture(t, run)
	f.gateway.queue(stream(sseAnswer("", "", "")))
	w := ccWave(t)
	w.Mode, w.Agent = "PARECER", "dba-agent"
	w.Policy = policy(t, "dba-agent")
	cp, _, _ := f.agent.Start(context.Background(), w)
	results, _, err := f.run(t, w, cp)
	if err != nil {
		t.Fatal(err)
	}
	last := results[len(results)-1]
	if last.Type != runtime.StepComplete || last.Opinion == nil || last.Opinion.Opinion != "reprovado" || len(last.Opinion.Reservations) != 1 {
		t.Fatalf("opinion: %+v", last)
	}
	if !strings.Contains(strings.Join(f.runner.requests[0].Argv, " "), `"parecer"`) {
		t.Fatal("the result schema asks for the opinion")
	}
}

// ADR-23: the pair of the wave picks the harness; the checkpoint keeps it for the next steps.
func TestDispatchPicksTheHarnessByThePair(t *testing.T) {
	f := newClaudeFixture(t)
	own := runtime.NewFileAgent(&cannedModel{}, 1000)
	steps := runtime.NewDispatch(own, f.agent)
	w := ccWave(t)
	cp, _, err := steps.Start(context.Background(), w)
	if err != nil || !strings.Contains(string(cp.State), `"harness":"claude-code"`) {
		t.Fatalf("claude-code pair: %v %s", err, cp.State)
	}
	w.Models = map[string]runtime.ModelRef{"execution": {ID: 3, Provider: "openai", Name: "gpt"}}
	cp, _, err = steps.Start(context.Background(), w)
	if err != nil || strings.Contains(string(cp.State), `"harness"`) {
		t.Fatalf("RN-3: openai stays in the own harness: %v", err)
	}
	if _, ok := steps.(runtime.Releaser); !ok {
		t.Fatal("the executor releases the live run through the dispatcher")
	}
	if runtime.NewDispatch(own, nil) != runtime.Steps(own) {
		t.Fatal("without Claude Code the own harness is used as is")
	}
}

// US-29: the Claude Code run gets the templates once, after its own tool guide.
func TestClaudeCodeSystemPromptHasTheTemplates(t *testing.T) {
	f := newClaudeFixture(t)
	w := ccWave(t)
	w.Templates = map[string]string{"VISAO": "molde da visao", "US": "molde da us"}
	cp, _, err := f.agent.Start(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	var state struct{ System string }
	if err := json.Unmarshal(cp.State, &state); err != nil {
		t.Fatal(err)
	}
	if strings.Count(state.System, "## Templates dos artefatos") != 1 || !strings.Contains(state.System, "### US\n\nmolde da us") ||
		strings.Index(state.System, "### US") > strings.Index(state.System, "### VISAO") || strings.Count(state.System, "read_file(path)") != 0 {
		t.Fatalf("templates once, sorted, with the Claude Code guide: %s", state.System)
	}
}
