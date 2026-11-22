package postgres_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/application/command"
	appgateway "easycode/internal/application/gateway"
	"easycode/internal/application/providerkey"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/provider/fake"
	"easycode/internal/infrastructure/sealing"
)

const canaryKey = "sk-ant-canary-RNF01-0123456789abcd"

// inProcessGateway runs the gateway use case in the test process with the real opener.
type inProcessGateway struct{ service *appgateway.Service }

func (g inProcessGateway) TestKey(ctx context.Context, providerName string, sealed []byte) (bool, string, error) {
	err := g.service.TestKey(ctx, providerName, sealed)
	var providerErr *provider.Error
	switch {
	case err == nil:
		return true, "", nil
	case errors.As(err, &providerErr) && providerErr.Class == provider.ErrorAuthentication:
		return false, providerErr.Message, nil
	default:
		return false, "", err
	}
}

type keyFixture struct {
	db      *postgrestest.Database
	dev     identity.Principal
	service *providerkey.Service
	keys    postgres.ProviderKeys
}

func newKeyFixture(t *testing.T, timeout time.Duration, withWorker bool) keyFixture {
	t.Helper()
	db := postgrestest.New(t)
	ctx := context.Background()
	api := db.PoolAs("easycode_api")
	worker := db.PoolAs("easycode_worker")
	dev := seedDev(t, db)
	dir := t.TempDir()
	if _, err := sealing.GenerateKeyPair(filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")); err != nil {
		t.Fatal(err)
	}
	sealer, _ := sealing.LoadSealer(filepath.Join(dir, "public.pem"))
	opener, _ := sealing.LoadOpener(filepath.Join(dir, "private.pem"))
	service := providerkey.NewService(postgres.NewTxRunner(api), postgres.NewProviderKeys(api), sealer, postgres.NewAuditTrail(api), timeout)
	service.SetPollInterval(20 * time.Millisecond)
	if withWorker {
		gateway := inProcessGateway{service: appgateway.NewService(opener, time.Minute, &fake.Provider{Alias: "anthropic"})}
		dispatcher := command.NewDispatcher(postgres.NewTxRunner(worker), postgres.NewCommands(worker))
		tester := providerkey.NewTester(postgres.NewProviderKeys(worker), gateway)
		dispatcher.Register("test_provider_key", func(ctx context.Context, cmd command.Command) error {
			return tester.Test(ctx, cmd.ProviderKeyID)
		})
		workerCtx, stop := context.WithCancel(ctx)
		t.Cleanup(stop)
		go func() {
			for workerCtx.Err() == nil {
				_, _ = dispatcher.Drain(workerCtx)
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}
	return keyFixture{db: db, dev: dev, service: service, keys: postgres.NewProviderKeys(worker)}
}

// CA-1, CA-2, RNF-01: a valid key is tested, becomes active and only the last 4 characters show.
func TestValidKeyIsTestedActivatedAndNeverStoredInClear(t *testing.T) {
	f := newKeyFixture(t, 5*time.Second, true)
	ctx := context.Background()
	status, err := f.service.Save(ctx, f.dev, "anthropic", canaryKey)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != providerkey.StatusActive || status.Last4 != "abcd" || status.SavedBy == nil || status.SavedBy.Subject != "dev" {
		t.Fatalf("CA-1: unexpected status %+v", status)
	}
	listed, err := f.service.List(ctx, f.dev)
	if err != nil || len(listed) != 2 || listed[0].Status != providerkey.StatusActive || listed[1].Status != providerkey.StatusMissing {
		t.Fatalf("list: %+v %v", listed, err)
	}
	// RNF-01 canary: the clear value is nowhere in the database.
	for _, query := range []string{
		"SELECT count(*) FROM easycode.provider_keys WHERE position(convert_to('" + canaryKey + "', 'UTF8') in coalesce(encrypted_value, ''::bytea)) > 0",
		"SELECT count(*) FROM easycode.commands WHERE payload::text LIKE '%" + canaryKey + "%'",
		"SELECT count(*) FROM easycode.audit_records WHERE row_to_json(audit_records)::text LIKE '%" + canaryKey + "%'",
		"SELECT count(*) FROM easycode.events WHERE envelope::text LIKE '%" + canaryKey + "%'",
	} {
		if n := count(t, f.db, query); n != 0 {
			t.Fatalf("RNF-01: key found in clear by %q", query)
		}
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'key.save' AND decision = 'valid'"); n != 1 {
		t.Fatalf("RNF-13: key.save must be audited, got %d", n)
	}
	// A second valid key replaces the first.
	if _, err := f.service.Save(ctx, f.dev, "anthropic", "sk-ant-second-key-000000000000wxyz"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.provider_keys WHERE state = 'replaced' AND encrypted_value IS NULL"); n != 1 {
		t.Fatalf("previous key must be replaced without value, got %d", n)
	}
}

// A key outside 8..512 characters is a validation error before anything is stored.
func TestShortKeyIsAValidationError(t *testing.T) {
	f := newKeyFixture(t, time.Second, false)
	if _, err := f.service.Save(context.Background(), f.dev, "anthropic", "short"); failure.From(err).Code != failure.CodeValidation {
		t.Fatalf("want validation, got %v", err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.provider_keys"); n != 0 {
		t.Fatalf("nothing stored, got %d", n)
	}
}

// CA-3: the provider refuses the key; the worker discards it with the summarized message.
func TestProviderRefusalDiscardsTheSealedValue(t *testing.T) {
	f := newKeyFixture(t, 5*time.Second, false)
	ctx := context.Background()
	worker := f.db.PoolAs("easycode_worker")
	dispatcher := command.NewDispatcher(postgres.NewTxRunner(worker), postgres.NewCommands(worker))
	tester := providerkey.NewTester(postgres.NewProviderKeys(worker), refusingGateway{})
	dispatcher.Register("test_provider_key", func(ctx context.Context, cmd command.Command) error { return tester.Test(ctx, cmd.ProviderKeyID) })
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		for workerCtx.Err() == nil {
			_, _ = dispatcher.Drain(workerCtx)
			time.Sleep(20 * time.Millisecond)
		}
	}()
	_, err := f.service.Save(ctx, f.dev, "anthropic", canaryKey)
	classified := failure.From(err)
	if classified.Code != providerkey.CodeInvalidKey || classified.Extensions["provider_message"] != "invalid x-api-key" {
		t.Fatalf("CA-3: want invalid-key with the provider message, got %v %v", err, classified.Extensions)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.provider_keys WHERE state = 'discarded' AND encrypted_value IS NULL AND test_result LIKE 'invalid%'"); n != 1 {
		t.Fatalf("CA-3: invalid key must be discarded without value, got %d", n)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.commands WHERE type = 'test_provider_key' AND status = 'applied'"); n != 1 {
		t.Fatalf("command must be applied, got %d", n)
	}
	listed, _ := f.service.List(ctx, f.dev)
	if listed[0].Status != providerkey.StatusMissing || listed[0].LastTest == nil || listed[0].LastTest.Result != providerkey.ResultInvalid {
		t.Fatalf("status shows the last test without the key: %+v", listed[0])
	}
}

type refusingGateway struct{}

func (refusingGateway) TestKey(context.Context, string, []byte) (bool, string, error) {
	return false, "invalid x-api-key", nil
}

// No worker answer within the deadline: the api discards the row itself and answers 503.
func TestUnansweredKeyTestIsDiscardedByTheAPI(t *testing.T) {
	f := newKeyFixture(t, 300*time.Millisecond, false)
	_, err := f.service.Save(context.Background(), f.dev, "anthropic", canaryKey)
	if failure.From(err).Kind != failure.KindUnavailable {
		t.Fatalf("want service unavailable, got %v", err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.provider_keys WHERE state = 'discarded' AND encrypted_value IS NULL AND test_result = 'no_answer'"); n != 1 {
		t.Fatalf("untested key must be discarded, got %d", n)
	}
}

// CA-4: without an active key the runtime gets provider-without-key before any provider call.
func TestRemovedKeyLeavesTheProviderWithoutKey(t *testing.T) {
	f := newKeyFixture(t, 5*time.Second, true)
	ctx := context.Background()
	if _, err := f.service.Save(ctx, f.dev, "anthropic", canaryKey); err != nil {
		t.Fatal(err)
	}
	project := f.db.CreateProject("key-project")
	if _, err := f.keys.ActiveSealedKey(ctx, project.ProjectID, "anthropic"); failure.From(err).Code != providerkey.CodeProviderWithoutKey {
		t.Fatalf("another organization has no key: %v", err)
	}
	devProject := devProjectID(t, f.db)
	sealed, err := f.keys.ActiveSealedKey(ctx, devProject, "anthropic")
	if err != nil || len(sealed) == 0 || strings.Contains(string(sealed), canaryKey) {
		t.Fatalf("active sealed key: %v", err)
	}
	if err := f.service.Remove(ctx, f.dev, "anthropic"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.keys.ActiveSealedKey(ctx, devProject, "anthropic"); failure.From(err).Code != providerkey.CodeProviderWithoutKey {
		t.Fatalf("CA-4: want provider-without-key, got %v", err)
	}
	if err := f.service.Remove(ctx, f.dev, "anthropic"); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("removing again is not found, got %v", err)
	}
	member := f.dev
	member.Administrator = false
	if _, err := f.service.Save(ctx, member, "anthropic", canaryKey); failure.From(err).Kind != failure.KindForbidden {
		t.Fatalf("only the administrator saves keys, got %v", err)
	}
}

// devProjectID inserts a project in the dev organization (fixture for organization-scoped reads).
func devProjectID(t *testing.T, db *postgrestest.Database) string {
	t.Helper()
	var id string
	err := db.Admin.QueryRow(context.Background(), `
WITH u AS (SELECT u.id, u.organization_id FROM easycode.users u WHERE u.sub = 'dev'),
p AS (INSERT INTO easycode.projects (organization_id, name, business_need, provider_acknowledgment_by, provider_acknowledgment_at, created_by)
      SELECT organization_id, 'dev project', 'need', id, now(), id FROM u RETURNING id, public_id),
m AS (INSERT INTO easycode.project_members (project_id, user_id, role, assigned_by) SELECT p.id, u.id, 'lead', u.id FROM p, u)
SELECT public_id::text FROM p`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
