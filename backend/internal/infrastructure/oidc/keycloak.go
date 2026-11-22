// Package oidc talks to the Keycloak realm of the organization (ADR-12): the web login of the api
// as a confidential client (authorization code + PKCE, ID token checked against the realm keys),
// the refresh that confirms the SSO session every 60 s, the back channel logout, and the admin
// API read by service accounts (user directory for US-02, login events for PL-36, offboarding
// for D-31). No token, code or secret is ever logged or returned in an error message.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"easycode/internal/application/session"
)

// Settings configure the realm.
type Settings struct {
	// Issuer is the public issuer (browser redirects and the iss claim), e.g.
	// https://easycode.example/auth/realms/easycode.
	Issuer string
	// InternalURL is the same realm as reached from the containers (token, keys, logout and admin
	// API); empty means Issuer.
	InternalURL  string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// AdministratorRole is the realm role that makes a person organization administrator.
	AdministratorRole string
	Timeout           time.Duration
}

func (s Settings) internal() string {
	if s.InternalURL != "" {
		return strings.TrimRight(s.InternalURL, "/")
	}
	return strings.TrimRight(s.Issuer, "/")
}

// adminBase turns .../realms/<realm> into .../admin/realms/<realm>.
func (s Settings) adminBase() string {
	base := s.internal()
	if i := strings.LastIndex(base, "/realms/"); i >= 0 {
		return base[:i] + "/admin" + base[i:]
	}
	return base
}

// Provider implements session.IdentityProvider.
type Provider struct {
	settings Settings
	client   *http.Client
	keys     *keySet
	now      func() time.Time
}

var _ session.IdentityProvider = (*Provider)(nil)

// NewProvider builds the provider.
func NewProvider(settings Settings) *Provider {
	if settings.Timeout <= 0 {
		settings.Timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: settings.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Provider{
		settings: settings, client: client,
		keys: &keySet{url: settings.internal() + "/protocol/openid-connect/certs", client: client},
		now:  time.Now,
	}
}

// Issuer implements session.IdentityProvider.
func (p *Provider) Issuer() string { return strings.TrimRight(p.settings.Issuer, "/") }

// AuthorizationURL implements session.IdentityProvider.
func (p *Provider) AuthorizationURL(state, nonce, challenge string, reauthenticate bool) string {
	query := url.Values{
		"response_type": {"code"}, "client_id": {p.settings.ClientID}, "redirect_uri": {p.settings.RedirectURL},
		"scope": {"openid"}, "state": {state}, "nonce": {nonce},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	if reauthenticate {
		query.Set("prompt", "login")
		query.Set("max_age", "0")
	}
	return p.Issuer() + "/protocol/openid-connect/auth?" + query.Encode()
}

type tokenResponse struct {
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token"`
	Error        string `json:"error"`
}

// Exchange implements session.IdentityProvider.
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (session.Tokens, error) {
	body, status, err := p.token(ctx, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.settings.RedirectURL}, "code_verifier": {verifier},
	})
	if err != nil {
		return session.Tokens{}, err
	}
	if status != http.StatusOK || body.IDToken == "" {
		return session.Tokens{}, fmt.Errorf("code exchange refused (status %d)", status)
	}
	claims, err := verify(ctx, p.keys, body.IDToken, p.Issuer(), p.settings.ClientID, nonce, p.now())
	if err != nil {
		return session.Tokens{}, err
	}
	name := claims.Name
	if strings.TrimSpace(name) == "" {
		name = claims.PreferredUsername
	}
	if strings.TrimSpace(name) == "" {
		name = claims.Subject
	}
	out := session.Tokens{RefreshToken: body.RefreshToken, Claims: session.Claims{
		Subject: claims.Subject, Name: name, Nonce: claims.Nonce, SessionID: claims.SessionID,
		Administrator: p.settings.AdministratorRole != "" && claims.hasRole(p.settings.AdministratorRole),
	}}
	if claims.AuthTime > 0 {
		out.Claims.AuthTime = time.Unix(claims.AuthTime, 0).UTC()
	}
	return out, nil
}

// Refresh implements session.IdentityProvider.
func (p *Provider) Refresh(ctx context.Context, refreshToken string) (string, error) {
	body, status, err := p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusOK && body.RefreshToken != "":
		return body.RefreshToken, nil
	case status == http.StatusBadRequest || status == http.StatusUnauthorized:
		return "", session.ErrRevoked
	default:
		return "", fmt.Errorf("refresh: status %d", status)
	}
}

// Logout implements session.IdentityProvider.
func (p *Provider) Logout(ctx context.Context, refreshToken string) error {
	form := p.clientForm(url.Values{"refresh_token": {refreshToken}})
	resp, err := p.post(ctx, p.settings.internal()+"/protocol/openid-connect/logout", form)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("logout: status %d", resp.StatusCode)
	}
	return nil
}

func (p *Provider) clientForm(values url.Values) url.Values {
	values.Set("client_id", p.settings.ClientID)
	values.Set("client_secret", p.settings.ClientSecret)
	return values
}

func (p *Provider) token(ctx context.Context, values url.Values) (tokenResponse, int, error) {
	resp, err := p.post(ctx, p.settings.internal()+"/protocol/openid-connect/token", p.clientForm(values))
	if err != nil {
		return tokenResponse{}, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	var body tokenResponse
	_ = json.NewDecoder(limited(resp)).Decode(&body)
	return body, resp.StatusCode, nil
}

func (p *Provider) post(ctx context.Context, endpoint string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		// net/http errors carry the URL, never the form; still keep only the operation.
		return nil, fmt.Errorf("sso request failed: %w", errors.Unwrap(err))
	}
	return resp, nil
}

func limited(resp *http.Response) io.Reader { return io.LimitReader(resp.Body, 1<<20) }

// Admin reads the admin API of the realm with a service account (client credentials). The
// service account has only the read roles it needs (view-users, view-events).
type Admin struct {
	settings Settings
	client   *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewAdmin builds the admin client for the service account settings.ClientID.
func NewAdmin(settings Settings) *Admin {
	if settings.Timeout <= 0 {
		settings.Timeout = 10 * time.Second
	}
	return &Admin{settings: settings, client: &http.Client{Timeout: settings.Timeout}}
}

func (a *Admin) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Now().Before(a.expires) {
		return a.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {a.settings.ClientID}, "client_secret": {a.settings.ClientSecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.settings.internal()+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("service account token: %w", errors.Unwrap(err))
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(limited(resp)).Decode(&body); err != nil || resp.StatusCode != http.StatusOK || body.AccessToken == "" {
		return "", fmt.Errorf("service account token refused (status %d)", resp.StatusCode)
	}
	a.token = body.AccessToken
	a.expires = time.Now().Add(time.Duration(max(body.ExpiresIn-10, 5)) * time.Second)
	return a.token, nil
}

// errNotFound marks a 404 of the admin API.
var errNotFound = errors.New("not found in the realm")

func (a *Admin) get(ctx context.Context, path string, query url.Values, out any) error {
	token, err := a.accessToken(ctx)
	if err != nil {
		return err
	}
	endpoint := a.settings.adminBase() + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("admin api: %w", errors.Unwrap(err))
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("admin api %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(limited(resp)).Decode(out)
}

// DirectoryUser is a person of the realm directory: opaque id and display name only (no e-mail
// leaves this package, PERSIST minimization).
type DirectoryUser struct {
	Subject string
	Name    string
	Enabled bool
}

type adminUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Enabled   bool   `json:"enabled"`
}

func (u adminUser) directory() DirectoryUser {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	return DirectoryUser{Subject: u.ID, Name: name, Enabled: u.Enabled}
}

// SearchUsers looks people up by name, username or e-mail (searchUsers, US-02).
func (a *Admin) SearchUsers(ctx context.Context, search string, first, limit int) ([]DirectoryUser, error) {
	var found []adminUser
	err := a.get(ctx, "/users", url.Values{"search": {search}, "first": {fmt.Sprint(first)}, "max": {fmt.Sprint(limit)}, "briefRepresentation": {"true"}}, &found)
	if err != nil {
		return nil, err
	}
	users := make([]DirectoryUser, 0, len(found))
	for _, u := range found {
		users = append(users, u.directory())
	}
	return users, nil
}

// User reads one person; found is false when the realm no longer has it (offboarding, D-31).
func (a *Admin) User(ctx context.Context, subject string) (DirectoryUser, bool, error) {
	var u adminUser
	err := a.get(ctx, "/users/"+url.PathEscape(subject), nil, &u)
	if errors.Is(err, errNotFound) {
		return DirectoryUser{}, false, nil
	}
	if err != nil {
		return DirectoryUser{}, false, err
	}
	return u.directory(), true, nil
}

// LoginEvent is a user event of the realm (LOGIN, LOGIN_ERROR, LOGOUT, CODE_TO_TOKEN).
type LoginEvent struct {
	ID       string
	Type     string
	Time     time.Time
	ClientID string
	Subject  string
	Error    string
}

// Events reads the user events of the given types since a moment, oldest first.
func (a *Admin) Events(ctx context.Context, types []string, since time.Time, first, limit int) ([]LoginEvent, error) {
	query := url.Values{"type": types, "first": {fmt.Sprint(first)}, "max": {fmt.Sprint(limit)},
		"dateFrom": {since.UTC().Format("2006-01-02")}, "direction": {"asc"}}
	var raw []struct {
		ID       string `json:"id"`
		Time     int64  `json:"time"`
		Type     string `json:"type"`
		ClientID string `json:"clientId"`
		UserID   string `json:"userId"`
		Error    string `json:"error"`
	}
	if err := a.get(ctx, "/events", query, &raw); err != nil {
		return nil, err
	}
	events := make([]LoginEvent, 0, len(raw))
	for _, e := range raw {
		events = append(events, LoginEvent{ID: e.ID, Type: e.Type, Time: time.UnixMilli(e.Time).UTC(), ClientID: e.ClientID, Subject: e.UserID, Error: e.Error})
	}
	return events, nil
}
