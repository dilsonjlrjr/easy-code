package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode-desktop/internal/config"
	"easycode-desktop/internal/keychain"
	"easycode-desktop/internal/oidc"
	"easycode-desktop/internal/sse"
)

const projectID = "0193a2b4-0000-7000-8000-000000000001"

// fakeServer is the api plus the Keycloak endpoints behind the same origin.
type fakeServer struct {
	t   *testing.T
	srv *httptest.Server

	mu           sync.Mutex
	revoked      bool
	pollAnswer   string // "" = approve
	logouts      int
	waveQueries  []string
	noticeAuth   []string
	noticeFrames chan string
	waveFrames   chan string
	issued       int
	keyInAnswer  bool
	userAgent    string
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{t: t, noticeFrames: make(chan string, 16), waveFrames: make(chan string, 16)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) json(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeServer) authorized(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	revoked := f.revoked
	f.mu.Unlock()
	if revoked || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") {
		f.json(w, 401, map[string]any{"type": "urn:easycode:problem:revoked-session", "title": "Sessão revogada", "status": 401})
		return false
	}
	return true
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/session/local-client":
		f.json(w, 200, map[string]any{
			"issuer": f.srv.URL + "/kc", "client_id": "easycode-desktop", "audience": "easycode-api",
			"scopes":                        []string{"openid", "easycode-read"},
			"device_authorization_endpoint": f.srv.URL + "/kc/device", "endpoint_token": f.srv.URL + "/kc/token",
			"revocation_endpoint": f.srv.URL + "/kc/revoke", "code_validity_s": 600,
		})
	case "/api/v1/health":
		f.json(w, 200, map[string]any{"status": "ok", "version": "1.0.0"})
	case "/kc/device":
		f.json(w, 200, map[string]any{
			"device_code": "dc", "user_code": "WXYZ-1234", "verification_uri": f.srv.URL + "/kc/verify",
			"verification_uri_complete": f.srv.URL + "/kc/verify?user_code=WXYZ-1234", "expires_in": 600, "interval": 1,
		})
	case "/kc/token":
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("grant_type") == "refresh_token" {
			if f.revoked {
				f.json(w, 400, map[string]string{"error": "invalid_grant"})
				return
			}
			f.issued++
			f.json(w, 200, map[string]any{"access_token": fmt.Sprintf("access-%d", f.issued), "refresh_token": "refresh", "expires_in": 300})
			return
		}
		if f.pollAnswer != "" {
			f.json(w, 400, map[string]string{"error": f.pollAnswer})
			return
		}
		f.issued++
		f.json(w, 200, map[string]any{"access_token": fmt.Sprintf("access-%d", f.issued), "refresh_token": "refresh", "expires_in": 300})
	case "/kc/revoke":
		w.WriteHeader(200)
	case "/api/v1/session/logout":
		f.mu.Lock()
		f.logouts++
		f.mu.Unlock()
		w.WriteHeader(204)
	case "/api/v1/me":
		f.mu.Lock()
		f.userAgent = r.Header.Get("User-Agent")
		f.mu.Unlock()
		if f.authorized(w, r) {
			f.json(w, 200, map[string]any{"id": "u1", "name": "Ana", "organization_roles": []string{}, "access_medium": "local_client"})
		}
	case "/api/v1/projects":
		if f.authorized(w, r) {
			f.json(w, 200, map[string]any{"items": []any{map[string]any{"id": projectID, "name": "Portal de compras", "state": "in_progress"}}, "next_cursor": nil, "count_by_state": map[string]int{}})
		}
	case "/api/v1/projects/" + projectID:
		if f.authorized(w, r) {
			f.json(w, 200, map[string]any{"id": projectID, "name": "Portal de compras", "state": "in_progress", "last_sequence": 41})
		}
	case "/api/v1/projects/" + projectID + "/waves/REQ-01":
		if f.authorized(w, r) {
			f.json(w, 200, map[string]any{"id": "REQ-01", "state": "running"})
		}
	case "/api/v1/projects/" + projectID + "/waves/REQ-01/calls":
		if f.authorized(w, r) {
			f.json(w, 200, map[string]any{"items": []any{}, "next_cursor": nil})
		}
	case "/api/v1/me/events":
		f.mu.Lock()
		f.noticeAuth = append(f.noticeAuth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		if f.authorized(w, r) {
			f.stream(w, r, f.noticeFrames)
		}
	case "/api/v1/projects/" + projectID + "/events":
		f.mu.Lock()
		f.waveQueries = append(f.waveQueries, r.URL.RawQuery)
		f.mu.Unlock()
		if f.authorized(w, r) {
			f.stream(w, r, f.waveFrames)
		}
	default:
		f.json(w, 404, map[string]any{"type": "urn:easycode:problem:not-found", "title": "n", "status": 404})
	}
}

func (f *fakeServer) stream(w http.ResponseWriter, r *http.Request, frames chan string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	fmt.Fprint(w, "retry: 2000\n\n")
	w.(http.Flusher).Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-frames:
			if frame == "CLOSE" {
				return
			}
			fmt.Fprint(w, frame)
			w.(http.Flusher).Flush()
		}
	}
}

type emitted struct {
	name string
	data any
}

type fakePlatform struct {
	mu       sync.Mutex
	events   []emitted
	opened   []string
	notified []string
	silent   []bool
	shown    int
}

func (p *fakePlatform) Emit(name string, data any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, emitted{name, data})
}
func (p *fakePlatform) OpenURL(address string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opened = append(p.opened, address)
	return nil
}
func (p *fakePlatform) Notify(id, title, body string, silent bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notified = append(p.notified, id+"|"+title+"|"+body)
	p.silent = append(p.silent, silent)
	return nil
}
func (p *fakePlatform) Show() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shown++
}

func (p *fakePlatform) snapshot() ([]emitted, []string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]emitted(nil), p.events...), append([]string(nil), p.opened...), append([]string(nil), p.notified...)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newService(t *testing.T, vault *keychain.Memory) (*Service, *fakePlatform) {
	svc, host := New(Deps{
		Version:  "1.0.0",
		Prefs:    config.NewStore(t.TempDir(), func(string) string { return "" }),
		Keychain: func(string) keychain.Store { return vault },
		Tune: func(o *sse.Options) {
			o.MinBackoff, o.MaxBackoff = 5*time.Millisecond, 20*time.Millisecond
		},
		TuneIdentity: func(c *oidc.Client) {
			c.Wait = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
		},
	})
	platform := &fakePlatform{}
	host.SetPlatform(platform)
	hosts[svc] = host
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { host.Shutdown(); cancel() })
	if err := host.Startup(ctx); err != nil {
		t.Fatal(err)
	}
	return svc, platform
}

// hosts keeps the host of each test service (tests run one at a time).
var hosts = map[*Service]*Host{}

func signIn(t *testing.T, svc *Service, server *fakeServer) {
	t.Helper()
	if _, err := svc.SetServer(server.srv.URL); err != nil {
		t.Fatal(err)
	}
	status, err := svc.SignIn()
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != PhaseAwaitingApproval && status.Phase != PhaseSignedIn {
		t.Fatalf("phase %s", status.Phase)
	}
	eventually(t, "signed in", func() bool { return svc.Status().Phase == PhaseSignedIn })
}

// US-45 CA-1 / CA-28-1: code shown, browser opened, token only in the keychain, user shown.
func TestSignInByDeviceFlowStoresTokenOnlyInKeychain(t *testing.T) {
	server := newFakeServer(t)
	vault := &keychain.Memory{}
	svc, platform := newService(t, vault)
	if svc.Status().Phase != PhaseSignedOut {
		t.Fatalf("start phase %s", svc.Status().Phase)
	}
	signIn(t, svc, server)
	_, opened, _ := platform.snapshot()
	if len(opened) == 0 || !strings.HasSuffix(opened[0], "/kc/verify?user_code=WXYZ-1234") {
		t.Fatalf("verification page not opened: %v", opened)
	}
	if stored, err := vault.Get(); err != nil || stored != "refresh" {
		t.Fatalf("refresh token not in the keychain: %q %v", stored, err)
	}
	status := svc.Status()
	var user struct{ Name string }
	_ = json.Unmarshal(status.User, &user)
	if user.Name != "Ana" || status.ServerVersion != "1.0.0" || status.VersionMismatch {
		t.Fatalf("status %+v", status)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), "refresh") || strings.Contains(string(raw), "access-") {
		t.Fatalf("tokens leaked to the interface: %s", raw)
	}
}

// US-45 CA-4 / ER-28-2.
func TestSignInCodeExpires(t *testing.T) {
	server := newFakeServer(t)
	server.pollAnswer = "expired_token"
	vault := &keychain.Memory{}
	svc, _ := newService(t, vault)
	_, _ = svc.SetServer(server.srv.URL)
	if _, err := svc.SignIn(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "code expired", func() bool { return svc.Status().Reason == ReasonCodeExpired })
	if _, err := vault.Get(); !errors.Is(err, keychain.ErrNotFound) {
		t.Fatal("no token may be stored")
	}
}

// ER-28-1: without a keychain the sign-in fails with a message and nothing is written.
func TestSignInWithoutKeychain(t *testing.T) {
	server := newFakeServer(t)
	svc, platform := newService(t, &keychain.Memory{Unavailable: true})
	_, _ = svc.SetServer(server.srv.URL)
	_, err := svc.SignIn()
	var ui *UIError
	if !errors.As(err, &ui) || ui.Kind != KindKeychainUnavailable {
		t.Fatalf("want keychain_unavailable, got %v", err)
	}
	if svc.Status().Reason != ReasonKeychainUnavailable {
		t.Fatalf("status %+v", svc.Status())
	}
	if _, opened, _ := platform.snapshot(); len(opened) != 0 {
		t.Fatal("no device flow may start without a keychain")
	}
}

// US-46 CA-3 / CA-28-8 and CA-4: native notification with type, project and stage; the click opens the web.
func TestNoticeBecomesNativeNotificationAndOpensWeb(t *testing.T) {
	server := newFakeServer(t)
	svc, platform := newService(t, &keychain.Memory{})
	signIn(t, svc, server)
	if _, err := svc.Projects(); err != nil {
		t.Fatal(err)
	}
	_ = svc.SetMuteSound(true)
	server.noticeFrames <- `id: 900
event: br.easycode.gate.awaiting_approval.v1
data: {"id":"ev-1","type":"br.easycode.gate.awaiting_approval.v1","projectid":"` + projectID + `","data":{"gate":"gate.requirements","round":2,"stage":"requirements"}}

`
	eventually(t, "notification", func() bool { _, _, n := platform.snapshot(); return len(n) == 1 })
	_, _, notified := platform.snapshot()
	if notified[0] != "ev-1|Gate aguardando aprovação|Projeto: Portal de compras · Etapa: Requisitos" {
		t.Fatalf("notification %q", notified[0])
	}
	if !platform.silent[0] {
		t.Fatal("muted sound ignored")
	}
	hosts[svc].NotificationClicked("ev-1")
	_, opened, _ := platform.snapshot()
	want := server.srv.URL + "/projects/" + projectID + "/gates/gate.requirements/rounds/2"
	if opened[len(opened)-1] != want {
		t.Fatalf("opened %v, want %s", opened, want)
	}
	events, _, _ := platform.snapshot()
	found := false
	for _, e := range events {
		if e.name == EventNotice {
			found = true
		}
	}
	if !found {
		t.Fatal("dashboard not told about the notice")
	}
}

// US-45 CA-2 / ER-28-3: the session revoked on the web ends the stream; the refresh fails; back to sign-in.
func TestRevokedSessionReturnsToSignIn(t *testing.T) {
	server := newFakeServer(t)
	vault := &keychain.Memory{}
	svc, _ := newService(t, vault)
	signIn(t, svc, server)
	server.mu.Lock()
	server.revoked = true
	server.mu.Unlock()
	server.noticeFrames <- "event: easycode.ended\ndata: {\"reason\":\"session_revoked\"}\n\n"
	eventually(t, "signed out", func() bool {
		s := svc.Status()
		return s.Phase == PhaseSignedOut && s.Reason == ReasonSessionRevoked
	})
	if _, err := vault.Get(); !errors.Is(err, keychain.ErrNotFound) {
		t.Fatal("keychain must be cleared")
	}
}

// A REST call with the revoked token also signs out.
func TestRevokedSessionOnRestCall(t *testing.T) {
	server := newFakeServer(t)
	vault := &keychain.Memory{}
	svc, _ := newService(t, vault)
	signIn(t, svc, server)
	server.mu.Lock()
	server.revoked = true
	server.mu.Unlock()
	_, err := svc.Projects()
	var ui *UIError
	if !errors.As(err, &ui) || ui.Kind != KindSessionEnded {
		t.Fatalf("want session_ended, got %v", err)
	}
	if svc.Status().Reason != ReasonSessionRevoked {
		t.Fatalf("status %+v", svc.Status())
	}
	if _, err := vault.Get(); !errors.Is(err, keychain.ErrNotFound) {
		t.Fatal("keychain must be cleared after revoked-session")
	}
}

// sse.md 5.4 as built: session_expired renews with the refresh token and reconnects.
func TestExpiredSessionOnStreamRenewsAndReconnects(t *testing.T) {
	server := newFakeServer(t)
	svc, _ := newService(t, &keychain.Memory{})
	signIn(t, svc, server)
	server.noticeFrames <- "event: easycode.ended\ndata: {\"reason\":\"session_expired\"}\n\n"
	eventually(t, "reconnected with a renewed token", func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(server.noticeAuth) >= 2 && server.noticeAuth[len(server.noticeAuth)-1] != server.noticeAuth[0]
	})
	if svc.Status().Phase != PhaseSignedIn {
		t.Fatalf("status %+v", svc.Status())
	}
}

// US-46 CA-2 / CA-5 and sse.md 5.1: the wave stream starts at last_sequence, filtered by the wave, and its
// events reach the view; a finished wave with its view open notifies and a click opens the app view.
func TestOpenWaveStreamsFromLastSequence(t *testing.T) {
	server := newFakeServer(t)
	svc, platform := newService(t, &keychain.Memory{})
	signIn(t, svc, server)
	view, err := svc.OpenWave(projectID, "REQ-01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(view.Wave), "REQ-01") {
		t.Fatalf("view %s", view.Wave)
	}
	eventually(t, "wave stream", func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(server.waveQueries) == 1
	})
	server.mu.Lock()
	query := server.waveQueries[0]
	server.mu.Unlock()
	if !strings.Contains(query, "waveId=REQ-01") || !strings.Contains(query, "since=41") {
		t.Fatalf("wave stream query %q", query)
	}
	server.waveFrames <- "id: 42\nevent: br.easycode.wave.checkpoint_written.v1\ndata: {\"id\":\"w42\",\"type\":\"br.easycode.wave.checkpoint_written.v1\"}\n\n"
	eventually(t, "wave event", func() bool {
		events, _, _ := platform.snapshot()
		for _, e := range events {
			if e.name == EventWave && strings.Contains(string(e.data.(json.RawMessage)), "w42") {
				return true
			}
		}
		return false
	})
	// The connection drops and comes back from Last-Event-ID 42 (no loss, no duplicate).
	server.waveFrames <- "CLOSE"
	eventually(t, "reconnection", func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(server.waveQueries) == 2
	})
	eventually(t, "reconnected state", func() bool {
		events, _, _ := platform.snapshot()
		for _, e := range events {
			if c, ok := e.data.(ConnectionState); ok && c.Stream == StreamWave && c.State == "live" && c.Reconnected {
				return true
			}
		}
		return false
	})
	server.noticeFrames <- "id: 901\nevent: br.easycode.wave.completed.v1\ndata: {\"id\":\"ev-done\",\"type\":\"br.easycode.wave.completed.v1\",\"projectid\":\"" + projectID + "\",\"waveid\":\"REQ-01\",\"data\":{\"wave\":\"REQ-01\",\"stage\":\"requirements\"}}\n\n"
	eventually(t, "finished wave notification", func() bool { _, _, n := platform.snapshot(); return len(n) == 1 })
	hosts[svc].NotificationClicked("ev-done")
	platform.mu.Lock()
	shown := platform.shown
	platform.mu.Unlock()
	if shown != 1 {
		t.Fatal("click on a finished wave must show the app")
	}
	svc.CloseWave()
}

// ER-28-5 / CA-28-5: the app opens only web screens built from known targets.
func TestOpenWebBuildsOnlyKnownTargets(t *testing.T) {
	server := newFakeServer(t)
	svc, platform := newService(t, &keychain.Memory{})
	_, _ = svc.SetServer(server.srv.URL)
	if err := svc.OpenPendingItem("finding_escalated", projectID, "finding", "ACH-07"); err != nil {
		t.Fatal(err)
	}
	var ui *UIError
	if err := svc.OpenPendingItem("gate", "../../evil", "gate_round", "x/1"); !errors.As(err, &ui) || ui.Kind != KindInvalidTarget {
		t.Fatalf("want invalid_target, got %v", err)
	}
	_, opened, _ := platform.snapshot()
	if len(opened) != 1 || opened[0] != server.srv.URL+"/projects/"+projectID+"/findings/ACH-07#decision" {
		t.Fatalf("opened %v", opened)
	}
}

// Sign out calls endSession and clears the keychain.
func TestSignOut(t *testing.T) {
	server := newFakeServer(t)
	vault := &keychain.Memory{}
	svc, _ := newService(t, vault)
	signIn(t, svc, server)
	status, err := svc.SignOut()
	if err != nil || status.Phase != PhaseSignedOut {
		t.Fatalf("%+v %v", status, err)
	}
	if server.logouts != 1 {
		t.Fatal("endSession not called")
	}
	if _, err := vault.Get(); !errors.Is(err, keychain.ErrNotFound) {
		t.Fatal("keychain not cleared")
	}
}

// A stored refresh token restores the session at start-up.
func TestStartupRestoresStoredSession(t *testing.T) {
	server := newFakeServer(t)
	vault := &keychain.Memory{}
	_ = vault.Set("refresh")
	dir := t.TempDir()
	store := config.NewStore(dir, func(string) string { return "" })
	_ = store.Save(config.Preferences{Server: server.srv.URL, Locale: config.LocaleEn})
	svc, host := New(Deps{Version: "2.0.0", Prefs: store, Keychain: func(string) keychain.Store { return vault }})
	platform := &fakePlatform{}
	host.SetPlatform(platform)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer host.Shutdown()
	_ = host.Startup(ctx)
	eventually(t, "restored", func() bool { return svc.Status().Phase == PhaseSignedIn })
	status := svc.Status()
	if status.Locale != config.LocaleEn || !status.VersionMismatch {
		t.Fatalf("status %+v", status)
	}
	server.mu.Lock()
	ua := server.userAgent
	server.mu.Unlock()
	if !strings.HasPrefix(ua, "easycode-desktop/2.0.0 (") || !strings.HasSuffix(ua, ")") {
		t.Fatalf("User-Agent %q", ua)
	}
}

// No version warning when either side is a development build.
func TestNoVersionWarningForDevBuilds(t *testing.T) {
	svc := &Service{deps: Deps{Version: "1.0.0"}, serverVersion: "dev"}
	if svc.Status().VersionMismatch {
		t.Fatal("server dev must not warn")
	}
	svc = &Service{deps: Deps{Version: "dev"}, serverVersion: "1.0.0"}
	if svc.Status().VersionMismatch {
		t.Fatal("app dev must not warn")
	}
}
