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

	"github.com/jackc/pgx/v5/pgxpool"

	appartifact "easycode/internal/application/artifact"
	appattachment "easycode/internal/application/attachment"
	appcontent "easycode/internal/application/content"
	appcost "easycode/internal/application/cost"
	appevent "easycode/internal/application/event"
	appfinding "easycode/internal/application/finding"
	appgate "easycode/internal/application/gate"
	apphealth "easycode/internal/application/health"
	"easycode/internal/application/idempotency"
	applocalclient "easycode/internal/application/localclient"
	apppending "easycode/internal/application/pending"
	apppricing "easycode/internal/application/pricing"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/providerkey"
	apprepository "easycode/internal/application/repository"
	appsearch "easycode/internal/application/search"
	appsession "easycode/internal/application/session"
	appskill "easycode/internal/application/skill"
	apptraceability "easycode/internal/application/traceability"
	apptrail "easycode/internal/application/trail"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/files"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/i18n"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/oidc"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/process"
	"easycode/internal/infrastructure/sealing"
	"easycode/internal/infrastructure/specs"
	"easycode/internal/infrastructure/telemetry"
	"easycode/internal/infrastructure/websession"
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
	if _, err = postgres.NewMigrator(pool, proc.Logger).Apply(ctx, files); err != nil {
		return err
	}
	identity := proc.Config.Identity
	if identity.Issuer == "" || identity.OrganizationName == "" {
		return nil
	}
	// The organization of the realm of this deployment (ADR-12): people of that issuer sign in to it.
	if err := postgres.EnsureOrganization(ctx, pool, identity.Issuer, identity.OrganizationName); err != nil {
		return err
	}
	proc.Logger.Info("organization of the sso realm ensured")
	return nil
}

func serve(ctx context.Context, proc process.Process) error {
	cfg := proc.Config
	if err := cfg.RequireIdentity(); err != nil {
		proc.Logger.Error("invalid configuration", logging.ErrorAttr(err))
		return err
	}
	tracing, err := telemetry.Setup(ctx, telemetry.Settings{
		Enabled: cfg.Telemetry.Enabled, Endpoint: cfg.Telemetry.Endpoint, Insecure: cfg.Telemetry.Insecure,
		SampleRatio: cfg.Telemetry.SampleRatio, Service: serviceName, Version: version,
	}, proc.Logger)
	if err != nil {
		return err
	}
	defer shutdownTelemetry(tracing, proc.Logger)

	pool, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: cfg.Database.URL, MaxConns: cfg.Database.MaxConns, GenericPlans: true}, proc.Logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	translator, err := i18n.NewTranslator()
	if err != nil {
		return err
	}
	deps, err := productDependencies(cfg, pool, proc.Logger)
	if err != nil {
		return err
	}
	broker := appevent.NewBroker()
	deps.Events = broker
	go func() {
		_ = postgres.NewEventListener(pool, proc.Logger, nil).Listen(ctx, broker.Dispatch)
	}()
	shutdown := make(chan struct{})
	deps.Shutdown = shutdown
	go func() { <-ctx.Done(); close(shutdown) }()
	deps.Logger = proc.Logger
	deps.Translator = translator
	deps.Health = apphealth.NewService(version, cfg.Database.ProbeTimeout, postgres.NewDatabaseProbe(pool))
	deps.Tracer = tracing.Provider.Tracer(serviceName)
	deps.Propagator = tracing.Propagator
	handler := httpapi.NewHandler(deps)
	server := httpapi.NewServer(handler, httpapi.ServerSettings{
		ReadTimeout: cfg.HTTP.ReadTimeout, WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout,
		MaxBodyBytes: max(4<<20, cfg.Providers.AttachmentLimitBytes+1<<20), // attachFile answers 413 itself (US-03 CA-4)
	}, proc.Logger)
	if pages, ok := deps.ContentURLs.(httpapi.ContentPages); ok {
		defer serveContentOrigin(cfg.Content, cfg.HTTP, pages, proc.Logger)()
	}
	return listenUntilDone(ctx, proc, server.ListenAndServe, server.ShutdownWithContext)
}

// serveContentOrigin starts the content origin on its own listener (ADR-19: no /api, no cookie, GET only)
// and returns its shutdown.
func serveContentOrigin(content config.Content, settings config.HTTP, pages httpapi.ContentPages, logger *slog.Logger) func() {
	server := httpapi.NewServer(httpapi.NewContentOriginHandler(pages, logger), httpapi.ServerSettings{
		ReadTimeout: settings.ReadTimeout, WriteTimeout: settings.WriteTimeout, IdleTimeout: settings.IdleTimeout,
	}, logger)
	go func() {
		logger.Info("content origin listening", slog.String("addr", content.Addr), slog.String("origin", content.Origin))
		if err := server.ListenAndServe(content.Addr); err != nil {
			logger.Error("content origin stopped", logging.ErrorAttr(err))
		}
	}()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
		defer cancel()
		if err := server.ShutdownWithContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("content origin shutdown failed", logging.ErrorAttr(err))
		}
	}
}

// productDependencies wires the use cases of the product routes over PostgreSQL.
func productDependencies(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) (httpapi.Dependencies, error) {
	tx := postgres.NewTxRunner(pool)
	id := cfg.Identity
	realm := oidc.Settings{
		Issuer: id.Issuer, InternalURL: id.InternalURL, ClientID: id.ClientID, ClientSecret: id.ClientSecret,
		RedirectURL: id.PublicOrigin + httpapi.APIPrefix + "/session/oidc/callback", AdministratorRole: id.AdministratorRole,
	}
	sessions := appsession.NewService(postgres.NewUsers(pool), postgres.NewWebSessions(pool), oidc.NewProvider(realm),
		websession.NewTokenSealer(id.SessionSecret), websession.NewFlowCodec(id.SessionSecret))
	trail := postgres.NewAuditTrail(pool)
	wf, err := specs.StandardWorkflow(specs.ReleaseAgents)
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	definition := wf.Definition
	// PC-20, PL-46: the exit criteria of a stage list the checks of its gate before any round.
	composition, err := specs.Governance()
	if err != nil {
		return httpapi.Dependencies{}, err
	}
	projects := postgres.NewProjects(pool, definition.ID, len(definition.Stages))
	workflows := postgres.NewWorkflow(pool, definition)
	waveStore := postgres.NewWaves(pool, definition)
	waves := appwave.NewService(tx, projects, waveStore, waveStore, trail).WithControls(waveStore)
	// US-08: the Release summary and its blocks read the gate engine ports and the repository (read only).
	release := appgate.NewReleaseQueries(projects, postgres.NewGates(pool), postgres.NewGates(pool), postgres.NewGates(pool), git.New(cfg.ReposDir), definition)
	members := appproject.NewMemberService(tx, projects, postgres.NewMembers(pool), oidc.NewPeopleDirectory(oidc.NewAdmin(realm)),
		postgres.NewOutbox(pool), trail)
	provider := oidc.NewProvider(realm)
	artifacts := appartifact.NewQueries(projects, postgres.NewArtifacts(pool)).WithFiles(git.New(cfg.ReposDir))
	deps := httpapi.Dependencies{
		Sessions: sessions,
		// US-45: the read-only token of the desktop app (ADR-20), checked by the realm keys and the SSO session.
		LocalClient: applocalclient.NewService(postgres.NewUsers(pool), postgres.NewLocalClientSessions(pool),
			oidc.NewLocalClientVerifier(provider), oidc.NewAppSessions(oidc.NewAdmin(realm)), trail, tx, id.Issuer, id.ClientID),
		Idempotency:          idempotency.NewGuard(tx, postgres.NewIdempotencyKeys(pool)),
		PublicOrigin:         cfg.Identity.PublicOrigin,
		AttachmentLimitBytes: cfg.Providers.AttachmentLimitBytes,
		Pricing:              apppricing.NewService(tx, postgres.NewCatalog(pool), trail),
		Projects:             appproject.NewService(tx, projects, postgres.NewOutbox(pool), trail, definition.ID, definition.Version),
		Members:              members,
		Configuration:        appproject.NewConfigurationService(tx, projects, projects, postgres.NewCatalog(pool), trail, definition),
		Workflow:             appworkflow.NewService(tx, projects, workflows, workflows, workflows, trail).WithLifecycle(workflows, members),
		Artifacts:            artifacts,
		Attachments: appattachment.NewService(tx, projects, postgres.NewAttachments(pool), files.New(cfg.FilesDir), trail,
			cfg.Providers.AttachmentLimitBytes),
		ArtifactEdits: appartifact.NewEditor(tx, projects, postgres.NewArtifactEdits(pool), trail),
		Gates: appgate.NewDecisions(tx, projects, postgres.NewGates(pool), postgres.NewOutbox(pool), trail).WithRelease(release).
			WithAnnotations(postgres.NewGateAnnotations(pool)), // US-57
		Release:      release,
		GateChecks:   appgate.NewExitCriteria(projects, composition, postgres.NewGates(pool)),
		Findings:     appfinding.NewDecisions(tx, projects, postgres.NewFindings(pool), postgres.NewOutbox(pool), trail),
		Traceability: apptraceability.NewService(projects, postgres.NewTraceability(pool)),
		History:      postgres.NewOutbox(pool),
		Pending:      apppending.NewService(postgres.NewPending(pool, definition)),
		Search:       appsearch.NewService(postgres.NewSearch(pool)),
		Waves:        waves,
		Access:       waves,
		Skills:       appskill.NewService(postgres.NewSkills(pool)),
		SkillImports: appskill.NewImporter(tx, postgres.NewSkillImports(pool), postgres.NewSkills(pool), trail, specs.YAMLToJSON),
		Agents:       appskill.NewAgents(tx, projects, specs.AgentProfiles{}, postgres.NewSkills(pool), postgres.NewSkills(pool), trail),
		Repository:   apprepository.NewService(projects, git.New(cfg.ReposDir), cfg.GitBaseURL, git.MainBranch),
		Costs:        appcost.NewService(projects, postgres.NewCosts(pool, definition), definition.StageIDs()),
		Audit:        apptrail.NewQueries(projects, postgres.NewAuditReader(pool)),
	}
	if cfg.Content.Origin == "" {
		logger.Warn("prototype viewer disabled: no content origin", slog.String("variable", config.EnvContentOrigin))
	} else {
		// ADR-19, ESP-11: signed URLs of the prototype, served by the content origin listener (serve).
		deps.ContentURLs = appcontent.NewService(artifacts, git.New(cfg.ReposDir), cfg.Content.Origin, appcontent.DeriveKey(id.SessionSecret))
	}
	if cfg.Providers.SealingPublicKeyFile == "" {
		logger.Warn("provider key routes disabled: no sealing public key", slog.String("variable", config.EnvSealingPublicKey))
	} else {
		sealer, err := sealing.LoadSealer(cfg.Providers.SealingPublicKeyFile)
		if err != nil {
			return deps, err
		}
		deps.Providers = providerkey.NewService(tx, postgres.NewProviderKeys(pool), sealer, trail, cfg.Providers.KeyTestTimeout)
	}
	return deps, nil
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
