package ssosync_test

import (
	"context"
	"testing"
	"time"

	"easycode/internal/application/session"
	"easycode/internal/application/ssosync"
	"easycode/internal/domain/identity"
	"easycode/internal/infrastructure/oidc"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
)

func setup(t *testing.T) (*postgrestest.Database, *oidctest.Server, *ssosync.Job) {
	t.Helper()
	db := postgrestest.New(t)
	idp := oidctest.New(t)
	ctx := context.Background()
	if err := postgres.EnsureOrganization(ctx, db.Admin, idp.Issuer(), "Test organization"); err != nil {
		t.Fatal(err)
	}
	worker := db.PoolAs("easycode_worker")
	settings := idp.Settings("")
	settings.ClientID, settings.ClientSecret = "easycode-events", "events-secret"
	job := ssosync.NewJob(idp.Issuer(), oidc.NewSSODirectory(oidc.NewAdmin(settings)), postgres.NewSSOSync(worker), postgres.NewAuditTrail(worker))
	return db, idp, job
}

func count(t *testing.T, db *postgrestest.Database, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Admin.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// US-01 CA-5, RNF-13, PL-36: login, logout and login failure of the SSO become records of the
// organization trail, with the account when known; reading again never duplicates.
func TestSSOEventsBecomeOrganizationAuditRecords(t *testing.T) {
	db, idp, job := setup(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	idp.AddEvent(oidctest.Event{ID: "e1", Type: "LOGIN", ClientID: "easycode-api", Subject: "ana", Time: now.Add(-3 * time.Minute)})
	idp.AddEvent(oidctest.Event{ID: "e2", Type: "CODE_TO_TOKEN", ClientID: "easycode-api", Subject: "ana", Time: now.Add(-3 * time.Minute)})
	idp.AddEvent(oidctest.Event{ID: "e3", Type: "LOGIN_ERROR", ClientID: "easycode-api", Subject: "ana", Error: "invalid_user_credentials", Time: now.Add(-2 * time.Minute)})
	idp.AddEvent(oidctest.Event{ID: "e4", Type: "LOGIN_ERROR", ClientID: "easycode-api", Error: "user_not_found", Time: now.Add(-2 * time.Minute)})
	idp.AddEvent(oidctest.Event{ID: "e5", Type: "LOGOUT", ClientID: "easycode-api", Subject: "ana", Time: now.Add(-time.Minute)})
	idp.AddEvent(oidctest.Event{ID: "e6", Type: "CODE_TO_TOKEN", ClientID: ssosync.DesktopClientID, Subject: "ana", Time: now})

	result, err := job.Run(context.Background())
	if err != nil || result.Recorded != 4 {
		t.Fatalf("first pass: %+v %v", result, err)
	}
	if again, err := job.Run(context.Background()); err != nil || again.Recorded != 0 {
		t.Fatalf("second pass must not duplicate: %+v %v", again, err)
	}
	for action, want := range map[string]int{"session.login": 2, "session.login_failed": 1, "session.logout": 1} {
		if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = $1 AND actor_id = 'ana' AND project_id IS NULL", action); n != want {
			t.Errorf("%s: %d records, want %d", action, n, want)
		}
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'session.login_failed' AND decision = 'invalid_user_credentials'"); n != 1 {
		t.Error("the failure keeps only the short reason")
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'session.login' AND origin = 'local_client'"); n != 1 {
		t.Error("the desktop login comes from local_client")
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE external_event_id = 'e1' AND recorded_at = $1", now.Add(-3*time.Minute)); n != 1 {
		t.Error("the record keeps the moment of the SSO event")
	}
}

// RNF-20, D-31: whoever the realm no longer has is deactivated, the display name anonymized, the
// sub kept as opaque id and the open sessions ended; people still in the realm stay.
func TestPersonRemovedFromTheRealmIsOffboarded(t *testing.T) {
	db, idp, job := setup(t)
	ctx := context.Background()
	api := db.PoolAs("easycode_api")
	users := postgres.NewUsers(api)
	ana, err := users.SignIn(ctx, idp.Issuer(), session.Claims{Subject: "ana", Name: "Ana Souza"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.SignIn(ctx, idp.Issuer(), session.Claims{Subject: "bruno", Name: "Bruno Lima"}); err != nil {
		t.Fatal(err)
	}
	idp.AddUser(oidctest.User{Subject: "bruno", Name: "Bruno Lima"})
	if _, err := postgres.NewWebSessions(api).Create(ctx, make([]byte, 32), session.Record{Session: sessionOf(ana)}); err != nil {
		t.Fatal(err)
	}
	result, err := job.Run(ctx)
	if err != nil || result.Offboarded != 1 {
		t.Fatalf("offboarding: %+v %v", result, err)
	}
	if n := count(t, db, `SELECT count(*) FROM easycode.users WHERE sub = 'ana' AND NOT active AND anonymized_at IS NOT NULL
   AND display_name = $1`, postgres.AnonymizedDisplayName); n != 1 {
		t.Fatal("ana must be deactivated with the name anonymized and the sub kept")
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.web_sessions WHERE ended_at IS NULL"); n != 0 {
		t.Fatal("the sessions of ana must end")
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.users WHERE sub = 'bruno' AND active AND display_name = 'Bruno Lima'"); n != 1 {
		t.Fatal("bruno is still in the realm")
	}
	if _, err := users.SignIn(ctx, idp.Issuer(), session.Claims{Subject: "ana", Name: "Ana Souza"}); err == nil {
		t.Fatal("an offboarded person does not sign in again")
	}
}

func sessionOf(p identity.Principal) identity.Session {
	now := time.Now()
	return identity.Session{Subject: p.Subject, OrganizationID: p.OrganizationID, AuthenticatedAt: now, IssuedAt: now, LastSeenAt: now,
		CSRFToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
}
