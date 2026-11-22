package http

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"easycode/internal/domain/failure"
	"easycode/internal/infrastructure/logging"
)

// HeaderRequestID carries the request id in both directions.
const HeaderRequestID = "X-Request-Id"

// projectIDParam is the path parameter whose value the access log records as project_id.
const projectIDParam = "projectId"

var acceptedRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Middleware wraps a fasthttp handler.
type Middleware func(fasthttp.RequestHandler) fasthttp.RequestHandler

// Chain applies middleware so the first one listed is the outermost.
func Chain(handler fasthttp.RequestHandler, middleware ...Middleware) fasthttp.RequestHandler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

// RequestID reuses a well-formed incoming X-Request-Id or creates one, echoes it in the
// response and stores it in the request context for log correlation.
func RequestID() Middleware {
	return func(next fasthttp.RequestHandler) fasthttp.RequestHandler {
		return func(rc *fasthttp.RequestCtx) {
			requestID := string(rc.Request.Header.Peek(HeaderRequestID))
			if !acceptedRequestID.MatchString(requestID) {
				requestID = newRequestID()
			}
			rc.Response.Header.Set(HeaderRequestID, requestID)
			setRequestContext(logging.WithRequestID(RequestContext(rc), requestID), rc)
			next(rc)
		}
	}
}

// Tracing opens one server span per request, continuing an incoming traceparent (ADR-15).
// Only allow-listed attributes are recorded: method, route template and status.
func Tracing(tracer trace.Tracer, propagator propagation.TextMapPropagator) Middleware {
	return func(next fasthttp.RequestHandler) fasthttp.RequestHandler {
		return func(rc *fasthttp.RequestCtx) {
			parent := propagator.Extract(RequestContext(rc), requestHeaderCarrier{header: &rc.Request.Header})
			ctx, span := tracer.Start(parent, "http.request", trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()
			setRequestContext(ctx, rc)
			next(rc)
			span.SetName(string(rc.Method()) + " " + RouteTemplate(rc))
			span.SetAttributes(
				attribute.String("http.request.method", string(rc.Method())),
				attribute.String("http.route", RouteTemplate(rc)),
				attribute.Int("http.response.status_code", rc.Response.StatusCode()),
			)
			if rc.Response.StatusCode() >= fasthttp.StatusInternalServerError {
				span.SetStatus(codes.Error, "server error")
			}
		}
	}
}

// AccessLog writes one line per request with ids and metadata only (owner decision 3).
func AccessLog(logger *slog.Logger) Middleware {
	return func(next fasthttp.RequestHandler) fasthttp.RequestHandler {
		return func(rc *fasthttp.RequestCtx) {
			started := time.Now()
			next(rc)
			logger.LogAttrs(RequestContext(rc), levelForStatus(rc.Response.StatusCode()), "http request", accessAttrs(rc, started)...)
		}
	}
}

func accessAttrs(rc *fasthttp.RequestCtx, started time.Time) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("method", string(rc.Method())),
		slog.String("route", RouteTemplate(rc)),
		slog.Int("status", rc.Response.StatusCode()),
		slog.Float64("duration_ms", float64(time.Since(started).Microseconds())/1000),
		slog.Int("response_bytes", len(rc.Response.Body())),
	}
	if id := userID(rc); id != "" {
		attrs = append(attrs, slog.String("user_id", id))
	}
	if id := PathParam(rc, projectIDParam); id != "" {
		attrs = append(attrs, slog.String("project_id", id))
	}
	return attrs
}

func levelForStatus(status int) slog.Level {
	switch {
	case status >= fasthttp.StatusInternalServerError:
		return slog.LevelError
	case status >= fasthttp.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// Recover turns a panic into a 500 problem and logs the stack.
func Recover(responder *Responder, logger *slog.Logger) Middleware {
	return func(next fasthttp.RequestHandler) fasthttp.RequestHandler {
		return func(rc *fasthttp.RequestCtx) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				logger.ErrorContext(RequestContext(rc), "handler panic", slog.String("stack", string(debug.Stack())))
				responder.Problem(rc, failure.New(failure.KindInternal, failure.CodeInternal, fmt.Errorf("panic: %v", recovered)))
			}()
			next(rc)
		}
	}
}

func newRequestID() string {
	buffer := make([]byte, 16)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}

// requestHeaderCarrier adapts fasthttp request headers to the OpenTelemetry carrier.
type requestHeaderCarrier struct {
	header *fasthttp.RequestHeader
}

func (c requestHeaderCarrier) Get(key string) string { return string(c.header.Peek(key)) }

func (c requestHeaderCarrier) Set(key, value string) { c.header.Set(key, value) }

func (c requestHeaderCarrier) Keys() []string {
	var keys []string
	for key := range c.header.All() {
		keys = append(keys, string(key))
	}
	return keys
}

var _ propagation.TextMapCarrier = requestHeaderCarrier{}
