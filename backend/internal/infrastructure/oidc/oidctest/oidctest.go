// Package oidctest is a test-only Keycloak realm on httptest: authorization code with PKCE, RS256
// ID tokens, refresh, logout, client credentials and the admin endpoints the platform reads (users
// and events). Tests sign people in through the real login routes with it, so no test calls a real
// SSO and no fixed development identity exists outside tests (PLANO MARCO-2).
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/infrastructure/oidc"
)

// Realm path of the fake server, like Keycloak's.
const realmPath = "/realms/easycode"

// Client credentials the fake accepts.
const (
	ClientID     = "easycode-api"
	ClientSecret = "test-only-client-secret"
)

// AdministratorRole is the realm role of the organization administrator in tests.
const AdministratorRole = "easycode-administrator"

// User is a person of the fake realm.
type User struct {
	Subject       string
	Name          string
	Administrator bool
	Disabled      bool
}

// Event is a user event served by the admin events endpoint.
type Event struct {
	ID, Type, ClientID, Subject, Error string
	Time                               time.Time
}

type grant struct {
	user      User
	nonce     string
	challenge string
	authTime  time.Time
}

// Server is the fake realm.
type Server struct {
	*httptest.Server
	key *rsa.PrivateKey

	mu       sync.Mutex
	codes    map[string]grant
	refresh  map[string]User // valid refresh tokens
	users    map[string]User
	events   []Event
	Logouts  int
	Refreshs int
	// appSessions are the SSO sessions of the desktop app per person (sid → open), device flow of US-45.
	appSessions map[string]map[string]bool
	// SessionChecks counts the reads of the sessions of a person (the api confirms at most every 60 s).
	SessionChecks int
}

// New starts the fake realm; it stops with the test.
func New(t *testing.T) *Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{key: key, codes: map[string]grant{}, refresh: map[string]User{}, users: map[string]User{}, appSessions: map[string]map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc(realmPath+"/protocol/openid-connect/certs", s.certs)
	mux.HandleFunc(realmPath+"/protocol/openid-connect/token", s.token)
	mux.HandleFunc(realmPath+"/protocol/openid-connect/logout", s.logout)
	mux.HandleFunc("/admin"+realmPath+"/users", s.searchUsers)
	mux.HandleFunc("/admin"+realmPath+"/users/", s.user)
	mux.HandleFunc("/admin"+realmPath+"/events", s.listEvents)
	mux.HandleFunc("/admin"+realmPath+"/sessions/", s.deleteSession)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// Issuer is the issuer of the fake realm.
func (s *Server) Issuer() string { return s.URL + realmPath }

// Settings are the oidc settings of the api for this realm.
func (s *Server) Settings(redirectURL string) oidc.Settings {
	return oidc.Settings{Issuer: s.Issuer(), ClientID: ClientID, ClientSecret: ClientSecret, RedirectURL: redirectURL,
		AdministratorRole: AdministratorRole, Timeout: 5 * time.Second}
}

// AddUser registers (or replaces) a person of the directory.
func (s *Server) AddUser(u User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.Subject] = u
}

// RemoveUser deletes a person of the directory (offboarding).
func (s *Server) RemoveUser(subject string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, subject)
}

// AddEvent records a user event.
func (s *Server) AddEvent(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

// RevokeSessions invalidates every refresh token of a person (logout elsewhere, account disabled).
func (s *Server) RevokeSessions(subject string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, u := range s.refresh {
		if u.Subject == subject {
			delete(s.refresh, token)
		}
	}
}

// Authorize plays the browser at the authorization endpoint: it checks the request the api built
// and answers the query string the SSO would send to the callback.
func (s *Server) Authorize(t *testing.T, authorizationURL string, u User, authTime time.Time) string {
	t.Helper()
	parsed, err := url.Parse(authorizationURL)
	if err != nil || !strings.HasPrefix(authorizationURL, s.Issuer()+"/protocol/openid-connect/auth?") {
		t.Fatalf("unexpected authorization url %q", authorizationURL)
	}
	q := parsed.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != ClientID || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") == "" || q.Get("nonce") == "" || q.Get("state") == "" || !strings.Contains(q.Get("scope"), "openid") {
		t.Fatalf("authorization request misses PKCE, nonce or state: %v", q)
	}
	code := random()
	s.mu.Lock()
	s.codes[code] = grant{user: u, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), authTime: authTime}
	s.users[u.Subject] = u
	s.mu.Unlock()
	return url.Values{"code": {code}, "state": {q.Get("state")}, "session_state": {"x"}}.Encode()
}

// DesktopClientID is the public OIDC client of the desktop app (ADR-20).
const DesktopClientID = "easycode-desktop"

// DesktopToken plays the device flow of the desktop app (US-45): it opens the SSO session sid of the person
// and returns an access token like Keycloak's (audience easycode-api, azp easycode-desktop, scope
// easycode-read), valid for ttl. change edits the claims before signing (tests of refusals).
func (s *Server) DesktopToken(u User, sid string, ttl time.Duration, change func(claims map[string]any)) string {
	s.mu.Lock()
	s.users[u.Subject] = u
	if s.appSessions[u.Subject] == nil {
		s.appSessions[u.Subject] = map[string]bool{}
	}
	s.appSessions[u.Subject][sid] = true
	s.mu.Unlock()
	now := time.Now()
	claims := map[string]any{
		"iss": s.Issuer(), "sub": u.Subject, "aud": ClientID, "azp": DesktopClientID, "typ": "Bearer",
		"exp": now.Add(ttl).Unix(), "iat": now.Unix(), "auth_time": now.Add(-time.Minute).Unix(), "sid": sid,
		"scope": "openid easycode-read profile", "name": u.Name, "preferred_username": strings.ToLower(u.Subject),
	}
	if u.Administrator {
		claims["realm_access"] = map[string]any{"roles": []string{AdministratorRole}}
	}
	if change != nil {
		change(claims)
	}
	return s.sign(claims)
}

// EndAppSession ends an SSO session of the desktop app (refresh token revoked, session removed in the realm).
func (s *Server) EndAppSession(subject, sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.appSessions[subject], sid)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) || r.Method != http.MethodDelete {
		return
	}
	sid := strings.TrimPrefix(r.URL.Path, "/admin"+realmPath+"/sessions/")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sessions := range s.appSessions {
		delete(sessions, sid)
	}
	w.WriteHeader(http.StatusNoContent)
}

// AppSessionOpen reports whether the realm still has the SSO session of the desktop app (refresh would work).
func (s *Server) AppSessionOpen(subject, sid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appSessions[subject][sid]
}

func (s *Server) sign(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test","typ":"JWT"}`))
	payloadJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	digest := sha256.Sum256([]byte(header + "." + payload))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) certs(w http.ResponseWriter, _ *http.Request) {
	e := big.NewInt(int64(s.key.E)).Bytes()
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kid": "test", "kty": "RSA", "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(s.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e),
	}}})
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	grantType := r.PostForm.Get("grant_type")
	secret := r.PostForm.Get("client_secret")
	if secret != ClientSecret && (grantType != "client_credentials" || secret == "") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized_client"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch grantType {
	case "authorization_code":
		g, ok := s.codes[r.PostForm.Get("code")]
		delete(s.codes, r.PostForm.Get("code"))
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		refresh := random()
		s.refresh[refresh] = g.user
		writeJSON(w, http.StatusOK, map[string]any{"id_token": s.idToken(g), "access_token": random(), "refresh_token": refresh, "token_type": "Bearer"})
	case "refresh_token":
		s.Refreshs++
		u, ok := s.refresh[r.PostForm.Get("refresh_token")]
		if current, exists := s.users[u.Subject]; !ok || !exists || current.Disabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		delete(s.refresh, r.PostForm.Get("refresh_token"))
		next := random()
		s.refresh[next] = u
		writeJSON(w, http.StatusOK, map[string]any{"access_token": random(), "refresh_token": next})
	case "client_credentials":
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "service-" + random(), "expires_in": 300})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

func (s *Server) idToken(g grant) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test","typ":"JWT"}`))
	now := time.Now()
	claims := map[string]any{
		"iss": s.Issuer(), "sub": g.user.Subject, "aud": ClientID, "azp": ClientID, "exp": now.Add(5 * time.Minute).Unix(),
		"iat": now.Unix(), "auth_time": g.authTime.Unix(), "nonce": g.nonce, "sid": "sid-" + g.user.Subject,
		"name": g.user.Name, "preferred_username": strings.ToLower(g.user.Subject),
	}
	if g.user.Administrator {
		claims["roles"] = []string{AdministratorRole}
	}
	payloadJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	digest := sha256.Sum256([]byte(header + "." + payload))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Logouts++
	delete(s.refresh, r.PostForm.Get("refresh_token"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) admin(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer service-") {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

func representation(u User) map[string]any {
	first, last, _ := strings.Cut(u.Name, " ")
	return map[string]any{"id": u.Subject, "username": strings.ToLower(u.Subject), "firstName": first, "lastName": last,
		"email": strings.ToLower(u.Subject) + "@example.test", "enabled": !u.Disabled}
}

func (s *Server) searchUsers(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	search := strings.ToLower(r.URL.Query().Get("search"))
	s.mu.Lock()
	defer s.mu.Unlock()
	found := []map[string]any{}
	for _, u := range s.users {
		if strings.Contains(strings.ToLower(u.Name), search) || strings.Contains(strings.ToLower(u.Subject), search) {
			found = append(found, representation(u))
		}
	}
	writeJSON(w, http.StatusOK, found)
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	subject := strings.TrimPrefix(r.URL.Path, "/admin"+realmPath+"/users/")
	s.mu.Lock()
	defer s.mu.Unlock()
	if person, found := strings.CutSuffix(subject, "/consents/"+DesktopClientID); found && r.Method == http.MethodDelete {
		delete(s.appSessions, person)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if person, found := strings.CutSuffix(subject, "/sessions"); found {
		s.SessionChecks++
		if _, ok := s.users[person]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "User not found"})
			return
		}
		sessions := []map[string]any{}
		for sid, open := range s.appSessions[person] {
			if open {
				sessions = append(sessions, map[string]any{"id": sid, "userId": person, "clients": map[string]string{"c-desktop": DesktopClientID}})
			}
		}
		writeJSON(w, http.StatusOK, sessions)
		return
	}
	u, ok := s.users[subject]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "User not found"})
		return
	}
	writeJSON(w, http.StatusOK, representation(u))
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	types := map[string]bool{}
	for _, t := range r.URL.Query()["type"] {
		types[t] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []map[string]any{}
	for _, e := range s.events {
		if len(types) == 0 || types[e.Type] {
			item := map[string]any{"id": e.ID, "time": e.Time.UnixMilli(), "type": e.Type, "clientId": e.ClientID, "realmId": "easycode",
				"details": map[string]string{"username": "never-recorded"}}
			if e.Subject != "" {
				item["userId"] = e.Subject
			}
			if e.Error != "" {
				item["error"] = e.Error
			}
			out = append(out, item)
		}
	}
	first, _ := strconv.Atoi(r.URL.Query().Get("first"))
	limit, err := strconv.Atoi(r.URL.Query().Get("max"))
	if err != nil || limit <= 0 {
		limit = 100
	}
	if first > len(out) {
		first = len(out)
	}
	writeJSON(w, http.StatusOK, out[first:min(len(out), first+limit)])
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// String keeps the server printable in failures without the key.
func (s *Server) String() string { return fmt.Sprintf("oidctest(%s)", s.URL) }
