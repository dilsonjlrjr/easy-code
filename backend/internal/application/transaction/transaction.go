// Package transaction holds the unit-of-work port. Every state transition writes state,
// checkpoint, audit and outbox in one transaction (ARQ: "nada de estado sem evento").
package transaction

import "context"

// Runner runs fn in one database transaction. Adapters called with the ctx passed to fn
// join that transaction; fn returning an error (or panicking) rolls everything back.
type Runner interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
