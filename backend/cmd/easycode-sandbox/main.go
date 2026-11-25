// Command easycode-sandbox is the only process with the Docker socket (ADR-06, ADR-02). It serves the
// worker over a Unix socket and runs each agent command in a throwaway hardened container (ADR-05):
// no network, read-only root, tmpfs workspace with a size quota, no capability, no new privileges,
// default seccomp, non-root user and CPU, memory, pids and wall-clock limits. The profile is constant
// here; nothing in the request can change it.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	appsandbox "easycode/internal/application/sandbox"
	"easycode/internal/domain/sandbox"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/docker"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/process"
	"easycode/internal/infrastructure/specs"
	sandboxapi "easycode/internal/interfaces/sandbox"
)

const serviceName = "easycode-sandbox"

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// socketMode lets only the runner and the worker group use the socket.
const socketMode = 0o660

// bridgeBinary is the agent bridge of the agent sessions, shipped next to this binary (ADR-23).
const bridgeBinary = "agent-bridge"

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	proc, err := process.Start(serviceName, version)
	if err != nil {
		return err
	}
	ctx, stop := process.SignalContext()
	defer stop()
	if err := serve(ctx, proc); err != nil {
		proc.Logger.Error("process failed", logging.ErrorAttr(err))
		return err
	}
	proc.Logger.Info("process stopped")
	return nil
}

// runnerConfig turns the settings into the fixed configuration of the runner.
func runnerConfig(cfg config.Sandbox) appsandbox.Config {
	runner := appsandbox.Config{Images: cfg.Images, Runtime: cfg.Runtime, Defaults: sandbox.Limits{
		CPU: cfg.CPU, MemoryMiB: cfg.MemoryMiB, DiskMiB: cfg.DiskMiB, Time: cfg.CommandTime, Processes: 256, MaxOutputBytes: 16 << 20,
	}}
	runner.AgentSession = appsandbox.AgentSessionConfig{ClaudeCodeBin: cfg.ClaudeCodeBin, ProxyVolume: cfg.AgentProxyVolume, DetectImage: cfg.Image}
	runner.Dependencies.Network = cfg.DependenciesNetwork
	if cfg.DependenciesProxy != "" {
		runner.Dependencies.Env = []string{"GOPROXY=" + cfg.DependenciesProxy + "/go", "GOSUMDB=off", "GONOSUMCHECK=1",
			"npm_config_registry=" + cfg.DependenciesProxy + "/npm/"}
	}
	return runner
}

func serve(ctx context.Context, proc process.Process) error {
	cfg := proc.Config.Sandbox
	if len(cfg.Images) == 0 {
		return fmt.Errorf("%s is required (logical=image, e.g. go=easycode-sandbox-go:dev)", config.EnvSandboxImages)
	}
	if cfg.ClaudeCodeBin != "" {
		// The agent sessions run the bridge from the shared volume (ADR-23); without it they are off.
		if err := installBridge(bridgeSource(), cfg.AgentProxyDir); err != nil {
			proc.Logger.Error("agent bridge not installed: Claude Code agent sessions are off", logging.ErrorAttr(err))
			cfg.ClaudeCodeBin = ""
		}
	}
	runner := appsandbox.NewRunner(docker.New(cfg.DockerSocket), specs.AgentPolicy, runnerConfig(cfg))
	// The runner refuses to serve without the daemon and the images of the allowlist.
	ready := false
	for attempt := 0; attempt < 30 && ctx.Err() == nil; attempt++ {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		images, err := runner.Health(checkCtx)
		cancel()
		if err == nil {
			proc.Logger.Info("sandbox images ready", slog.Any("images", images), slog.String("runtime", cfg.Runtime))
			status := runner.ClaudeCode(ctx)
			proc.Logger.Info("claude code", slog.Bool("available", status.Available), slog.String("version", status.Version))
			ready = true
			break
		}
		proc.Logger.Warn("docker daemon or sandbox image not ready", logging.ErrorAttr(err))
		time.Sleep(2 * time.Second)
	}
	if !ready {
		return errors.New("docker daemon or sandbox images unavailable")
	}
	proc.Logger.Warn("workspace disk quota uses a size-limited tmpfs (no XFS prjquota on this host; ADR-05 v2 fallback)")
	listener, err := listenSocket(cfg.Socket)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: sandboxapi.NewServer(runner, proc.Logger).Handler(), ReadHeaderTimeout: 10 * time.Second}
	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	proc.Logger.Info("sandbox runner listening", slog.String("socket", cfg.Socket))
	select {
	case err := <-failed:
		return fmt.Errorf("sandbox server: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), proc.Config.HTTP.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("sandbox shutdown: %w", err)
	}
	return nil
}

// installBridge copies the agent-bridge shipped in the image (from) to <dir>/bin, the
// directory the agent session containers mount read-only. The copy is renamed in place, so a running
// session keeps the file it started with.
func installBridge(from, dir string) error {
	source, err := os.Open(from) //nolint:gosec // G304: fixed file of the image
	if err != nil {
		return fmt.Errorf("open bridge: %w", err)
	}
	defer source.Close()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil { //nolint:gosec // G301: read by the sandbox containers (uid 10001)
		return fmt.Errorf("create bridge directory: %w", err)
	}
	if err := os.Chmod(bin, 0o755); err != nil { //nolint:gosec // G302: read by the sandbox containers (uid 10001)
		return fmt.Errorf("chmod bridge directory: %w", err)
	}
	temp, err := os.CreateTemp(bin, ".agent-bridge-*")
	if err != nil {
		return fmt.Errorf("create bridge: %w", err)
	}
	defer os.Remove(temp.Name())
	if _, err := io.Copy(temp, source); err != nil {
		_ = temp.Close()
		return fmt.Errorf("copy bridge: %w", err)
	}
	if err := temp.Chmod(0o755); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod bridge: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close bridge: %w", err)
	}
	return os.Rename(temp.Name(), filepath.Join(bin, bridgeBinary))
}

// bridgeSource is the agent bridge next to this binary (/agent-bridge in the image, bin/ in development).
func bridgeSource() string {
	self, err := os.Executable()
	if err != nil {
		return bridgeBinary
	}
	return filepath.Join(filepath.Dir(self), bridgeBinary)
}

func listenSocket(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on socket: %w", err)
	}
	if err := os.Chmod(path, socketMode); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return listener, nil
}
