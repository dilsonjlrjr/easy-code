package http

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	appcontent "easycode/internal/application/content"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/infrastructure/logging"
)

// ContentURLUseCases issue the signed URLs of the content origin (getArtifactContentUrl, ADR-19).
type ContentURLUseCases interface {
	Issue(ctx context.Context, principal identity.Principal, projectID, code string, version int, page string) (appcontent.URL, error)
}

type contentURLJSON struct {
	URL       string    `json:"url"`
	Prefix    string    `json:"prefix"`
	ExpiresAt time.Time `json:"expires_at"`
}

// getArtifactContentURL (getArtifactContentUrl) is the URL of a prototype page of the version (US-21, ESP-11, ER-11-1, ER-11-8).
func (a *api) getArtifactContentURL(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := a.artifactPath(rc)
	if err != nil {
		return err
	}
	version, err := strconv.Atoi(PathParam(rc, "version"))
	if err != nil || version < 1 {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	issued, err := a.deps.ContentURLs.Issue(RequestContext(rc), principal, projectID, code, version, string(rc.QueryArgs().Peek("path")))
	if err != nil {
		return err
	}
	rc.Response.Header.Set("Cache-Control", "no-store")
	a.responder.JSON(rc, fasthttp.StatusOK, contentURLJSON{URL: issued.URL, Prefix: issued.Prefix, ExpiresAt: issued.ExpiresAt})
	return nil
}

// ContentPages open the files of a signed URL (the content origin side of appcontent.Service).
type ContentPages interface {
	Open(ctx context.Context, token, rel string) (appcontent.Page, error)
}

// ContentSecurityPolicy is the closed CSP of the content origin (ER-11-12, ADR-19, PL-53).
const ContentSecurityPolicy = "sandbox allow-scripts; default-src 'self'; script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'none'; form-action 'none'"

// NewContentOriginHandler serves the content origin content.<domain> (ADR-19): only GET and HEAD of
// /c/<token>/<path>, no route to /api, no cookie read or set, the closed CSP on every answer. The token is
// a credential: the access log never writes the path.
func NewContentOriginHandler(pages ContentPages, logger *slog.Logger) fasthttp.RequestHandler {
	return func(rc *fasthttp.RequestCtx) {
		started := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("content handler panic", slog.String("stack", string(debug.Stack())))
				contentRefusal(rc, fasthttp.StatusInternalServerError)
			}
			logger.Debug("content request", slog.String("method", string(rc.Method())), slog.Int("status", rc.Response.StatusCode()),
				slog.Duration("duration", time.Since(started)))
		}()
		contentHeaders(rc)
		if !rc.IsGet() && !rc.IsHead() {
			rc.Response.Header.Set("Allow", "GET, HEAD")
			contentRefusal(rc, fasthttp.StatusMethodNotAllowed)
			return
		}
		rest, ok := strings.CutPrefix(string(rc.Path()), "/c/")
		token, rel, found := strings.Cut(rest, "/")
		if !ok || !found || token == "" {
			contentRefusal(rc, fasthttp.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(RequestContext(rc), 30*time.Second)
		defer cancel()
		page, err := pages.Open(ctx, token, rel)
		if err != nil {
			if !errors.Is(err, appcontent.ErrNoFile) {
				logger.Error("content file failed", logging.ErrorAttr(err))
			}
			contentRefusal(rc, fasthttp.StatusNotFound)
			return
		}
		rc.Response.Header.SetContentType(page.MediaType)
		rc.SetStatusCode(fasthttp.StatusOK)
		rc.SetBody(page.Body)
	}
}

func contentHeaders(rc *fasthttp.RequestCtx) {
	h := &rc.Response.Header
	h.Set("Content-Security-Policy", ContentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
}

func contentRefusal(rc *fasthttp.RequestCtx, status int) {
	rc.Response.Header.SetContentType("text/plain; charset=utf-8")
	rc.SetStatusCode(status)
	rc.SetBodyString(fasthttp.StatusMessage(status) + "\n")
}
