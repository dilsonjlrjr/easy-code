package postgres_test

import (
	"context"
	"testing"
	"time"

	"easycode/internal/application/command"
	"easycode/internal/application/providerkey"
	"easycode/internal/infrastructure/postgres"
)

type acceptingGateway struct{}

func (acceptingGateway) TestKey(context.Context, string, []byte) (bool, string, error) {
	return true, "", nil
}

// US-58 CA-1, CA-2: the worker records the detection, the api reads it; without a row Claude Code
// is not detected, and the api role cannot write it (ADR-18: api reads, worker writes).
func TestClaudeCodeRuntimeIsRecordedByTheWorkerAndReadByTheAPI(t *testing.T) {
	f := newKeyFixture(t, time.Second, false)
	ctx := context.Background()
	api := postgres.NewProviderKeys(f.db.PoolAs("easycode_api"))

	runtime, err := api.ClaudeCodeRuntime(ctx)
	if err != nil || runtime.Detected || runtime.CheckedAt != nil {
		t.Fatalf("CA-2: no row is not detected: %+v %v", runtime, err)
	}
	if err := api.RecordClaudeCodeRuntime(ctx, true, "2.1.0"); err == nil {
		t.Fatal("the api role must not write the detection")
	}
	if err := f.keys.RecordClaudeCodeRuntime(ctx, true, "2.1.0 (Claude Code)"); err != nil {
		t.Fatal(err)
	}
	runtime, err = api.ClaudeCodeRuntime(ctx)
	if err != nil || !runtime.Detected || runtime.Version != "2.1.0 (Claude Code)" || runtime.CheckedAt == nil {
		t.Fatalf("CA-1: %+v %v", runtime, err)
	}
	if err := f.keys.RecordClaudeCodeRuntime(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	runtime, err = api.ClaudeCodeRuntime(ctx)
	if err != nil || runtime.Detected || runtime.Version != "" {
		t.Fatalf("not detected again: %+v %v", runtime, err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.claude_code_runtime"); n != 1 {
		t.Fatalf("one row only, got %d", n)
	}
}

// US-58 CA-1, CA-2, CA-3, CA-4: a claude-code key keeps the kind derived from its prefix, the list
// shows it with the detection, and the provider becomes usable in a model pair only with both.
func TestClaudeCodeKeyKeepsItsKindAndGatesTheModelPair(t *testing.T) {
	f := newKeyFixture(t, 5*time.Second, false)
	ctx := context.Background()
	worker := f.db.PoolAs("easycode_worker")
	dispatcher := command.NewDispatcher(postgres.NewTxRunner(worker), postgres.NewCommands(worker))
	tester := providerkey.NewTester(postgres.NewProviderKeys(worker), acceptingGateway{})
	dispatcher.Register("test_provider_key", func(ctx context.Context, cmd command.Command) error { return tester.Test(ctx, cmd.ProviderKeyID) })
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		for workerCtx.Err() == nil {
			_, _ = dispatcher.Drain(workerCtx)
			time.Sleep(20 * time.Millisecond)
		}
	}()
	catalog := postgres.NewCatalog(f.db.PoolAs("easycode_api"))

	if usable, err := catalog.ProviderUsable(ctx, f.dev.OrganizationID, providerkey.ProviderClaudeCode); err != nil || usable {
		t.Fatalf("CA-2: not detected and without key: %v %v", usable, err)
	}
	if usable, err := catalog.ProviderUsable(ctx, f.dev.OrganizationID, "anthropic"); err != nil || !usable {
		t.Fatalf("other providers stay usable: %v %v", usable, err)
	}

	status, err := f.service.Save(ctx, f.dev, providerkey.ProviderClaudeCode, "sk-ant-oat01-canary-0123456789wxyz")
	if err != nil {
		t.Fatal(err)
	}
	if status.CredentialKind != providerkey.CredentialSubscriptionToken || status.Last4 != "wxyz" || status.Runtime == nil || status.Runtime.Detected {
		t.Fatalf("CA-3/CA-4: %+v", status)
	}
	if usable, _ := catalog.ProviderUsable(ctx, f.dev.OrganizationID, providerkey.ProviderClaudeCode); usable {
		t.Fatal("CA-2: a key without detection is not enough")
	}
	if err := f.keys.RecordClaudeCodeRuntime(ctx, true, "2.1.0"); err != nil {
		t.Fatal(err)
	}
	if usable, err := catalog.ProviderUsable(ctx, f.dev.OrganizationID, providerkey.ProviderClaudeCode); err != nil || !usable {
		t.Fatalf("detected with active key: %v %v", usable, err)
	}

	if _, err := f.service.Save(ctx, f.dev, providerkey.ProviderClaudeCode, "sk-ant-api03-canary-0123456789abcd"); err != nil {
		t.Fatal(err)
	}
	listed, err := f.service.List(ctx, f.dev)
	if err != nil {
		t.Fatal(err)
	}
	claudeCode := listed[2]
	if claudeCode.Provider != providerkey.ProviderClaudeCode || claudeCode.CredentialKind != providerkey.CredentialAPIKey || claudeCode.Runtime == nil || !claudeCode.Runtime.Detected {
		t.Fatalf("CA-1: %+v", claudeCode)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.provider_keys WHERE provider = 'anthropic' OR credential_kind IS NULL"); n != 0 {
		t.Fatalf("only claude-code keys, all with kind; got %d without", n)
	}
}
