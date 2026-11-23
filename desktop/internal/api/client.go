// Package api is the read-only REST client of easycode-desktop over /api/v1 (openapi.yaml). It only sends
// GET, plus endSession on sign out (the local client token is read-only, PD-07). Errors are decided by the
// Problem Details type, never by its text (ESP-28 ER-28-9).
package api

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
)

// BasePath is the api prefix under the server address.
const BasePath = "/api/v1"

// Problem types the app reacts to (openapi.yaml, Problem).
const (
	ProblemNotAuthenticated     = "urn:easycode:problem:not-authenticated"
	ProblemSessionExpired       = "urn:easycode:problem:session-expired"
	ProblemRevokedSession       = "urn:easycode:problem:revoked-session"
	ProblemNotFound             = "urn:easycode:problem:not-found"
	ProblemForbidden            = "urn:easycode:problem:forbidden"
	ProblemRequiresWebSession   = "urn:easycode:problem:requires-web-session"
	ProblemAccountWithoutAccess = "urn:easycode:problem:account-without-access"
)

// maxBody bounds what the app reads from one answer.
const maxBody = 8 << 20

// Problem is an RFC 9457 answer of the api.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func (p *Problem) Error() string { return fmt.Sprintf("api: %d %s", p.Status, p.Type) }

// ErrSessionEnded is returned when the api still answers 401 after one renewal: the caller signs out.
var ErrSessionEnded = errors.New("session ended")

// TokenSource gives the Bearer token and renews it after a 401 (session.Tokens).
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
	Renew(ctx context.Context, stale string) (string, error)
}

// Client calls the api of one server.
type Client struct {
	Server    string
	Tokens    TokenSource
	HTTP      *http.Client
	UserAgent string
	// Locale is sent as Accept-Language so messages come in the interface language.
	Locale func() string
}

// New returns a client with a 30 s timeout.
func New(server string, tokens TokenSource, userAgent string, locale func() string) *Client {
	return &Client{Server: server, Tokens: tokens, HTTP: &http.Client{Timeout: 30 * time.Second}, UserAgent: userAgent, Locale: locale}
}

// URL builds the address of an api path with a query.
func (c *Client) URL(path string, query url.Values) string {
	address := strings.TrimRight(c.Server, "/") + BasePath + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	return address
}

// GetPublic reads an operation that needs no token (getLocalClientConfiguration, verifyHealth).
func (c *Client) GetPublic(ctx context.Context, path string, out any) error {
	body, err := c.do(ctx, http.MethodGet, c.URL(path, nil), "")
	if err != nil {
		return err
	}
	return decode(body, out)
}

// Get reads an authenticated operation and returns the raw JSON body, so the interface shows the same
// data as the web without the app re-modelling every schema (US-46 CA-2).
func (c *Client) Get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.authenticated(ctx, http.MethodGet, c.URL(path, query))
}

// GetInto reads an authenticated operation into out.
func (c *Client) GetInto(ctx context.Context, path string, query url.Values, out any) error {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return err
	}
	return decode(body, out)
}

// EndSession calls endSession (POST /session/logout): the api revokes the Keycloak session tied to the
// token and closes its streams.
func (c *Client) EndSession(ctx context.Context) error {
	_, err := c.authenticated(ctx, http.MethodPost, c.URL("/session/logout", nil))
	return err
}

func (c *Client) authenticated(ctx context.Context, method, address string) (json.RawMessage, error) {
	token, err := c.Tokens.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	body, err := c.do(ctx, method, address, token)
	var problem *Problem
	if !errors.As(err, &problem) || problem.Status != http.StatusUnauthorized {
		return body, err
	}
	// cliente-local.md section 2: renew after a 401 and try once more.
	token, err = c.Tokens.Renew(ctx, token)
	if err != nil {
		return nil, err
	}
	body, err = c.do(ctx, method, address, token)
	if errors.As(err, &problem) && problem.Status == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: %s", ErrSessionEnded, problem.Type)
	}
	return body, err
}

func (c *Client) do(ctx context.Context, method, address, token string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, application/problem+json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if c.Locale != nil {
		req.Header.Set("Accept-Language", c.Locale())
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	if resp.StatusCode/100 == 2 {
		return body, nil
	}
	problem := &Problem{Status: resp.StatusCode}
	_ = json.Unmarshal(body, problem)
	problem.Status = resp.StatusCode
	if problem.Type == "" {
		problem.Type = fmt.Sprintf("about:blank#%d", resp.StatusCode)
	}
	return nil, problem
}

func decode(body []byte, out any) error {
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("api: invalid answer: %w", err)
	}
	return nil
}

// Health is the answer of verifyHealth.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// ProjectRef is the part of a project the Go side needs (names in notifications, last_sequence).
type ProjectRef struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	LastSequence *int64 `json:"last_sequence,omitempty"`
}

// ProjectsPage is the part of listProjects the Go side needs.
type ProjectsPage struct {
	Items      []ProjectRef `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}
