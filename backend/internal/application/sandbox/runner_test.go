package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/permission"
	"easycode/internal/domain/sandbox"
)

// fakeEngine records the containers and processes the runner asks for and answers them without Docker.
type fakeEngine struct {
	mu         sync.Mutex
	created    []ContainerSpec
	execs      []ExecSpec
	version    string // answer of claude --version
	versionErr bool
	files      map[string][]byte // workspace left by the command
}

func (f *fakeEngine) Ping(context.Context) error { return nil }

func (f *fakeEngine) VolumeMountpoint(_ context.Context, name string) (string, error) {
	return "/var/lib/docker/volumes/" + name + "/_data", nil
}
func (f *fakeEngine) ImageDigest(context.Context, string) (string, error) {
	return "sha256:" + strings.Repeat("a", 64), nil
}
func (f *fakeEngine) Create(_ context.Context, spec ContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, spec)
	return fmt.Sprintf("c%d", len(f.created)), nil
}
func (f *fakeEngine) Start(context.Context, string) error { return nil }
func (f *fakeEngine) Inspect(context.Context, string) (ContainerState, error) {
	return ContainerState{}, nil
}
func (f *fakeEngine) Remove(context.Context, string) error { return nil }
func (f *fakeEngine) Exec(_ context.Context, _ string, spec ExecSpec, stdin io.Reader, out io.Writer) (int, error) {
	f.mu.Lock()
	f.execs = append(f.execs, spec)
	f.mu.Unlock()
	if stdin != nil {
		_, _ = io.Copy(io.Discard, stdin)
	}
	switch {
	case slices.Equal(spec.Cmd, []string{claudeCodePath, "--version"}):
		if f.versionErr {
			return 127, nil
		}
		_, _ = io.WriteString(out, f.version)
	case len(spec.Cmd) > 0 && spec.Cmd[0] == "tar":
		archive, err := Archive(f.files)
		if err != nil {
			return 1, err
		}
		_, _ = out.Write(archive)
	case len(spec.Cmd) > 0 && spec.Cmd[0] == bridgePath:
		_, _ = io.WriteString(out, `{"type":"result"}`)
	}
	return 0, nil
}

const project = "3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14"

func policies(agent string) (permission.Policy, error) {
	switch agent {
	case "developer-agent":
		return permission.Policy{Agent: agent, Execute: true, Categories: []string{sandbox.CategoryBuild},
			Commands: map[string]sandbox.Category{sandbox.CategoryBuild: {Argv0: []string{"go"}}}}, nil
	case "requirements-agent":
		return permission.Policy{Agent: agent}, nil // a file agent: no command
	}
	return permission.Policy{}, errors.New("unknown agent")
}

func newTestRunner(engine *fakeEngine, bin string) *Runner {
	config := Config{Images: map[string]string{"go": "easycode-sandbox-go:dev"}, Runtime: "runc",
		Defaults:     sandbox.Limits{CPU: 2, MemoryMiB: 4096, DiskMiB: 1024, Time: 30 * time.Minute, Processes: 256, MaxOutputBytes: 16 << 20},
		AgentSession: AgentSessionConfig{ClaudeCodeBin: bin, ProxyVolume: "easycode-agent-proxy", DetectImage: "go"}}
	runner := NewRunner(engine, policies, config)
	archive, _ := Archive(map[string][]byte{"docs/a.md": []byte("a")})
	_ = runner.PrepareWorkspace(project, archive)
	return runner
}

func agentRequest() sandbox.ExecutionRequest {
	return sandbox.ExecutionRequest{ExecutionID: "e1", ProjectID: project, WaveID: "w1", Agent: "requirements-agent", Attempt: 1,
		Image: "go", Category: sandbox.CategoryAgentSession, Argv: []string{"-p", "--model", "claude-x", "--output-format", "stream-json"},
		Environment:  map[string]string{"ANTHROPIC_SMALL_FAST_MODEL": "claude-small"},
		AgentSession: &sandbox.AgentSession{SocketDir: "wave-0193a5c0", Token: "session-token-123", Writable: []string{"docs/requisitos/", "README.md"}}}
}

func code(err error) string {
	var classified *failure.Error
	if errors.As(err, &classified) {
		return classified.Code
	}
	return fmt.Sprint(err)
}

func TestAgentSessionRunsClaudeCodeThroughTheBridge(t *testing.T) {
	engine := &fakeEngine{version: "2.1.332 (Claude Code)\n", files: map[string][]byte{
		"docs/requisitos/US-01.md": []byte("x"), ".easycode/home/.claude.json": []byte("{}"), ".easycode/denials.jsonl": []byte("{}")}}
	runner := newTestRunner(engine, "/home/u/.local/share/claude/versions/2.1.332")
	result, err := runner.Execute(context.Background(), agentRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded() || result.AgentVersion != "2.1.332" || result.Profile != "agent_session" {
		t.Fatalf("result: %+v", result)
	}
	if len(engine.created) != 2 {
		t.Fatalf("want the version check and the session containers, got %d", len(engine.created))
	}
	check, session := engine.created[0], engine.created[1]
	if len(check.Mounts) != 1 || check.Mounts[0] != (Mount{Type: MountBind, Source: "/home/u/.local/share/claude/versions/2.1.332", Target: claudeCodePath, ReadOnly: true}) || check.Network != "" {
		t.Fatalf("version check container: %+v", check)
	}
	wantMounts := []Mount{
		{Type: MountBind, Source: "/home/u/.local/share/claude/versions/2.1.332", Target: "/opt/claude-code/claude", ReadOnly: true},
		{Type: MountBind, Source: "/var/lib/docker/volumes/easycode-agent-proxy/_data/bin", Target: "/opt/easycode-bridge", ReadOnly: true},
		{Type: MountBind, Source: "/var/lib/docker/volumes/easycode-agent-proxy/_data/wave-0193a5c0", Target: "/run/easycode-model"},
	}
	if !slices.Equal(session.Mounts, wantMounts) || session.Network != "" || session.User != "10001:10001" {
		t.Fatalf("session mounts: %+v", session)
	}
	for _, want := range []string{"HOME=/workspace/.easycode/home", "ANTHROPIC_BASE_URL=http://127.0.0.1:8099", "ANTHROPIC_AUTH_TOKEN=session-token-123",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1",
		"EASYCODE_WRITABLE=/workspace/docs/requisitos:/workspace/README.md", "ANTHROPIC_SMALL_FAST_MODEL=claude-small", "GOPROXY=off"} {
		if !slices.Contains(session.Env, want) {
			t.Errorf("env: want %q in %v", want, session.Env)
		}
	}
	if slices.Contains(session.Env, "HOME=/tmp/home") {
		t.Errorf("env: the profile HOME must be replaced: %v", session.Env)
	}
	var prepare, command []string
	for _, e := range engine.execs {
		switch {
		case len(e.Cmd) == 3 && e.Cmd[0] == "sh":
			prepare = e.Cmd
		case e.Cmd[0] == bridgePath:
			command = e.Cmd
		}
	}
	if !strings.Contains(prepare[2], "mkdir -p /workspace/.easycode/home") {
		t.Errorf("prepare: %q", prepare)
	}
	wantCommand := []string{"/opt/easycode-bridge/agent-bridge", "exec", "--listen", "127.0.0.1:8099", "--socket", "/run/easycode-model/model.sock", "--",
		"/opt/claude-code/claude", "-p", "--model", "claude-x", "--output-format", "stream-json"}
	if !slices.Equal(command, wantCommand) {
		t.Errorf("command: %q", command)
	}
	files, _, err := runner.Collect(project)
	if err != nil || len(files) != 1 || string(files["docs/requisitos/US-01.md"]) != "x" {
		t.Fatalf("collect keeps .easycode out: %v %v", files, err)
	}
}

func TestAgentSessionValidation(t *testing.T) {
	cases := map[string]struct {
		change func(*sandbox.ExecutionRequest)
		bin    string
		code   string
	}{
		"feature off":           {func(*sandbox.ExecutionRequest) {}, "", sandbox.ProblemUnavailable},
		"no session":            {func(r *sandbox.ExecutionRequest) { r.AgentSession = nil }, "/c", sandbox.ProblemInvalidRequest},
		"socket dir with slash": {func(r *sandbox.ExecutionRequest) { r.AgentSession.SocketDir = "a/b" }, "/c", sandbox.ProblemInvalidRequest},
		"socket dir dot dot":    {func(r *sandbox.ExecutionRequest) { r.AgentSession.SocketDir = ".." }, "/c", sandbox.ProblemInvalidRequest},
		"socket dir bin":        {func(r *sandbox.ExecutionRequest) { r.AgentSession.SocketDir = "bin" }, "/c", sandbox.ProblemInvalidRequest},
		"socket dir empty":      {func(r *sandbox.ExecutionRequest) { r.AgentSession.SocketDir = "" }, "/c", sandbox.ProblemInvalidRequest},
		"no token":              {func(r *sandbox.ExecutionRequest) { r.AgentSession.Token = "" }, "/c", sandbox.ProblemInvalidRequest},
		"token with newline":    {func(r *sandbox.ExecutionRequest) { r.AgentSession.Token = "a\nB=1" }, "/c", sandbox.ProblemInvalidRequest},
		"writable outside":      {func(r *sandbox.ExecutionRequest) { r.AgentSession.Writable = []string{"../etc"} }, "/c", sandbox.ProblemInvalidRequest},
		"writable absolute":     {func(r *sandbox.ExecutionRequest) { r.AgentSession.Writable = []string{"/etc"} }, "/c", sandbox.ProblemInvalidRequest},
		"writable separator":    {func(r *sandbox.ExecutionRequest) { r.AgentSession.Writable = []string{"a:/etc"} }, "/c", sandbox.ProblemInvalidRequest},
		"no argv":               {func(r *sandbox.ExecutionRequest) { r.Argv = nil }, "/c", sandbox.ProblemInvalidRequest},
		"host variable": {func(r *sandbox.ExecutionRequest) {
			r.Environment = map[string]string{"ANTHROPIC_BASE_URL": "http://evil"}
		}, "/c", sandbox.ProblemForbiddenVariable},
		"secret variable": {func(r *sandbox.ExecutionRequest) {
			r.Environment = map[string]string{"ANTHROPIC_API_KEY": "x"}
		}, "/c", sandbox.ProblemForbiddenVariable},
		"unknown agent":    {func(r *sandbox.ExecutionRequest) { r.Agent = "nobody" }, "/c", sandbox.ProblemCommandNotAllowed},
		"dependency route": {func(*sandbox.ExecutionRequest) {}, "/c", sandbox.ProblemCommandNotAllowed},
	}
	for name, c := range cases {
		engine := &fakeEngine{version: "2.1.332"}
		runner := newTestRunner(engine, c.bin)
		runner.config.Dependencies.Network = "deps"
		request := agentRequest()
		c.change(&request)
		var err error
		if name == "dependency route" {
			_, err = runner.InstallDependencies(context.Background(), request)
		} else {
			_, err = runner.Execute(context.Background(), request)
		}
		if code(err) != c.code {
			t.Errorf("%s: want %s, got %v", name, c.code, err)
		}
		if len(engine.created) != 0 {
			t.Errorf("%s: no container may start", name)
		}
	}
}

func TestOtherCategoriesRefuseTheAgentSessionAndItsVariables(t *testing.T) {
	runner := newTestRunner(&fakeEngine{}, "/c")
	build := sandbox.ExecutionRequest{ExecutionID: "b1", ProjectID: project, Agent: "developer-agent", Image: "go", Category: sandbox.CategoryBuild,
		Argv: []string{"go", "build"}, AgentSession: &sandbox.AgentSession{SocketDir: "w", Token: "t"}}
	if _, err := runner.Execute(context.Background(), build); code(err) != sandbox.ProblemInvalidRequest {
		t.Fatalf("agent session in a build: %v", err)
	}
	build.AgentSession, build.Environment = nil, map[string]string{"ANTHROPIC_SMALL_FAST_MODEL": "m"}
	if _, err := runner.Execute(context.Background(), build); code(err) != sandbox.ProblemForbiddenVariable {
		t.Fatalf("agent variable in a build: %v", err)
	}
}

func TestAgentSessionNeedsTheDetectedBinary(t *testing.T) {
	engine := &fakeEngine{versionErr: true}
	runner := newTestRunner(engine, "/c")
	if _, err := runner.Execute(context.Background(), agentRequest()); code(err) != sandbox.ProblemUnavailable {
		t.Fatalf("binary that does not answer: %v", err)
	}
	if len(engine.created) != 1 {
		t.Fatalf("only the version check runs: %d", len(engine.created))
	}
}

func TestClaudeCodeStatusIsCachedAndOffWithoutBinary(t *testing.T) {
	if status := newTestRunner(&fakeEngine{}, "").ClaudeCode(context.Background()); status.Available {
		t.Fatalf("no binary configured: %+v", status)
	}
	engine := &fakeEngine{version: "2.1.332 (Claude Code)"}
	runner := newTestRunner(engine, "/c")
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }
	for range 3 {
		if status := runner.ClaudeCode(context.Background()); status != (ClaudeCodeStatus{Available: true, Version: "2.1.332"}) {
			t.Fatalf("status: %+v", status)
		}
	}
	if len(engine.created) != 1 {
		t.Fatalf("cached: want 1 check, got %d", len(engine.created))
	}
	now = now.Add(2 * time.Minute)
	engine.versionErr = true
	if status := runner.ClaudeCode(context.Background()); status.Available || len(engine.created) != 2 {
		t.Fatalf("checked again after the cache: %+v (%d)", status, len(engine.created))
	}
	if got := parseVersion("!!! not a version"); got != "unknown" {
		t.Fatalf("parse: %q", got)
	}
}
