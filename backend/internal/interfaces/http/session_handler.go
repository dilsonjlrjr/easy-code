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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency,omitempty"` // budget currency of the organization (ISO 4217)
}

type currentUserJSON struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	OrganizationRoles []string   `json:"organization_roles"`
	AccessMedium      string     `json:"access_medium"`
	SessionExpiresAt  *time.Time `json:"session_expires_at,omitempty"`
	AuthenticatedAt   time.Time  `json:"authenticated_at"`
	CSRFToken         string     `json:"csrf_token,omitempty"`
	// LocalClientSessionsToReview counts the new sessions of the desktop app the person has not acknowledged
	// (web only; S-19 notice of every new session).
	LocalClientSessionsToReview *int             `json:"local_client_sessions_to_review,omitempty"`
	LeadInAnyProject            bool             `json:"lead_in_any_project"`
	Organization                organizationJSON `json:"organization"`
	AttachmentLimitBytes        int              `json:"attachment_limit_bytes"`
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
	body := currentUserJSON{
		ID: principal.Subject, Name: principal.Name, OrganizationRoles: roles, AccessMedium: string(identity.MediumWeb),
		AuthenticatedAt: current.Session.AuthenticatedAt, LeadInAnyProject: current.LeadInAnyProject,
		Organization:         organizationJSON{ID: principal.OrganizationID, Name: principal.OrganizationName, Currency: principal.OrganizationCurrency},
		AttachmentLimitBytes: a.deps.AttachmentLimitBytes,
	}
	if current.Session.LocalClient() {
		// The desktop app has no cookie, no CSRF token and no expiry of its own (the token renews itself).
		body.AccessMedium = string(identity.MediumLocalClient)
	} else {
		expires := current.Session.ExpiresAt()
		body.SessionExpiresAt, body.CSRFToken = &expires, current.Session.CSRFToken
		if a.deps.LocalClient != nil {
			count, err := a.deps.LocalClient.Unacknowledged(RequestContext(rc), principal)
			if err != nil {
				return err
			}
			body.LocalClientSessionsToReview = &count
		}
	}
	a.responder.JSON(rc, fasthttp.StatusOK, body)
	return nil
}

// endSession is endSession: the session ends on the server (a copy of the cookie stops working at
// once) and on the SSO, and the cookie is deleted (US-01 CA-3).
func (a *api) endSession(rc *fasthttp.RequestCtx, _ identity.Principal) error {
	if current := currentSession(rc); current.LocalClient() {
		// The desktop app: the session ends here at once; the app revokes its refresh token at the SSO.
		if err := a.deps.LocalClient.End(RequestContext(rc), current); err != nil {
			return err
		}
		a.responder.NoContent(rc)
		return nil
	}
	if err := a.deps.Sessions.End(RequestContext(rc), currentSession(rc)); err != nil {
		return err
	}
	setSessionCookie(rc, "", time.Time{})
	a.responder.NoContent(rc)
	return nil
}

type localClientConfigurationJSON struct {
	Issuer                      string   `json:"issuer"`
	ClientID                    string   `json:"client_id"`
	Audience                    string   `json:"audience"`
	Scopes                      []string `json:"scopes"`
	DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint"`
	TokenEndpoint               string   `json:"endpoint_token"`
	RevocationEndpoint          string   `json:"revocation_endpoint"`
	CodeValiditySeconds         int      `json:"code_validity_s"`
}

// getLocalClientConfiguration gives the desktop app the device flow parameters (US-45 CA-1; no session).
func (a *api) getLocalClientConfiguration(rc *fasthttp.RequestCtx) error {
	c := a.deps.LocalClient.Configuration()
	a.responder.JSON(rc, fasthttp.StatusOK, localClientConfigurationJSON{
		Issuer: c.Issuer, ClientID: c.ClientID, Audience: c.Audience, Scopes: c.Scopes,
		DeviceAuthorizationEndpoint: c.DeviceAuthorizationEndpoint, TokenEndpoint: c.TokenEndpoint,
		RevocationEndpoint: c.RevocationEndpoint, CodeValiditySeconds: c.CodeValiditySeconds,
	})
	return nil
}

type localClientSessionJSON struct {
	ID           string    `json:"id"`
	Device       string    `json:"device"`
	StartedAt    time.Time `json:"started_at"`
	LastUseAt    time.Time `json:"last_use_at"`
	IPAddress    string    `json:"ip_address,omitempty"`
	Acknowledged bool      `json:"acknowledged"`
	Current      bool      `json:"current"`
}

// listLocalClientSessions is Minha conta › Cliente local: the open sessions of the person, never a token
// (US-45 CA-5, RN-3).
func (a *api) listLocalClientSessions(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	records, err := a.deps.LocalClient.List(RequestContext(rc), principal)
	if err != nil {
		return err
	}
	current := currentSession(rc)
	items := make([]localClientSessionJSON, 0, len(records))
	for _, r := range records {
		items = append(items, localClientSessionJSON{
			ID: r.PublicID, Device: r.Device, StartedAt: r.StartedAt, LastUseAt: r.LastUsedAt, IPAddress: r.IPAddress,
			Acknowledged: r.Acknowledged, Current: current.LocalClient() && current.ID == r.ID,
		})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items})
	return nil
}

// revokeLocalClientSession ends a session of the app of the person (US-45 CA-2, CA-5).
func (a *api) revokeLocalClientSession(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	id := PathParam(rc, "sessionId")
	if !uuidPattern.MatchString(id) {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err := a.deps.LocalClient.Revoke(RequestContext(rc), principal, id); err != nil {
		return err
	}
	a.responder.NoContent(rc)
	return nil
}

// acknowledgeLocalClientSession records that the person saw the notice of a new session of the app (S-19).
func (a *api) acknowledgeLocalClientSession(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	id := PathParam(rc, "sessionId")
	if !uuidPattern.MatchString(id) {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err := a.deps.LocalClient.Acknowledge(RequestContext(rc), principal, id); err != nil {
		return err
	}
	a.responder.NoContent(rc)
	return nil
}
