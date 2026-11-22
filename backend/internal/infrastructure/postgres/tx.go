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
	return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
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

// classify turns constraint violations into typed failures; other errors stay internal.
func classify(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
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
