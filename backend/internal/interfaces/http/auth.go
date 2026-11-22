package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

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

// authenticator checks the session cookie and the Origin of writes (ADR-12, ADR-19).
type authenticator struct {
	sessions     SessionUseCases
	publicOrigin string
}

// require wraps a handler that needs a session. Writes with the cookie must come from the
// platform origin (CSRF protection together with SameSite=Strict).
func (a authenticator) require(next AuthedHandler) HandlerFunc {
	return func(rc *fasthttp.RequestCtx) error {
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
