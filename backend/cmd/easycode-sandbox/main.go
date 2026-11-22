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
	runner := appsandbox.NewRunner(docker.New(cfg.DockerSocket), specs.AgentPolicy, runnerConfig(cfg))
	// The runner refuses to serve without the daemon and the images of the allowlist.
	ready := false
	for attempt := 0; attempt < 30 && ctx.Err() == nil; attempt++ {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		images, err := runner.Health(checkCtx)
		cancel()
		if err == nil {
			proc.Logger.Info("sandbox images ready", slog.Any("images", images), slog.String("runtime", cfg.Runtime))
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
