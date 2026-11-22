package postgres_test

import (
	"context"
	"testing"

	"easycode/internal/application/idempotency"
	"easycode/internal/domain/failure"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
)

func seedDev(t *testing.T, db *postgrestest.Database) {
	t.Helper()
	if err := postgres.SeedDevIdentity(context.Background(), db.Admin, "dev", "Dev"); err != nil {
		t.Fatal(err)
	}
}

func TestSeedDevIdentityCreatesTheAdministratorDevUserOnce(t *testing.T) {
	db := postgrestest.New(t)
	seedDev(t, db)
	seedDev(t, db) // idempotent
	users := postgres.NewUsers(db.PoolAs("easycode_api"))
	ctx := context.Background()
	dev, err := users.DevPrincipal(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !dev.Administrator || dev.Name != "Dev" || dev.OrganizationName != postgres.DevOrganizationName {
		t.Fatalf("unexpected dev principal: %+v", dev)
	}
	same, err := users.Principal(ctx, dev.OrganizationID, "dev")
	if err != nil || same != dev {
		t.Fatalf("principal by organization: %+v %v", same, err)
	}
	if _, err := users.Principal(ctx, dev.OrganizationID, "someone"); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("unknown user must be not found, got %v", err)
	}
	if lead, err := users.LeadInAnyProject(ctx, dev); err != nil || lead {
		t.Fatalf("dev leads nothing yet: %v %v", lead, err)
	}
}

func TestIdempotencyKeysReplayAndRefuseRace(t *testing.T) {
	db := postgrestest.New(t)
	seedDev(t, db)
	api := db.PoolAs("easycode_api")
	ctx := context.Background()
	dev, err := postgres.NewUsers(api).DevPrincipal(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
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
