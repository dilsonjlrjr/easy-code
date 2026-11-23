package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"easycode-desktop/internal/keychain"
	"easycode-desktop/internal/oidc"
)

type fakeRefresher struct {
	calls  int
	reject bool
	now    func() time.Time
}

func (f *fakeRefresher) Refresh(_ context.Context, refresh string) (oidc.Tokens, error) {
	f.calls++
	if f.reject {
		return oidc.Tokens{}, oidc.ErrRefreshRejected
	}
	return oidc.Tokens{AccessToken: "access-" + refresh, RefreshToken: refresh + "+", ExpiresAt: f.now().Add(5 * time.Minute)}, nil
}

func TestTokensRenewNearExpiryAndRotateRefreshToken(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	vault := &keychain.Memory{}
	refresher := &fakeRefresher{now: clock}
	tokens := New(vault, refresher, clock)
	if err := tokens.Establish(oidc.Tokens{AccessToken: "a1", RefreshToken: "r1", ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	got, err := tokens.AccessToken(context.Background())
	if err != nil || got != "a1" || refresher.calls != 0 {
		t.Fatalf("fresh token: %q %v calls=%d", got, err, refresher.calls)
	}
	now = now.Add(31 * time.Second) // 29 s left: renew
	got, err = tokens.AccessToken(context.Background())
	if err != nil || got != "access-r1" || refresher.calls != 1 {
		t.Fatalf("renewed token: %q %v calls=%d", got, err, refresher.calls)
	}
	if stored, _ := vault.Get(); stored != "r1+" {
		t.Fatalf("rotated refresh token not stored: %q", stored)
	}
	// A 401 on an already replaced token does not renew again.
	got, err = tokens.Renew(context.Background(), "a1")
	if err != nil || got != "access-r1" || refresher.calls != 1 {
		t.Fatalf("renew of stale token: %q %v calls=%d", got, err, refresher.calls)
	}
	got, err = tokens.Renew(context.Background(), "access-r1")
	if err != nil || got != "access-r1+" || refresher.calls != 2 {
		t.Fatalf("forced renew: %q %v calls=%d", got, err, refresher.calls)
	}
}

func TestTokensRevokedClearsTheKeychain(t *testing.T) {
	now := time.Now()
	vault := &keychain.Memory{}
	tokens := New(vault, &fakeRefresher{reject: true, now: time.Now}, func() time.Time { return now })
	_ = tokens.Establish(oidc.Tokens{AccessToken: "a1", RefreshToken: "r1", ExpiresAt: now})
	if _, err := tokens.AccessToken(context.Background()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("want ErrRevoked, got %v", err)
	}
	if ok, _ := tokens.HasStoredSession(); ok {
		t.Fatal("keychain must be cleared after a refused refresh")
	}
	if _, err := tokens.AccessToken(context.Background()); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("want ErrSignedOut, got %v", err)
	}
}

func TestEstablishFailsWithoutKeychainAndKeepsNothing(t *testing.T) {
	vault := &keychain.Memory{Unavailable: true}
	tokens := New(vault, &fakeRefresher{now: time.Now}, nil)
	err := tokens.Establish(oidc.Tokens{AccessToken: "a1", RefreshToken: "r1", ExpiresAt: time.Now().Add(time.Hour)})
	if !errors.Is(err, keychain.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	vault.Unavailable = false
	if _, err := tokens.AccessToken(context.Background()); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("no access token may be kept after a keychain failure, got %v", err)
	}
}

func TestClear(t *testing.T) {
	vault := &keychain.Memory{}
	tokens := New(vault, &fakeRefresher{now: time.Now}, nil)
	_ = tokens.Establish(oidc.Tokens{AccessToken: "a1", RefreshToken: "r1", ExpiresAt: time.Now().Add(time.Hour)})
	if err := tokens.Clear(); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tokens.HasStoredSession(); ok {
		t.Fatal("keychain not cleared")
	}
}
