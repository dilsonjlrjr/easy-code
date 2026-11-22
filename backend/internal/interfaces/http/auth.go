package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/session"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// SessionCookie is the web session cookie (openapi securitySchemes.webSession).
const SessionCookie = "ec_session"

// SessionUseCases are the session use cases the handlers call.
type SessionUseCases interface {
	DevEnabled() bool
	IssueDev(ctx context.Context) (identity.Session, string, error)
	Authenticate(ctx context.Context, token string) (identity.Principal, identity.Session, string, error)
	Current(ctx context.Context, principal identity.Principal, session identity.Session) (session.CurrentUser, error)
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
		principal, current, renewed, err := a.sessions.Authenticate(RequestContext(rc), token)
		if err != nil {
			return err
		}
		if isWrite(rc) && a.publicOrigin != "" && string(rc.Request.Header.Peek("Origin")) != a.publicOrigin {
			return failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
		}
		rc.SetUserValue(authValue, authenticated{principal: principal, session: current})
		SetUserID(rc, principal.Subject)
		setSessionCookie(rc, renewed, current.ExpiresAt())
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
	cookie := fasthttp.AcquireCookie()
	defer fasthttp.ReleaseCookie(cookie)
	cookie.SetKey(SessionCookie)
	cookie.SetValue(value)
	cookie.SetPath("/")
	cookie.SetHTTPOnly(true)
	cookie.SetSecure(true)
	cookie.SetSameSite(fasthttp.CookieSameSiteStrictMode)
	if value == "" {
		cookie.SetExpire(fasthttp.CookieExpireDelete)
	} else {
		cookie.SetExpire(expires)
	}
	rc.Response.Header.SetCookie(cookie)
}
