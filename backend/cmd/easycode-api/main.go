// Command easycode-api serves the REST API (ADR-02). "easycode-api migrate" applies the
// embedded migrations and exits; it runs as the one-shot migrate service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	apphealth "easycode/internal/application/health"
	"easycode/internal/infrastructure/i18n"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/process"
	"easycode/internal/infrastructure/telemetry"
	httpapi "easycode/internal/interfaces/http"
	"easycode/migrations"
)

const serviceName = "easycode-api"

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		os.Exit(1)
	}
}

func run(args []string) error {
	proc, err := process.Start(serviceName, version)
	if err != nil {
		return err
	}
	if err := proc.Config.RequireDatabase(); err != nil {
		proc.Logger.Error("invalid configuration", logging.ErrorAttr(err))
		return err
	}
	ctx, stop := process.SignalContext()
	defer stop()

	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "serve":
		err = serve(ctx, proc)
	case "migrate":
		err = migrate(ctx, proc)
	default:
		err = fmt.Errorf("unknown command %q (use serve or migrate)", command)
	}
	if err != nil {
		proc.Logger.Error("process failed", slog.String("command", command), logging.ErrorAttr(err))
		return err
	}
	proc.Logger.Info("process stopped", slog.String("command", command))
	return nil
}

func migrate(ctx context.Context, proc process.Process) error {
	files, err := postgres.LoadMigrations(migrations.Files())
	if err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: proc.Config.Database.MigrationURL, MaxConns: 1}, proc.Logger)
	if err != nil {
		return err
	}
	defer pool.Close()
	_, err = postgres.NewMigrator(pool, proc.Logger).Apply(ctx, files)
	return err
}

func serve(ctx context.Context, proc process.Process) error {
	cfg := proc.Config
	tracing, err := telemetry.Setup(ctx, telemetry.Settings{
		Enabled: cfg.Telemetry.Enabled, Endpoint: cfg.Telemetry.Endpoint, Insecure: cfg.Telemetry.Insecure,
		SampleRatio: cfg.Telemetry.SampleRatio, Service: serviceName, Version: version,
	}, proc.Logger)
	if err != nil {
		return err
	}
	defer shutdownTelemetry(tracing, proc.Logger)

	pool, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: cfg.Database.URL, MaxConns: cfg.Database.MaxConns}, proc.Logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	translator, err := i18n.NewTranslator()
	if err != nil {
		return err
	}
	handler := httpapi.NewHandler(httpapi.Dependencies{
		Logger:     proc.Logger,
		Translator: translator,
		Health:     apphealth.NewService(version, cfg.Database.ProbeTimeout, postgres.NewDatabaseProbe(pool)),
		Tracer:     tracing.Provider.Tracer(serviceName),
		Propagator: tracing.Propagator,
	})
	server := httpapi.NewServer(handler, httpapi.ServerSettings{
		ReadTimeout: cfg.HTTP.ReadTimeout, WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout,
	}, proc.Logger)
	return listenUntilDone(ctx, proc, server.ListenAndServe, server.ShutdownWithContext)
}

func listenUntilDone(ctx context.Context, proc process.Process, listen func(string) error, shutdown func(context.Context) error) error {
	failed := make(chan error, 1)
	go func() { failed <- listen(proc.Config.HTTP.Addr) }()
	proc.Logger.Info("http server listening", slog.String("addr", proc.Config.HTTP.Addr))

	select {
	case err := <-failed:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}
	proc.Logger.Info("http server shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), proc.Config.HTTP.ShutdownTimeout)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("http shutdown: %w", err)
	}
	return nil
}

func shutdownTelemetry(tracing telemetry.Tracing, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tracing.Shutdown(ctx); err != nil {
		logger.Error("telemetry shutdown failed", logging.ErrorAttr(err))
	}
}
