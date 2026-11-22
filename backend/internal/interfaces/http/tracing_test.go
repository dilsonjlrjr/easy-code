package http_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"easycode/internal/infrastructure/i18n"
	httpapi "easycode/internal/interfaces/http"
)

func TestServerSpanContinuesIncomingTraceWithRouteTemplate(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	translator, err := i18n.NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(httpapi.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Translator: translator, Health: fakeHealth{},
		Tracer: provider.Tracer("test"), Propagator: propagation.TraceContext{},
	})

	rc := &fasthttp.RequestCtx{}
	rc.Request.Header.SetMethod(fasthttp.MethodGet)
	rc.Request.SetRequestURI("/api/v1/health")
	rc.Request.Header.Set("traceparent", "00-a1b2c3d4e5f60718293a4b5c6d7e8f90-99aabbccddeeff00-01")
	handler(rc)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.Name != "GET /api/v1/health" || span.SpanKind != trace.SpanKindServer {
		t.Fatalf("span %q kind %v", span.Name, span.SpanKind)
	}
	if span.SpanContext.TraceID().String() != "a1b2c3d4e5f60718293a4b5c6d7e8f90" {
		t.Fatalf("trace not continued: %s", span.SpanContext.TraceID())
	}
	want := attribute.Int("http.response.status_code", fasthttp.StatusOK)
	found := false
	for _, attr := range span.Attributes {
		found = found || attr == want
	}
	if !found {
		t.Fatalf("status attribute missing in %v", span.Attributes)
	}
}
