// Package session holds the signed-in state of easycode-desktop: the short-lived access token in memory and
// the refresh token in the OS keychain only (cliente-local.md section 2). The access token is renewed when
// less than 30 s remain or after a 401; when the renewal is refused the keychain is cleared and the caller
// goes back to the sign-in view with "session revoked" (US-45 CA-2).
package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"easycode-desktop/internal/keychain"
	"easycode-desktop/internal/oidc"
)

// RenewBefore is how long before expiry the access token is renewed.
const RenewBefore = 30 * time.Second

var (
	// ErrSignedOut means there is no session (no refresh token stored).
	ErrSignedOut = errors.New("signed out")
	// ErrRevoked means the identity provider refused the refresh token; the keychain was cleared.
	ErrRevoked = errors.New("session revoked")
)

// Refresher renews tokens (oidc.Client in production).
type Refresher interface {
	Refresh(ctx context.Context, refreshToken string) (oidc.Tokens, error)
}

// Tokens is the token source shared by the REST and SSE clients.
type Tokens struct {
	mu        sync.Mutex
	vault     keychain.Store
	refresher Refresher
	now       func() time.Time
	access    string
	expiresAt time.Time
}

// New returns a token source over the keychain.
func New(vault keychain.Store, refresher Refresher, now func() time.Time) *Tokens {
	if now == nil {
		now = time.Now
	}
	return &Tokens{vault: vault, refresher: refresher, now: now}
}

// Establish stores the result of a device flow: the refresh token goes to the keychain first; if the
// keychain fails nothing is kept and keychain.ErrUnavailable is returned (ESP-28 ER-28-1).
func (t *Tokens) Establish(tokens oidc.Tokens) error {
	if tokens.RefreshToken == "" {
		return errors.New("identity provider returned no refresh token")
	}
	if err := t.vault.Set(tokens.RefreshToken); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.access, t.expiresAt = tokens.AccessToken, tokens.ExpiresAt
	return nil
}

// HasStoredSession says whether the keychain holds a refresh token.
func (t *Tokens) HasStoredSession() (bool, error) {
	_, err := t.vault.Get()
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, keychain.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

// AccessToken returns a valid access token, renewing it when needed.
func (t *Tokens) AccessToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.access != "" && t.now().Add(RenewBefore).Before(t.expiresAt) {
		return t.access, nil
	}
	return t.renewLocked(ctx)
}

// Renew forces a renewal, after the api answered 401 to the current access token. stale is the token
// that got the 401: when another caller already renewed it, the new one is returned without a new round.
func (t *Tokens) Renew(ctx context.Context, stale string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.access != "" && t.access != stale && t.now().Add(RenewBefore).Before(t.expiresAt) {
		return t.access, nil
	}
	return t.renewLocked(ctx)
}

func (t *Tokens) renewLocked(ctx context.Context) (string, error) {
	refresh, err := t.vault.Get()
	if errors.Is(err, keychain.ErrNotFound) {
		t.access = ""
		return "", ErrSignedOut
	}
	if err != nil {
		return "", err
	}
	tokens, err := t.refresher.Refresh(ctx, refresh)
	if errors.Is(err, oidc.ErrRefreshRejected) {
		t.access = ""
		_ = t.vault.Delete()
		return "", ErrRevoked
	}
	if err != nil {
		return "", fmt.Errorf("renew access token: %w", err)
	}
	if tokens.RefreshToken != refresh {
		if err := t.vault.Set(tokens.RefreshToken); err != nil {
			return "", err
		}
	}
	t.access, t.expiresAt = tokens.AccessToken, tokens.ExpiresAt
	return t.access, nil
}

// RefreshToken returns the stored refresh token (used to revoke it on sign out).
func (t *Tokens) RefreshToken() (string, error) { return t.vault.Get() }

// Clear forgets the access token and deletes the keychain entry.
func (t *Tokens) Clear() error {
	t.mu.Lock()
	t.access, t.expiresAt = "", time.Time{}
	t.mu.Unlock()
	return t.vault.Delete()
}
