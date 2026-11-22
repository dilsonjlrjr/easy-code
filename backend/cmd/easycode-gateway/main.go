// Command easycode-gateway is the only process that opens provider keys and talks to AI
// providers (S-03, ADR-02, interno.md section 3). It serves the worker over a Unix socket.
// Until Onda 1 brings the real provider adapter, only the deterministic "fake" provider exists.
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

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/process"
	fakeprovider "easycode/internal/infrastructure/provider/fake"
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
	providers, opener, err := buildProviders(cfg.Gateway.Providers)
	if err != nil {
		return err
	}
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

// buildProviders enables the configured adapters. Only "fake" exists before Onda 1.
func buildProviders(names []string) ([]provider.Provider, appgateway.KeyOpener, error) {
	if len(names) == 0 {
		return nil, nil, fmt.Errorf("%s is required (available: %s)", config.EnvGatewayProviders, fakeprovider.Name)
	}
	var providers []provider.Provider
	for _, name := range names {
		if name != fakeprovider.Name {
			return nil, nil, fmt.Errorf("%s: unknown provider %q", config.EnvGatewayProviders, name)
		}
		providers = append(providers, &fakeprovider.Provider{})
	}
	return providers, fakeprovider.KeyOpener{}, nil
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
