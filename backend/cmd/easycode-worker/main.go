// Command easycode-worker runs the background work (ADR-02): partition maintenance, the
// outbox publisher of the live event bus (ADR-11) and a heartbeat that checks the database.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"easycode/internal/application/command"
	apphealth "easycode/internal/application/health"
	"easycode/internal/application/providerkey"
	"easycode/internal/application/retention"
	appskill "easycode/internal/application/skill"
	"easycode/internal/application/ssosync"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/oidc"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/process"
	"easycode/internal/infrastructure/specs"
	"easycode/internal/infrastructure/telemetry"
	gatewayapi "easycode/internal/interfaces/gateway"
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

	jobs := jobRunner{logger: proc.Logger, tracer: tracing.Provider.Tracer(serviceName)}
	health := apphealth.NewService(version, cfg.Database.ProbeTimeout, postgres.NewDatabaseProbe(pool))
	go runPartitionJob(ctx, jobs, postgres.NewPartitionMaintainer(pool), partitionInterval)

	wake := make(chan struct{}, 1)
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	reconcileWake := make(chan struct{}, 1)
	both := func() {
		signal()
		select {
		case reconcileWake <- struct{}{}:
		default:
		}
	}
	outboxListener := postgres.NewListener(pool, proc.Logger, func(context.Context) { both() }, postgres.ChannelOutbox)
	go func() { _ = outboxListener.Run(ctx, func(string, string) { both() }) }()
	go runOutboxPublisher(ctx, jobs, postgres.NewOutboxPublisher(pool), wake, cfg.Worker.OutboxPollInterval)

	dispatcher, parts, err := newDispatcher(cfg, pool, tracing.Propagator, proc.Logger)
	if err != nil {
		return err
	}
	// US-43: the native catalog (agentes/skills) is published in the library; a changed skill gets a new
	// version, which projects use only after the lead accepts it.
	jobs.run(ctx, "publish_native_skills", false, func(ctx context.Context) ([]slog.Attr, error) {
		packages, err := specs.NativeSkills()
		if err != nil {
			return nil, err
		}
		published, err := appskill.PublishNative(ctx, postgres.NewTxRunner(pool), postgres.NewSkills(pool), packages)
		return []slog.Attr{slog.Int("skills", len(packages)), slog.Int("published", published)}, err
	})
	waveWake := make(chan struct{}, 1)
	waveListener := postgres.NewListener(pool, proc.Logger, nil, postgres.ChannelWaves)
	go func() {
		_ = waveListener.Run(ctx, func(string, string) {
			select {
			case waveWake <- struct{}{}:
			default:
			}
		})
	}()
	go parts.executor.Run(ctx, waveWake)
	go runReconcile(ctx, jobs, parts.progress, parts.findings, parts.orchestrator, postgres.NewTxRunner(pool), reconcileWake, cfg.Worker.CommandPollInterval)
	commandWake := make(chan struct{}, 1)
	commandSignal := func() {
		select {
		case commandWake <- struct{}{}:
		default:
		}
	}
	commandListener := postgres.NewListener(pool, proc.Logger, func(context.Context) { commandSignal() }, postgres.ChannelCommands)
	go func() { _ = commandListener.Run(ctx, func(string, string) { commandSignal() }) }()
	go runCommandQueue(ctx, jobs, dispatcher, commandWake, cfg.Worker.CommandPollInterval)
	startSSOSync(ctx, cfg, pool, jobs, proc.Logger)
	startRetention(ctx, cfg, pool, jobs)

	runHeartbeat(ctx, jobs, health, cfg.Worker.HeartbeatInterval)
	return nil
}

// jobRunner runs one job execution inside a span and writes one log line with its outcome (RNF-14).
type jobRunner struct {
	logger *slog.Logger
	tracer trace.Tracer
}

// run executes fn. A quiet job logs successful runs at debug level (frequent jobs).
func (j jobRunner) run(ctx context.Context, name string, quiet bool, fn func(ctx context.Context) ([]slog.Attr, error)) {
	started := time.Now()
	ctx, span := j.tracer.Start(ctx, "job "+name, trace.WithAttributes(attribute.String("easycode.job", name)))
	defer span.End()
	attrs, err := fn(ctx)
	attrs = append(attrs, slog.String("job", name), slog.Float64("duration_ms", float64(time.Since(started).Microseconds())/1000))
	if err != nil {
		span.SetStatus(codes.Error, "job failed")
		j.logger.LogAttrs(ctx, slog.LevelError, "job failed", append(attrs, logging.ErrorAttr(err))...)
		return
	}
	level := slog.LevelInfo
	if quiet {
		level = slog.LevelDebug
	}
	j.logger.LogAttrs(ctx, level, "job finished", attrs...)
}

// partitionInterval is how often the partition job runs after the run at startup.
const partitionInterval = 24 * time.Hour

// partitionMaintainer creates the monthly partitions that are missing.
type partitionMaintainer interface {
	EnsureMonthlyPartitions(ctx context.Context) (int, error)
}

// runPartitionJob creates the monthly partitions at startup and then at every interval
// until ctx is canceled. A failure is logged and retried at the next tick.
func runPartitionJob(ctx context.Context, jobs jobRunner, maintainer partitionMaintainer, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		jobs.run(ctx, "create_monthly_partitions", false, func(ctx context.Context) ([]slog.Attr, error) {
			created, err := maintainer.EnsureMonthlyPartitions(ctx)
			return []slog.Attr{slog.Int("created", created)}, err
		})
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// publishBatch bounds one publisher transaction.
const publishBatch = 200

// outboxPublisher publishes pending outbox events.
type outboxPublisher interface {
	PublishPending(ctx context.Context, limit int) (int, error)
}

// runOutboxPublisher publishes when a transaction with events commits (wake) and, as a
// fallback for lost notifications, at every poll interval. It drains full batches at once.
func runOutboxPublisher(ctx context.Context, jobs jobRunner, publisher outboxPublisher, wake <-chan struct{}, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		for full := true; full && ctx.Err() == nil; {
			full = false
			jobs.run(ctx, "publish_outbox", true, func(ctx context.Context) ([]slog.Attr, error) {
				published, err := publisher.PublishPending(ctx, publishBatch)
				full = err == nil && published == publishBatch
				return []slog.Attr{slog.Int("published", published)}, err
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}

// newDispatcher registers the handler of each command type the worker applies.
func newDispatcher(cfg config.Config, pool *pgxpool.Pool, propagator propagation.TextMapPropagator, logger *slog.Logger) (*command.Dispatcher, runtimeParts, error) {
	tx := postgres.NewTxRunner(pool)
	dispatcher := command.NewDispatcher(tx, postgres.NewCommands(pool))
	tester := providerkey.NewTester(postgres.NewProviderKeys(pool), gatewayapi.NewClient(cfg.Gateway.Socket, propagator))
	dispatcher.Register("test_provider_key", func(ctx context.Context, cmd command.Command) error {
		return tester.Test(ctx, cmd.ProviderKeyID)
	})
	wf, err := specs.StandardWorkflow(specs.ReleaseAgents)
	if err != nil {
		return nil, runtimeParts{}, err
	}
	workflows := postgres.NewWorkflow(pool, wf.Definition)
	orchestrator := appworkflow.NewOrchestrator(workflows, workflows, postgres.NewOutbox(pool), defaultAgent, wf.Definition, wf.JSON, wf.Hash).
		WithLifecycle(workflows)
	dispatcher.Register("start", orchestrator.Start)
	// US-51, US-52: project lifecycle (qualified: "cancel" alone is the cancel of a wave).
	dispatcher.Register("project.pause", orchestrator.PauseProject)
	dispatcher.Register("project.resume", orchestrator.ResumeProject)
	dispatcher.Register("project.cancel", orchestrator.CancelProject)
	dispatcher.Register("project.record_publication", orchestrator.CompleteProject)
	parts, err := newRuntime(cfg, pool, propagator, orchestrator, workflows, wf, logger)
	if err != nil {
		return nil, runtimeParts{}, err
	}
	dispatcher.Register("answer", parts.answers.Handle)
	waves := postgres.NewWaves(pool, wf.Definition)
	dispatcher.Register("cancel", appwave.NewCancelHandler(waves, postgres.NewOutbox(pool)).Handle)
	controls := appwave.NewControlHandler(waves, postgres.NewOutbox(pool))
	dispatcher.Register("wave.pause", controls.Pause)
	dispatcher.Register("wave.pause_now", controls.PauseNow)
	dispatcher.Register("wave.resume", controls.Resume)
	dispatcher.Register("wave.restart", controls.Restart)
	return dispatcher, parts, nil
}

// defaultAgent is the platform default configuration of an agent (permissoes.yaml and instructions).
func defaultAgent(agent string) (appworkflow.AgentConfiguration, error) {
	defaults, err := specs.DefaultAgent(agent)
	if err != nil {
		return appworkflow.AgentConfiguration{}, err
	}
	return appworkflow.AgentConfiguration{
		Agent: agent, InstructionVersion: defaults.InstructionVersion, InstructionHash: defaults.InstructionHash,
		Tools: defaults.Tools, Limits: defaults.Limits,
	}, nil
}

// commandDrainer applies pending commands.
type commandDrainer interface {
	Drain(ctx context.Context) (int, error)
}

// runCommandQueue drains the command queue when the api notifies and at every poll interval.
func runCommandQueue(ctx context.Context, jobs jobRunner, dispatcher commandDrainer, wake <-chan struct{}, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		jobs.run(ctx, "apply_commands", true, func(ctx context.Context) ([]slog.Attr, error) {
			processed, err := dispatcher.Drain(ctx)
			return []slog.Attr{slog.Int("processed", processed)}, err
		})
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}

// runHeartbeat runs the heartbeat job at every interval until ctx is canceled.
func runHeartbeat(ctx context.Context, jobs jobRunner, health *apphealth.Service, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	jobs.logger.InfoContext(ctx, "worker loop started", slog.String("heartbeat_interval", interval.String()))
	for {
		jobs.run(ctx, "heartbeat", false, func(ctx context.Context) ([]slog.Attr, error) {
			_, err := health.Readiness(ctx)
			return nil, err
		})
		select {
		case <-ctx.Done():
			jobs.logger.Info("worker loop stopping")
			return
		case <-ticker.C:
		}
	}
}

// startRetention runs the deletion of projects (US-04): repository destroyed and personal data anonymized
// with the retention role, every SSO poll interval (the anonymization deadline is 24 h, RNF-20).
func startRetention(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, jobs jobRunner) {
	job := retention.NewJob(postgres.NewTxRunner(pool), postgres.NewRetention(pool), git.New(cfg.ReposDir), postgres.NewOutbox(pool))
	go func() {
		ticker := time.NewTicker(cfg.Worker.SSOPollInterval)
		defer ticker.Stop()
		for {
			jobs.run(ctx, "retention", true, func(ctx context.Context) ([]slog.Attr, error) {
				done, err := job.Run(ctx)
				return []slog.Attr{slog.Int("anonymized_projects", done)}, err
			})
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// startSSOSync runs the SSO trail and offboarding job (interno.md section 7) when the read-only
// service account of the realm is configured.
func startSSOSync(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, jobs jobRunner, logger *slog.Logger) {
	id := cfg.Identity
	if id.Issuer == "" || id.EventsClientID == "" || id.EventsClientSecret == "" {
		logger.Warn("sso sync disabled: no read-only service account of the realm", slog.String("variable", config.EnvEventsClientID))
		return
	}
	admin := oidc.NewAdmin(oidc.Settings{Issuer: id.Issuer, InternalURL: id.InternalURL, ClientID: id.EventsClientID, ClientSecret: id.EventsClientSecret})
	job := ssosync.NewJob(id.Issuer, oidc.NewSSODirectory(admin), postgres.NewSSOSync(pool), postgres.NewAuditTrail(pool))
	go func() {
		ticker := time.NewTicker(cfg.Worker.SSOPollInterval)
		defer ticker.Stop()
		for {
			jobs.run(ctx, "sso_sync", true, func(ctx context.Context) ([]slog.Attr, error) {
				result, err := job.Run(ctx)
				return []slog.Attr{slog.Int("recorded", result.Recorded), slog.Int("offboarded", result.Offboarded)}, err
			})
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
