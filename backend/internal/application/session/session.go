// Package session holds the web session use cases (ADR-12, option B): the login by the SSO with
// authorization code + PKCE, the server-side session behind the ec_session cookie, its expiry
// (30 min idle, 8 h in total), the check with the SSO every 60 s (revocation, RNF-06), the step-up
// for critical decisions (ADR-19) and the logout. The platform never sees a password (US-01 RN-1).
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Failure codes of sessions (openapi Problem).
const (
	CodeSessionExpired       = "session-expired"
	CodeRevokedSession       = "revoked-session"
	CodeAccountWithoutAccess = "account-without-access"
)

// LoginFlowLifetime is how long the transient ec_login cookie lives (openapi startWebLogin).
const LoginFlowLifetime = 10 * time.Minute

// LoginFailedPath is the web login page after an invalid or expired return from the SSO.
const LoginFailedPath = "/login?reason=login_failed"

// AccessDeniedPath is the web page of a refused login (US-01 CA-2, completeWebLogin).
const AccessDeniedPath = "/access-denied"

// ErrRevoked is what the identity provider answers when the SSO session no longer holds
// (logout elsewhere, account disabled, session revoked by the administrator).
var ErrRevoked = errors.New("sso session revoked")

// Claims are the validated claims of the ID token.
type Claims struct {
	Subject       string
	Name          string
	Nonce         string
	AuthTime      time.Time
	SessionID     string // sid
	Administrator bool   // organization administrator role from the SSO
}

// Tokens is the result of the code exchange: validated ID token claims and the refresh token.
type Tokens struct {
	Claims       Claims
	RefreshToken string
}

// IdentityProvider is the OIDC provider (Keycloak).
type IdentityProvider interface {
	// Issuer is the issuer whose organization signs in (organizations.oidc_issuer).
	Issuer() string
	// AuthorizationURL builds the redirect to the authorization endpoint with PKCE (S256). With
	// reauthenticate it asks max_age=0 and prompt=login (step-up, ADR-19).
	AuthorizationURL(state, nonce, challenge string, reauthenticate bool) string
	// Exchange trades the code with the verifier and validates the ID token (signature, issuer,
	// audience, expiry and nonce).
	Exchange(ctx context.Context, code, verifier, nonce string) (Tokens, error)
	// Refresh confirms the SSO session and returns the rotated refresh token, or ErrRevoked.
	Refresh(ctx context.Context, refreshToken string) (string, error)
	// Logout ends the SSO session (back channel).
	Logout(ctx context.Context, refreshToken string) error
}

// Users finds and records the people the sessions belong to.
type Users interface {
	// Principal returns the active user with that subject in the organization, or not-found.
	Principal(ctx context.Context, organizationID, subject string) (identity.Principal, error)
	// SignIn records the person of the issuer's organization from the ID token (display name and
	// administrator role refreshed on every login). Not found when the issuer has no organization;
	// forbidden account-without-access when the person was deactivated (offboarding).
	SignIn(ctx context.Context, issuer string, claims Claims) (identity.Principal, error)
	// LeadInAnyProject reports whether the user is lead of some project (getCurrentUser).
	LeadInAnyProject(ctx context.Context, principal identity.Principal) (bool, error)
}

// Record is a stored web session.
type Record struct {
	Session            identity.Session
	IDPSessionID       string
	SealedRefreshToken []byte
	IDPCheckedAt       time.Time
	Ended              bool
}

// Store keeps the web sessions (web_sessions).
type Store interface {
	Create(ctx context.Context, tokenHash []byte, record Record) (int64, error)
	// Find returns the session of the cookie hash, or not-found.
	Find(ctx context.Context, tokenHash []byte) (Record, error)
	// Touch records activity and, when checked, the SSO check with the rotated refresh token.
	Touch(ctx context.Context, id int64, seenAt time.Time, checkedAt *time.Time, sealedRefreshToken []byte) error
	// End closes the session and returns its sealed refresh token (for the SSO logout).
	End(ctx context.Context, id int64, reason string) ([]byte, error)
	// Active reports whether the session was not ended (SSE streams, cheap check).
	Active(ctx context.Context, id int64) (bool, error)
}

// Sealer protects the refresh token at rest (AES-GCM with the session key).
type Sealer interface {
	Seal(plain []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}

// LoginFlow is what the transient ec_login cookie carries between startWebLogin and the callback.
type LoginFlow struct {
	State          string
	Nonce          string
	Verifier       string
	Callback       string
	Reauthenticate bool
	ExpiresAt      time.Time
}

// FlowCodec signs the login flow cookie; a changed value fails to open.
type FlowCodec interface {
	Seal(flow LoginFlow) (string, error)
	Open(value string) (LoginFlow, error)
}

// Service issues and checks web sessions.
type Service struct {
	users  Users
	store  Store
	idp    IdentityProvider
	sealer Sealer
	flows  FlowCodec
	now    func() time.Time
}

// NewService builds the service.
func NewService(users Users, store Store, idp IdentityProvider, sealer Sealer, flows FlowCodec) *Service {
	return &Service{users: users, store: store, idp: idp, sealer: sealer, flows: flows, now: time.Now}
}

// WithClock replaces the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Login is the start of the login: where to send the browser and the flow cookie.
type Login struct {
	RedirectURL string
	FlowCookie  string
	ExpiresAt   time.Time
}

// BeginLogin is startWebLogin: state, nonce and PKCE verifier go to the flow cookie.
func (s *Service) BeginLogin(callback string, reauthenticate bool) (Login, error) {
	flow := LoginFlow{
		State: newToken(), Nonce: newToken(), Verifier: newToken(), Callback: SafeCallback(callback),
		Reauthenticate: reauthenticate, ExpiresAt: s.now().UTC().Add(LoginFlowLifetime),
	}
	cookie, err := s.flows.Seal(flow)
	if err != nil {
		return Login{}, err
	}
	sum := sha256.Sum256([]byte(flow.Verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return Login{
		RedirectURL: s.idp.AuthorizationURL(flow.State, flow.Nonce, challenge, reauthenticate),
		FlowCookie:  cookie, ExpiresAt: flow.ExpiresAt,
	}, nil
}

// Callback is what the SSO sent back to completeWebLogin.
type Callback struct {
	Code       string
	State      string
	Error      string // error parameter of the authorization response
	FlowCookie string
}

// Completed is the result of the callback: the session (none when access was denied) and where
// the browser goes next.
type Completed struct {
	Session  identity.Session
	Token    string
	Redirect string
}

// CompleteLogin is completeWebLogin. A refused login (SSO error access_denied, issuer without
// organization, deactivated person) creates no session and leads to the access denied page; an
// invalid or expired flow is a validation error.
func (s *Service) CompleteLogin(ctx context.Context, cb Callback) (Completed, error) {
	flow, err := s.flows.Open(cb.FlowCookie)
	if err != nil || s.now().After(flow.ExpiresAt) || cb.State == "" || cb.State != flow.State {
		return Completed{}, failure.Invalid(failure.FieldError{Field: "state", Message: "invalid"})
	}
	if cb.Error != "" {
		if cb.Error == "access_denied" {
			return Completed{Redirect: AccessDeniedPath}, nil
		}
		return Completed{}, failure.Invalid(failure.FieldError{Field: "code", Message: "invalid"})
	}
	if cb.Code == "" {
		return Completed{}, failure.Invalid(failure.FieldError{Field: "code", Message: "required"})
	}
	tokens, err := s.idp.Exchange(ctx, cb.Code, flow.Verifier, flow.Nonce)
	if err != nil {
		return Completed{}, failure.Invalid(failure.FieldError{Field: "code", Message: "invalid"})
	}
	principal, err := s.users.SignIn(ctx, s.idp.Issuer(), tokens.Claims)
	if err != nil {
		kind := failure.From(err).Kind
		if kind == failure.KindNotFound || kind == failure.KindForbidden {
			return Completed{Redirect: AccessDeniedPath}, nil
		}
		return Completed{}, err
	}
	authenticatedAt := tokens.Claims.AuthTime
	if authenticatedAt.IsZero() || authenticatedAt.After(s.now()) {
		authenticatedAt = s.now()
	}
	current, token, err := s.Start(ctx, principal, Started{
		AuthenticatedAt: authenticatedAt, IDPSessionID: tokens.Claims.SessionID, RefreshToken: tokens.RefreshToken,
	})
	if err != nil {
		return Completed{}, err
	}
	return Completed{Session: current, Token: token, Redirect: flow.Callback}, nil
}

// Started describes a successful SSO authentication.
type Started struct {
	AuthenticatedAt time.Time
	IDPSessionID    string
	RefreshToken    string
}

// Start opens a server-side session for an authenticated principal and returns the cookie value.
// CompleteLogin calls it after the SSO; integration tests call it directly (test identity).
func (s *Service) Start(ctx context.Context, principal identity.Principal, started Started) (identity.Session, string, error) {
	now := s.now().UTC()
	current := identity.Session{
		Subject: principal.Subject, OrganizationID: principal.OrganizationID,
		AuthenticatedAt: started.AuthenticatedAt.UTC(), IssuedAt: now, LastSeenAt: now, CSRFToken: newToken(),
	}
	record := Record{Session: current, IDPSessionID: started.IDPSessionID, IDPCheckedAt: now}
	if started.RefreshToken != "" {
		sealed, err := s.sealer.Seal([]byte(started.RefreshToken))
		if err != nil {
			return identity.Session{}, "", err
		}
		record.SealedRefreshToken = sealed
	}
	token := newToken()
	id, err := s.store.Create(ctx, hashToken(token), record)
	if err != nil {
		return identity.Session{}, "", err
	}
	current.ID = id
	return current, token, nil
}

// Authenticate finds the session of the cookie, checks expiry, the person and, every 60 s, the
// SSO session; it records the activity (sliding 30 min, at most 8 h).
func (s *Service) Authenticate(ctx context.Context, token string) (identity.Principal, identity.Session, error) {
	unauthenticated := failure.New(failure.KindUnauthenticated, failure.CodeNotAuthenticated, nil)
	if token == "" || len(token) > 128 {
		return identity.Principal{}, identity.Session{}, unauthenticated
	}
	record, err := s.store.Find(ctx, hashToken(token))
	if err != nil {
		if failure.From(err).Kind == failure.KindNotFound {
			return identity.Principal{}, identity.Session{}, unauthenticated
		}
		return identity.Principal{}, identity.Session{}, err
	}
	if record.Ended {
		return identity.Principal{}, identity.Session{}, unauthenticated
	}
	now := s.now().UTC()
	current := record.Session
	if current.Expired(now) {
		return identity.Principal{}, identity.Session{}, failure.New(failure.KindUnauthenticated, CodeSessionExpired, nil)
	}
	principal, err := s.users.Principal(ctx, current.OrganizationID, current.Subject)
	if err != nil {
		if failure.From(err).Kind == failure.KindNotFound {
			_, _ = s.store.End(ctx, current.ID, "offboarded")
			return identity.Principal{}, identity.Session{}, failure.New(failure.KindUnauthenticated, CodeRevokedSession, nil)
		}
		return identity.Principal{}, identity.Session{}, err
	}
	checked, sealed, err := s.checkWithIDP(ctx, record, now)
	if err != nil {
		return identity.Principal{}, identity.Session{}, err
	}
	if err := s.store.Touch(ctx, current.ID, now, checked, sealed); err != nil {
		return identity.Principal{}, identity.Session{}, err
	}
	current.LastSeenAt = now
	return principal, current, nil
}

// checkWithIDP refreshes the SSO session when the last check is older than 60 s. A refused
// refresh ends the session (revoked-session); an unreachable SSO keeps it until the next request,
// so an outage of the SSO does not sign everybody out.
func (s *Service) checkWithIDP(ctx context.Context, record Record, now time.Time) (*time.Time, []byte, error) {
	if len(record.SealedRefreshToken) == 0 || now.Sub(record.IDPCheckedAt) < identity.IDPCheckEvery {
		return nil, nil, nil
	}
	refresh, err := s.sealer.Open(record.SealedRefreshToken)
	if err != nil {
		return nil, nil, s.revoke(ctx, record.Session.ID)
	}
	rotated, err := s.idp.Refresh(ctx, string(refresh))
	if errors.Is(err, ErrRevoked) {
		return nil, nil, s.revoke(ctx, record.Session.ID)
	}
	if err != nil {
		return nil, nil, nil
	}
	sealed, err := s.sealer.Seal([]byte(rotated))
	if err != nil {
		return nil, nil, err
	}
	return &now, sealed, nil
}

func (s *Service) revoke(ctx context.Context, id int64) error {
	if _, err := s.store.End(ctx, id, "revoked"); err != nil {
		return err
	}
	return failure.New(failure.KindUnauthenticated, CodeRevokedSession, nil)
}

// Active reports whether the session still holds (SSE heartbeat: logout in another tab ends the
// stream with session_revoked).
func (s *Service) Active(ctx context.Context, current identity.Session) (bool, error) {
	return s.store.Active(ctx, current.ID)
}

// End is endSession: the session ends on the server and on the SSO (back channel, best effort:
// the session is already closed here when the SSO cannot be reached).
func (s *Service) End(ctx context.Context, current identity.Session) error {
	sealed, err := s.store.End(ctx, current.ID, "logout")
	if err != nil || len(sealed) == 0 {
		return err
	}
	if refresh, err := s.sealer.Open(sealed); err == nil {
		_ = s.idp.Logout(ctx, string(refresh))
	}
	return nil
}

// CurrentUser is what getCurrentUser shows about the session.
type CurrentUser struct {
	Principal        identity.Principal
	Session          identity.Session
	LeadInAnyProject bool
}

// Current describes the authenticated user.
func (s *Service) Current(ctx context.Context, principal identity.Principal, current identity.Session) (CurrentUser, error) {
	lead, err := s.users.LeadInAnyProject(ctx, principal)
	if err != nil {
		return CurrentUser{}, err
	}
	return CurrentUser{Principal: principal, Session: current, LeadInAnyProject: lead}, nil
}

// SafeCallback keeps only a relative path of the same origin (openapi startWebLogin callback;
// ESP-02 ER-02-3): anything else becomes "/".
func SafeCallback(callback string) string {
	if callback == "" || len(callback) > 512 || !strings.HasPrefix(callback, "/") ||
		strings.HasPrefix(callback, "//") || strings.HasPrefix(callback, "/\\") || strings.ContainsAny(callback, "\r\n\t\\") {
		return "/"
	}
	return callback
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
