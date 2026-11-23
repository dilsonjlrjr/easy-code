package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeKeycloak answers the device authorization and token endpoints with a scripted list of poll answers.
type fakeKeycloak struct {
	mu        sync.Mutex
	polls     []string // "" = success, otherwise the OAuth error code
	forms     []map[string]string
	refreshOK bool
}

func (f *fakeKeycloak) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.record(r)
		if r.Form.Get("client_id") != "easycode-desktop" || r.Form.Get("scope") != "openid easycode-read" {
			t.Errorf("device request form %v", r.Form)
		}
		writeJSON(w, 200, map[string]any{
			"device_code": "dev-code", "user_code": "ABCD-EFGH",
			"verification_uri":          "https://idp.example/device",
			"verification_uri_complete": "https://idp.example/device?user_code=ABCD-EFGH",
			"expires_in":                600, "interval": 5,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.record(r)
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("grant_type") == "refresh_token" {
			if !f.refreshOK {
				writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
				return
			}
			writeJSON(w, 200, map[string]any{"access_token": "access-2", "refresh_token": "refresh-2", "expires_in": 300})
			return
		}
		if r.Form.Get("grant_type") != DeviceGrantType || r.Form.Get("device_code") != "dev-code" {
			t.Errorf("token request form %v", r.Form)
		}
		next := f.polls[0]
		f.polls = f.polls[1:]
		if next != "" {
			writeJSON(w, 400, map[string]string{"error": next})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 300})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.record(r)
		w.WriteHeader(200)
	})
	return mux
}

func (f *fakeKeycloak) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	form := map[string]string{"path": r.URL.Path}
	for key := range r.Form {
		form[key] = r.Form.Get(key)
	}
	f.forms = append(f.forms, form)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type clock struct{ now time.Time }

func newTestClient(t *testing.T, kc *fakeKeycloak) (*Client, *clock, *[]time.Duration) {
	server := httptest.NewServer(kc.handler(t))
	t.Cleanup(server.Close)
	clk := &clock{now: time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)}
	var waits []time.Duration
	client := NewClient(Configuration{
		ClientID:                    "easycode-desktop",
		Scopes:                      []string{"openid", "easycode-read"},
		DeviceAuthorizationEndpoint: server.URL + "/device",
		TokenEndpoint:               server.URL + "/token",
		RevocationEndpoint:          server.URL + "/revoke",
	}, "easycode-desktop/test")
	client.Now = func() time.Time { return clk.now }
	client.Wait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		clk.now = clk.now.Add(d)
		return ctx.Err()
	}
	return client, clk, &waits
}

func TestDeviceFlowApprovedAfterPendingAndSlowDown(t *testing.T) {
	kc := &fakeKeycloak{polls: []string{"authorization_pending", "slow_down", ""}}
	client, _, waits := newTestClient(t, kc)
	auth, err := client.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if auth.UserCode != "ABCD-EFGH" || auth.VerificationURIComplete == "" {
		t.Fatalf("auth %+v", auth)
	}
	tokens, err := client.Poll(context.Background(), auth)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "access-1" || tokens.RefreshToken != "refresh-1" {
		t.Fatalf("tokens %+v", tokens)
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second}
	if len(*waits) != len(want) {
		t.Fatalf("waits %v", *waits)
	}
	for i := range want {
		if (*waits)[i] != want[i] {
			t.Fatalf("slow_down must add 5 s: waits %v", *waits)
		}
	}
}

func TestDeviceFlowExpiresWithoutToken(t *testing.T) {
	kc := &fakeKeycloak{polls: []string{"authorization_pending", "expired_token"}}
	client, _, _ := newTestClient(t, kc)
	auth, _ := client.Start(context.Background())
	if _, err := client.Poll(context.Background(), auth); !errors.Is(err, ErrExpired) {
		t.Fatalf("want ErrExpired, got %v", err)
	}
}

func TestDeviceFlowExpiresByLocalDeadline(t *testing.T) {
	pending := make([]string, 200)
	for i := range pending {
		pending[i] = "authorization_pending"
	}
	client, _, waits := newTestClient(t, &fakeKeycloak{polls: pending})
	auth, _ := client.Start(context.Background())
	if _, err := client.Poll(context.Background(), auth); !errors.Is(err, ErrExpired) {
		t.Fatalf("want ErrExpired, got %v", err)
	}
	if len(*waits) != 120 { // 600 s / 5 s
		t.Fatalf("polled %d times", len(*waits))
	}
}

func TestDeviceFlowDenied(t *testing.T) {
	client, _, _ := newTestClient(t, &fakeKeycloak{polls: []string{"access_denied"}})
	auth, _ := client.Start(context.Background())
	if _, err := client.Poll(context.Background(), auth); !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
}

func TestDeviceFlowStopsWhenCanceled(t *testing.T) {
	client, _, _ := newTestClient(t, &fakeKeycloak{polls: []string{"authorization_pending"}})
	auth, _ := client.Start(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Poll(ctx, auth); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestRefreshAndRevoke(t *testing.T) {
	kc := &fakeKeycloak{refreshOK: true}
	client, _, _ := newTestClient(t, kc)
	tokens, err := client.Refresh(context.Background(), "refresh-1")
	if err != nil || tokens.RefreshToken != "refresh-2" || tokens.AccessToken != "access-2" {
		t.Fatalf("refresh %+v %v", tokens, err)
	}
	kc.refreshOK = false
	if _, err := client.Refresh(context.Background(), "refresh-2"); !errors.Is(err, ErrRefreshRejected) {
		t.Fatalf("want ErrRefreshRejected, got %v", err)
	}
	if err := client.Revoke(context.Background(), "refresh-2"); err != nil {
		t.Fatal(err)
	}
	last := kc.forms[len(kc.forms)-1]
	if last["path"] != "/revoke" || last["token"] != "refresh-2" || last["token_type_hint"] != "refresh_token" {
		t.Fatalf("revoke form %v", last)
	}
}

func TestConfigurationRefusesClearTextRemoteEndpoints(t *testing.T) {
	good := Configuration{
		ClientID: "easycode-desktop", Scopes: []string{"easycode-read"},
		DeviceAuthorizationEndpoint: "https://idp.example/device", TokenEndpoint: "http://localhost:8180/token",
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.TokenEndpoint = "http://idp.example/token"
	if err := bad.Validate(); !errors.Is(err, ErrInsecureEndpoint) {
		t.Fatalf("want ErrInsecureEndpoint, got %v", err)
	}
	bad = good
	bad.Scopes = nil
	if err := bad.Validate(); err == nil {
		t.Fatal("configuration without scopes accepted")
	}
}
