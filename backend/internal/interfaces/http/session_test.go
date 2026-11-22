package http_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"

	"easycode/internal/application/session"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/infrastructure/i18n"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/websession"
	httpapi "easycode/internal/interfaces/http"
)

const devOrg = "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b30"

type fakeUsers struct{}

func (fakeUsers) Principal(_ context.Context, org, sub string) (identity.Principal, error) {
	if org != devOrg || sub != "dev" {
		return identity.Principal{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return identity.Principal{Subject: "dev", Name: "Dev", OrganizationID: devOrg, OrganizationName: "Dev org", Administrator: true}, nil
}

func (u fakeUsers) DevPrincipal(ctx context.Context, sub string) (identity.Principal, error) {
	return u.Principal(ctx, devOrg, sub)
}

func (fakeUsers) LeadInAnyProject(context.Context, identity.Principal) (bool, error) {
	return false, nil
}

// productHarness builds the handler with the real session service over fakes.
func productHarness(t *testing.T, devSubject string, options ...func(*httpapi.Dependencies)) harness {
	t.Helper()
	translator, err := i18n.NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	logs := &strings.Builder{}
	deps := httpapi.Dependencies{
		Logger:               logging.New(logs, slog.LevelDebug, "test", "dev"),
		Translator:           translator,
		Health:               fakeHealth{},
		Tracer:               noop.NewTracerProvider().Tracer("test"),
		Propagator:           propagation.TraceContext{},
		Sessions:             session.NewService(fakeUsers{}, websession.NewCodec(strings.Repeat("s", 32)), devSubject),
		PublicOrigin:         "https://easycode.test",
		AttachmentLimitBytes: 1024,
	}
	for _, option := range options {
		option(&deps)
	}
	return harness{handler: httpapi.NewHandler(deps), logText: logs}
}

func sessionCookie(t *testing.T, h harness) string {
	t.Helper()
	rc := h.do(fasthttp.MethodPost, "/api/v1/dev/session", nil)
	if rc.Response.StatusCode() != fasthttp.StatusNoContent {
		t.Fatalf("dev session: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	cookie := fasthttp.AcquireCookie()
	defer fasthttp.ReleaseCookie(cookie)
	cookie.SetKey(httpapi.SessionCookie)
	if !rc.Response.Header.Cookie(cookie) || !cookie.HTTPOnly() || !cookie.Secure() || cookie.SameSite() != fasthttp.CookieSameSiteStrictMode {
		t.Fatalf("session cookie must be HttpOnly, Secure and SameSite=Strict: %s", cookie.String())
	}
	return httpapi.SessionCookie + "=" + string(cookie.Value())
}

func TestDevSessionGivesTheFixedUserWithAdministratorRole(t *testing.T) {
	h := productHarness(t, "dev")
	rc := h.do(fasthttp.MethodGet, "/api/v1/me", map[string]string{"Cookie": sessionCookie(t, h)})
	if rc.Response.StatusCode() != fasthttp.StatusOK {
		t.Fatalf("me: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	body := decode(t, rc)
	roles, _ := body["organization_roles"].([]any)
	if body["id"] != "dev" || len(roles) != 1 || roles[0] != "administrator" || body["csrf_token"] == "" || body["access_medium"] != "web" {
		t.Fatalf("unexpected current user: %v", body)
	}
}

func TestDevSessionRouteDoesNotExistOutsideMarco1(t *testing.T) {
	h := productHarness(t, "")
	if rc := h.do(fasthttp.MethodPost, "/api/v1/dev/session", nil); rc.Response.StatusCode() != fasthttp.StatusNotFound {
		t.Fatalf("dev route outside marco1 must not exist, got %d", rc.Response.StatusCode())
	}
}

func TestProtectedRouteWithoutValidSessionIsUnauthenticated(t *testing.T) {
	h := productHarness(t, "dev")
	for name, headers := range map[string]map[string]string{
		"no cookie":     nil,
		"forged cookie": {"Cookie": httpapi.SessionCookie + "=eyJzdWIiOiJkZXYifQ.AAAA"},
	} {
		rc := h.do(fasthttp.MethodGet, "/api/v1/me", headers)
		if rc.Response.StatusCode() != fasthttp.StatusUnauthorized || decode(t, rc)["type"] != "urn:easycode:problem:not-authenticated" {
			t.Errorf("%s: %d %s", name, rc.Response.StatusCode(), rc.Response.Body())
		}
	}
}

func TestWriteWithCookieFromAnotherOriginIsForbidden(t *testing.T) {
	h := productHarness(t, "dev")
	cookie := sessionCookie(t, h)
	rc := h.do(fasthttp.MethodPost, "/api/v1/session/logout", map[string]string{"Cookie": cookie, "Origin": "https://evil.test"})
	if rc.Response.StatusCode() != fasthttp.StatusForbidden {
		t.Fatalf("cross-origin write must be refused, got %d", rc.Response.StatusCode())
	}
	rc = h.do(fasthttp.MethodPost, "/api/v1/session/logout", map[string]string{"Cookie": cookie, "Origin": "https://easycode.test"})
	if rc.Response.StatusCode() != fasthttp.StatusNoContent {
		t.Fatalf("logout: %d", rc.Response.StatusCode())
	}
}
