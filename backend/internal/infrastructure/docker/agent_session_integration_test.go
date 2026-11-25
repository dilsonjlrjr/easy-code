package docker_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appsandbox "easycode/internal/application/sandbox"
	"easycode/internal/domain/sandbox"
	"easycode/internal/infrastructure/docker"
	"easycode/internal/infrastructure/specs"
)

// fakeClaude stands in for the Claude Code binary of the host: it answers --version and otherwise makes
// one model call through the bridge, checks the write hook and writes a file of the agent.
const fakeClaude = `#!/bin/bash
if [ "$1" = "--version" ]; then echo "9.9.9 (Claude Code)"; exit 0; fi
echo "args=$*"
echo "home=$HOME writable=$EASYCODE_WRITABLE"
exec 3<>/dev/tcp/127.0.0.1/8099 || exit 10
printf 'POST /v1/messages HTTP/1.1\r\nHost: bridge\r\nAuthorization: Bearer %s\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}' "$ANTHROPIC_AUTH_TOKEN" >&3
cat <&3
echo '{"tool_name":"Write","tool_input":{"file_path":"/workspace/src/x.go"}}' | /opt/easycode-bridge/agent-bridge guard; echo "guard-outside=$?"
echo '{"tool_name":"Write","tool_input":{"file_path":"docs/out.md"}}' | /opt/easycode-bridge/agent-bridge guard; echo "guard-inside=$?"
test -d "$HOME" && echo home-ready
echo written > /workspace/docs/out.md
`

// ADR-23, US-58: an agent session runs the host binary read-only, without network, and reaches the wave
// model proxy only through the bridge and the socket of the shared volume. Needs, besides the daemon and
// the sandbox image, the host path of the module (EASYCODE_TEST_HOST_DIR, for the bind of the binary) and
// a volume mounted in the test (EASYCODE_TEST_AGENT_PROXY_VOLUME at EASYCODE_TEST_AGENT_PROXY_DIR).
func TestSandboxRunsAnAgentSessionThroughTheBridge(t *testing.T) {
	socket, image := os.Getenv("EASYCODE_TEST_DOCKER_SOCKET"), os.Getenv("EASYCODE_TEST_SANDBOX_IMAGE")
	hostDir, volume, proxyDir := os.Getenv("EASYCODE_TEST_HOST_DIR"), os.Getenv("EASYCODE_TEST_AGENT_PROXY_VOLUME"), os.Getenv("EASYCODE_TEST_AGENT_PROXY_DIR")
	if socket == "" || image == "" || hostDir == "" || volume == "" || proxyDir == "" {
		t.Skip("EASYCODE_TEST_DOCKER_SOCKET, EASYCODE_TEST_SANDBOX_IMAGE, EASYCODE_TEST_HOST_DIR and EASYCODE_TEST_AGENT_PROXY_* not set")
	}
	if _, err := os.Stat(socket); err != nil {
		t.Skip("no Docker socket")
	}
	// The bridge in bin/ of the volume, as easycode-sandbox installs it.
	bin := filepath.Join(proxyDir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "agent-bridge"), "easycode/cmd/easycode-agent-bridge")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build bridge: %v %s", err, out)
	}
	_ = os.Chmod(bin, 0o755)
	// The fake binary under the module, whose host path the daemon binds.
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	scratch, err := os.MkdirTemp(root, ".agent-session-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(scratch) })
	if err := os.WriteFile(filepath.Join(scratch, "claude"), []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	hostBinary := filepath.Join(hostDir, filepath.Base(scratch), "claude")
	// The wave model proxy of the worker: a Unix socket in a directory of the volume.
	socketDir := "wave-" + uuid.NewString()
	waveDir := filepath.Join(proxyDir, socketDir)
	if err := os.Mkdir(waveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(waveDir) })
	_ = os.Chmod(waveDir, 0o777)
	listener, err := net.Listen("unix", filepath.Join(waveDir, "model.sock"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(waveDir, "model.sock"), 0o666)
	calls := make(chan string, 4)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls <- r.Method + " " + r.URL.Path + " " + r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "model-answer")
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	config := appsandbox.Config{Images: map[string]string{"go": image}, Runtime: "runc",
		Defaults:     sandbox.Limits{CPU: 1, MemoryMiB: 1024, DiskMiB: 256, Time: 2 * time.Minute, Processes: 128, MaxOutputBytes: 1 << 20},
		AgentSession: appsandbox.AgentSessionConfig{ClaudeCodeBin: hostBinary, ProxyVolume: volume, DetectImage: "go"}}
	runner := appsandbox.NewRunner(docker.New(socket), specs.AgentPolicy, config)
	if status := runner.ClaudeCode(context.Background()); status != (appsandbox.ClaudeCodeStatus{Available: true, Version: "9.9.9"}) {
		t.Fatalf("detection: %+v", status)
	}
	req := request(sandbox.CategoryAgentSession, "-p", "--model", "claude-test")
	req.Agent = "requirements-agent" // a file agent runs through Claude Code too
	req.AgentSession = &sandbox.AgentSession{SocketDir: socketDir, Token: "wave-session-token", Writable: []string{"docs"}}
	result := run(t, runner, map[string]string{"docs/a.md": "a"}, req)
	out := result.OutputStart
	if !result.Succeeded() || result.AgentVersion != "9.9.9" || result.Profile != "agent_session" {
		t.Fatalf("result: %+v %q", result, out)
	}
	for _, want := range []string{"args=-p --model claude-test", "home=/workspace/.easycode/home writable=/workspace/docs", "model-answer",
		"guard-outside=2", "guard-inside=0", "home-ready", "easycode: write to /workspace/src/x.go is not allowed for this agent"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in %q", want, out)
		}
	}
	select {
	case call := <-calls:
		if call != "POST /v1/messages Bearer wave-session-token" {
			t.Errorf("proxy saw %q", call)
		}
	default:
		t.Error("no model call reached the socket")
	}
	files, _, err := runner.Collect(req.ProjectID)
	if err != nil || string(files["docs/out.md"]) != "written\n" {
		t.Fatalf("collect: %v %v", files, err)
	}
	for p := range files {
		if strings.HasPrefix(p, ".easycode/") {
			t.Errorf("collected %s", p)
		}
	}
}
