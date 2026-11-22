package http

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// APIPrefix is the versioned prefix of every REST route.
const APIPrefix = "/api/v1"

// Dependencies are the collaborators the HTTP layer needs, wired in cmd. Product use cases
// left nil keep their routes out (health-only tests).
type Dependencies struct {
	Logger     *slog.Logger
	Translator Translator
	Health     HealthUseCases
	Tracer     trace.Tracer
	Propagator propagation.TextMapPropagator

	Sessions    SessionUseCases
	Idempotency IdempotencyGuard
	// PublicOrigin is the web origin that writes with the cookie must send in Origin ("" skips the check).
	PublicOrigin string
	// AttachmentLimitBytes is shown in getCurrentUser.
	AttachmentLimitBytes int

	Providers ProviderUseCases
	Pricing   PricingUseCases
	Projects  ProjectUseCases
	// Members is required with Projects for the role routes (US-02).
	Members MemberUseCases
	// Configuration is required with Projects.
	Configuration ConfigurationUseCases
	Workflow      WorkflowUseCases
	Artifacts     ArtifactUseCases
	Waves         WaveUseCases
	Skills        SkillUseCases // US-43
	Agents        AgentUseCases // US-43
	Gates         GateUseCases
	Findings      FindingUseCases      // US-32, US-33
	Traceability  TraceabilityUseCases // US-28
	Pending       PendingUseCases
	// Repository serves getRepository and the git clone (US-42).
	Repository RepositoryUseCases
	// Notices serves watchNotices (/me/events) together with Events.
	Notices NoticeHistory
	// Events, History and Access serve the SSE stream; Shutdown closes when the api stops.
	Events   EventFeed
	History  EventHistory
	Access   ProjectAccess
	Shutdown <-chan struct{}
}

// ServerSettings configures the fasthttp server.
type ServerSettings struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// NewHandler builds the routed handler with the middleware chain.
func NewHandler(deps Dependencies) fasthttp.RequestHandler {
	responder := NewResponder(deps.Translator, deps.Logger)
	router := NewRouter(responder)
	registerRoutes(router, NewHealthHandler(deps.Health, responder))
	if deps.Sessions != nil {
		product := &api{deps: deps, responder: responder, auth: authenticator{sessions: deps.Sessions, publicOrigin: deps.PublicOrigin}}
		product.register(router)
	}
	return Chain(router.Serve,
		RequestID(),
		Tracing(deps.Tracer, deps.Propagator),
		AccessLog(deps.Logger),
		Recover(responder, deps.Logger),
	)
}

// RouteTemplates lists "METHOD template" of every route the dependencies enable (authorization
// tests walk all of them, RNF-04 and RNF-06).
func RouteTemplates(deps Dependencies) []string {
	responder := NewResponder(deps.Translator, deps.Logger)
	router := NewRouter(responder)
	registerRoutes(router, NewHealthHandler(deps.Health, responder))
	if deps.Sessions != nil {
		(&api{deps: deps, responder: responder, auth: authenticator{sessions: deps.Sessions, publicOrigin: deps.PublicOrigin}}).register(router)
	}
	templates := make([]string, 0, len(router.routes))
	for _, r := range router.routes {
		templates = append(templates, r.method+" "+r.template)
	}
	return templates
}

func registerRoutes(router *Router, health *HealthHandler) {
	router.Handle(fasthttp.MethodGet, APIPrefix+"/health", health.Liveness)
	router.Handle(fasthttp.MethodGet, APIPrefix+"/ready", health.Readiness)
}

// NewServer builds the fasthttp server around handler.
func NewServer(handler fasthttp.RequestHandler, settings ServerSettings, logger *slog.Logger) *fasthttp.Server {
	return &fasthttp.Server{
		Handler:     handler,
		ReadTimeout: settings.ReadTimeout,
		// No write deadline: fasthttp sets it once per response, which would cut the SSE streams
		// (sse.md); a dead client is detected by the heartbeat write instead.
		WriteTimeout:          0,
		IdleTimeout:           settings.IdleTimeout,
		NoDefaultServerHeader: true,
		Logger:                serverLogger{logger: logger},
	}
}

// serverLogger routes fasthttp's internal messages to the structured logger.
type serverLogger struct {
	logger *slog.Logger
}

func (l serverLogger) Printf(format string, args ...any) {
	l.logger.Warn("fasthttp", slog.String("detail", fmt.Sprintf(format, args...)))
}
