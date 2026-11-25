package providerkey

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/identity"
)

type directTx struct{}

func (directTx) InTx(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

type nopTrail struct{}

func (nopTrail) Append(context.Context, domainaudit.Record) (domainaudit.Entry, error) {
	return domainaudit.Entry{}, nil
}

type plainSealer struct{}

func (plainSealer) Seal([]byte) ([]byte, string, error) { return []byte("sealed"), "k1", nil }

// fakeRepo activates every key at once and remembers the kind it got.
type fakeRepo struct {
	runtime   Runtime
	statuses  map[string]KeyStatus
	gotKind   string
	gotLast4  string
	runtimeOK bool
}

func (r *fakeRepo) Statuses(context.Context, string) (map[string]KeyStatus, error) {
	return r.statuses, nil
}

func (r *fakeRepo) StartTest(_ context.Context, _ identity.Principal, providerName string, _ []byte, _, last4, kind string) (int64, error) {
	r.gotKind, r.gotLast4 = kind, last4
	r.statuses[providerName] = KeyStatus{Provider: providerName, Status: StatusActive, Last4: last4, CredentialKind: kind}
	return 1, nil
}

func (r *fakeRepo) TestState(context.Context, int64) (string, TestResult, error) {
	return StateActive, TestResult{Result: ResultValid}, nil
}

func (r *fakeRepo) DiscardUntested(context.Context, int64, string) (bool, error) { return false, nil }

func (r *fakeRepo) RemoveActive(context.Context, string, string) error { return nil }

func (r *fakeRepo) ClaudeCodeRuntime(context.Context) (Runtime, error) {
	r.runtimeOK = true
	return r.runtime, nil
}

var admin = identity.Principal{OrganizationID: "org", Subject: "admin", Administrator: true}

func TestCredentialKindComesFromThePrefixOnlyForClaudeCode(t *testing.T) {
	cases := []struct{ provider, key, want string }{
		{ProviderClaudeCode, "sk-ant-oat01-abcdefgh", CredentialSubscriptionToken},
		{ProviderClaudeCode, "sk-ant-api03-abcdefgh", CredentialAPIKey},
		{"anthropic", "sk-ant-oat01-abcdefgh", ""},
		{"openai", "sk-abcdefgh", ""},
	}
	for _, c := range cases {
		if got := CredentialKind(c.provider, c.key); got != c.want {
			t.Errorf("CredentialKind(%s, …) = %q, want %q", c.provider, got, c.want)
		}
	}
}

func TestListShowsClaudeCodeWithItsDetection(t *testing.T) {
	checked := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	repo := &fakeRepo{statuses: map[string]KeyStatus{}, runtime: Runtime{Detected: true, Version: "2.1.0", CheckedAt: &checked}}
	service := NewService(directTx{}, repo, plainSealer{}, nopTrail{}, time.Second)

	list, err := service.List(context.Background(), admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[2].Provider != ProviderClaudeCode {
		t.Fatalf("providers = %+v", list)
	}
	if list[2].Runtime == nil || !list[2].Runtime.Detected || list[2].Runtime.Version != "2.1.0" {
		t.Fatalf("claude-code runtime = %+v", list[2].Runtime)
	}
	if list[0].Runtime != nil || list[1].Runtime != nil {
		t.Fatal("only claude-code carries the detection")
	}
}

func TestSaveSealsTheClaudeCodeKeyWithItsKind(t *testing.T) {
	repo := &fakeRepo{statuses: map[string]KeyStatus{}}
	service := NewService(directTx{}, repo, plainSealer{}, nopTrail{}, time.Second)

	status, err := service.Save(context.Background(), admin, ProviderClaudeCode, "sk-ant-oat01-secret-wxyz")
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotKind != CredentialSubscriptionToken || repo.gotLast4 != "wxyz" {
		t.Fatalf("kind %q last4 %q", repo.gotKind, repo.gotLast4)
	}
	if status.CredentialKind != CredentialSubscriptionToken || status.Runtime == nil {
		t.Fatalf("status = %+v", status)
	}

	if _, err := service.Save(context.Background(), admin, "anthropic", "sk-ant-api03-secret"); err != nil {
		t.Fatal(err)
	}
	if repo.gotKind != "" {
		t.Fatalf("anthropic key got kind %q", repo.gotKind)
	}
}

type fakeProbe struct {
	available bool
	version   string
	err       error
}

func (p fakeProbe) ClaudeCode(context.Context) (bool, string, error) {
	return p.available, p.version, p.err
}

type recordedRuntime struct {
	detected bool
	version  string
	calls    int
}

func (r *recordedRuntime) RecordClaudeCodeRuntime(_ context.Context, detected bool, version string) error {
	r.detected, r.version = detected, version
	r.calls++
	return nil
}

func TestSyncRuntimeRecordsWhatTheRunnerReports(t *testing.T) {
	store := &recordedRuntime{}
	got, err := SyncRuntime(context.Background(), fakeProbe{available: true, version: " 2.1.0 (Claude Code)\n"}, store)
	if err != nil {
		t.Fatal(err)
	}
	if !store.detected || store.version != "2.1.0 (Claude Code)" || !got.Detected {
		t.Fatalf("recorded %+v", store)
	}
}

func TestSyncRuntimeRecordsNotDetectedWhenTheRunnerFails(t *testing.T) {
	store := &recordedRuntime{detected: true}
	probeErr := errors.New("runner down")
	if _, err := SyncRuntime(context.Background(), fakeProbe{available: true, version: "x", err: probeErr}, store); !errors.Is(err, probeErr) {
		t.Fatalf("err = %v", err)
	}
	if store.calls != 1 || store.detected || store.version != "" {
		t.Fatalf("recorded %+v", store)
	}
}

func TestUsableRequiresDetectionAndKeyOnlyForClaudeCode(t *testing.T) {
	detected := Runtime{Detected: true}
	if Usable(ProviderClaudeCode, Runtime{}, true) || Usable(ProviderClaudeCode, detected, false) {
		t.Fatal("claude-code needs detection and an active key")
	}
	if !Usable(ProviderClaudeCode, detected, true) || !Usable("anthropic", Runtime{}, false) {
		t.Fatal("usable providers refused")
	}
}
