// Package session holds the web session use cases: issue, authenticate and end (ADR-12).
// Until Keycloak arrives (MARCO-2, US-01) the only way in is the fixed development identity
// of the marco1 profile; the OIDC flow will issue sessions through the same Service.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Failure codes of sessions (openapi Problem).
const (
	CodeSessionExpired = "session-expired"
)

// Users finds the people the sessions belong to.
type Users interface {
	// Principal returns the active user with that subject in the organization, or not-found.
	Principal(ctx context.Context, organizationID, subject string) (identity.Principal, error)
	// DevPrincipal returns the fixed development user (marco1), or not-found.
	DevPrincipal(ctx context.Context, subject string) (identity.Principal, error)
	// LeadInAnyProject reports whether the user is lead of some project (getCurrentUser).
	LeadInAnyProject(ctx context.Context, principal identity.Principal) (bool, error)
}

// Codec turns a session into the cookie value and back. A tampered value fails to open.
type Codec interface {
	Seal(session identity.Session) (string, error)
	Open(token string) (identity.Session, error)
}

// Service issues and checks web sessions.
type Service struct {
	users      Users
	codec      Codec
	devSubject string
	now        func() time.Time
}

// NewService builds the service. devSubject is the fixed user of the marco1 profile ("" disables it).
func NewService(users Users, codec Codec, devSubject string) *Service {
	return &Service{users: users, codec: codec, devSubject: devSubject, now: time.Now}
}

// DevEnabled reports whether the development identity is on (marco1 profile).
func (s *Service) DevEnabled() bool { return s.devSubject != "" }

// IssueDev opens a session for the fixed "dev" user (marco1 only; ADR-12, BACKLOG S-2).
func (s *Service) IssueDev(ctx context.Context) (identity.Session, string, error) {
	if !s.DevEnabled() {
		return identity.Session{}, "", failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	principal, err := s.users.DevPrincipal(ctx, s.devSubject)
	if err != nil {
		return identity.Session{}, "", err
	}
	now := s.now().UTC()
	session := identity.Session{
		Subject: principal.Subject, OrganizationID: principal.OrganizationID,
		AuthenticatedAt: now, IssuedAt: now, LastSeenAt: now, CSRFToken: newToken(),
	}
	token, err := s.codec.Seal(session)
	return session, token, err
}

// Authenticate opens the cookie, checks expiry and the user, and returns the session renewed
// for the activity (sliding 30 min, at most 8 h) with its new cookie value.
func (s *Service) Authenticate(ctx context.Context, token string) (identity.Principal, identity.Session, string, error) {
	unauthenticated := failure.New(failure.KindUnauthenticated, failure.CodeNotAuthenticated, nil)
	if token == "" {
		return identity.Principal{}, identity.Session{}, "", unauthenticated
	}
	session, err := s.codec.Open(token)
	if err != nil {
		return identity.Principal{}, identity.Session{}, "", unauthenticated
	}
	now := s.now().UTC()
	if session.Expired(now) {
		return identity.Principal{}, identity.Session{}, "", failure.New(failure.KindUnauthenticated, CodeSessionExpired, nil)
	}
	principal, err := s.users.Principal(ctx, session.OrganizationID, session.Subject)
	if err != nil {
		if failure.From(err).Kind == failure.KindNotFound {
			return identity.Principal{}, identity.Session{}, "", unauthenticated
		}
		return identity.Principal{}, identity.Session{}, "", err
	}
	session.LastSeenAt = now
	renewed, err := s.codec.Seal(session)
	if err != nil {
		return identity.Principal{}, identity.Session{}, "", err
	}
	return principal, session, renewed, nil
}

// CurrentUser is what getCurrentUser shows about the session.
type CurrentUser struct {
	Principal        identity.Principal
	Session          identity.Session
	LeadInAnyProject bool
}

// Current describes the authenticated user.
func (s *Service) Current(ctx context.Context, principal identity.Principal, session identity.Session) (CurrentUser, error) {
	lead, err := s.users.LeadInAnyProject(ctx, principal)
	if err != nil {
		return CurrentUser{}, err
	}
	return CurrentUser{Principal: principal, Session: session, LeadInAnyProject: lead}, nil
}

func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
