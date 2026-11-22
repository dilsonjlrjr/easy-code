// Command easycode-worker runs the background work (ADR-02). In this skeleton it only
// keeps a heartbeat job that checks the database, so the process lifecycle can be exercised.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	apphealth "easycode/internal/application/health"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/process"
	"easycode/internal/infrastructure/telemetry"
)

const serviceName = "easycode-worker"

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

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
	if err := proc.Config.RequireDatabase(); err != nil {
		proc.Logger.Error("invalid configuration", logging.ErrorAttr(err))
		return err
	}
	ctx, stop := process.SignalContext()
	defer stop()

	if err := work(ctx, proc); err != nil {
		proc.Logger.Error("process failed", logging.ErrorAttr(err))
		return err
	}
	proc.Logger.Info("process stopped")
	return nil
}

func work(ctx context.Context, proc process.Process) error {
	cfg := proc.Config
	tracing, err := telemetry.Setup(ctx, telemetry.Settings{
		Enabled: cfg.Telemetry.Enabled, Endpoint: cfg.Telemetry.Endpoint, Insecure: cfg.Telemetry.Insecure,
		SampleRatio: cfg.Telemetry.SampleRatio, Service: serviceName, Version: version,
	}, proc.Logger)
	if err != nil {
		return err
	}
	defer func() { _ = tracing.Shutdown(context.Background()) }()

	pool, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: cfg.Database.URL, MaxConns: cfg.Database.MaxConns}, proc.Logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	health := apphealth.NewService(version, cfg.Database.ProbeTimeout, postgres.NewDatabaseProbe(pool))
	go runPartitionJob(ctx, postgres.NewPartitionMaintainer(pool), partitionInterval, proc.Logger)
	runHeartbeat(ctx, health, cfg.Worker.HeartbeatInterval, proc.Logger)
	return nil
}

// partitionInterval is how often the partition job runs after the run at startup.
const partitionInterval = 24 * time.Hour

// partitionMaintainer creates the monthly partitions that are missing.
type partitionMaintainer interface {
	EnsureMonthlyPartitions(ctx context.Context) (int, error)
}

// runPartitionJob creates the monthly partitions at startup and then at every interval
// until ctx is canceled. A failure is logged and retried at the next tick.
func runPartitionJob(ctx context.Context, maintainer partitionMaintainer, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		started := time.Now()
		created, err := maintainer.EnsureMonthlyPartitions(ctx)
		attrs := []slog.Attr{
			slog.String("job", "create_monthly_partitions"),
			slog.Float64("duration_ms", float64(time.Since(started).Microseconds())/1000),
		}
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "job failed", append(attrs, logging.ErrorAttr(err))...)
		} else {
			logger.LogAttrs(ctx, slog.LevelInfo, "job finished", append(attrs, slog.Int("created", created))...)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runHeartbeat runs the heartbeat job at every interval until ctx is canceled.
func runHeartbeat(ctx context.Context, health *apphealth.Service, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger.InfoContext(ctx, "worker loop started", slog.String("heartbeat_interval", interval.String()))
	for {
		heartbeat(ctx, health, logger)
		select {
		case <-ctx.Done():
			logger.Info("worker loop stopping")
			return
		case <-ticker.C:
		}
	}
}

func heartbeat(ctx context.Context, health *apphealth.Service, logger *slog.Logger) {
	started := time.Now()
	logger.DebugContext(ctx, "job started", slog.String("job", "heartbeat"))
	_, err := health.Readiness(ctx)
	attrs := []slog.Attr{
		slog.String("job", "heartbeat"),
		slog.Float64("duration_ms", float64(time.Since(started).Microseconds())/1000),
	}
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "job failed", append(attrs, logging.ErrorAttr(err))...)
		return
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "job finished", attrs...)
}
