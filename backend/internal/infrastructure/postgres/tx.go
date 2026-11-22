package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/transaction"
	"easycode/internal/domain/failure"
)

// querier is what both a pool and a transaction offer.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
}

type txKey struct{}

// TxRunner implements transaction.Runner. Adapters given the ctx of fn join its transaction.
type TxRunner struct {
	pool *pgxpool.Pool
}

var _ transaction.Runner = TxRunner{}

// NewTxRunner builds the unit of work over pool.
func NewTxRunner(pool *pgxpool.Pool) TxRunner {
	return TxRunner{pool: pool}
}

// InTx runs fn in a transaction, or inside the caller's transaction when ctx already has one.
// The transaction rolls back when fn returns an error or panics.
func (r TxRunner) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
	var typed *failure.Error
	if err == nil || errors.As(err, &typed) {
		return err
	}
	// Deferred constraint triggers fail at commit: classify them like any statement.
	return classify(err)
}

// conn returns the transaction in ctx, or the pool outside a transaction.
func conn(ctx context.Context, pool *pgxpool.Pool) querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// Codes of database failures that reach the callers.
const (
	CodeConstraintViolated = "constraint-violated"
	CodeDuplicate          = "duplicate"
)

// ruleCodes maps database rules (constraint triggers) to the problem codes of the contract.
var ruleCodes = map[string]string{
	"project_members_require_approver": "last-approver",        // US-02 CA-4, RN-2
	"project_require_lead":             "lead-required",        // US-02 RN-3
	"gate_confirmations_recent_role":   "separation-of-duties", // US-02 CA-8, RN-4
	"gate_confirmations_separation":    "separation-of-duties", // US-08 CA-5
}

// separationReasons tell the separation-of-duties cases apart (problem member reason).
var separationReasons = map[string]string{
	"gate_confirmations_recent_role": "approver_grace_period",
	"gate_confirmations_separation":  "risk_acceptance_same_cycle",
}

// classify turns constraint violations into typed failures; other errors stay internal.
func classify(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	if code, ok := ruleCodes[pgErr.ConstraintName]; ok {
		typed := failure.New(failure.KindConflict, code, err)
		if reason, ok := separationReasons[pgErr.ConstraintName]; ok {
			typed = typed.With("reason", reason)
		}
		return typed
	}
	if pgErr.ConstraintName == "gate_confirmations_require_approver" && pgErr.Code == "42501" {
		return failure.New(failure.KindForbidden, failure.CodeForbidden, err) // US-02 CA-2
	}
	switch pgErr.Code {
	case "23505":
		return failure.New(failure.KindConflict, CodeDuplicate, err)
	case "23514", "23502", "23503", "22P02":
		return failure.New(failure.KindInvalidInput, CodeConstraintViolated, err)
	default:
		return err
	}
}

// nullable maps the empty string to SQL NULL.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
