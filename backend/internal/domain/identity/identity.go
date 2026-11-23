// Package identity holds who is acting: the authenticated person, the organization and the
// roles that authorize each action (ADR-12, RNF-04). Names and e-mails never go to events or
// audit records: only the opaque subject does.
package identity

import (
	"context"
	"time"
)

// Role is a project role (project_members.role).
type Role string

// Project roles (US-02). The administrator is an organization role, not a project one.
const (
	RoleLead     Role = "lead"
	RoleApprover Role = "approver"
	RoleMember   Role = "member"
)

// OrganizationAdministrator is the only organization role (users.administrator).
const OrganizationAdministrator = "administrator"

// Principal is the authenticated person.
type Principal struct {
	Subject          string // OIDC sub: opaque id used in events and audit records
	Name             string // display name, only for screens
	OrganizationID   string // organizations.public_id
	OrganizationName string
	// OrganizationCurrency is the budget currency of the organization (policy.project_budget currency).
	OrganizationCurrency string
	Administrator        bool
}

// Medium is how the session reached the api (CurrentUser.access_medium): the web cookie or the
// read-only token of the desktop app (ADR-20).
type Medium string

// Access media.
const (
	MediumWeb         Medium = "web"
	MediumLocalClient Medium = "local_client"
)

// Session is a web session (cookie ec_session), stored on the server (ADR-12), or a session of the
// desktop app (Bearer token of the easycode-desktop client, local_client_sessions; ADR-20).
type Session struct {
	ID              int64  // internal key of web_sessions or local_client_sessions (by Medium)
	Medium          Medium // empty means web
	PublicID        string // public id of a local client session (listLocalClientSessions); empty on the web
	Subject         string
	OrganizationID  string
	AuthenticatedAt time.Time // last authentication; critical decisions need it recent (ADR-19)
	IssuedAt        time.Time
	LastSeenAt      time.Time
	CSRFToken       string
}

// LocalClient reports whether the session is the read-only token of the desktop app (PD-07).
func (s Session) LocalClient() bool { return s.Medium == MediumLocalClient }

// Session lifetimes of RNF-06 and ADR-19.
const (
	SessionIdle     = 30 * time.Minute
	SessionLifetime = 8 * time.Hour
	// IDPCheckEvery is how often the api confirms with the SSO that the session still holds
	// (revoked token or disabled account refused within 60 s, RNF-06).
	IDPCheckEvery = 60 * time.Second
	// CriticalDecisionWindow is the maximum age of the last SSO authentication for a critical
	// decision (approve gate, accept risk; ADR-19).
	CriticalDecisionWindow = 10 * time.Minute
)

// RecentlyAuthenticated reports whether the last SSO authentication allows a critical decision.
func (s Session) RecentlyAuthenticated(now time.Time) bool {
	return !s.AuthenticatedAt.IsZero() && now.Sub(s.AuthenticatedAt) <= CriticalDecisionWindow
}

// ExpiresAt is when the session ends without activity.
func (s Session) ExpiresAt() time.Time {
	idle := s.LastSeenAt.Add(SessionIdle)
	absolute := s.IssuedAt.Add(SessionLifetime)
	if idle.Before(absolute) {
		return idle
	}
	return absolute
}

// Expired reports whether the session is over at now.
func (s Session) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt())
}

// Has reports whether roles contains any of wanted.
func Has(roles []Role, wanted ...Role) bool {
	for _, role := range roles {
		for _, candidate := range wanted {
			if role == candidate {
				return true
			}
		}
	}
	return false
}

type sessionKey struct{}

// WithSession carries the web session of the request in ctx (once-per-session rules, such as
// project.access_as_administrator, RNF-13).
func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFrom returns the session of the request, if any.
func SessionFrom(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(Session)
	return s, ok
}
