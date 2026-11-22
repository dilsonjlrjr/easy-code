package http

import (
	"time"

	"github.com/valyala/fasthttp"

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

// devSession issues the cookie of the fixed "dev" user. The route exists only in the marco1
// profile and is not part of openapi.yaml (ADR-12, ARQ section 9).
func (a *api) devSession(rc *fasthttp.RequestCtx) error {
	current, token, err := a.deps.Sessions.IssueDev(RequestContext(rc))
	if err != nil {
		return err
	}
	SetUserID(rc, current.Subject)
	setSessionCookie(rc, token, current.ExpiresAt())
	a.responder.NoContent(rc)
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

// endSession is endSession: the cookie is deleted. The marco1 cookie is stateless, so a copy
// kept elsewhere stays valid until it expires; the server-side session of MARCO-2 revokes it.
func (a *api) endSession(rc *fasthttp.RequestCtx, _ identity.Principal) error {
	setSessionCookie(rc, "", time.Time{})
	a.responder.NoContent(rc)
	return nil
}
