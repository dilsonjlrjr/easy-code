// Package installer is the HTTP layer of easycode-installer (US-55, ESP-31, ADR-22): a small JSON API
// under /api for the wizard and the static files of its page. Every route but /api/state needs the
// one-time installation token in the X-Install-Token header. Bodies are never logged.
package installer

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/install"
)

// TokenHeader carries the one-time installation token.
const TokenHeader = "X-Install-Token"

// MaxBodyBytes bounds a request (a plan carries the skill packages).
const MaxBodyBytes = 64 << 20

// UseCases is what the HTTP layer calls (install.Service).
type UseCases interface {
	State() install.Status
	CheckDatabase(ctx context.Context, database install.Database) (install.DatabaseProbe, error)
	CheckOrganization(organization install.Organization) error
	CheckSSO(ctx context.Context, sso install.SSO, organization install.Organization) (install.SSOProbe, error)
	SearchUsers(ctx context.Context, sso install.SSO, query string) ([]install.DirectoryUser, error)
	CheckPerson(ctx context.Context, sso install.SSO, person install.Person, field string) error
	NativeSkills() ([]install.SkillSummary, error)
	CheckSkill(pkg install.SkillPackage) (install.SkillSummary, error)
	Apply(ctx context.Context, plan install.Plan) error
	Retry(ctx context.Context) error
}

// Defaults prefill the wizard (the bundled PostgreSQL and Keycloak of install/compose.yaml).
type Defaults struct {
	Database     install.Database     `json:"database"`
	SSO          install.SSO          `json:"sso"`
	Organization install.Organization `json:"organization"`
	Bundled      bool                 `json:"bundled"`
}

// Settings wire the handler.
type Settings struct {
	UseCases UseCases
	Logger   *slog.Logger
	Token    string
	Version  string
	Defaults Defaults
	// WebDir holds the built page (installer.html and assets/); "" serves only the API.
	WebDir string
	// ConfigWritable checks the configuration volume (step 1).
	ConfigWritable func() error
	// Finish is called once the person closes the wizard after the lock; the process then stops.
	Finish func()
	// Timeout bounds each check against the database or the SSO.
	Timeout time.Duration
}

type handler struct {
	s      Settings
	static fasthttp.RequestHandler
}

// NewHandler builds the request handler.
func NewHandler(s Settings) fasthttp.RequestHandler {
	if s.Timeout <= 0 {
		s.Timeout = 20 * time.Second
	}
	h := &handler{s: s}
	if s.WebDir != "" {
		fs := &fasthttp.FS{Root: s.WebDir, IndexNames: []string{"installer.html"}, Compress: false, AcceptByteRange: false,
			PathNotFound: func(rc *fasthttp.RequestCtx) { rc.SendFile(filepath.Join(s.WebDir, "installer.html")) }}
		h.static = fs.NewRequestHandler()
	}
	return h.serve
}

func (h *handler) serve(rc *fasthttp.RequestCtx) {
	start := time.Now()
	rc.Response.Header.Set("X-Content-Type-Options", "nosniff")
	rc.Response.Header.Set("X-Frame-Options", "DENY")
	rc.Response.Header.Set("Referrer-Policy", "no-referrer")
	rc.Response.Header.Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	path := string(rc.Path())
	switch {
	case strings.HasPrefix(path, "/api/"):
		rc.Response.Header.Set("Cache-Control", "no-store")
		h.api(rc, path)
	case h.static != nil && (rc.IsGet() || rc.IsHead()):
		if path == "/" || !strings.Contains(filepath.Base(path), ".") {
			rc.Response.Header.Set("Cache-Control", "no-cache")
		}
		h.static(rc)
	default:
		rc.SetStatusCode(fasthttp.StatusNotFound)
	}
	h.s.Logger.Info("request", slog.String("method", string(rc.Method())), slog.String("path", path),
		slog.Int("status", rc.Response.StatusCode()), slog.Duration("duration", time.Since(start)))
}

func (h *handler) authorized(rc *fasthttp.RequestCtx) bool {
	given := rc.Request.Header.Peek(TokenHeader)
	return len(given) > 0 && subtle.ConstantTimeCompare(given, []byte(h.s.Token)) == 1
}

func (h *handler) api(rc *fasthttp.RequestCtx, path string) {
	route := string(rc.Method()) + " " + path
	if route == "GET /api/state" {
		state := h.s.UseCases.State()
		h.json(rc, fasthttp.StatusOK, map[string]any{"installed": state.Installed, "version": h.s.Version})
		return
	}
	if !h.authorized(rc) {
		h.problem(rc, fasthttp.StatusUnauthorized, "invalid_token", nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.s.Timeout)
	defer cancel()
	switch route {
	case "POST /api/token":
		rc.SetStatusCode(fasthttp.StatusNoContent)
	case "GET /api/defaults":
		h.json(rc, fasthttp.StatusOK, h.s.Defaults)
	case "POST /api/check/environment":
		h.checkEnvironment(rc)
	case "POST /api/check/database":
		var in struct {
			Database install.Database `json:"database"`
		}
		if h.decode(rc, &in) {
			probe, err := h.s.UseCases.CheckDatabase(ctx, in.Database)
			h.result(rc, probe, err)
		}
	case "POST /api/check/organization":
		var in struct {
			Organization install.Organization `json:"organization"`
		}
		if h.decode(rc, &in) {
			h.result(rc, map[string]bool{"ok": true}, h.s.UseCases.CheckOrganization(in.Organization))
		}
	case "POST /api/check/sso":
		var in struct {
			SSO          install.SSO          `json:"sso"`
			Organization install.Organization `json:"organization"`
		}
		if h.decode(rc, &in) {
			probe, err := h.s.UseCases.CheckSSO(ctx, in.SSO, in.Organization)
			h.result(rc, probe, err)
		}
	case "POST /api/sso/users":
		var in struct {
			SSO   install.SSO `json:"sso"`
			Query string      `json:"query"`
		}
		if h.decode(rc, &in) {
			users, err := h.s.UseCases.SearchUsers(ctx, in.SSO, in.Query)
			h.result(rc, map[string]any{"users": users}, err)
		}
	case "POST /api/check/person":
		var in struct {
			SSO    install.SSO    `json:"sso"`
			Person install.Person `json:"person"`
			Field  string         `json:"field"`
		}
		if h.decode(rc, &in) {
			h.result(rc, map[string]bool{"ok": true}, h.s.UseCases.CheckPerson(ctx, in.SSO, in.Person, in.Field))
		}
	case "GET /api/skills/native":
		skills, err := h.s.UseCases.NativeSkills()
		h.result(rc, map[string]any{"skills": skills}, err)
	case "POST /api/check/skill":
		var in install.SkillPackage
		if h.decode(rc, &in) {
			summary, err := h.s.UseCases.CheckSkill(in)
			h.result(rc, summary, err)
		}
	case "POST /api/install":
		var in struct {
			Plan install.Plan `json:"plan"`
		}
		if h.decode(rc, &in) {
			h.result(rc, h.s.UseCases.State(), h.s.UseCases.Apply(context.Background(), in.Plan))
		}
	case "GET /api/install":
		h.json(rc, fasthttp.StatusOK, h.s.UseCases.State())
	case "POST /api/install/retry":
		h.result(rc, h.s.UseCases.State(), h.s.UseCases.Retry(context.Background()))
	case "POST /api/finish":
		if !h.s.UseCases.State().Installed {
			h.problem(rc, fasthttp.StatusConflict, "not_installed", nil)
			return
		}
		rc.SetStatusCode(fasthttp.StatusNoContent)
		if h.s.Finish != nil {
			go h.s.Finish()
		}
	default:
		h.problem(rc, fasthttp.StatusNotFound, "not_found", nil)
	}
}

func (h *handler) checkEnvironment(rc *fasthttp.RequestCtx) {
	checks := []map[string]any{{"name": "config_volume", "ok": true}}
	if h.s.ConfigWritable != nil {
		if err := h.s.ConfigWritable(); err != nil {
			checks[0]["ok"] = false
		}
	}
	hostname, _ := os.Hostname()
	h.json(rc, fasthttp.StatusOK, map[string]any{"checks": checks, "version": h.s.Version, "host": hostname})
}

func (h *handler) decode(rc *fasthttp.RequestCtx, out any) bool {
	if err := json.Unmarshal(rc.PostBody(), out); err != nil {
		h.problem(rc, fasthttp.StatusBadRequest, "invalid_json", nil)
		return false
	}
	return true
}

// result writes value, or the problem of err. A failed check may come with what it found (probe),
// so the value goes along with the problem.
func (h *handler) result(rc *fasthttp.RequestCtx, value any, err error) {
	var invalid install.Invalids
	switch {
	case err == nil:
		h.json(rc, fasthttp.StatusOK, value)
	case errors.As(err, &invalid):
		h.json(rc, fasthttp.StatusUnprocessableEntity, map[string]any{"code": "invalid", "errors": invalid, "found": value})
	case errors.Is(err, install.ErrInstalled):
		h.problem(rc, fasthttp.StatusConflict, "installed", nil)
	case errors.Is(err, install.ErrApplying):
		h.problem(rc, fasthttp.StatusConflict, "applying", nil)
	case errors.Is(err, context.DeadlineExceeded):
		h.problem(rc, fasthttp.StatusGatewayTimeout, "timeout", nil)
	default:
		// Adapter errors carry no credential (they are built without the input values).
		h.s.Logger.Warn("check failed", slog.String("reason", err.Error()))
		h.problem(rc, fasthttp.StatusBadGateway, "failed", map[string]any{"message": err.Error()})
	}
}

func (h *handler) problem(rc *fasthttp.RequestCtx, status int, code string, extra map[string]any) {
	body := map[string]any{"code": code}
	for k, v := range extra {
		body[k] = v
	}
	h.json(rc, status, body)
}

func (h *handler) json(rc *fasthttp.RequestCtx, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		status, encoded = fasthttp.StatusInternalServerError, []byte(`{"code":"encoding"}`)
	}
	rc.SetContentType("application/json")
	rc.SetStatusCode(status)
	rc.SetBody(encoded)
}
