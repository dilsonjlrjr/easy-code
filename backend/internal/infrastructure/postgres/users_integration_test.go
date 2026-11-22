package postgres_test

import (
	"context"
	"testing"

	"easycode/internal/application/idempotency"
	"easycode/internal/application/session"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
)

const testIssuer = "https://sso.example/realms/easycode"

// seedDev signs the test administrator in (test identity: organization of the issuer and person).
func seedDev(t *testing.T, db *postgrestest.Database) identity.Principal {
	t.Helper()
	ctx := context.Background()
	if err := postgres.EnsureOrganization(ctx, db.Admin, testIssuer, "Test organization"); err != nil {
		t.Fatal(err)
	}
	dev, err := postgres.NewUsers(db.PoolAs("easycode_api")).SignIn(ctx, testIssuer, session.Claims{Subject: "dev", Name: "Dev", Administrator: true})
	if err != nil {
		t.Fatal(err)
	}
	return dev
}

// US-01: signing in records the person of the issuer's organization, refreshes name and role on
// each login, and refuses an unknown issuer or a deactivated person (CA-2, D-31).
func TestSignInRecordsThePersonOfTheIssuer(t *testing.T) {
	db := postgrestest.New(t)
	dev := seedDev(t, db)
	users := postgres.NewUsers(db.PoolAs("easycode_api"))
	ctx := context.Background()
	if !dev.Administrator || dev.Name != "Dev" || dev.OrganizationName != "Test organization" {
		t.Fatalf("unexpected principal: %+v", dev)
	}
	again, err := users.SignIn(ctx, testIssuer, session.Claims{Subject: "dev", Name: "Dev Renamed"})
	if err != nil || again.Name != "Dev Renamed" || again.Administrator || again.OrganizationID != dev.OrganizationID {
		t.Fatalf("second login refreshes name and role: %+v %v", again, err)
	}
	same, err := users.Principal(ctx, dev.OrganizationID, "dev")
	if err != nil || same != again {
		t.Fatalf("principal by organization: %+v %v", same, err)
	}
	if _, err := users.Principal(ctx, dev.OrganizationID, "someone"); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("unknown user must be not found, got %v", err)
	}
	if lead, err := users.LeadInAnyProject(ctx, dev); err != nil || lead {
		t.Fatalf("dev leads nothing yet: %v %v", lead, err)
	}
	if _, err := users.SignIn(ctx, "https://other.example/realms/x", session.Claims{Subject: "dev", Name: "Dev"}); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("issuer without organization: %v", err)
	}
	if _, err := db.Admin.Exec(ctx, "UPDATE easycode.users SET active = false WHERE sub = 'dev'"); err != nil {
		t.Fatal(err)
	}
	if _, err := users.SignIn(ctx, testIssuer, session.Claims{Subject: "dev", Name: "Dev"}); failure.From(err).Code != session.CodeAccountWithoutAccess {
		t.Fatalf("deactivated person: %v", err)
	}
}

func TestIdempotencyKeysReplayAndRefuseRace(t *testing.T) {
	db := postgrestest.New(t)
	dev := seedDev(t, db)
	api := db.PoolAs("easycode_api")
	ctx := context.Background()
	keys := postgres.NewIdempotencyKeys(api)
	req := idempotency.Request{Principal: dev, Key: "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b31", Operation: "createProject", BodyHash: idempotency.HashBody([]byte("{}"))}
	if found, err := keys.Find(ctx, dev, req.Key); err != nil || found != nil {
		t.Fatalf("new key: %v %v", found, err)
	}
	resp := idempotency.Response{Status: 201, Body: []byte(`{"id":"x"}`), ResourceID: "/api/v1/projects/x"}
	if err := keys.Save(ctx, req, resp); err != nil {
		t.Fatal(err)
	}
	found, err := keys.Find(ctx, dev, req.Key)
	if err != nil || found == nil || found.Response.Status != 201 || found.Response.ResourceID != resp.ResourceID || found.BodyHash != req.BodyHash {
		t.Fatalf("stored answer: %+v %v", found, err)
	}
	if err := keys.Save(ctx, req, resp); failure.From(err).Code != idempotency.CodeRace {
		t.Fatalf("second save in the window must be a race, got %v", err)
	}
}
