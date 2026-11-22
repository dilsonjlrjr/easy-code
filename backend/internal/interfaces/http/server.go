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

func registerRoutes(router *Router, health *HealthHandler) {
	router.Handle(fasthttp.MethodGet, APIPrefix+"/health", health.Liveness)
	router.Handle(fasthttp.MethodGet, APIPrefix+"/ready", health.Readiness)
}

// NewServer builds the fasthttp server around handler.
func NewServer(handler fasthttp.RequestHandler, settings ServerSettings, logger *slog.Logger) *fasthttp.Server {
	return &fasthttp.Server{
		Handler:               handler,
		ReadTimeout:           settings.ReadTimeout,
		WriteTimeout:          settings.WriteTimeout,
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
