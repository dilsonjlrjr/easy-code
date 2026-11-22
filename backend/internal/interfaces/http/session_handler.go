package http

import (
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/session"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

type organizationJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type currentUserJSON struct {
	ID                   string           `json:"id"`
	Name                 string           `json:"name"`
	OrganizationRoles    []string         `json:"organization_roles"`
	AccessMedium         string           `json:"access_medium"`
	SessionExpiresAt     time.Time        `json:"session_expires_at"`
	AuthenticatedAt      time.Time        `json:"authenticated_at"`
	CSRFToken            string           `json:"csrf_token"`
	LeadInAnyProject     bool             `json:"lead_in_any_project"`
	Organization         organizationJSON `json:"organization"`
	AttachmentLimitBytes int              `json:"attachment_limit_bytes"`
}

// startWebLogin redirects to the SSO with state, nonce and PKCE in the ec_login cookie (US-01 CA-1;
// reauthenticate=true is the step-up of ADR-19).
func (a *api) startWebLogin(rc *fasthttp.RequestCtx) error {
	reauthenticate := false
	if raw := string(rc.QueryArgs().Peek("reauthenticate")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return failure.Invalid(failure.FieldError{Field: "reauthenticate", Message: "invalid"})
		}
		reauthenticate = parsed
	}
	login, err := a.deps.Sessions.BeginLogin(string(rc.QueryArgs().Peek("callback")), reauthenticate)
	if err != nil {
		return err
	}
	setLoginFlowCookie(rc, login.FlowCookie, login.ExpiresAt)
	rc.Response.Header.Set(fasthttp.HeaderCacheControl, "no-store")
	rc.Response.Header.Set(headerLocation, login.RedirectURL)
	rc.SetStatusCode(fasthttp.StatusFound)
	return nil
}

// completeWebLogin checks the return of the SSO, creates the session and goes back to the page
// asked (or to /access-denied without a session, US-01 CA-2).
func (a *api) completeWebLogin(rc *fasthttp.RequestCtx) error {
	args := rc.QueryArgs()
	done, err := a.deps.Sessions.CompleteLogin(RequestContext(rc), session.Callback{
		Code: string(args.Peek("code")), State: string(args.Peek("state")), Error: string(args.Peek("error")),
		FlowCookie: string(rc.Request.Header.Cookie(LoginFlowCookie)),
	})
	setLoginFlowCookie(rc, "", time.Time{})
	if err != nil {
		if failure.From(err).Kind != failure.KindInvalidInput {
			return err
		}
		// The browser is on the api route: an invalid or expired return goes to the login page of
		// the web with the reason, never to a JSON answer (ESP-02 "Erro no retorno do SSO").
		rc.Response.Header.Set(fasthttp.HeaderCacheControl, "no-store")
		rc.Response.Header.Set(headerLocation, session.LoginFailedPath)
		rc.SetStatusCode(fasthttp.StatusFound)
		return nil
	}
	if done.Token != "" {
		SetUserID(rc, done.Session.Subject)
		setSessionCookie(rc, done.Token, done.Session.ExpiresAt())
	}
	rc.Response.Header.Set(fasthttp.HeaderCacheControl, "no-store")
	// Relative Location: the browser stays on the platform origin (callback is a same-origin path).
	rc.Response.Header.Set(headerLocation, done.Redirect)
	rc.SetStatusCode(fasthttp.StatusFound)
	return nil
}

// currentUser is getCurrentUser.
func (a *api) currentUser(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	current, err := a.deps.Sessions.Current(RequestContext(rc), principal, currentSession(rc))
	if err != nil {
		return err
	}
	roles := []string{}
	if principal.Administrator {
		roles = append(roles, identity.OrganizationAdministrator)
	}
	a.responder.JSON(rc, fasthttp.StatusOK, currentUserJSON{
		ID: principal.Subject, Name: principal.Name, OrganizationRoles: roles, AccessMedium: "web",
		SessionExpiresAt: current.Session.ExpiresAt(), AuthenticatedAt: current.Session.AuthenticatedAt,
		CSRFToken: current.Session.CSRFToken, LeadInAnyProject: current.LeadInAnyProject,
		Organization:         organizationJSON{ID: principal.OrganizationID, Name: principal.OrganizationName},
		AttachmentLimitBytes: a.deps.AttachmentLimitBytes,
	})
	return nil
}

// endSession is endSession: the session ends on the server (a copy of the cookie stops working at
// once) and on the SSO, and the cookie is deleted (US-01 CA-3).
func (a *api) endSession(rc *fasthttp.RequestCtx, _ identity.Principal) error {
	if err := a.deps.Sessions.End(RequestContext(rc), currentSession(rc)); err != nil {
		return err
	}
	setSessionCookie(rc, "", time.Time{})
	a.responder.NoContent(rc)
	return nil
}
