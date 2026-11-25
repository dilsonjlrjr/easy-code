package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/logging"
)

// maxBodyBytes bounds a request body (messages with images).
const maxBodyBytes = 32 << 20

// Sessions are the gateway use cases the handler calls.
type Sessions interface {
	Open(ctx context.Context, scope appgateway.Scope, sealedKey []byte) (string, error)
	Session(id string) (appgateway.Scope, error)
	Call(ctx context.Context, id string, req provider.Request, onChunk func(provider.Chunk)) (provider.Response, error)
	Forward(ctx context.Context, id string, req appgateway.ForwardRequest) (appgateway.ForwardResponse, error)
	Close(id string) error
	TestKey(ctx context.Context, providerName string, sealedKey []byte) error
	Providers() []string
}

// Server serves the gateway protocol. Prompt and answer content pass through, never into logs or spans.
type Server struct {
	sessions   Sessions
	logger     *slog.Logger
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

// NewHandler builds the HTTP handler of easycode-gateway.
func NewHandler(sessions Sessions, logger *slog.Logger, tracer trace.Tracer, propagator propagation.TextMapPropagator) http.Handler {
	server := &Server{sessions: sessions, logger: logger, tracer: tracer, propagator: propagator}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+routeSessions, server.traced(routeSessions, server.open))
	mux.HandleFunc("POST "+routeSessions+"/{id}/calls", server.traced(routeSessions+"/{id}/calls", server.call))
	mux.HandleFunc("POST "+routeSessions+"/{id}/forward/{path...}", server.traced(routeSessions+"/{id}/forward/{path...}", server.forward))
	mux.HandleFunc("DELETE "+routeSessions+"/{id}", server.traced(routeSessions+"/{id}", server.close))
	mux.HandleFunc("POST "+routeKeyTests, server.traced(routeKeyTests, server.testKey))
	mux.HandleFunc("GET "+routeHealth, server.health)
	return mux
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// traced opens one server span per request (ADR-15) and writes one log line with metadata only.
func (s *Server) traced(route string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		ctx := s.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := s.tracer.Start(ctx, r.Method+" "+route, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(recorder, r.WithContext(ctx))
		span.SetAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", recorder.status),
		)
		if recorder.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "server error")
		}
		s.logger.LogAttrs(ctx, slog.LevelInfo, "gateway request",
			slog.String("method", r.Method), slog.String("route", route), slog.Int("status", recorder.status),
			slog.Float64("duration_ms", float64(time.Since(started).Microseconds())/1000))
	}
}

func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	var body OpenSessionRequest
	if !s.decode(w, r, &body) {
		return
	}
	id, err := s.sessions.Open(r.Context(), body.Scope, body.SealedKey)
	if err != nil {
		s.problem(w, r, err)
		return
	}
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.String("easycode.project_id", body.ProjectID), attribute.String("easycode.wave_id", body.WaveID),
		attribute.String("gen_ai.system", body.Provider), attribute.String("gen_ai.request.model", body.Model))
	writeJSON(w, http.StatusCreated, OpenSessionResponse{ID: id})
}

func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := s.sessions.Session(id)
	if err != nil {
		s.problem(w, r, err)
		return
	}
	var req provider.Request
	if !s.decode(w, r, &req) {
		return
	}
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(
		attribute.String("easycode.project_id", scope.ProjectID), attribute.String("easycode.wave_id", scope.WaveID),
		attribute.String("gen_ai.system", scope.Provider), attribute.String("gen_ai.request.model", scope.Model))

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	send := func(line CallLine) {
		_ = encoder.Encode(line)
		if flusher != nil {
			flusher.Flush()
		}
	}
	response, err := s.sessions.Call(r.Context(), id, req, func(chunk provider.Chunk) { send(CallLine{Chunk: &chunk}) })
	var providerErr *provider.Error
	switch {
	case err == nil:
		span.SetAttributes(usageAttributes(response.Usage)...)
		span.SetAttributes(attribute.String("gen_ai.response.finish_reason", response.Stop))
		send(CallLine{Response: &response})
	case errors.As(err, &providerErr):
		span.SetAttributes(usageAttributes(providerErr.PartialUsage)...)
		span.SetAttributes(attribute.String("error.type", string(providerErr.Class)))
		span.SetStatus(codes.Error, string(providerErr.Class))
		send(CallLine{Error: providerErr})
	default:
		problem := problemFor(err)
		span.SetStatus(codes.Error, problem.Code)
		s.logger.ErrorContext(r.Context(), "gateway call failed", logging.ErrorAttr(err))
		send(CallLine{Failure: &problem})
	}
}

func usageAttributes(usage provider.Usage) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int("gen_ai.usage.input_tokens", usage.Input),
		attribute.Int("gen_ai.usage.output_tokens", usage.Output),
		attribute.Int("easycode.usage.cache_read_tokens", usage.CacheRead),
		attribute.Int("easycode.usage.cache_write_tokens", usage.CacheWrite),
	}
}

func (s *Server) close(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Close(r.PathValue("id")); err != nil {
		s.problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testKey(w http.ResponseWriter, r *http.Request) {
	var body KeyTestRequest
	if !s.decode(w, r, &body) {
		return
	}
	err := s.sessions.TestKey(r.Context(), body.Provider, body.SealedKey)
	var providerErr *provider.Error
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, KeyTestResponse{Result: KeyValid})
	case errors.As(err, &providerErr) && providerErr.Class == provider.ErrorAuthentication:
		writeJSON(w, http.StatusOK, KeyTestResponse{Result: KeyInvalid, Message: providerErr.Message})
	case errors.As(err, &providerErr):
		writeJSON(w, http.StatusBadGateway, Problem{Code: CodeProviderError, Status: http.StatusBadGateway, Class: providerErr.Class})
	default:
		s.problem(w, r, err)
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok", Providers: s.sessions.Providers()})
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, Problem{Code: "gateway-invalid-body", Status: http.StatusBadRequest})
		return false
	}
	return true
}

func (s *Server) problem(w http.ResponseWriter, r *http.Request, err error) {
	problem := problemFor(err)
	if problem.Status >= http.StatusInternalServerError {
		s.logger.ErrorContext(r.Context(), "gateway request failed", logging.ErrorAttr(err))
	}
	writeJSON(w, problem.Status, problem)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
