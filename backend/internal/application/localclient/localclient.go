// Package localclient holds the server side of the desktop app (US-45, US-46; ADR-12, ADR-20): the app
// signs in by the device authorization grant straight at Keycloak (client easycode-desktop, scope
// easycode-read) and calls the api with the access token as Bearer. The api checks the token (signature,
// issuer, audience easycode-api, client easycode-desktop, scope easycode-read, expiry), keeps one record per
// SSO session of the app (local_client_sessions: device, last use, revocation) and confirms the SSO session
// at most every 60 s (RNF-06). The token is read-only (PD-07): the http layer refuses every write but
// endSession with 403 requires-web-session. The app never receives an AI key (US-45 CA-3).
package localclient

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"easycode/internal/application/audit"
	"easycode/internal/application/session"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// OIDC client and scope of the desktop app (openapi localClientToken).
const (
	ClientID  = "easycode-desktop"
	Scope     = "easycode-read"
	CodeValid = 10 * time.Minute // device code lifetime (US-45 CA-4, realm attribute oauth2.device.code.lifespan)
)

// CodeRequiresWebSession is the refusal of a write made with the token of the app (PD-07).
const CodeRequiresWebSession = "requires-web-session"

// TouchEvery is how often the last use of a session is written (as for the web, RNF-09).
const TouchEvery = 30 * time.Second

// Token failures of Verifier.
var (
	ErrInvalidToken = errors.New("invalid local client token")
	ErrExpiredToken = errors.New("expired local client token")
)

// Token is a checked access token of the desktop app.
type Token struct {
	Subject       string
	Name          string
	SessionID     string // sid: the Keycloak session of the app
	Administrator bool
	AuthTime      time.Time
	ExpiresAt     time.Time
}

// Verifier checks an access token of the desktop app against the realm keys.
type Verifier interface {
	// VerifyAccessToken returns ErrExpiredToken for a token past its expiry and ErrInvalidToken for any other
	// refusal (signature, issuer, audience, client, scope).
	VerifyAccessToken(ctx context.Context, raw string) (Token, error)
}

// Directory confirms the SSO session of the app (admin API, read-only service account).
type Directory interface {
	// AppSessionAlive reports whether the person still has the SSO session with the app client in it.
	AppSessionAlive(ctx context.Context, subject, sessionID string) (bool, error)
	// RevokeAppSession ends the SSO session of the app, so its refresh token fails with invalid_grant.
	RevokeAppSession(ctx context.Context, subject, sessionID string) error
}

// Record is a stored session of the desktop app.
type Record struct {
	ID             int64
	PublicID       string
	Subject        string
	OrganizationID string
	SessionID      string // sid of the SSO session
	Device         string
	IPAddress      string
	StartedAt      time.Time
	LastUsedAt     time.Time
	CheckedAt      time.Time
	Acknowledged   bool
	Ended          bool
}

// Opening is what a first request of a new session records.
type Opening struct {
	OrganizationID string
	Subject        string
	SessionID      string
	Device         string
	IPAddress      string
	At             time.Time
}

// Store keeps the sessions of the app (local_client_sessions).
type Store interface {
	// Find returns the session of the SSO session of the person (any organization: the sub is unique in
	// the realm), or not-found.
	Find(ctx context.Context, subject, sessionID string) (Record, error)
	// Open records a new session (or returns the existing one when another request created it first).
	Open(ctx context.Context, opening Opening) (Record, error)
	// Get returns a session by internal id.
	Get(ctx context.Context, id int64) (Record, error)
	// Touch records the use and, when checked, the SSO confirmation.
	Touch(ctx context.Context, id int64, usedAt time.Time, checkedAt *time.Time, ipAddress string) error
	// End closes the session (logout, revoked, offboarded); ending twice keeps the first reason.
	End(ctx context.Context, id int64, reason string) error
	// List returns the open sessions of the person, newest first.
	List(ctx context.Context, principal identity.Principal) ([]Record, error)
	// Own returns an open session of the person by public id, or not-found.
	Own(ctx context.Context, principal identity.Principal, publicID string) (Record, error)
	// Acknowledge marks the new-session notice as seen.
	Acknowledge(ctx context.Context, id int64, at time.Time) error
}

// Configuration is getLocalClientConfiguration.
type Configuration struct {
	Issuer                      string
	ClientID                    string
	Audience                    string
	Scopes                      []string
	DeviceAuthorizationEndpoint string
	TokenEndpoint               string
	RevocationEndpoint          string
	CodeValiditySeconds         int
}

// Service authenticates the app and manages its sessions.
type Service struct {
	users     session.Users
	store     Store
	verifier  Verifier
	directory Directory
	trail     audit.Trail
	tx        txRunner
	issuer    string
	audience  string
	now       func() time.Time
}

type txRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// NewService builds the service for the realm of issuer; audience is the client id of the api.
func NewService(users session.Users, store Store, verifier Verifier, directory Directory, trail audit.Trail, tx txRunner, issuer, audience string) *Service {
	return &Service{users: users, store: store, verifier: verifier, directory: directory, trail: trail, tx: tx,
		issuer: strings.TrimRight(issuer, "/"), audience: audience, now: time.Now}
}

// WithClock replaces the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Configuration returns the device flow parameters, built from the public issuer (RNF-17: the app never
// fixes the Keycloak address in code).
func (s *Service) Configuration() Configuration {
	base := s.issuer + "/protocol/openid-connect"
	return Configuration{
		Issuer: s.issuer, ClientID: ClientID, Audience: s.audience, Scopes: []string{"openid", Scope},
		DeviceAuthorizationEndpoint: base + "/auth/device", TokenEndpoint: base + "/token", RevocationEndpoint: base + "/revoke",
		CodeValiditySeconds: int(CodeValid / time.Second),
	}
}

// Client describes the request of the app (device and address, for the person to recognize the session).
type Client struct {
	UserAgent string
	IPAddress string
}

// Authenticate checks the Bearer token of the app and returns the person and the session. An expired
// token answers 401 session-expired (the app refreshes and tries again); a session revoked on the web or
// in the SSO, or a person deactivated, answers 401 revoked-session (US-45 CA-2).
func (s *Service) Authenticate(ctx context.Context, raw string, client Client) (identity.Principal, identity.Session, error) {
	unauthenticated := failure.New(failure.KindUnauthenticated, failure.CodeNotAuthenticated, nil)
	if raw == "" || len(raw) > 16<<10 {
		return identity.Principal{}, identity.Session{}, unauthenticated
	}
	token, err := s.verifier.VerifyAccessToken(ctx, raw)
	switch {
	case errors.Is(err, ErrExpiredToken):
		return identity.Principal{}, identity.Session{}, failure.New(failure.KindUnauthenticated, session.CodeSessionExpired, nil)
	case errors.Is(err, ErrInvalidToken):
		return identity.Principal{}, identity.Session{}, unauthenticated
	case err != nil:
		return identity.Principal{}, identity.Session{}, err
	}
	now := s.now().UTC()
	record, err := s.store.Find(ctx, token.Subject, token.SessionID)
	if failure.From(err).Kind == failure.KindNotFound {
		record, err = s.open(ctx, token, client, now)
	}
	if err != nil {
		return identity.Principal{}, identity.Session{}, err
	}
	revoked := failure.New(failure.KindUnauthenticated, session.CodeRevokedSession, nil)
	if record.Ended {
		return identity.Principal{}, identity.Session{}, revoked
	}
	principal, err := s.users.Principal(ctx, record.OrganizationID, record.Subject)
	if err != nil {
		if failure.From(err).Kind == failure.KindNotFound {
			_ = s.store.End(ctx, record.ID, "offboarded")
			return identity.Principal{}, identity.Session{}, revoked
		}
		return identity.Principal{}, identity.Session{}, err
	}
	alive, checked := s.confirm(ctx, record, token.SessionID, now)
	if !alive {
		_ = s.store.End(ctx, record.ID, "revoked")
		return identity.Principal{}, identity.Session{}, revoked
	}
	ip := truncate(client.IPAddress, 64)
	if checked != nil || now.Sub(record.LastUsedAt) >= TouchEvery || (ip != "" && ip != record.IPAddress) {
		if err := s.store.Touch(ctx, record.ID, now, checked, ip); err != nil {
			return identity.Principal{}, identity.Session{}, err
		}
	}
	authenticated := token.AuthTime
	if authenticated.IsZero() || authenticated.After(now) {
		authenticated = record.StartedAt
	}
	return principal, identity.Session{
		ID: record.ID, Medium: identity.MediumLocalClient, PublicID: record.PublicID, Subject: principal.Subject,
		OrganizationID: principal.OrganizationID, AuthenticatedAt: authenticated, IssuedAt: now, LastSeenAt: now,
	}, nil
}

// open records the first request of a new session of the app: the person is recorded as in the web login
// (display name and administrator role from the token).
func (s *Service) open(ctx context.Context, token Token, client Client, now time.Time) (Record, error) {
	principal, err := s.users.SignIn(ctx, s.issuer, session.Claims{
		Subject: token.Subject, Name: token.Name, AuthTime: token.AuthTime, SessionID: token.SessionID, Administrator: token.Administrator,
	})
	if err != nil {
		kind := failure.From(err).Kind
		if kind == failure.KindNotFound || kind == failure.KindForbidden {
			return Record{}, failure.New(failure.KindUnauthenticated, session.CodeAccountWithoutAccess, nil)
		}
		return Record{}, err
	}
	return s.store.Open(ctx, Opening{
		OrganizationID: principal.OrganizationID, Subject: principal.Subject, SessionID: token.SessionID,
		Device: Device(client.UserAgent), IPAddress: truncate(client.IPAddress, 64), At: now,
	})
}

// confirm asks the SSO whether the session of the app still holds when the last check is older than 60 s.
// An unreachable SSO keeps the session until the next check (an SSO outage does not sign everybody out).
func (s *Service) confirm(ctx context.Context, record Record, sessionID string, now time.Time) (bool, *time.Time) {
	if now.Sub(record.CheckedAt) < identity.IDPCheckEvery {
		return true, nil
	}
	alive, err := s.directory.AppSessionAlive(ctx, record.Subject, sessionID)
	if err != nil {
		return true, nil
	}
	return alive, &now
}

// Active reports whether the session still holds (SSE heartbeat): ended on the web, or the SSO session gone
// (checked at most every 60 s), ends the stream with session_revoked.
func (s *Service) Active(ctx context.Context, current identity.Session) (bool, error) {
	record, err := s.store.Get(ctx, current.ID)
	if err != nil {
		return false, err
	}
	if record.Ended {
		return false, nil
	}
	now := s.now().UTC()
	if now.Sub(record.CheckedAt) < identity.IDPCheckEvery {
		return true, nil
	}
	alive, checked := s.confirm(ctx, record, record.SessionID, now)
	if !alive {
		_ = s.store.End(ctx, record.ID, "revoked")
		return false, nil
	}
	if checked != nil {
		_ = s.store.Touch(ctx, record.ID, record.LastUsedAt, checked, "")
	}
	return true, nil
}

// End is endSession with the token of the app: the session ends here at once (the app also revokes its
// refresh token at the SSO revocation endpoint and clears the vault). Audited as session.logout from the
// local client (PL-36).
func (s *Service) End(ctx context.Context, current identity.Session) error {
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.store.End(ctx, current.ID, "logout"); err != nil {
			return err
		}
		_, err := s.trail.Append(ctx, domainaudit.Record{
			OrganizationID: current.OrganizationID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: current.Subject},
			Origin: domainaudit.OriginLocalClient, Action: "session.logout", Target: "local-client-sessions/" + current.PublicID,
		})
		return err
	})
}

// List is listLocalClientSessions: the open sessions of the person, never a token (RN-3: each person sees
// only their own).
func (s *Service) List(ctx context.Context, principal identity.Principal) ([]Record, error) {
	return s.store.List(ctx, principal)
}

// Unacknowledged counts the sessions whose new-session notice the person has not seen (getCurrentUser on
// the web, S-19: notice of every new session of the app).
func (s *Service) Unacknowledged(ctx context.Context, principal identity.Principal) (int, error) {
	records, err := s.store.List(ctx, principal)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, r := range records {
		if !r.Acknowledged {
			count++
		}
	}
	return count, nil
}

// Revoke is revokeLocalClientSession: the session ends at once here, so the next call of the app answers
// 401 revoked-session and its streams end with session_revoked (US-45 CA-2). Audited from the web.
func (s *Service) Revoke(ctx context.Context, principal identity.Principal, publicID string) error {
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		record, err := s.store.Own(ctx, principal, publicID)
		if err != nil {
			return err
		}
		if err := s.store.End(ctx, record.ID, "revoked"); err != nil {
			return err
		}
		if _, err = s.trail.Append(ctx, domainaudit.Record{
			OrganizationID: principal.OrganizationID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
			Origin: domainaudit.OriginWeb, Action: "local_client_session.revoke", Target: "local-client-sessions/" + record.PublicID,
		}); err != nil {
			return err
		}
		// The SSO ends the session of the app too (its refresh fails with invalid_grant). The api already refuses
		// the session, so an unreachable SSO does not block the revocation: the refresh then only yields tokens
		// the api refuses with revoked-session.
		_ = s.directory.RevokeAppSession(ctx, record.Subject, record.SessionID)
		return nil
	})
}

// Acknowledge is acknowledgeLocalClientSession: the person saw the notice of the new session.
func (s *Service) Acknowledge(ctx context.Context, principal identity.Principal, publicID string) error {
	record, err := s.store.Own(ctx, principal, publicID)
	if err != nil {
		return err
	}
	if record.Acknowledged {
		return nil
	}
	return s.store.Acknowledge(ctx, record.ID, s.now().UTC())
}

// Device turns the User-Agent of the app ("easycode-desktop/1.0.0 (linux; amd64)") into the device text shown
// to the person: printable characters only, at most 120.
func Device(userAgent string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, strings.TrimSpace(userAgent))
	if cleaned == "" {
		return "unknown"
	}
	return truncate(cleaned, 120)
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}
