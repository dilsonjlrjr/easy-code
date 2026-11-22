// Package idempotency makes every POST of creation, command and synchronous decision safe to
// repeat (ADR-17): the first answer is stored with the effect, in the same transaction, for
// 24 h; a repetition with the same body gets the same answer, and with another body a conflict.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"easycode/internal/application/transaction"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Failure codes (openapi Problem).
const (
	CodeConflict = "idempotency-conflict"
	// CodeRace is returned by Store.Save when another request stored the same key first.
	CodeRace = "idempotency-race"
)

// Request identifies one intention of action.
type Request struct {
	Principal identity.Principal
	Key       string // Idempotency-Key (UUID)
	Operation string // operationId of the contract
	BodyHash  string // SHA-256 of the request body, hex
}

// Response is the stored first answer.
type Response struct {
	Status     int
	Body       []byte
	ResourceID string // Location of the created resource or command
}

// Stored is a key in its 24 h window.
type Stored struct {
	Operation string
	BodyHash  string
	Response  Response
}

// Store keeps the keys (idempotency_keys).
type Store interface {
	// Find returns the key in its window, or nil.
	Find(ctx context.Context, principal identity.Principal, key string) (*Stored, error)
	// Save stores the first answer in the caller's transaction; a key already in its window
	// fails with CodeRace.
	Save(ctx context.Context, req Request, resp Response) error
}

// Guard runs effects at most once per key.
type Guard struct {
	tx    transaction.Runner
	store Store
}

// NewGuard builds the guard.
func NewGuard(tx transaction.Runner, store Store) Guard {
	return Guard{tx: tx, store: store}
}

// HashBody is the body hash of a request.
func HashBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Run returns the stored answer of a repeated request, or runs effect in one transaction with
// the storage of its answer. A failed effect stores nothing, so the client may retry it.
func (g Guard) Run(ctx context.Context, req Request, effect func(ctx context.Context) (Response, error)) (Response, error) {
	for attempt := 0; ; attempt++ {
		if resp, done, err := g.replay(ctx, req); done || err != nil {
			return resp, err
		}
		var resp Response
		err := g.tx.InTx(ctx, func(ctx context.Context) error {
			var err error
			if resp, err = effect(ctx); err != nil {
				return err
			}
			return g.store.Save(ctx, req, resp)
		})
		if err != nil && failure.From(err).Code == CodeRace && attempt == 0 {
			continue
		}
		return resp, err
	}
}

func (g Guard) replay(ctx context.Context, req Request) (Response, bool, error) {
	stored, err := g.store.Find(ctx, req.Principal, req.Key)
	if err != nil || stored == nil {
		return Response{}, false, err
	}
	if stored.Operation != req.Operation || stored.BodyHash != req.BodyHash {
		return Response{}, true, failure.New(failure.KindConflict, CodeConflict, nil)
	}
	return stored.Response, true, nil
}
