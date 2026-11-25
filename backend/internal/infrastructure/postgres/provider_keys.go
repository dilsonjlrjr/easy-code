package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/idempotency"
	"easycode/internal/application/providerkey"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// ChannelCommands wakes the worker when the api queues a command (interno.md section 1).
const ChannelCommands = "easycode_commands"

// ProviderKeys stores sealed provider keys (provider_keys). The api role cannot read
// encrypted_value (column grants); only the worker reads it, to pass it to the gateway.
type ProviderKeys struct {
	pool *pgxpool.Pool
}

var (
	_ providerkey.Repository = ProviderKeys{}
	_ providerkey.TestStore  = ProviderKeys{}
)

// NewProviderKeys builds the adapter.
func NewProviderKeys(pool *pgxpool.Pool) ProviderKeys { return ProviderKeys{pool: pool} }

// Statuses implements providerkey.Repository: the active or testing row of each provider, else
// the latest row for its last test result.
func (k ProviderKeys) Statuses(ctx context.Context, organizationID string) (map[string]providerkey.KeyStatus, error) {
	rows, err := conn(ctx, k.pool).Query(WithOperation(ctx, "provider_keys.statuses"), `
SELECT DISTINCT ON (pk.provider) pk.id, pk.provider, pk.state, pk.last4, pk.created_at, u.sub, u.display_name,
       pk.tested_at, pk.test_result, pk.credential_kind
  FROM easycode.provider_keys pk
  JOIN easycode.organizations o ON o.id = pk.organization_id
  JOIN easycode.users u ON u.id = pk.created_by
 WHERE o.public_id = $1::uuid
 ORDER BY pk.provider, (pk.state IN ('active','testing')) DESC, pk.created_at DESC, pk.id DESC`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("provider key statuses: %w", classify(err))
	}
	defer rows.Close()
	statuses := map[string]providerkey.KeyStatus{}
	for rows.Next() {
		var (
			id                 int64
			name, state, last4 string
			createdAt          time.Time
			sub, display       string
			testedAt           *time.Time
			testResult, kind   *string
		)
		if err := rows.Scan(&id, &name, &state, &last4, &createdAt, &sub, &display, &testedAt, &testResult, &kind); err != nil {
			return nil, fmt.Errorf("scan provider key: %w", err)
		}
		status := providerkey.KeyStatus{Provider: name, Status: providerkey.StatusMissing, TestedAt: testedAt}
		if testResult != nil && testedAt != nil {
			result, message := providerkey.DecodeResult(*testResult)
			status.LastTest = &providerkey.TestResult{Result: result, Message: message, TestedAt: *testedAt}
		}
		switch state {
		case providerkey.StateActive, providerkey.StateTesting:
			status.Status = state
			status.Last4 = last4
			status.SavedAt = &createdAt
			status.SavedBy = &identity.Principal{Subject: sub, Name: display}
			if kind != nil {
				status.CredentialKind = *kind
			}
			if state == providerkey.StateActive {
				status.ActiveKeyID = id
			}
		}
		statuses[name] = status
	}
	return statuses, rows.Err()
}

// StartTest implements providerkey.Repository.
// credentialKind "" is stored as NULL (providers without kinds).
func (k ProviderKeys) StartTest(ctx context.Context, p identity.Principal, providerName string, sealed []byte, keyRef, last4, credentialKind string) (int64, error) {
	q := conn(ctx, k.pool)
	var keyID, organizationID, userID int64
	err := q.QueryRow(WithOperation(ctx, "provider_keys.insert_testing"), `
INSERT INTO easycode.provider_keys (organization_id, provider, encrypted_value, encryption_key_ref, last4, created_by, credential_kind)
SELECT o.id, $3, $4, $5, $6, u.id, nullif($7, '')
  FROM easycode.organizations o JOIN easycode.users u ON u.organization_id = o.id
 WHERE o.public_id = $1::uuid AND u.sub = $2
RETURNING id, organization_id, created_by`, p.OrganizationID, p.Subject, providerName, sealed, keyRef, last4, credentialKind).Scan(&keyID, &organizationID, &userID)
	if err != nil {
		if failure.From(classify(err)).Code == CodeDuplicate {
			return 0, failure.New(failure.KindConflict, failure.CodeStateConflict, err).With("current_state", providerkey.StateTesting)
		}
		return 0, fmt.Errorf("insert provider key: %w", classify(err))
	}
	commandKey, err := uuid.NewV7()
	if err != nil {
		return 0, err
	}
	commandID, err := uuid.NewV7()
	if err != nil {
		return 0, err
	}
	_, err = q.Exec(WithOperation(ctx, "commands.insert_key_test"), `
INSERT INTO easycode.commands (public_id, organization_id, target_type, provider_key_id, type, payload, requested_by, origin,
  idempotency_key, body_hash)
VALUES ($1::uuid, $2, 'provider_key', $3, 'test_provider_key', jsonb_build_object('provider_key_id', $3::bigint), $4, 'web', $5::uuid, $6)`,
		commandID.String(), organizationID, keyID, userID, commandKey.String(), idempotency.HashBody(fmt.Appendf(nil, "test_provider_key:%d", keyID)))
	if err != nil {
		return 0, fmt.Errorf("queue key test: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "commands.notify"), "SELECT pg_notify($1, 'organization')", ChannelCommands); err != nil {
		return 0, fmt.Errorf("notify commands: %w", err)
	}
	return keyID, nil
}

// TestState implements providerkey.Repository.
func (k ProviderKeys) TestState(ctx context.Context, keyID int64) (string, providerkey.TestResult, error) {
	var state string
	var stored *string
	var testedAt *time.Time
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "provider_keys.test_state"),
		"SELECT state, test_result, tested_at FROM easycode.provider_keys WHERE id = $1", keyID).Scan(&state, &stored, &testedAt)
	if err != nil {
		return "", providerkey.TestResult{}, fmt.Errorf("read key test: %w", classify(err))
	}
	result := providerkey.TestResult{}
	if stored != nil {
		result.Result, result.Message = providerkey.DecodeResult(*stored)
	}
	if testedAt != nil {
		result.TestedAt = *testedAt
	}
	return state, result, nil
}

// DiscardUntested implements providerkey.Repository.
func (k ProviderKeys) DiscardUntested(ctx context.Context, keyID int64, result string) (bool, error) {
	tag, err := conn(ctx, k.pool).Exec(WithOperation(ctx, "provider_keys.discard_untested"), `
UPDATE easycode.provider_keys SET state = 'discarded', encrypted_value = NULL, test_result = $2, tested_at = now()
 WHERE id = $1 AND state = 'testing'`, keyID, result)
	if err != nil {
		return false, fmt.Errorf("discard key test: %w", classify(err))
	}
	return tag.RowsAffected() == 1, nil
}

// RemoveActive implements providerkey.Repository.
func (k ProviderKeys) RemoveActive(ctx context.Context, organizationID, providerName string) error {
	tag, err := conn(ctx, k.pool).Exec(WithOperation(ctx, "provider_keys.remove"), `
UPDATE easycode.provider_keys pk SET state = 'removed', encrypted_value = NULL, removed_at = now()
  FROM easycode.organizations o
 WHERE o.id = pk.organization_id AND o.public_id = $1::uuid AND pk.provider = $2 AND pk.state = 'active'`, organizationID, providerName)
	if err != nil {
		return fmt.Errorf("remove provider key: %w", classify(err))
	}
	if tag.RowsAffected() == 0 {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return nil
}

// LockForTest implements providerkey.TestStore (worker role).
func (k ProviderKeys) LockForTest(ctx context.Context, keyID int64) (providerkey.KeyUnderTest, error) {
	key := providerkey.KeyUnderTest{ID: keyID}
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "provider_keys.lock_for_test"), `
SELECT provider, state, coalesce(encrypted_value, ''::bytea), test_expires_at FROM easycode.provider_keys WHERE id = $1 FOR UPDATE`, keyID).
		Scan(&key.Provider, &key.State, &key.Sealed, &key.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return key, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return key, fmt.Errorf("lock key for test: %w", classify(err))
	}
	return key, nil
}

// Activate implements providerkey.TestStore: the previous active key of the provider is
// replaced first (one active key per provider and organization).
func (k ProviderKeys) Activate(ctx context.Context, keyID int64) error {
	q := conn(ctx, k.pool)
	_, err := q.Exec(WithOperation(ctx, "provider_keys.replace_previous"), `
UPDATE easycode.provider_keys old SET state = 'replaced', encrypted_value = NULL
  FROM easycode.provider_keys new
 WHERE new.id = $1 AND old.organization_id = new.organization_id AND old.provider = new.provider AND old.state = 'active'`, keyID)
	if err != nil {
		return fmt.Errorf("replace previous key: %w", classify(err))
	}
	_, err = q.Exec(WithOperation(ctx, "provider_keys.activate"), `
UPDATE easycode.provider_keys SET state = 'active', test_result = 'valid', tested_at = now() WHERE id = $1 AND state = 'testing'`, keyID)
	if err != nil {
		return fmt.Errorf("activate key: %w", classify(err))
	}
	return nil
}

// Discard implements providerkey.TestStore.
func (k ProviderKeys) Discard(ctx context.Context, keyID int64, result string) error {
	_, err := conn(ctx, k.pool).Exec(WithOperation(ctx, "provider_keys.discard"), `
UPDATE easycode.provider_keys SET state = 'discarded', encrypted_value = NULL, test_result = $2, tested_at = now()
 WHERE id = $1 AND state = 'testing'`, keyID, result)
	if err != nil {
		return fmt.Errorf("discard key: %w", classify(err))
	}
	return nil
}

// ActiveSealedKey returns the sealed active key of the project's organization for the provider,
// for the agent runtime to open a gateway session; without one it fails with
// provider-without-key, before any provider call (US-15 CA-4). Worker role only.
func (k ProviderKeys) ActiveSealedKey(ctx context.Context, projectID, providerName string) ([]byte, error) {
	var sealed []byte
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "provider_keys.active_sealed"), `
SELECT pk.encrypted_value FROM easycode.provider_keys pk JOIN easycode.projects p ON p.organization_id = pk.organization_id
 WHERE p.public_id = $1::uuid AND pk.provider = $2 AND pk.state = 'active'`, projectID, providerName).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, failure.New(failure.KindConflict, providerkey.CodeProviderWithoutKey, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("read active key: %w", classify(err))
	}
	return sealed, nil
}
