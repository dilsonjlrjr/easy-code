package http_test

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"

	"easycode/internal/application/session"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/infrastructure/i18n"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/oidc"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/websession"
	httpapi "easycode/internal/interfaces/http"
)

const devOrg = "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b30"

const testSessionSecret = "test-only-session-secret-0123456789"

// devUser is the administrator the tests sign in by default (a test identity, not a profile).
var devUser = oidctest.User{Subject: "dev", Name: "Dev", Administrator: true}

// fakeUsers is the directory of the unit harness: every person of the fake realm signs in.
type fakeUsers struct {
	mu     sync.Mutex
	known  map[string]identity.Principal
	denied map[string]bool
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{known: map[string]identity.Principal{}, denied: map[string]bool{}}
}

func (u *fakeUsers) Principal(_ context.Context, org, sub string) (identity.Principal, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	p, ok := u.known[sub]
	if !ok || org != devOrg || u.denied[sub] {
		return identity.Principal{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return p, nil
}

func (u *fakeUsers) SignIn(_ context.Context, _ string, c session.Claims) (identity.Principal, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.denied[c.Subject] {
		return identity.Principal{}, failure.New(failure.KindForbidden, session.CodeAccountWithoutAccess, nil)
	}
	p := identity.Principal{Subject: c.Subject, Name: c.Name, OrganizationID: devOrg, OrganizationName: "Dev org", Administrator: c.Administrator}
	u.known[c.Subject] = p
	return p, nil
}

func (*fakeUsers) LeadInAnyProject(context.Context, identity.Principal) (bool, error) {
	return false, nil
}

// memoryStore keeps web sessions in memory for the unit harness.
type memoryStore struct {
	mu      sync.Mutex
	next    int64
	records map[string]*session.Record
}

func newMemoryStore() *memoryStore { return &memoryStore{records: map[string]*session.Record{}} }

func (m *memoryStore) Create(_ context.Context, hash []byte, r session.Record) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	r.Session.ID = m.next
	m.records[string(hash)] = &r
	return m.next, nil
}

func (m *memoryStore) Find(_ context.Context, hash []byte) (session.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[string(hash)]
	if !ok {
		return session.Record{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return *r, nil
}

func (m *memoryStore) byID(id int64) *session.Record {
	for _, r := range m.records {
		if r.Session.ID == id {
			return r
		}
	}
	return &session.Record{}
}

func (m *memoryStore) Touch(_ context.Context, id int64, seen time.Time, checked *time.Time, sealed []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.byID(id)
	r.Session.LastSeenAt = seen
	if checked != nil {
		r.IDPCheckedAt, r.SealedRefreshToken = *checked, sealed
	}
	return nil
}

func (m *memoryStore) End(_ context.Context, id int64, _ string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.byID(id)
	if r.Ended {
		return nil, nil
	}
	r.Ended = true
	return r.SealedRefreshToken, nil
}

func (m *memoryStore) Active(_ context.Context, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.byID(id).Ended, nil
}

// productHarness builds the handler with the real session service over fakes and the fake realm.
func productHarness(t *testing.T, options ...func(*httpapi.Dependencies)) harness {
	t.Helper()
	translator, err := i18n.NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	idp := oidctest.New(t)
	users := newFakeUsers()
	logs := &strings.Builder{}
	deps := httpapi.Dependencies{
		Logger:               logging.New(logs, slog.LevelDebug, "test", "dev"),
		Translator:           translator,
		Health:               fakeHealth{},
		Tracer:               noop.NewTracerProvider().Tracer("test"),
		Propagator:           propagation.TraceContext{},
		Sessions:             testSessions(idp, users, newMemoryStore()),
		PublicOrigin:         "https://easycode.test",
		AttachmentLimitBytes: 1024,
	}
	for _, option := range options {
		option(&deps)
	}
	return harness{handler: httpapi.NewHandler(deps), logText: logs, idp: idp, users: users}
}

func testSessions(idp *oidctest.Server, users session.Users, store session.Store) *session.Service {
	return session.NewService(users, store, oidc.NewProvider(idp.Settings("https://easycode.test/api/v1/session/oidc/callback")),
		websession.NewTokenSealer(testSessionSecret), websession.NewFlowCodec(testSessionSecret))
}

// login signs a person in through startWebLogin, the fake SSO and completeWebLogin, and returns
// the Cookie header of the new session.
func login(t *testing.T, h harness, u oidctest.User, authTime time.Time) string {
	t.Helper()
	start := h.do(fasthttp.MethodGet, "/api/v1/session/login?callback="+url.QueryEscape("/projects"), nil)
	if start.Response.StatusCode() != fasthttp.StatusFound {
		t.Fatalf("start login: %d %s", start.Response.StatusCode(), start.Response.Body())
	}
	flow := responseCookie(t, start, httpapi.LoginFlowCookie)
	if flow.SameSite() != fasthttp.CookieSameSiteLaxMode || !flow.HTTPOnly() || !flow.Secure() {
		t.Fatalf("ec_login must be HttpOnly, Secure and SameSite=Lax: %s", flow.String())
	}
	query := h.idp.Authorize(t, string(start.Response.Header.Peek("Location")), u, authTime)
	done := h.do(fasthttp.MethodGet, "/api/v1/session/oidc/callback?"+query,
		map[string]string{"Cookie": httpapi.LoginFlowCookie + "=" + string(flow.Value())})
	if done.Response.StatusCode() != fasthttp.StatusFound || string(done.Response.Header.Peek("Location")) != "/projects" {
		t.Fatalf("callback: %d %s %s", done.Response.StatusCode(), done.Response.Header.Peek("Location"), done.Response.Body())
	}
	cookie := responseCookie(t, done, httpapi.SessionCookie)
	if !cookie.HTTPOnly() || !cookie.Secure() || cookie.SameSite() != fasthttp.CookieSameSiteStrictMode || string(cookie.Path()) != "/" {
		t.Fatalf("session cookie must be HttpOnly, Secure and SameSite=Strict: %s", cookie.String())
	}
	return httpapi.SessionCookie + "=" + string(cookie.Value())
}

func responseCookie(t *testing.T, rc *fasthttp.RequestCtx, name string) *fasthttp.Cookie {
	t.Helper()
	cookie := &fasthttp.Cookie{}
	cookie.SetKey(name)
	if !rc.Response.Header.Cookie(cookie) {
		t.Fatalf("response without cookie %s", name)
	}
	return cookie
}

// sessionCookie signs the default administrator in.
func sessionCookie(t *testing.T, h harness) string {
	t.Helper()
	return login(t, h, devUser, time.Now())
}

// US-01 CA-1: the login goes to the SSO with PKCE and comes back to the page asked; the session
// shows the person, the administrator role from the SSO and the anti-CSRF token.
func TestLoginThroughTheSSOCreatesTheSession(t *testing.T) {
	h := productHarness(t)
	rc := h.do(fasthttp.MethodGet, "/api/v1/me", map[string]string{"Cookie": sessionCookie(t, h)})
	if rc.Response.StatusCode() != fasthttp.StatusOK {
		t.Fatalf("me: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	body := decode(t, rc)
	roles, _ := body["organization_roles"].([]any)
	if body["id"] != "dev" || len(roles) != 1 || roles[0] != "administrator" || body["csrf_token"] == "" || body["access_medium"] != "web" ||
		body["authenticated_at"] == nil {
		t.Fatalf("unexpected current user: %v", body)
	}
}

// ESP-02 ER-02-3: the callback accepts only a relative path of the same origin.
func TestLoginCallbackOfAnotherSiteGoesHome(t *testing.T) {
	h := productHarness(t)
	for _, callback := range []string{"https://evil.test/x", "//evil.test", "/\\evil.test"} {
		start := h.do(fasthttp.MethodGet, "/api/v1/session/login?callback="+url.QueryEscape(callback), nil)
		flow := responseCookie(t, start, httpapi.LoginFlowCookie)
		query := h.idp.Authorize(t, string(start.Response.Header.Peek("Location")), devUser, time.Now())
		done := h.do(fasthttp.MethodGet, "/api/v1/session/oidc/callback?"+query, map[string]string{"Cookie": httpapi.LoginFlowCookie + "=" + string(flow.Value())})
		if location := string(done.Response.Header.Peek("Location")); location != "/" {
			t.Errorf("%s: went to %q", callback, location)
		}
	}
}

// completeWebLogin: a state that does not match the flow cookie creates no session and goes to
// the login page with the reason.
func TestLoginWithForgedStateIsRefused(t *testing.T) {
	h := productHarness(t)
	start := h.do(fasthttp.MethodGet, "/api/v1/session/login", nil)
	flow := responseCookie(t, start, httpapi.LoginFlowCookie)
	query, _ := url.ParseQuery(h.idp.Authorize(t, string(start.Response.Header.Peek("Location")), devUser, time.Now()))
	query.Set("state", "forged")
	for name, cookie := range map[string]string{"forged state": httpapi.LoginFlowCookie + "=" + string(flow.Value()), "no flow cookie": ""} {
		done := h.do(fasthttp.MethodGet, "/api/v1/session/oidc/callback?"+query.Encode(), map[string]string{"Cookie": cookie})
		if done.Response.StatusCode() != fasthttp.StatusFound || string(done.Response.Header.Peek("Location")) != "/login?reason=login_failed" ||
			len(done.Response.Header.PeekCookie(httpapi.SessionCookie)) > 0 {
			t.Errorf("%s: %d %s", name, done.Response.StatusCode(), done.Response.Header.PeekCookie(httpapi.SessionCookie))
		}
	}
}

// US-01 CA-2: a deactivated account and an SSO refusal end on /access-denied without a session.
func TestLoginOfAccountWithoutAccessIsDenied(t *testing.T) {
	h := productHarness(t)
	h.users.denied["gone"] = true
	start := h.do(fasthttp.MethodGet, "/api/v1/session/login", nil)
	flow := responseCookie(t, start, httpapi.LoginFlowCookie)
	query := h.idp.Authorize(t, string(start.Response.Header.Peek("Location")), oidctest.User{Subject: "gone", Name: "Gone"}, time.Now())
	done := h.do(fasthttp.MethodGet, "/api/v1/session/oidc/callback?"+query, map[string]string{"Cookie": httpapi.LoginFlowCookie + "=" + string(flow.Value())})
	if done.Response.StatusCode() != fasthttp.StatusFound || string(done.Response.Header.Peek("Location")) != "/access-denied" ||
		len(done.Response.Header.PeekCookie(httpapi.SessionCookie)) > 0 {
		t.Fatalf("CA-2 deactivated: %d %s", done.Response.StatusCode(), done.Response.Header.Peek("Location"))
	}
	start = h.do(fasthttp.MethodGet, "/api/v1/session/login", nil)
	flow = responseCookie(t, start, httpapi.LoginFlowCookie)
	parsed, _ := url.Parse(string(start.Response.Header.Peek("Location")))
	denied := url.Values{"error": {"access_denied"}, "state": {parsed.Query().Get("state")}}
	done = h.do(fasthttp.MethodGet, "/api/v1/session/oidc/callback?"+denied.Encode(), map[string]string{"Cookie": httpapi.LoginFlowCookie + "=" + string(flow.Value())})
	if string(done.Response.Header.Peek("Location")) != "/access-denied" || len(done.Response.Header.PeekCookie(httpapi.SessionCookie)) > 0 {
		t.Fatalf("CA-2 refused by the SSO: %d %s", done.Response.StatusCode(), done.Response.Header.Peek("Location"))
	}
}

// ADR-19 step-up: reauthenticate=true asks the SSO for a fresh login (prompt=login, max_age=0).
func TestReauthenticationAsksTheSSOForANewLogin(t *testing.T) {
	h := productHarness(t)
	start := h.do(fasthttp.MethodGet, "/api/v1/session/login?reauthenticate=true", nil)
	parsed, _ := url.Parse(string(start.Response.Header.Peek("Location")))
	if parsed.Query().Get("prompt") != "login" || parsed.Query().Get("max_age") != "0" {
		t.Fatalf("step-up must force the login: %s", parsed.RawQuery)
	}
	if rc := h.do(fasthttp.MethodGet, "/api/v1/session/login?reauthenticate=maybe", nil); rc.Response.StatusCode() != fasthttp.StatusBadRequest {
		t.Fatalf("invalid reauthenticate: %d", rc.Response.StatusCode())
	}
}

// PLANO MARCO-2: the fixed development identity of marco1 no longer exists.
func TestDevSessionRouteNoLongerExists(t *testing.T) {
	h := productHarness(t)
	if rc := h.do(fasthttp.MethodPost, "/api/v1/dev/session", nil); rc.Response.StatusCode() != fasthttp.StatusNotFound {
		t.Fatalf("dev route must not exist, got %d", rc.Response.StatusCode())
	}
}

// RNF-06: no route but health and the login start answers without a valid session.
func TestProtectedRouteWithoutValidSessionIsUnauthenticated(t *testing.T) {
	h := productHarness(t)
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

// US-01 CA-3: after the logout the same cookie no longer works (server-side session) and the SSO
// session is ended by the back channel.
func TestLogoutEndsTheSessionOnTheServerAndTheSSO(t *testing.T) {
	h := productHarness(t)
	cookie := sessionCookie(t, h)
	rc := h.do(fasthttp.MethodPost, "/api/v1/session/logout", map[string]string{"Cookie": cookie, "Origin": "https://evil.test"})
	if rc.Response.StatusCode() != fasthttp.StatusForbidden {
		t.Fatalf("cross-origin write must be refused, got %d", rc.Response.StatusCode())
	}
	rc = h.do(fasthttp.MethodPost, "/api/v1/session/logout", map[string]string{"Cookie": cookie, "Origin": "https://easycode.test"})
	if rc.Response.StatusCode() != fasthttp.StatusNoContent || h.idp.Logouts != 1 {
		t.Fatalf("logout: %d, sso logouts %d", rc.Response.StatusCode(), h.idp.Logouts)
	}
	if again := h.do(fasthttp.MethodGet, "/api/v1/me", map[string]string{"Cookie": cookie}); again.Response.StatusCode() != fasthttp.StatusUnauthorized {
		t.Fatalf("CA-3: the old cookie must be refused, got %d", again.Response.StatusCode())
	}
}

// RNF-06: a session revoked in the SSO (account disabled, logout elsewhere) is refused within 60 s.
func TestSessionRevokedInTheSSOIsRefusedWithinOneMinute(t *testing.T) {
	idp := oidctest.New(t)
	users := newFakeUsers()
	now := time.Now()
	sessions := testSessions(idp, users, newMemoryStore()).WithClock(func() time.Time { return now })
	translator, _ := i18n.NewTranslator()
	h := harness{idp: idp, users: users, handler: httpapi.NewHandler(httpapi.Dependencies{
		Logger: logging.New(&strings.Builder{}, slog.LevelDebug, "test", "dev"), Translator: translator, Health: fakeHealth{},
		Tracer: noop.NewTracerProvider().Tracer("test"), Propagator: propagation.TraceContext{}, Sessions: sessions,
		PublicOrigin: "https://easycode.test",
	})}
	cookie := login(t, h, devUser, now)
	idp.RevokeSessions("dev")
	now = now.Add(30 * time.Second)
	if rc := h.do(fasthttp.MethodGet, "/api/v1/me", map[string]string{"Cookie": cookie}); rc.Response.StatusCode() != fasthttp.StatusOK {
		t.Fatalf("within the check window the session holds: %d", rc.Response.StatusCode())
	}
	now = now.Add(31 * time.Second)
	rc := h.do(fasthttp.MethodGet, "/api/v1/me", map[string]string{"Cookie": cookie})
	if rc.Response.StatusCode() != fasthttp.StatusUnauthorized || decode(t, rc)["type"] != "urn:easycode:problem:revoked-session" {
		t.Fatalf("revoked session: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if rc := h.do(fasthttp.MethodGet, "/api/v1/me", map[string]string{"Cookie": cookie}); rc.Response.StatusCode() != fasthttp.StatusUnauthorized {
		t.Fatalf("an ended session stays ended: %d", rc.Response.StatusCode())
	}
}

// RNF-06: 30 min idle or 8 h in total end the session (session-expired); activity slides it.
func TestSessionExpiresAfterIdleTime(t *testing.T) {
	idp := oidctest.New(t)
	now := time.Now()
	sessions := testSessions(idp, newFakeUsers(), newMemoryStore()).WithClock(func() time.Time { return now })
	translator, _ := i18n.NewTranslator()
	h := harness{idp: idp, handler: httpapi.NewHandler(httpapi.Dependencies{
		Logger: logging.New(&strings.Builder{}, slog.LevelDebug, "test", "dev"), Translator: translator, Health: fakeHealth{},
		Tracer: noop.NewTracerProvider().Tracer("test"), Propagator: propagation.TraceContext{}, Sessions: sessions,
		PublicOrigin: "https://easycode.test",
	})}
	cookie := login(t, h, devUser, now)
	now = now.Add(29 * time.Minute)
	if rc := h.do(fasthttp.MethodGet, "/api/v1/me", map[string]string{"Cookie": cookie}); rc.Response.StatusCode() != fasthttp.StatusOK {
		t.Fatalf("active session: %d", rc.Response.StatusCode())
	}
	now = now.Add(30 * time.Minute)
	rc := h.do(fasthttp.MethodGet, "/api/v1/me", map[string]string{"Cookie": cookie})
	if rc.Response.StatusCode() != fasthttp.StatusUnauthorized || decode(t, rc)["type"] != "urn:easycode:problem:session-expired" {
		t.Fatalf("idle session: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
}
