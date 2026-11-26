// Package sandbox is the use case of easycode-sandbox (ADR-05, ADR-06, interno.md section 2): it checks
// every request against the fixed profile (image allowlist, command categories of permissoes.yaml,
// ceilings of the agent, environment names) and runs the command in a throwaway hardened container
// whose workspace is a size-limited tmpfs filled from the wave working copy, so the disk limit is a
// real quota (S-13) and the runner keeps nothing of the host. Collect returns only regular files (S-02).
package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/permission"
	"easycode/internal/domain/sandbox"
)

// ErrNotFound is returned by the engine for an unknown object.
var ErrNotFound = errors.New("not found")

// Engine is the container engine (the Docker daemon through its socket).
type Engine interface {
	Ping(ctx context.Context) error
	ImageDigest(ctx context.Context, ref string) (string, error)
	Create(ctx context.Context, spec ContainerSpec) (string, error)
	Start(ctx context.Context, id string) error
	Exec(ctx context.Context, id string, spec ExecSpec, stdin io.Reader, out io.Writer) (int, error)
	Inspect(ctx context.Context, id string) (ContainerState, error)
	Remove(ctx context.Context, id string) error
	// VolumeMountpoint is the host directory of a named volume: agent sessions bind its subdirectories, because
	// the volume subpath option is not honored by every engine (podman's compat API mounts the whole volume).
	VolumeMountpoint(ctx context.Context, name string) (string, error)
}

// ContainerSpec is a container of the fixed profile; the engine adds the constant hardening.
type ContainerSpec struct {
	Name       string
	Image      string
	Cmd        []string
	User       string
	Env        []string
	WorkingDir string
	Labels     map[string]string
	Network    string // empty = none; only the dependency action names its network (ADR-07)
	CPU        float64
	MemoryMiB  int
	Processes  int
	Tmpfs      map[string]string
	Runtime    string
	Mounts     []Mount
}

// Mount types of a ContainerSpec.
const (
	MountBind   = "bind"
	MountVolume = "volume"
)

// Mount is a fixed mount of the agent session profile (ADR-23): a host file bound read-only or a
// subdirectory of a named volume. Requests never name one; the runner builds them.
type Mount struct {
	Type     string // MountBind | MountVolume
	Source   string // host path (bind) or volume name
	Target   string
	Subpath  string // directory inside the volume
	ReadOnly bool
}

// ExecSpec is a process run inside the container.
type ExecSpec struct {
	Cmd        []string
	Env        []string
	WorkingDir string
	User       string
}

// ContainerState is what the runner reads back of a container.
type ContainerState struct {
	Running      bool
	OOMKilled    bool
	Network      string
	ReadOnlyRoot bool
	MemoryBytes  int64
	NanoCPUs     int64
	Processes    int64
}

// AgentProfile is what the runner allows an agent (permissoes.yaml).
type AgentProfile func(agent string) (permission.Policy, error)

// Config is the fixed configuration of the runner.
type Config struct {
	Images       map[string]string // logical id → local image reference (allowlist)
	Runtime      string            // runc | runsc
	Defaults     sandbox.Limits    // RNF-03 defaults and the hard ceiling of the runner
	Dependencies struct {
		Network string   // internal network whose only exit is the deps-proxy (ADR-07); empty = action unavailable
		Env     []string // proxy settings of the package managers (GOPROXY, npm_config_registry...)
	}
	AgentSession AgentSessionConfig
}

// AgentSessionConfig turns on CategoryAgentSession (ADR-23, US-58).
type AgentSessionConfig struct {
	ClaudeCodeBin string // host path of the Claude Code binary, bound read-only; empty = category unavailable
	ProxyVolume   string // Docker volume shared with the worker: bin/ holds the bridge, one directory per wave socket
	DetectImage   string // logical image of the version check container (the default project stack)
}

// ClaudeCodeStatus says whether agent sessions can run and with which Claude Code version (RNF-15).
type ClaudeCodeStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
}

const (
	sandboxUser       = "10001:10001"
	workspaceDir      = "/workspace"
	seedDir           = "/opt/easycode/seed"
	maxWorkspaceFiles = 20000
	maxFileBytes      = 8 << 20
	resultRetention   = time.Hour
	killGrace         = 10 * time.Second // RNF-03: killed within 10 s
)

// Fixed paths of an agent session container (ADR-23): the Claude Code binary of the host, the bridge
// directory and the wave model proxy socket directory of the shared volume, the HOME of Claude Code.
const (
	claudeCodePath   = "/opt/claude-code/claude"
	bridgeDir        = "/opt/easycode-bridge"
	bridgePath       = bridgeDir + "/agent-bridge"
	modelSocketDir   = "/run/easycode-model"
	bridgeListen     = "127.0.0.1:8099"
	agentHome        = workspaceDir + "/.easycode/home"
	claudeCodeTTL    = time.Minute
	claudeCodeDetect = 30 * time.Second
)

var allowedVariables = map[string]bool{"GOFLAGS": true, "CI": true, "TZ": true, "LANG": true, "CGO_ENABLED": true, "NODE_ENV": true}

// agentVariables are the extra names an agent session may set (the model of the background calls).
var agentVariables = map[string]bool{"ANTHROPIC_SMALL_FAST_MODEL": true}

var (
	socketDirName = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)
	sessionToken  = regexp.MustCompile(`^[\x21-\x7e]{1,512}$`)
	versionText   = regexp.MustCompile(`^[0-9A-Za-z.+_-]{1,64}$`)
)

// Runner runs the executions.
type Runner struct {
	engine     Engine
	profiles   AgentProfile
	config     Config
	now        func() time.Time
	mu         sync.Mutex
	workspaces map[string][]byte     // project → tar of the working copy
	collected  map[string]collection // project → files after the last execution
	executions map[string]*execution // execution id → state
	busy       map[string]bool       // project → running
	dependency map[string][]byte     // project → tar of the dependency directory kept by the runner
	detectMu   sync.Mutex            // one Claude Code version check at a time
	claudeCode ClaudeCodeStatus      // last version check
	checkedAt  time.Time
}

type collection struct {
	files   map[string][]byte
	refused []Refused
}

// Refused is a workspace entry the collection did not accept, with the reason (S-02).
type Refused struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type execution struct {
	request  sandbox.ExecutionRequest
	result   sandbox.ExecutionResult
	output   []byte
	done     chan struct{}
	abort    context.CancelFunc
	finished time.Time
}

// NewRunner builds the runner.
func NewRunner(engine Engine, profiles AgentProfile, config Config) *Runner {
	return &Runner{engine: engine, profiles: profiles, config: config, now: time.Now, workspaces: map[string][]byte{},
		collected: map[string]collection{}, executions: map[string]*execution{}, busy: map[string]bool{}, dependency: map[string][]byte{}}
}

func problem(kind failure.Kind, code string) error { return failure.New(kind, code, nil) }

// Health checks the daemon and the images of the allowlist.
func (r *Runner) Health(ctx context.Context) (map[string]string, error) {
	if err := r.engine.Ping(ctx); err != nil {
		return nil, problem(failure.KindUnavailable, sandbox.ProblemUnavailable)
	}
	images := map[string]string{}
	for logical, ref := range r.config.Images {
		digest, err := r.engine.ImageDigest(ctx, ref)
		if err != nil {
			return nil, problem(failure.KindUnavailable, sandbox.ProblemUnavailable)
		}
		images[logical] = digest
	}
	return images, nil
}

// PrepareWorkspace stores the working copy of the wave (a tar of regular files) for the next executions
// of the project. The worker sends it before every execution: the runner never reads the repository.
func (r *Runner) PrepareWorkspace(projectID string, archive []byte) error {
	if _, err := readArchive(archive, maxFileBytes, int64(r.config.Defaults.DiskMiB)<<20); err != nil {
		return failure.New(failure.KindInvalidInput, sandbox.ProblemInvalidRequest, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaces[projectID] = archive
	return nil
}

// DestroyWorkspace forgets everything of the project (exclusion, US-04).
func (r *Runner) DestroyWorkspace(projectID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.workspaces, projectID)
	delete(r.collected, projectID)
	delete(r.dependency, projectID)
}

// validate checks the request against the profile and returns the limits to apply.
func (r *Runner) validate(request sandbox.ExecutionRequest, dependencies bool) (sandbox.Limits, string, error) {
	invalid := func(code string) error { return problem(failure.KindInvalidInput, code) }
	if request.ExecutionID == "" || request.ProjectID == "" || request.Agent == "" {
		return sandbox.Limits{}, "", invalid(sandbox.ProblemInvalidRequest)
	}
	image, ok := r.config.Images[request.Image]
	if !ok {
		return sandbox.Limits{}, "", invalid(sandbox.ProblemImageNotAllowed)
	}
	policy, err := r.profiles(request.Agent)
	if err != nil {
		return sandbox.Limits{}, "", invalid(sandbox.ProblemCommandNotAllowed)
	}
	if dependencies != (request.Category == sandbox.CategoryDependencies) {
		return sandbox.Limits{}, "", invalid(sandbox.ProblemCommandNotAllowed)
	}
	agentSession := request.Category == sandbox.CategoryAgentSession
	switch {
	case agentSession:
		// Every agent runs through Claude Code, file agents included: the argv is the Claude Code
		// arguments and the runner builds the whole command (no shell).
		if err := r.validateAgentSession(request); err != nil {
			return sandbox.Limits{}, "", err
		}
	case request.AgentSession != nil:
		return sandbox.Limits{}, "", invalid(sandbox.ProblemInvalidRequest)
	default:
		category, ok := policy.Category(request.Category)
		if !ok || !policy.Execute || category.Allowed(request.Argv) != nil {
			return sandbox.Limits{}, "", invalid(sandbox.ProblemCommandNotAllowed)
		}
	}
	if len(request.Services) > 0 {
		return sandbox.Limits{}, "", invalid(sandbox.ProblemServiceNotAllowed)
	}
	if _, ok := sandbox.CleanDirectory(request.Directory); !ok {
		return sandbox.Limits{}, "", invalid(sandbox.ProblemInvalidRequest)
	}
	for name := range request.Environment {
		allowed := allowedVariables[name] || agentSession && agentVariables[name]
		if !allowed || sandbox.ForbiddenVariable.MatchString(name) {
			return sandbox.Limits{}, "", invalid(sandbox.ProblemForbiddenVariable)
		}
	}
	ceiling := policy.Ceiling.Min(r.config.Defaults)
	ceiling.Processes, ceiling.MaxOutputBytes = r.config.Defaults.Processes, r.config.Defaults.MaxOutputBytes
	if !request.Limits.Within(ceiling) {
		return sandbox.Limits{}, "", problem(failure.KindConflict, sandbox.ProblemLimitAboveCeiling)
	}
	return request.Limits.Min(ceiling), image, nil
}

// validateAgentSession checks what only an agent session carries: the feature is on, the socket
// directory is one plain name of the shared volume, the session token is set and the writable paths are
// clean workspace-relative paths.
func (r *Runner) validateAgentSession(request sandbox.ExecutionRequest) error {
	invalid := problem(failure.KindInvalidInput, sandbox.ProblemInvalidRequest)
	if r.config.AgentSession.ClaudeCodeBin == "" || r.config.AgentSession.ProxyVolume == "" {
		return problem(failure.KindUnavailable, sandbox.ProblemUnavailable)
	}
	session := request.AgentSession
	if session == nil || !socketDirName.MatchString(session.SocketDir) || session.SocketDir == "bin" || !sessionToken.MatchString(session.Token) {
		return invalid
	}
	if len(request.Argv) == 0 {
		return invalid
	}
	for _, argument := range request.Argv {
		if strings.ContainsRune(argument, 0) {
			return invalid
		}
	}
	if _, err := writablePaths(session.Writable); err != nil {
		return invalid
	}
	return nil
}

// writablePaths turns the workspace-relative writable prefixes into the absolute list of
// EASYCODE_WRITABLE (separated by ':').
func writablePaths(writable []string) ([]string, error) {
	paths := make([]string, 0, len(writable))
	for _, p := range writable {
		clean, ok := sandbox.CleanDirectory(p)
		if strings.TrimSpace(p) == "" || !ok || strings.Contains(p, ":") {
			return nil, fmt.Errorf("writable path not allowed: %q", p)
		}
		paths = append(paths, path.Join(workspaceDir, clean))
	}
	return paths, nil
}

// agentCommand is the command of an agent session: the bridge listens where Claude Code sends its model
// calls and forwards them to the wave socket, then runs Claude Code with the arguments of the worker.
func agentCommand(argv []string) []string {
	return append([]string{bridgePath, "exec", "--listen", bridgeListen, "--socket", modelSocketDir + "/model.sock", "--", claudeCodePath}, argv...)
}

// agentEnvironment is the fixed environment of Claude Code in the container: model calls only to the
// bridge with the wave session token (never a provider key), no update, telemetry or other traffic.
func agentEnvironment(session *sandbox.AgentSession) []string {
	writable, _ := writablePaths(session.Writable)
	return []string{"ANTHROPIC_BASE_URL=http://" + bridgeListen, "ANTHROPIC_AUTH_TOKEN=" + session.Token,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1",
		"EASYCODE_WRITABLE=" + strings.Join(writable, ":")}
}

// agentMounts are the mounts of an agent session: the binary of the host and the bridge read-only, the
// wave socket directory read-write.
func (r *Runner) agentMounts(ctx context.Context, socketDir string) ([]Mount, error) {
	root, err := r.engine.VolumeMountpoint(ctx, r.config.AgentSession.ProxyVolume)
	if err != nil || root == "" {
		return nil, fmt.Errorf("agent proxy volume: %w", err)
	}
	return []Mount{
		{Type: MountBind, Source: r.config.AgentSession.ClaudeCodeBin, Target: claudeCodePath, ReadOnly: true},
		{Type: MountBind, Source: path.Join(root, "bin"), Target: bridgeDir, ReadOnly: true},
		{Type: MountBind, Source: path.Join(root, socketDir), Target: modelSocketDir},
	}, nil
}

// ClaudeCode reports whether agent sessions can run: the binary is configured and answers --version in a
// short container of the default stack image. The answer is kept for a minute (health asks often).
func (r *Runner) ClaudeCode(ctx context.Context) ClaudeCodeStatus {
	if r.config.AgentSession.ClaudeCodeBin == "" || r.config.AgentSession.ProxyVolume == "" {
		return ClaudeCodeStatus{}
	}
	r.detectMu.Lock()
	defer r.detectMu.Unlock()
	if !r.checkedAt.IsZero() && r.now().Sub(r.checkedAt) < claudeCodeTTL {
		return r.claudeCode
	}
	status := r.detectClaudeCode(ctx)
	if ctx.Err() == nil {
		r.claudeCode, r.checkedAt = status, r.now()
	}
	return status
}

func (r *Runner) detectClaudeCode(ctx context.Context) ClaudeCodeStatus {
	image, ok := r.config.Images[r.config.AgentSession.DetectImage]
	if !ok {
		return ClaudeCodeStatus{}
	}
	ctx, cancel := context.WithTimeout(ctx, claudeCodeDetect)
	defer cancel()
	id, err := r.engine.Create(ctx, ContainerSpec{
		Name: fmt.Sprintf("easycode-sbx-claude-version-%d", r.now().UnixNano()), Image: image, Cmd: []string{"sleep", "infinity"},
		User: sandboxUser, Env: []string{"HOME=/tmp/home", "DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1"},
		WorkingDir: "/tmp", CPU: 1, MemoryMiB: 1024, Processes: 64, Runtime: r.config.Runtime,
		Tmpfs:  map[string]string{"/tmp": "rw,nosuid,nodev,size=64m,mode=1777"},
		Mounts: []Mount{{Type: MountBind, Source: r.config.AgentSession.ClaudeCodeBin, Target: claudeCodePath, ReadOnly: true}},
		Labels: map[string]string{"br.easycode.sandbox": "1", "br.easycode.check": "claude-code-version"},
	})
	if err != nil {
		return ClaudeCodeStatus{}
	}
	defer func() { _ = r.engine.Remove(context.WithoutCancel(ctx), id) }()
	if err := r.engine.Start(ctx, id); err != nil {
		return ClaudeCodeStatus{}
	}
	out := &limitedBuffer{limit: 4096}
	code, err := r.engine.Exec(ctx, id, ExecSpec{User: sandboxUser, WorkingDir: "/tmp", Cmd: []string{claudeCodePath, "--version"}}, nil, out)
	if err != nil || code != 0 {
		return ClaudeCodeStatus{}
	}
	return ClaudeCodeStatus{Available: true, Version: parseVersion(out.buffer.String())}
}

// parseVersion keeps the version number of "2.1.332 (Claude Code)".
func parseVersion(output string) string {
	fields := strings.Fields(output)
	if len(fields) == 0 || !versionText.MatchString(fields[0]) {
		return "unknown"
	}
	return fields[0]
}

// Execute runs the command and waits for the end. Repeating the same execution id never runs the
// command again: it waits for and returns the first result (RNF-12).
func (r *Runner) Execute(ctx context.Context, request sandbox.ExecutionRequest) (sandbox.ExecutionResult, error) {
	return r.start(ctx, request, false)
}

// InstallDependencies is the separate dependency action (ADR-07): the package manager only, with the
// manifest and lock files, on the internal network whose only exit is the deps-proxy; install
// scripts are off by the argument patterns of the category. The result directory stays with the runner.
func (r *Runner) InstallDependencies(ctx context.Context, request sandbox.ExecutionRequest) (sandbox.ExecutionResult, error) {
	if r.config.Dependencies.Network == "" {
		return sandbox.ExecutionResult{}, problem(failure.KindUnavailable, sandbox.ProblemUnavailable)
	}
	return r.start(ctx, request, true)
}

func (r *Runner) start(ctx context.Context, request sandbox.ExecutionRequest, dependencies bool) (sandbox.ExecutionResult, error) {
	limits, image, err := r.validate(request, dependencies)
	if err != nil {
		return sandbox.ExecutionResult{}, err
	}
	agentVersion := ""
	if request.Category == sandbox.CategoryAgentSession {
		status := r.ClaudeCode(ctx)
		if !status.Available {
			return sandbox.ExecutionResult{}, problem(failure.KindUnavailable, sandbox.ProblemUnavailable)
		}
		agentVersion = status.Version
	}
	r.mu.Lock()
	r.expire()
	if existing, ok := r.executions[request.ExecutionID]; ok {
		r.mu.Unlock()
		if existing.request.ProjectID != request.ProjectID {
			return sandbox.ExecutionResult{}, problem(failure.KindForbidden, sandbox.ProblemOtherProject)
		}
		return r.wait(ctx, existing)
	}
	workspace, prepared := r.workspaces[request.ProjectID]
	if !prepared {
		r.mu.Unlock()
		return sandbox.ExecutionResult{}, problem(failure.KindConflict, sandbox.ProblemWorkspaceNotLoaded)
	}
	if r.busy[request.ProjectID] {
		r.mu.Unlock()
		return sandbox.ExecutionResult{}, problem(failure.KindConflict, sandbox.ProblemBusy)
	}
	runCtx, abort := context.WithCancel(context.WithoutCancel(ctx))
	current := &execution{request: request, done: make(chan struct{}), abort: abort,
		result: sandbox.ExecutionResult{ExecutionID: request.ExecutionID, Status: "running", Start: r.now(), Runtime: r.runtime()}}
	r.executions[request.ExecutionID] = current
	r.busy[request.ProjectID] = true
	deps := r.dependency[request.ProjectID]
	r.mu.Unlock()

	go func() {
		defer abort()
		result, output, files, refused, kept := r.run(runCtx, request, image, limits, workspace, deps, dependencies)
		result.AgentVersion = agentVersion
		r.mu.Lock()
		current.result, current.output, current.finished = result, output, r.now()
		if files != nil {
			r.collected[request.ProjectID] = collection{files: files, refused: refused}
		}
		if kept != nil {
			r.dependency[request.ProjectID] = kept
		}
		delete(r.busy, request.ProjectID)
		r.mu.Unlock()
		close(current.done)
	}()
	return r.wait(ctx, current)
}

func (r *Runner) runtime() string {
	if r.config.Runtime == "" {
		return "runc"
	}
	return r.config.Runtime
}

func (r *Runner) wait(ctx context.Context, current *execution) (sandbox.ExecutionResult, error) {
	select {
	case <-current.done:
		return current.result, nil
	case <-ctx.Done():
		current.abort() // the caller left (pause now, cancel): abort the execution (RNF-10)
		<-current.done
		return current.result, nil
	}
}

// expire drops results older than the retention (called with the lock held).
func (r *Runner) expire() {
	for id, e := range r.executions {
		if !e.finished.IsZero() && r.now().Sub(e.finished) > resultRetention {
			delete(r.executions, id)
		}
	}
}

// Result returns the execution, also while it runs.
func (r *Runner) Result(executionID string) (sandbox.ExecutionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[executionID]
	if !ok {
		return sandbox.ExecutionResult{}, problem(failure.KindNotFound, sandbox.ProblemExecutionNotFound)
	}
	return e.result, nil
}

// Output returns the full output of a finished execution.
func (r *Runner) Output(executionID string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[executionID]
	if !ok || e.finished.IsZero() {
		return nil, problem(failure.KindNotFound, sandbox.ProblemExecutionNotFound)
	}
	return e.output, nil
}

// Abort stops a running execution (docker rm -f).
func (r *Runner) Abort(executionID string) error {
	r.mu.Lock()
	e, ok := r.executions[executionID]
	r.mu.Unlock()
	if !ok {
		return problem(failure.KindNotFound, sandbox.ProblemExecutionNotFound)
	}
	e.abort()
	<-e.done
	return nil
}

// Collect returns the regular files the last execution of the project left in the workspace and the
// entries refused (links, devices, oversize), never following any of them (S-02).
func (r *Runner) Collect(projectID string) (map[string][]byte, []Refused, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.collected[projectID]
	if !ok {
		return nil, nil, problem(failure.KindNotFound, sandbox.ProblemExecutionNotFound)
	}
	return c.files, c.refused, nil
}

// limitedBuffer keeps at most limit bytes and reports the overflow.
type limitedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int64
	total    int64
	overflow func()
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += int64(len(p))
	room := b.limit - int64(b.buffer.Len())
	if room > 0 {
		if int64(len(p)) > room {
			b.buffer.Write(p[:room])
		} else {
			b.buffer.Write(p)
		}
	}
	if b.total > b.limit && b.overflow != nil {
		b.overflow()
		b.overflow = nil
	}
	return len(p), nil
}

// run is one execution: create, start, fill the workspace, run, collect, remove.
func (r *Runner) run(ctx context.Context, request sandbox.ExecutionRequest, image string, limits sandbox.Limits, workspace, deps []byte, dependencies bool) (
	result sandbox.ExecutionResult, output []byte, files map[string][]byte, refused []Refused, kept []byte) {
	if limits.Time <= 0 {
		limits.Time = 30 * time.Minute
	}
	if limits.DiskMiB <= 0 {
		limits.DiskMiB = 10240
	}
	if limits.MemoryMiB <= 0 {
		limits.MemoryMiB = 4096
	}
	if limits.CPU <= 0 {
		limits.CPU = 2
	}
	if limits.Processes <= 0 {
		limits.Processes = 256
	}
	result = sandbox.ExecutionResult{ExecutionID: request.ExecutionID, Status: "finished", Start: r.now(), Runtime: r.runtime(), Profile: "code_execution"}
	if dependencies {
		result.Profile = "dependency_action"
	}
	fail := func(reason string) (sandbox.ExecutionResult, []byte, map[string][]byte, []Refused, []byte) {
		result.EndReason, result.End = reason, r.now()
		result.Duration = result.End.Sub(result.Start)
		return result, output, nil, nil, nil
	}
	digest, err := r.engine.ImageDigest(ctx, image)
	if err != nil {
		return fail(sandbox.EndExecutorError)
	}
	result.ImageDigest = digest
	// Fixed environment of the profile: nothing of the runner or the worker reaches the container.
	env := []string{"HOME=/tmp/home", "GOCACHE=/tmp/gocache", "GOPATH=/tmp/gopath", "GOMODCACHE=" + workspaceDir + "/.easycode/deps/gomod",
		"GOTOOLCHAIN=local", "GOFLAGS=-mod=mod", "CGO_ENABLED=0", "npm_config_cache=/tmp/npm", "CI=1"}
	network := ""
	if dependencies {
		network = r.config.Dependencies.Network
		env = append(env, "npm_config_ignore_scripts=true")
		env = append(env, r.config.Dependencies.Env...)
	} else {
		env = append(env, "GOPROXY=off", "GOSUMDB=off")
	}
	agentSession := request.Category == sandbox.CategoryAgentSession
	var mounts []Mount
	if agentSession {
		result.Profile = "agent_session"
		env[0] = "HOME=" + agentHome
		env = append(env, agentEnvironment(request.AgentSession)...)
		if mounts, err = r.agentMounts(ctx, request.AgentSession.SocketDir); err != nil {
			return fail(sandbox.EndExecutorError)
		}
	}
	for name, value := range request.Environment {
		env = append(env, name+"="+value)
	}
	tmpfs := map[string]string{
		"/tmp": "rw,exec,nosuid,nodev,size=1024m,mode=1777",
		// No uid/gid option: podman refuses it ("unknown mount option"); the sticky world-writable root lets the
		// sandbox user (10001) work in it on every engine, and the container has no other user.
		workspaceDir: fmt.Sprintf("rw,exec,nosuid,nodev,size=%dm,mode=1777", limits.DiskMiB),
	}
	id, err := r.engine.Create(ctx, ContainerSpec{
		Name: "easycode-sbx-" + request.ExecutionID, Image: image, Cmd: []string{"sleep", "infinity"}, User: sandboxUser, Env: env,
		WorkingDir: workspaceDir, Network: network, CPU: limits.CPU, MemoryMiB: limits.MemoryMiB, Processes: limits.Processes,
		Tmpfs: tmpfs, Runtime: r.config.Runtime, Mounts: mounts,
		Labels: map[string]string{"br.easycode.sandbox": "1", "br.easycode.project": request.ProjectID, "br.easycode.wave": request.WaveID,
			"br.easycode.agent": request.Agent, "br.easycode.execution": request.ExecutionID},
	})
	if err != nil {
		return fail(sandbox.EndExecutorError)
	}
	cleanup := context.WithoutCancel(ctx)
	defer func() { _ = r.engine.Remove(cleanup, id) }()
	if err := r.engine.Start(ctx, id); err != nil {
		return fail(sandbox.EndExecutorError)
	}
	// Fill the tmpfs workspace: the stack seed (warm build cache) to /tmp, then the working copy and the
	// dependency directory the runner keeps for the project.
	input := workspace
	if !dependencies && len(deps) > 0 {
		input = merge(workspace, deps)
	}
	var prepare bytes.Buffer
	script := "mkdir -p /tmp/home && { [ -d " + seedDir + " ] && cp -a " + seedDir + "/. /tmp/ ; true; } && tar -x -f - -C " + workspaceDir
	if agentSession {
		// The HOME of Claude Code (session, settings) lives in the workspace, out of the collection.
		script += " && mkdir -p " + agentHome
	}
	code, err := r.engine.Exec(ctx, id, ExecSpec{User: sandboxUser, WorkingDir: "/", Cmd: []string{"sh", "-c", script}}, bytes.NewReader(input), &prepare)
	if err != nil || code != 0 {
		if strings.Contains(strings.ToLower(prepare.String()), "no space left") {
			return fail(sandbox.EndDiskLimit)
		}
		return fail(sandbox.EndExecutorError)
	}
	// The command, with the wall-clock limit and the output limit.
	maxOutput := limits.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 16 << 20
	}
	commandCtx, stop := context.WithTimeout(ctx, limits.Time)
	defer stop()
	var outputLimit bool
	buffer := &limitedBuffer{limit: maxOutput, overflow: func() { outputLimit = true; stop() }}
	directory, _ := sandbox.CleanDirectory(request.Directory)
	command := request.Argv
	if agentSession {
		command = agentCommand(request.Argv)
	}
	exit, execErr := r.engine.Exec(commandCtx, id, ExecSpec{User: sandboxUser, WorkingDir: path.Join(workspaceDir, directory), Cmd: command}, nil, buffer)
	timedOut := errors.Is(commandCtx.Err(), context.DeadlineExceeded)
	aborted := ctx.Err() != nil
	if timedOut || aborted || outputLimit {
		killCtx, cancel := context.WithTimeout(cleanup, killGrace)
		_ = r.engine.Remove(killCtx, id)
		cancel()
	}
	output = buffer.buffer.Bytes()
	result.End = r.now()
	result.Duration = result.End.Sub(result.Start)
	result.OutputBytes = buffer.total
	sum := sha256.Sum256(output)
	result.OutputSHA256 = hex.EncodeToString(sum[:])
	result.OutputStart = string(output[:min(len(output), sandbox.OutputStartBytes)])
	if request.Category == sandbox.CategoryTests {
		result.Tests = sandbox.ParseTests(string(output))
	}
	switch {
	case outputLimit:
		result.EndReason = sandbox.EndOutputLimit
	case timedOut:
		result.EndReason = sandbox.EndTimeLimit
	case aborted:
		result.EndReason = sandbox.EndAborted
	case execErr != nil:
		result.EndReason = sandbox.EndExecutorError
	default:
		result.EndReason = classifyExit(exit, string(output))
		if result.EndReason == sandbox.EndCompleted {
			result.ExitCode = &exit
		}
	}
	if result.EndReason != sandbox.EndCompleted {
		return result, output, nil, nil, nil
	}
	// Collect the workspace (regular files only) and, after the dependency action, keep its directory.
	var archive bytes.Buffer
	archiveBuffer := &limitedBuffer{limit: int64(limits.DiskMiB) << 20}
	collectCode, err := r.engine.Exec(ctx, id, ExecSpec{User: sandboxUser, WorkingDir: "/", Cmd: []string{"tar", "-c", "-f", "-", "-C", workspaceDir, "."}}, nil, archiveBuffer)
	if err != nil || collectCode != 0 {
		return result, output, nil, nil, nil
	}
	archive.Write(archiveBuffer.buffer.Bytes())
	files, refused = collect(archive.Bytes())
	if dependencies {
		kept = depsArchive(files)
	}
	for p := range files {
		if strings.HasPrefix(p, ".easycode/") {
			delete(files, p)
		}
	}
	return result, output, files, refused, kept
}

// classifyExit names the limit behind a failed exit (RNF-03, US-41 CA-3): SIGKILL by the OOM killer,
// fork refused by the pids limit, ENOSPC of the tmpfs quota.
func classifyExit(code int, output string) string {
	lower := strings.ToLower(output)
	switch {
	case code == 137:
		return sandbox.EndMemoryLimit
	case code != 0 && strings.Contains(lower, "no space left on device"):
		return sandbox.EndDiskLimit
	case code != 0 && (strings.Contains(lower, "can't fork") || strings.Contains(lower, "cannot fork") ||
		strings.Contains(lower, "resource temporarily unavailable")):
		return sandbox.EndProcessLimit
	}
	return sandbox.EndCompleted
}

// merge joins the working copy and the dependency directory kept by the runner in one archive.
func merge(workspace, deps []byte) []byte {
	files, err := readArchive(workspace, maxFileBytes, 1<<40)
	if err != nil {
		return workspace
	}
	kept, err := readArchive(deps, maxFileBytes, 1<<40)
	if err != nil {
		return workspace
	}
	for p, content := range kept {
		files[p] = content
	}
	merged, err := Archive(files)
	if err != nil {
		return workspace
	}
	return merged
}

// readArchive reads a tar of the worker: only regular files with clean relative paths.
func readArchive(archive []byte, maxFile, maxTotal int64) (map[string][]byte, error) {
	files := map[string][]byte{}
	reader := tar.NewReader(bytes.NewReader(archive))
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		clean, ok := permission.CleanPath(header.Name)
		if header.Typeflag != tar.TypeReg || !ok || header.Size > maxFile {
			return nil, fmt.Errorf("entry not allowed: %s", header.Name)
		}
		total += header.Size
		if total > maxTotal || len(files) >= maxWorkspaceFiles {
			return nil, fmt.Errorf("workspace too large")
		}
		content, err := io.ReadAll(io.LimitReader(reader, maxFile))
		if err != nil {
			return nil, err
		}
		files[clean] = content
	}
}

// collect reads the archive of the container: regular files are accepted; links, devices, fifos and
// files over the size limit are refused with the reason and never followed (S-02).
func collect(archive []byte) (map[string][]byte, []Refused) {
	files := map[string][]byte{}
	var refused []Refused
	reader := tar.NewReader(bytes.NewReader(archive))
	for len(files) < maxWorkspaceFiles {
		header, err := reader.Next()
		if err != nil {
			break
		}
		name := strings.TrimPrefix(header.Name, "./")
		if name == "" || header.Typeflag == tar.TypeDir {
			continue
		}
		clean, ok := permission.CleanPath(name)
		switch {
		case !ok:
			refused = append(refused, Refused{Path: name, Reason: "path"})
		case header.Typeflag == tar.TypeSymlink:
			refused = append(refused, Refused{Path: clean, Reason: "symlink"})
		case header.Typeflag == tar.TypeLink:
			refused = append(refused, Refused{Path: clean, Reason: "hardlink"})
		case header.Typeflag != tar.TypeReg:
			refused = append(refused, Refused{Path: clean, Reason: "special_file"})
		case header.Size > maxFileBytes:
			refused = append(refused, Refused{Path: clean, Reason: "size"})
		default:
			content, err := io.ReadAll(io.LimitReader(reader, maxFileBytes))
			if err != nil {
				refused = append(refused, Refused{Path: clean, Reason: "read"})
				continue
			}
			files[clean] = content
		}
	}
	return files, refused
}

// depsArchive keeps the dependency directory (.easycode/deps) produced by the dependency action.
func depsArchive(files map[string][]byte) []byte {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for p, content := range files {
		if !strings.HasPrefix(p, ".easycode/deps/") {
			continue
		}
		_ = writer.WriteHeader(&tar.Header{Name: p, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg, Uid: 10001, Gid: 10001})
		_, _ = writer.Write(content)
	}
	_ = writer.Close()
	return buffer.Bytes()
}

// Archive builds the tar of a working copy (worker side and tests).
func Archive(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		content := files[p]
		if err := writer.WriteHeader(&tar.Header{Name: p, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg, Uid: 10001, Gid: 10001}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(content); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
