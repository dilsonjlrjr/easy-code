package identity_test

import (
	"testing"
	"time"

	"easycode/internal/domain/identity"
)

func TestSessionExpiresAfterIdleOrLifetime(t *testing.T) {
	start := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	session := identity.Session{IssuedAt: start, LastSeenAt: start}
	if session.Expired(start.Add(29 * time.Minute)) {
		t.Fatal("session must live 30 min without activity")
	}
	if !session.Expired(start.Add(30 * time.Minute)) {
		t.Fatal("session must end after 30 min idle (RNF-06)")
	}
	session.LastSeenAt = start.Add(7*time.Hour + 50*time.Minute)
	if !session.Expired(start.Add(8 * time.Hour)) {
		t.Fatal("session must end after 8 h in total (RNF-06)")
	}
}

func TestHasMatchesAnyRole(t *testing.T) {
	roles := []identity.Role{identity.RoleApprover}
	if !identity.Has(roles, identity.RoleLead, identity.RoleApprover) || identity.Has(roles, identity.RoleLead) {
		t.Fatal("unexpected role match")
	}
}
