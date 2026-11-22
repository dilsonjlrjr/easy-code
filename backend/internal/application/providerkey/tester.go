package providerkey

import (
	"context"
	"errors"
	"time"

	"easycode/internal/domain/failure"
)

// KeyUnderTest is a sealed key waiting for its test (worker side).
type KeyUnderTest struct {
	ID        int64
	Provider  string
	State     string
	Sealed    []byte
	ExpiresAt time.Time
}

// TestStore is what the worker reads and writes for a key test.
type TestStore interface {
	// LockForTest locks the key row of the command (SELECT … FOR UPDATE) in the caller's transaction.
	LockForTest(ctx context.Context, keyID int64) (KeyUnderTest, error)
	// Activate makes the key active and the previous active key of the provider replaced.
	Activate(ctx context.Context, keyID int64) error
	// Discard erases the sealed value and keeps the result.
	Discard(ctx context.Context, keyID int64, result string) error
}

// KeyTestGateway asks easycode-gateway to open and test a sealed key (interno.md section 3).
type KeyTestGateway interface {
	TestKey(ctx context.Context, providerName string, sealed []byte) (valid bool, message string, err error)
}

// Tester applies test_provider_key commands in the worker (US-15, A4).
type Tester struct {
	store   TestStore
	gateway KeyTestGateway
	now     func() time.Time
}

// NewTester builds the tester.
func NewTester(store TestStore, gateway KeyTestGateway) *Tester {
	return &Tester{store: store, gateway: gateway, now: time.Now}
}

// Test runs inside the transaction of the command: only valid within the deadline activates.
func (t *Tester) Test(ctx context.Context, keyID int64) error {
	key, err := t.store.LockForTest(ctx, keyID)
	if err != nil {
		if failure.From(err).Kind == failure.KindNotFound {
			return nil // nothing left to test
		}
		return err
	}
	if key.State != StateTesting {
		return nil // the api discarded it after the deadline
	}
	callCtx, cancel := context.WithDeadline(ctx, key.ExpiresAt)
	defer cancel()
	valid, message, err := t.gateway.TestKey(callCtx, key.Provider, key.Sealed)
	switch {
	case err != nil || errors.Is(callCtx.Err(), context.DeadlineExceeded) || !t.now().Before(key.ExpiresAt):
		return t.store.Discard(ctx, keyID, ResultNoAnswer)
	case valid:
		return t.store.Activate(ctx, keyID)
	default:
		return t.store.Discard(ctx, keyID, EncodeResult(ResultInvalid, message))
	}
}

// EncodeResult stores a result with its summarized message in provider_keys.test_result.
func EncodeResult(result, message string) string {
	if message == "" {
		return result
	}
	if len(message) > 300 {
		message = message[:300]
	}
	return result + ": " + message
}

// DecodeResult splits provider_keys.test_result.
func DecodeResult(stored string) (string, string) {
	for i := 0; i+1 < len(stored); i++ {
		if stored[i] == ':' && stored[i+1] == ' ' {
			return stored[:i], stored[i+2:]
		}
	}
	return stored, ""
}
