// Command easycode-gateway is the only process that opens provider keys and talks to AI
// providers (S-03, ADR-02, interno.md section 3). It serves the worker over a Unix socket.
// Providers: "anthropic" (real adapter) and "fake" (deterministic, development and tests). Sealed keys
// open only with the platform private key (ADR-13). "easycode-gateway keygen <private> <public>"
// creates the key pair once at installation.
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
	"strings"
	"time"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/process"
	"easycode/internal/infrastructure/provider/anthropic"
	fakeprovider "easycode/internal/infrastructure/provider/fake"
	"easycode/internal/infrastructure/sealing"
	"easycode/internal/infrastructure/telemetry"
	gatewayapi "easycode/internal/interfaces/gateway"
)

const serviceName = "easycode-gateway"

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// socketMode lets only the gateway and the worker group use the socket.
const socketMode = 0o660

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "keygen" {
		return keygen(os.Args[2:])
	}
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

func serve(ctx context.Context, proc process.Process) error {
	cfg := proc.Config
	providers, err := buildProviders(cfg.Gateway)
	if err != nil {
		return err
	}
	if cfg.Gateway.PrivateKeyFile == "" {
		return fmt.Errorf("%s is required (create it with: easycode-gateway keygen)", config.EnvGatewayPrivateKey)
	}
	opener, err := sealing.LoadOpener(cfg.Gateway.PrivateKeyFile)
	if err != nil {
		return err
	}
	proc.Logger.Info("platform private key loaded", slog.String("key_ref", opener.KeyRef()))
	tracing, err := telemetry.Setup(ctx, telemetry.Settings{
		Enabled: cfg.Telemetry.Enabled, Endpoint: cfg.Telemetry.Endpoint, Insecure: cfg.Telemetry.Insecure,
		SampleRatio: cfg.Telemetry.SampleRatio, Service: serviceName, Version: version,
	}, proc.Logger)
	if err != nil {
		return err
	}
	defer func() { _ = tracing.Shutdown(context.Background()) }()

	sessions := appgateway.NewService(opener, cfg.Gateway.SessionIdle, providers...)
	listener, err := listenSocket(cfg.Gateway.Socket)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           gatewayapi.NewHandler(sessions, proc.Logger, tracing.Provider.Tracer(serviceName), tracing.Propagator),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go expireIdleSessions(ctx, sessions, proc.Logger)

	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	proc.Logger.Info("gateway listening", slog.String("socket", cfg.Gateway.Socket), slog.Any("providers", sessions.Providers()))
	select {
	case err := <-failed:
		return fmt.Errorf("gateway server: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("gateway shutdown: %w", err)
	}
	return nil
}

// buildProviders enables the configured adapters.
func buildProviders(cfg config.Gateway) ([]provider.Provider, error) {
	available := anthropic.Name + ", " + fakeprovider.Name
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("%s is required (available: %s)", config.EnvGatewayProviders, available)
	}
	var providers []provider.Provider
	for _, name := range cfg.Providers {
		if alias, adapter, ok := strings.Cut(name, "="); ok && adapter == fakeprovider.Name && alias != "" {
			// Development stand-in: the fake answers under a real provider name, never with a real key.
			providers = append(providers, &fakeprovider.Provider{Alias: alias})
			continue
		}
		switch name {
		case anthropic.Name:
			providers = append(providers, anthropic.New(cfg.AnthropicBaseURL, nil, cfg.CallTimeout))
		case fakeprovider.Name:
			providers = append(providers, &fakeprovider.Provider{})
		default:
			return nil, fmt.Errorf("%s: unknown provider %q (available: %s)", config.EnvGatewayProviders, name, available)
		}
	}
	return providers, nil
}

// keygen creates the platform key pair if it does not exist (installation step, ADR-13).
func keygen(args []string) error {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: easycode-gateway keygen <private-key-file> <public-key-file>")
		return errors.New("keygen needs two paths")
	}
	created, err := sealing.GenerateKeyPair(args[0], args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "keygen failed:", err)
		return err
	}
	if created {
		fmt.Println("platform key pair created")
	} else {
		fmt.Println("platform key pair already exists; kept")
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

func expireIdleSessions(ctx context.Context, sessions *appgateway.Service, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if expired := sessions.ExpireIdle(); expired > 0 {
				logger.InfoContext(ctx, "gateway sessions expired", slog.Int("expired", expired))
			}
		}
	}
}
