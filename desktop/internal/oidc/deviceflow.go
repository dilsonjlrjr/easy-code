// Package oidc is the OAuth 2.0 device authorization grant (RFC 8628) of easycode-desktop against Keycloak,
// with the public client easycode-desktop and the scope easycode-read (ADR-12, ADR-20, cliente-local.md
// section 2). The api only publishes the parameters (getLocalClientConfiguration) and validates the token.
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
	"time"

	"easycode-desktop/internal/config"
)

// DeviceGrantType is the grant_type of the token poll (RFC 8628 section 3.4).
const DeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

var (
	// ErrExpired means the user code was not approved in time; no token was issued (US-45 CA-4).
	ErrExpired = errors.New("device code expired")
	// ErrDenied means the user refused the request in the browser.
	ErrDenied = errors.New("device authorization denied")
	// ErrRefreshRejected means the refresh token no longer works: session revoked on the web, account
	// disabled or session past its limit (US-45 CA-2).
	ErrRefreshRejected = errors.New("refresh token rejected")
	// ErrInsecureEndpoint means the server announced an endpoint the app refuses to send credentials to.
	ErrInsecureEndpoint = errors.New("insecure identity endpoint")
)

// Configuration is LocalClientConfiguration of openapi.yaml.
type Configuration struct {
	Issuer                      string   `json:"issuer"`
	ClientID                    string   `json:"client_id"`
	Audience                    string   `json:"audience"`
	Scopes                      []string `json:"scopes"`
	DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint"`
	TokenEndpoint               string   `json:"endpoint_token"`
	RevocationEndpoint          string   `json:"revocation_endpoint,omitempty"`
	CodeValiditySeconds         int      `json:"code_validity_s,omitempty"`
}

// Validate refuses a configuration that would send the device code or a token in clear text to a remote
// host or that misses a required field.
func (c Configuration) Validate() error {
	if c.ClientID == "" || len(c.Scopes) == 0 {
		return fmt.Errorf("%w: missing client or scopes", ErrInsecureEndpoint)
	}
	for _, endpoint := range []string{c.DeviceAuthorizationEndpoint, c.TokenEndpoint} {
		if err := checkEndpoint(endpoint); err != nil {
			return err
		}
	}
	if c.RevocationEndpoint != "" {
		return checkEndpoint(c.RevocationEndpoint)
	}
	return nil
}

func checkEndpoint(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%w: %q", ErrInsecureEndpoint, raw)
	}
	if parsed.Scheme == "https" || (parsed.Scheme == "http" && config.IsLoopback(parsed.Hostname())) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInsecureEndpoint, raw)
}

// DeviceAuthorization is the answer of the device authorization endpoint.
type DeviceAuthorization struct {
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int       `json:"expires_in"`
	Interval                int       `json:"interval,omitempty"`
	ExpiresAt               time.Time `json:"-"`
}

// Tokens is the result of a successful grant. The access token stays in memory; the refresh token goes to
// the OS keychain.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Client talks to the identity provider.
type Client struct {
	Config Configuration
	HTTP   *http.Client
	Now    func() time.Time
	// Wait sleeps for d or until ctx ends (replaced in tests).
	Wait func(ctx context.Context, d time.Duration) error
	// UserAgent is sent on every request (cliente-local.md section 6).
	UserAgent string
}

// NewClient returns a client with real time and a 30 s HTTP timeout.
func NewClient(cfg Configuration, userAgent string) *Client {
	return &Client{
		Config:    cfg,
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		Now:       time.Now,
		Wait:      wait,
		UserAgent: userAgent,
	}
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Start asks for a device code and a user code.
func (c *Client) Start(ctx context.Context) (DeviceAuthorization, error) {
	form := url.Values{"client_id": {c.Config.ClientID}, "scope": {strings.Join(c.Config.Scopes, " ")}}
	var auth DeviceAuthorization
	if status, oauthErr, err := c.post(ctx, c.Config.DeviceAuthorizationEndpoint, form, &auth); err != nil {
		return DeviceAuthorization{}, err
	} else if status != http.StatusOK {
		return DeviceAuthorization{}, fmt.Errorf("device authorization: status %d %s", status, oauthErr.Code)
	}
	if auth.DeviceCode == "" || auth.UserCode == "" || auth.VerificationURI == "" {
		return DeviceAuthorization{}, errors.New("device authorization: incomplete answer")
	}
	if auth.ExpiresIn <= 0 {
		auth.ExpiresIn = c.Config.CodeValiditySeconds
	}
	if auth.ExpiresIn <= 0 {
		auth.ExpiresIn = 600
	}
	if auth.Interval <= 0 {
		auth.Interval = 5
	}
	auth.ExpiresAt = c.Now().Add(time.Duration(auth.ExpiresIn) * time.Second)
	return auth, nil
}

// Poll asks for the tokens every interval until the user approves, refuses or the code expires
// (cliente-local.md section 2, step 4).
func (c *Client) Poll(ctx context.Context, auth DeviceAuthorization) (Tokens, error) {
	interval := time.Duration(auth.Interval) * time.Second
	form := url.Values{
		"grant_type":  {DeviceGrantType},
		"device_code": {auth.DeviceCode},
		"client_id":   {c.Config.ClientID},
	}
	for {
		if !c.Now().Before(auth.ExpiresAt) {
			return Tokens{}, ErrExpired
		}
		if err := c.Wait(ctx, interval); err != nil {
			return Tokens{}, err
		}
		tokens, oauthErr, err := c.token(ctx, form)
		if err != nil {
			return Tokens{}, err
		}
		switch oauthErr.Code {
		case "":
			return tokens, nil
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return Tokens{}, ErrExpired
		case "access_denied":
			return Tokens{}, ErrDenied
		default:
			return Tokens{}, fmt.Errorf("device token: %s", oauthErr.Code)
		}
	}
}

// Refresh exchanges the refresh token for new tokens. Keycloak rotates the refresh token, so the caller
// stores the one returned.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.Config.ClientID},
	}
	tokens, oauthErr, err := c.token(ctx, form)
	if err != nil {
		return Tokens{}, err
	}
	switch oauthErr.Code {
	case "":
		if tokens.RefreshToken == "" {
			tokens.RefreshToken = refreshToken
		}
		return tokens, nil
	case "invalid_grant", "unauthorized_client", "invalid_client":
		return Tokens{}, ErrRefreshRejected
	default:
		return Tokens{}, fmt.Errorf("refresh: %s", oauthErr.Code)
	}
}

// Revoke revokes the refresh token at the identity provider (RFC 7009), when it publishes the endpoint.
// It complements endSession on sign out; failures are ignored by the caller.
func (c *Client) Revoke(ctx context.Context, refreshToken string) error {
	if c.Config.RevocationEndpoint == "" {
		return nil
	}
	form := url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {c.Config.ClientID},
	}
	status, _, err := c.post(ctx, c.Config.RevocationEndpoint, form, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("revoke: status %d", status)
	}
	return nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type oauthError struct {
	Code string `json:"error"`
}

func (c *Client) token(ctx context.Context, form url.Values) (Tokens, oauthError, error) {
	var answer tokenResponse
	status, oauthErr, err := c.post(ctx, c.Config.TokenEndpoint, form, &answer)
	if err != nil {
		return Tokens{}, oauthError{}, err
	}
	if status != http.StatusOK {
		if oauthErr.Code == "" {
			return Tokens{}, oauthError{}, fmt.Errorf("token endpoint: status %d", status)
		}
		return Tokens{}, oauthErr, nil
	}
	if answer.AccessToken == "" {
		return Tokens{}, oauthError{}, errors.New("token endpoint: no access token")
	}
	return Tokens{
		AccessToken:  answer.AccessToken,
		RefreshToken: answer.RefreshToken,
		ExpiresAt:    c.Now().Add(time.Duration(answer.ExpiresIn) * time.Second),
	}, oauthError{}, nil
}

// post sends a form; on 2xx it decodes into out, otherwise it decodes the OAuth error body.
func (c *Client) post(ctx context.Context, endpoint string, form url.Values, out any) (int, oauthError, error) {
	if err := checkEndpoint(endpoint); err != nil {
		return 0, oauthError{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, oauthError{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, oauthError{}, fmt.Errorf("identity provider: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, oauthError{}, fmt.Errorf("identity provider: %w", err)
	}
	if resp.StatusCode/100 == 2 {
		if out != nil && len(body) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return 0, oauthError{}, fmt.Errorf("identity provider: invalid answer: %w", err)
			}
		}
		return resp.StatusCode, oauthError{}, nil
	}
	var oauthErr oauthError
	_ = json.Unmarshal(body, &oauthErr)
	return resp.StatusCode, oauthErr, nil
}
