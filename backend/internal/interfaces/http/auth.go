package http

import (
	"context"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/localclient"
	"easycode/internal/application/session"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Cookies of the web session (openapi securitySchemes.webSession and startWebLogin).
const (
	SessionCookie   = "ec_session"
	LoginFlowCookie = "ec_login"
)

// SessionUseCases are the session use cases the handlers call.
type SessionUseCases interface {
	BeginLogin(callback string, reauthenticate bool) (session.Login, error)
	CompleteLogin(ctx context.Context, callback session.Callback) (session.Completed, error)
	Authenticate(ctx context.Context, token string) (identity.Principal, identity.Session, error)
	Active(ctx context.Context, current identity.Session) (bool, error)
	End(ctx context.Context, current identity.Session) error
	Current(ctx context.Context, principal identity.Principal, current identity.Session) (session.CurrentUser, error)
}

type authenticated struct {
	principal identity.Principal
	session   identity.Session
}

const authValue userValueKey = 100

// AuthedHandler handles a request of an authenticated user.
type AuthedHandler func(rc *fasthttp.RequestCtx, principal identity.Principal) error

// LocalClientUseCases are the use cases of the desktop app (US-45, ADR-20).
type LocalClientUseCases interface {
	Configuration() localclient.Configuration
	Authenticate(ctx context.Context, raw string, client localclient.Client) (identity.Principal, identity.Session, error)
	Active(ctx context.Context, current identity.Session) (bool, error)
	End(ctx context.Context, current identity.Session) error
	List(ctx context.Context, principal identity.Principal) ([]localclient.Record, error)
	Unacknowledged(ctx context.Context, principal identity.Principal) (int, error)
	Revoke(ctx context.Context, principal identity.Principal, publicID string) error
	Acknowledge(ctx context.Context, principal identity.Principal, publicID string) error
}

// authenticator checks the session cookie and the Origin of writes (ADR-12, ADR-19), or the Bearer token
// of the desktop app, which is read-only (PD-07).
type authenticator struct {
	sessions     SessionUseCases
	local        LocalClientUseCases
	publicOrigin string
}

// bearerToken returns the token of "Authorization: Bearer <token>" ("" when absent).
func bearerToken(rc *fasthttp.RequestCtx) string {
	header := string(rc.Request.Header.Peek(fasthttp.HeaderAuthorization))
	if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

// clientAddress is the address of the caller: the first X-Forwarded-For hop set by the proxy, else the peer.
func clientAddress(rc *fasthttp.RequestCtx) string {
	if forwarded := string(rc.Request.Header.Peek("X-Forwarded-For")); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if first = strings.TrimSpace(first); first != "" {
			return first
		}
	}
	return rc.RemoteIP().String()
}

// localClientWrites are the only writes the token of the desktop app may make (openapi localClientToken).
var localClientWrites = map[string]bool{APIPrefix + "/session/logout": true}

// requireLocalClient authenticates the Bearer token of the desktop app; any write other than endSession is
// refused with 403 requires-web-session before the handler runs (PD-07, US-45 CA-6).
func (a authenticator) requireLocalClient(rc *fasthttp.RequestCtx, token string, next AuthedHandler) error {
	if a.local == nil {
		return failure.New(failure.KindUnauthenticated, failure.CodeNotAuthenticated, nil)
	}
	principal, current, err := a.local.Authenticate(RequestContext(rc), token, localclient.Client{
		UserAgent: string(rc.Request.Header.UserAgent()), IPAddress: clientAddress(rc),
	})
	if err != nil {
		return err
	}
	if isWrite(rc) && !localClientWrites[string(rc.Path())] {
		return failure.New(failure.KindForbidden, localclient.CodeRequiresWebSession, nil)
	}
	rc.SetUserValue(authValue, authenticated{principal: principal, session: current})
	setRequestContext(identity.WithSession(RequestContext(rc), current), rc)
	SetUserID(rc, principal.Subject)
	return next(rc, principal)
}

// require wraps a handler that needs a session. Writes with the cookie must come from the
// platform origin (CSRF protection together with SameSite=Strict).
func (a authenticator) require(next AuthedHandler) HandlerFunc {
	return func(rc *fasthttp.RequestCtx) error {
		if bearer := bearerToken(rc); bearer != "" {
			return a.requireLocalClient(rc, bearer, next)
		}
		token := string(rc.Request.Header.Cookie(SessionCookie))
		principal, current, err := a.sessions.Authenticate(RequestContext(rc), token)
		if err != nil {
			return err
		}
		if isWrite(rc) && a.publicOrigin != "" && string(rc.Request.Header.Peek("Origin")) != a.publicOrigin {
			return failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
		}
		rc.SetUserValue(authValue, authenticated{principal: principal, session: current})
		setRequestContext(identity.WithSession(RequestContext(rc), current), rc)
		SetUserID(rc, principal.Subject)
		setSessionCookie(rc, token, current.ExpiresAt())
		return next(rc, principal)
	}
}

func currentSession(rc *fasthttp.RequestCtx) identity.Session {
	auth, _ := rc.UserValue(authValue).(authenticated)
	return auth.session
}

// sessionActive reports whether the session of a stream still holds, web or desktop app (SSE heartbeat).
func (a *api) sessionActive(ctx context.Context, current identity.Session) (bool, error) {
	if current.LocalClient() {
		if a.deps.LocalClient == nil {
			return false, nil
		}
		return a.deps.LocalClient.Active(ctx, current)
	}
	return a.deps.Sessions.Active(ctx, current)
}

func isWrite(rc *fasthttp.RequestCtx) bool {
	method := string(rc.Method())
	return method != fasthttp.MethodGet && method != fasthttp.MethodHead && method != fasthttp.MethodOptions
}

func setSessionCookie(rc *fasthttp.RequestCtx, value string, expires time.Time) {
	setCookie(rc, SessionCookie, "/", fasthttp.CookieSameSiteStrictMode, value, expires)
}

// setLoginFlowCookie writes ec_login: SameSite=Lax, because the return from the SSO is a
// navigation from another site (ADR-12); only the session routes receive it.
func setLoginFlowCookie(rc *fasthttp.RequestCtx, value string, expires time.Time) {
	setCookie(rc, LoginFlowCookie, APIPrefix+"/session", fasthttp.CookieSameSiteLaxMode, value, expires)
}

func setCookie(rc *fasthttp.RequestCtx, name, path string, sameSite fasthttp.CookieSameSite, value string, expires time.Time) {
	cookie := fasthttp.AcquireCookie()
	defer fasthttp.ReleaseCookie(cookie)
	cookie.SetKey(name)
	cookie.SetValue(value)
	cookie.SetPath(path)
	cookie.SetHTTPOnly(true)
	cookie.SetSecure(true)
	cookie.SetSameSite(sameSite)
	if value == "" {
		cookie.SetExpire(fasthttp.CookieExpireDelete)
	} else {
		cookie.SetExpire(expires)
	}
	rc.Response.Header.SetCookie(cookie)
}
