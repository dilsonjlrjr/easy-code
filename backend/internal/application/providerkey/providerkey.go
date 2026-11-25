// Package providerkey holds the BYOK provider key use cases (US-15, ADR-13): the administrator
// saves a key, the api seals it and asks the worker to test it through easycode-gateway, and
// only a key that passed the test becomes active. The key value never returns, never goes to
// logs, events or audit records (RNF-01).
package providerkey

import (
	"context"
	"strings"
	"time"

	"easycode/internal/application/audit"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Failure codes (openapi Problem).
const (
	CodeInvalidKey = "invalid-key"
	// CodeProviderWithoutKey is the wave failure reason when no active key exists (US-15 CA-4).
	CodeProviderWithoutKey = "provider-without-key"
)

// ProviderClaudeCode is the Claude Code of the server run as the agent executor (US-58, ADR-23).
const ProviderClaudeCode = "claude-code"

// Providers are the provider ids of the contract (ProviderId).
var Providers = []string{"anthropic", "openai", ProviderClaudeCode}

// Credential kinds of a claude-code key (provider_keys.credential_kind, US-58 CA-1 and CA-4).
const (
	CredentialAPIKey            = "api_key"
	CredentialSubscriptionToken = "subscription_token"
)

// subscriptionPrefix starts the OAuth token made by `claude setup-token`.
const subscriptionPrefix = "sk-ant-oat"

// CredentialKind derives the kind of a key from its prefix before it is sealed: only claude-code
// keys have a kind, the other providers answer "".
func CredentialKind(providerName, key string) string {
	if providerName != ProviderClaudeCode {
		return ""
	}
	if strings.HasPrefix(key, subscriptionPrefix) {
		return CredentialSubscriptionToken
	}
	return CredentialAPIKey
}

// Key statuses shown by the api (Provider.key.status).
const (
	StatusMissing = "missing"
	StatusTesting = "testing"
	StatusActive  = "active"
)

// Test results (Provider.key.last_test.result).
const (
	ResultValid    = "valid"
	ResultInvalid  = "invalid"
	ResultNoAnswer = "no_answer"
)

// Key row states (provider_keys.state).
const (
	StateTesting   = "testing"
	StateActive    = "active"
	StateDiscarded = "discarded"
)

// TestResult is the outcome of the last key test, without the key.
type TestResult struct {
	Result   string
	Message  string
	TestedAt time.Time
}

// KeyStatus is what anyone may see about a provider key (US-15 CA-2: never the value).
type KeyStatus struct {
	Provider    string
	Status      string
	Last4       string
	SavedAt     *time.Time
	SavedBy     *identity.Principal // subject and name only
	TestedAt    *time.Time
	LastTest    *TestResult
	ActiveKeyID int64
	// CredentialKind is the kind of the active or testing claude-code key ("" otherwise).
	CredentialKind string
	// Runtime is the Claude Code detection, only on the claude-code provider.
	Runtime *Runtime
}

// Runtime is the last Claude Code detection made by easycode-sandbox (claude_code_runtime).
// CheckedAt is nil while no check was recorded (not detected).
type Runtime struct {
	Detected  bool
	Version   string
	CheckedAt *time.Time
}

// Sealer seals a key with the platform public key (ADR-13); the api cannot open it.
type Sealer interface {
	Seal(plain []byte) (sealed []byte, keyRef string, err error)
}

// Repository stores provider keys (provider_keys) and the test command.
type Repository interface {
	// Statuses lists the key status of each provider that has rows in the organization.
	Statuses(ctx context.Context, organizationID string) (map[string]KeyStatus, error)
	// StartTest stores the sealed key in testing and queues test_provider_key in the caller's
	// transaction; a test already running for the provider is a state conflict.
	// credentialKind is "" for providers without kinds.
	StartTest(ctx context.Context, principal identity.Principal, providerName string, sealed []byte, keyRef, last4, credentialKind string) (int64, error)
	// TestState reads the state and result of a key row.
	TestState(ctx context.Context, keyID int64) (string, TestResult, error)
	// DiscardUntested discards a row still in testing (deadline passed); false if it had a result.
	DiscardUntested(ctx context.Context, keyID int64, result string) (bool, error)
	// RemoveActive moves the active key to removed and erases its value; not-found without one.
	RemoveActive(ctx context.Context, organizationID, providerName string) error
	// ClaudeCodeRuntime reads the last detection; without a row it is not detected.
	ClaudeCodeRuntime(ctx context.Context) (Runtime, error)
}

// Service is the api side of provider keys.
type Service struct {
	tx       transaction.Runner
	repo     Repository
	sealer   Sealer
	trail    audit.Trail
	timeout  time.Duration
	interval time.Duration
	now      func() time.Time
}

// NewService builds the service. timeout bounds the key test (15 s).
func NewService(tx transaction.Runner, repo Repository, sealer Sealer, trail audit.Trail, timeout time.Duration) *Service {
	return &Service{tx: tx, repo: repo, sealer: sealer, trail: trail, timeout: timeout, interval: 200 * time.Millisecond, now: time.Now}
}

// SetPollInterval changes how often the test result is read (tests).
func (s *Service) SetPollInterval(interval time.Duration) { s.interval = interval }

// List is listProviders.
func (s *Service) List(ctx context.Context, principal identity.Principal) ([]KeyStatus, error) {
	statuses, err := s.repo.Statuses(ctx, principal.OrganizationID)
	if err != nil {
		return nil, err
	}
	runtime, err := s.repo.ClaudeCodeRuntime(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]KeyStatus, 0, len(Providers))
	for _, name := range Providers {
		status, ok := statuses[name]
		if !ok {
			status = KeyStatus{Provider: name, Status: StatusMissing}
		}
		if name == ProviderClaudeCode {
			detected := runtime
			status.Runtime = &detected
		}
		list = append(list, status)
	}
	return list, nil
}

// Save is saveProviderKey: seal, test through the gateway and answer only after the result.
func (s *Service) Save(ctx context.Context, principal identity.Principal, providerName, key string) (KeyStatus, error) {
	if err := s.authorize(principal, providerName); err != nil {
		return KeyStatus{}, err
	}
	if len(key) < 8 || len(key) > 512 {
		return KeyStatus{}, failure.Invalid(failure.FieldError{Field: "key", Message: "length"})
	}
	sealed, keyRef, err := s.sealer.Seal([]byte(key))
	if err != nil {
		return KeyStatus{}, err
	}
	var keyID int64
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		keyID, err = s.repo.StartTest(ctx, principal, providerName, sealed, keyRef, key[len(key)-4:], CredentialKind(providerName, key))
		return err
	})
	if err != nil {
		return KeyStatus{}, err
	}
	state, result, err := s.await(ctx, keyID)
	if err != nil {
		return KeyStatus{}, err
	}
	if err := s.record(ctx, principal, "key.save", providerName, result.Result); err != nil {
		return KeyStatus{}, err
	}
	switch {
	case state == StateActive:
		statuses, err := s.repo.Statuses(ctx, principal.OrganizationID)
		if err != nil {
			return KeyStatus{}, err
		}
		status := statuses[providerName]
		if providerName == ProviderClaudeCode {
			runtime, err := s.repo.ClaudeCodeRuntime(ctx)
			if err != nil {
				return KeyStatus{}, err
			}
			status.Runtime = &runtime
		}
		return status, nil
	case result.Result == ResultInvalid:
		return KeyStatus{}, failure.New(failure.KindConflict, CodeInvalidKey, nil).With("provider_message", result.Message)
	default:
		return KeyStatus{}, failure.New(failure.KindUnavailable, failure.CodeServiceUnavailable, nil)
	}
}

// await polls the test until it has a result or the deadline passes; then it discards the row
// itself so that no state keeps an untested key (A4).
func (s *Service) await(ctx context.Context, keyID int64) (string, TestResult, error) {
	deadline := s.now().Add(s.timeout)
	for {
		state, result, err := s.repo.TestState(ctx, keyID)
		if err != nil {
			return "", TestResult{}, err
		}
		if state != StateTesting {
			return state, result, nil
		}
		if !s.now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return "", TestResult{}, ctx.Err()
		case <-time.After(s.interval):
		}
	}
	discarded, err := s.repo.DiscardUntested(context.WithoutCancel(ctx), keyID, ResultNoAnswer)
	if err != nil {
		return "", TestResult{}, err
	}
	if !discarded { // the worker answered at the last moment
		state, result, err := s.repo.TestState(ctx, keyID)
		return state, result, err
	}
	return StateDiscarded, TestResult{Result: ResultNoAnswer}, nil
}

// Remove is removeProviderKey (US-15 CA-4: waves then fail with provider_without_key).
func (s *Service) Remove(ctx context.Context, principal identity.Principal, providerName string) error {
	if err := s.authorize(principal, providerName); err != nil {
		return err
	}
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.RemoveActive(ctx, principal.OrganizationID, providerName); err != nil {
			return err
		}
		return s.record(ctx, principal, "key.remove", providerName, "removed")
	})
}

func (s *Service) authorize(principal identity.Principal, providerName string) error {
	if !known(providerName) {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if !principal.Administrator {
		return failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	return nil
}

func (s *Service) record(ctx context.Context, principal identity.Principal, action, providerName, decision string) error {
	_, err := s.trail.Append(ctx, domainaudit.Record{
		OrganizationID: principal.OrganizationID,
		Actor:          domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
		Origin:         domainaudit.OriginWeb,
		Action:         action,
		Target:         "providers/" + providerName + "/key",
		Decision:       decision,
	})
	return err
}

func known(providerName string) bool {
	for _, name := range Providers {
		if name == providerName {
			return true
		}
	}
	return false
}
