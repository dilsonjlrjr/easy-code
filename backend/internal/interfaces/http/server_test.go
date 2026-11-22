package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/health"
	"easycode/internal/infrastructure/i18n"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/oidc/oidctest"
	httpapi "easycode/internal/interfaces/http"
)

type fakeHealth struct{ readinessErr error }

func (fakeHealth) Liveness() health.Report {
	return health.Report{Status: health.StatusOK, Version: "test"}
}

func (f fakeHealth) Readiness(context.Context) (health.Report, error) {
	healthy := f.readinessErr == nil
	report := health.NewReadinessReport("test", []health.Check{{Dependency: health.DependencyDatabase, Healthy: healthy}})
	if f.readinessErr != nil {
		return report, failure.New(failure.KindUnavailable, failure.CodeServiceUnavailable, f.readinessErr)
	}
	return report, nil
}

type harness struct {
	handler fasthttp.RequestHandler
	logs    *bytes.Buffer
	logText *strings.Builder
	idp     *oidctest.Server
	users   *fakeUsers
}

func newHarness(t *testing.T, useCases httpapi.HealthUseCases) harness {
	t.Helper()
	translator, err := i18n.NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	logger := logging.New(logs, slog.LevelDebug, "test", "dev")
	handler := httpapi.NewHandler(httpapi.Dependencies{
		Logger:     logger,
		Translator: translator,
		Health:     useCases,
		Tracer:     noop.NewTracerProvider().Tracer("test"),
		Propagator: propagation.TraceContext{},
	})
	return harness{handler: handler, logs: logs}
}

func (h harness) do(method, path string, headers map[string]string) *fasthttp.RequestCtx {
	rc := &fasthttp.RequestCtx{}
	rc.Request.Header.SetMethod(method)
	rc.Request.SetRequestURI(path)
	for key, value := range headers {
		rc.Request.Header.Set(key, value)
	}
	h.handler(rc)
	return rc
}

func decode(t *testing.T, rc *fasthttp.RequestCtx) map[string]any {
	t.Helper()
	body := map[string]any{}
	if err := json.Unmarshal(rc.Response.Body(), &body); err != nil {
		t.Fatalf("invalid json %q: %v", rc.Response.Body(), err)
	}
	return body
}

func TestHealthAnswersInPortugueseByDefault(t *testing.T) {
	h := newHarness(t, fakeHealth{})

	rc := h.do(fasthttp.MethodGet, "/api/v1/health", nil)

	body := decode(t, rc)
	if rc.Response.StatusCode() != fasthttp.StatusOK || body["status"] != "ok" || body["message"] != "Serviço no ar." {
		t.Fatalf("unexpected response %d %v", rc.Response.StatusCode(), body)
	}
	if got := string(rc.Response.Header.Peek("Content-Language")); got != i18n.PortugueseBrazil {
		t.Fatalf("unexpected Content-Language %q", got)
	}
	if len(rc.Response.Header.Peek(httpapi.HeaderRequestID)) == 0 {
		t.Fatal("request id header missing")
	}
}

func TestReadyAnswersInEnglishWhenAsked(t *testing.T) {
	h := newHarness(t, fakeHealth{})

	rc := h.do(fasthttp.MethodGet, "/api/v1/ready", map[string]string{"Accept-Language": "en-US,en;q=0.9"})

	body := decode(t, rc)
	if body["status"] != "ready" || body["message"] != "Ready to serve requests." {
		t.Fatalf("unexpected body %v", body)
	}
}

func TestReadyReturnsTranslatedProblemWhenDatabaseFails(t *testing.T) {
	h := newHarness(t, fakeHealth{readinessErr: errors.New("connection refused")})
	traceparent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	rc := h.do(fasthttp.MethodGet, "/api/v1/ready", map[string]string{"Accept-Language": "en", "traceparent": traceparent})

	body := decode(t, rc)
	if rc.Response.StatusCode() != fasthttp.StatusServiceUnavailable {
		t.Fatalf("unexpected status %d", rc.Response.StatusCode())
	}
	if string(rc.Response.Header.ContentType()) != "application/problem+json" {
		t.Fatalf("unexpected content type %q", rc.Response.Header.ContentType())
	}
	if body["type"] != "urn:easycode:problem:service-unavailable" || body["title"] != "Service unavailable" {
		t.Fatalf("unexpected problem %v", body)
	}
	if body["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id not propagated: %v", body)
	}
	if !strings.Contains(h.logs.String(), "connection refused") {
		t.Fatalf("cause chain not logged: %s", h.logs.String())
	}
}

func TestAccessLogRecordsRequestMetadata(t *testing.T) {
	h := newHarness(t, fakeHealth{})

	h.do(fasthttp.MethodGet, "/api/v1/health", map[string]string{
		httpapi.HeaderRequestID: "req-123",
		"Authorization":         "Bearer secret-token-value",
	})

	logs := h.logs.String()
	for _, want := range []string{`"msg":"http request"`, `"request_id":"req-123"`, `"route":"/api/v1/health"`, `"status":200`, `"method":"GET"`, `"duration_ms"`, `"response_bytes"`} {
		if !strings.Contains(logs, want) {
			t.Errorf("access log missing %s: %s", want, logs)
		}
	}
	if strings.Contains(logs, "secret-token-value") {
		t.Fatalf("authorization leaked: %s", logs)
	}
}

func TestUnknownRouteAndWrongMethodReturnProblems(t *testing.T) {
	h := newHarness(t, fakeHealth{})

	if rc := h.do(fasthttp.MethodGet, "/api/v1/nothing", nil); rc.Response.StatusCode() != fasthttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", rc.Response.StatusCode())
	}
	rc := h.do(fasthttp.MethodPost, "/api/v1/health", nil)
	if rc.Response.StatusCode() != fasthttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rc.Response.StatusCode())
	}
	if body := decode(t, rc); body["detail"] != "O método POST não é aceito neste recurso." {
		t.Fatalf("unexpected detail %v", body)
	}
}

type panickingHealth struct{ fakeHealth }

func (panickingHealth) Liveness() health.Report { panic("boom") }

func TestPanicBecomesInternalProblem(t *testing.T) {
	h := newHarness(t, panickingHealth{})

	rc := h.do(fasthttp.MethodGet, "/api/v1/health", nil)

	if rc.Response.StatusCode() != fasthttp.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rc.Response.StatusCode())
	}
	if body := decode(t, rc); body["type"] != "urn:easycode:problem:internal" {
		t.Fatalf("unexpected problem %v", body)
	}
}
