package http_test

import (
	"strings"
	"testing"
	"time"

	appevent "easycode/internal/application/event"
	"easycode/internal/application/localclient"
	"easycode/internal/infrastructure/oidc/oidctest"
	httpapi "easycode/internal/interfaces/http"
)

var carla = oidctest.User{Subject: "carla", Name: "Carla Membro"}

// US-45 CA-1: the app reads the device flow parameters without a session and never fixes the Keycloak
// address (RNF-17).
func TestLocalClientConfigurationGivesTheDeviceFlow(t *testing.T) {
	live := newLiveAPI(t)
	rc := live.do("GET", "/api/v1/session/local-client", nil)
	if rc.Response.StatusCode() != 200 {
		t.Fatalf("configuration: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	body := decode(t, rc)
	issuer := live.idp.Issuer()
	if body["issuer"] != issuer || body["client_id"] != "easycode-desktop" || body["audience"] != oidctest.ClientID ||
		body["device_authorization_endpoint"] != issuer+"/protocol/openid-connect/auth/device" ||
		body["endpoint_token"] != issuer+"/protocol/openid-connect/token" || body["code_validity_s"] != float64(600) {
		t.Fatalf("configuration: %v", body)
	}
	if scopes, _ := body["scopes"].([]any); len(scopes) != 2 || scopes[1] != "easycode-read" {
		t.Fatalf("scopes: %v", body["scopes"])
	}
}

// US-45 CA-1, CA-5: the token of the app opens a session the person sees on the web with the device, with
// the notice of the new session until acknowledged (S-19); the app sees itself as local_client, no CSRF token.
func TestLocalClientSessionIsListedAndAcknowledged(t *testing.T) {
	live := newLiveAPI(t)
	web := live.as(t, carla)
	token := live.idp.DesktopToken(carla, "sid-carla-1", 5*time.Minute, nil)

	me := live.bearer("GET", "/api/v1/me", token, "")
	if me.Response.StatusCode() != 200 {
		t.Fatalf("me by the app: %d %s", me.Response.StatusCode(), me.Response.Body())
	}
	body := decode(t, me)
	if body["access_medium"] != "local_client" || body["csrf_token"] != nil || body["id"] != "carla" || body["name"] != "Carla Membro" {
		t.Fatalf("me by the app: %v", body)
	}
	if got := web.json(t, "GET", "/api/v1/me", "", nil, 200)["local_client_sessions_to_review"]; got != float64(1) {
		t.Fatalf("new session notice: %v", got)
	}
	items := web.json(t, "GET", "/api/v1/me/local-client-sessions", "", nil, 200)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("sessions: %v", items)
	}
	item := items[0].(map[string]any)
	if item["device"] != "easycode-desktop/1.0.0 (linux; amd64)" || item["acknowledged"] != false || item["current"] != false ||
		item["last_use_at"] == nil || strings.Contains(string(web.send("GET", "/api/v1/me/local-client-sessions", "", nil).Response.Body()), token) {
		t.Fatalf("session: %v", item)
	}
	if mine := decode(t, live.bearer("GET", "/api/v1/me/local-client-sessions", token, ""))["items"].([]any); mine[0].(map[string]any)["current"] != true {
		t.Fatalf("the app sees its own session as current: %v", mine)
	}
	id := item["id"].(string)
	if rc := web.send("POST", "/api/v1/me/local-client-sessions/"+id+"/acknowledgement", "", nil); rc.Response.StatusCode() != 204 {
		t.Fatalf("acknowledge: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if got := web.json(t, "GET", "/api/v1/me", "", nil, 200)["local_client_sessions_to_review"]; got != float64(0) {
		t.Fatalf("notice after acknowledge: %v", got)
	}
	// RN-3: each person sees and revokes only their own sessions.
	other := live.as(t, oidctest.User{Subject: "bruno", Name: "Bruno"})
	if n := other.json(t, "GET", "/api/v1/me/local-client-sessions", "", nil, 200)["items"].([]any); len(n) != 0 {
		t.Fatalf("bruno sees carla's sessions: %v", n)
	}
	if rc := other.send("DELETE", "/api/v1/me/local-client-sessions/"+id, "", nil); rc.Response.StatusCode() != 404 {
		t.Fatalf("bruno revokes carla's session: %d", rc.Response.StatusCode())
	}
}

// US-45 CA-6, PD-07: every write with the token of the app is refused with requires-web-session before
// anything changes; only endSession is accepted.
func TestLocalClientTokenIsReadOnly(t *testing.T) {
	live := newLiveAPI(t, liveRuntimeDeps(appevent.NewBroker(), make(chan struct{})))
	token := live.idp.DesktopToken(devUser, "sid-dev-1", 5*time.Minute, nil)
	before := count(t, live.db, "SELECT count(*) FROM easycode.projects")
	walked := 0
	for _, route := range httpapi.RouteTemplates(live.deps) {
		method, template, _ := strings.Cut(route, " ")
		if method == "GET" || template == "/api/v1/session/logout" || strings.HasPrefix(template, "/git/") {
			continue
		}
		path := template
		for param, value := range pathValues {
			path = strings.ReplaceAll(path, param, value)
		}
		path = strings.ReplaceAll(path, "{projectId}", "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b00")
		path = strings.ReplaceAll(path, "{sessionId}", "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b01")
		rc := live.bearer(method, path, token, "{}")
		if rc.Response.StatusCode() != 403 || decode(t, rc)["type"] != "urn:easycode:problem:requires-web-session" {
			t.Errorf("%s with the app token: %d %s", route, rc.Response.StatusCode(), rc.Response.Body())
		}
		walked++
	}
	if walked < 20 {
		t.Fatalf("only %d writes walked", walked)
	}
	if rc := live.bearer("POST", "/api/v1/projects", token, newProjectBody); rc.Response.StatusCode() != 403 {
		t.Fatalf("create project by the app: %d", rc.Response.StatusCode())
	}
	if count(t, live.db, "SELECT count(*) FROM easycode.projects") != before {
		t.Fatal("a write of the app changed the base")
	}
}

// RNF-06: a token of another client, without the read scope, for another audience, badly signed or
// expired is refused; expired answers session-expired so the app refreshes.
func TestLocalClientRefusesForeignTokens(t *testing.T) {
	live := newLiveAPI(t)
	cases := map[string]func(map[string]any){
		"other client":   func(c map[string]any) { c["azp"] = "easycode-api" },
		"no read scope":  func(c map[string]any) { c["scope"] = "openid profile" },
		"other audience": func(c map[string]any) { c["aud"] = "account" },
		"other issuer":   func(c map[string]any) { c["iss"] = "https://evil.example/realms/easycode" },
		"no session":     func(c map[string]any) { delete(c, "sid") },
	}
	for name, change := range cases {
		token := live.idp.DesktopToken(carla, "sid-"+name, 5*time.Minute, change)
		if rc := live.bearer("GET", "/api/v1/me", token, ""); rc.Response.StatusCode() != 401 || decode(t, rc)["type"] != "urn:easycode:problem:not-authenticated" {
			t.Errorf("%s: %d %s", name, rc.Response.StatusCode(), rc.Response.Body())
		}
	}
	token := live.idp.DesktopToken(carla, "sid-ok", 5*time.Minute, nil)
	if rc := live.bearer("GET", "/api/v1/me", token[:len(token)-4]+"AAAA", ""); rc.Response.StatusCode() != 401 {
		t.Errorf("bad signature: %d", rc.Response.StatusCode())
	}
	expired := live.idp.DesktopToken(carla, "sid-ok", -5*time.Minute, nil)
	if rc := live.bearer("GET", "/api/v1/me", expired, ""); rc.Response.StatusCode() != 401 || decode(t, rc)["type"] != "urn:easycode:problem:session-expired" {
		t.Errorf("expired: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if n := count(t, live.db, "SELECT count(*) FROM easycode.local_client_sessions"); n != 0 {
		t.Fatalf("refused tokens opened %d sessions", n)
	}
}

// US-45 CA-2: revoking on the web refuses the next call of the app at once with revoked-session, and the
// trail records it; an SSO session ended in the realm is refused within 60 s (RNF-06).
func TestLocalClientRevocation(t *testing.T) {
	live := newLiveAPI(t)
	service := live.deps.LocalClient.(*localclient.Service)
	now := time.Now()
	service.WithClock(func() time.Time { return now })
	web := live.as(t, carla)

	token := live.idp.DesktopToken(carla, "sid-revoke", 10*time.Minute, nil)
	if rc := live.bearer("GET", "/api/v1/projects", token, ""); rc.Response.StatusCode() != 200 {
		t.Fatalf("projects by the app: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	id := web.json(t, "GET", "/api/v1/me/local-client-sessions", "", nil, 200)["items"].([]any)[0].(map[string]any)["id"].(string)
	if rc := web.send("DELETE", "/api/v1/me/local-client-sessions/"+id, "", nil); rc.Response.StatusCode() != 204 {
		t.Fatalf("revoke: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if rc := live.bearer("GET", "/api/v1/projects", token, ""); rc.Response.StatusCode() != 401 || decode(t, rc)["type"] != "urn:easycode:problem:revoked-session" {
		t.Fatalf("after revoke: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if rc := web.send("DELETE", "/api/v1/me/local-client-sessions/"+id, "", nil); rc.Response.StatusCode() != 404 {
		t.Fatalf("revoke twice: %d", rc.Response.StatusCode())
	}
	if n := count(t, live.db, `SELECT count(*) FROM easycode.audit_records WHERE action = 'local_client_session.revoke'
	   AND origin = 'web' AND target = 'local-client-sessions/`+id+`'`); n != 1 {
		t.Fatalf("revocation audit records: %d", n)
	}

	// The SSO session ends in the realm (refresh token revoked): refused at the next check, within 60 s.
	second := live.idp.DesktopToken(carla, "sid-sso", 10*time.Minute, nil)
	if rc := live.bearer("GET", "/api/v1/me", second, ""); rc.Response.StatusCode() != 200 {
		t.Fatalf("second session: %d", rc.Response.StatusCode())
	}
	live.idp.EndAppSession("carla", "sid-sso")
	now = now.Add(30 * time.Second)
	if rc := live.bearer("GET", "/api/v1/me", second, ""); rc.Response.StatusCode() != 200 {
		t.Fatalf("inside the 60 s cache: %d", rc.Response.StatusCode())
	}
	now = now.Add(31 * time.Second)
	if rc := live.bearer("GET", "/api/v1/me", second, ""); rc.Response.StatusCode() != 401 || decode(t, rc)["type"] != "urn:easycode:problem:revoked-session" {
		t.Fatalf("after the SSO ended the session: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
}

// US-45 "Sair": endSession with the token ends the session here and records session.logout from the app.
func TestLocalClientLogout(t *testing.T) {
	live := newLiveAPI(t)
	token := live.idp.DesktopToken(carla, "sid-logout", 5*time.Minute, nil)
	if rc := live.bearer("POST", "/api/v1/session/logout", token, ""); rc.Response.StatusCode() != 204 {
		t.Fatalf("logout: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if rc := live.bearer("GET", "/api/v1/me", token, ""); rc.Response.StatusCode() != 401 {
		t.Fatalf("after logout: %d", rc.Response.StatusCode())
	}
	if n := count(t, live.db, `SELECT count(*) FROM easycode.audit_records WHERE action = 'session.logout' AND origin = 'local_client'`); n != 1 {
		t.Fatalf("logout audit records: %d", n)
	}
	if n := count(t, live.db, `SELECT count(*) FROM easycode.local_client_sessions WHERE end_reason = 'logout'`); n != 1 {
		t.Fatalf("ended sessions: %d", n)
	}
}
