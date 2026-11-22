// Package audit holds the port of the append-only audit trail (RNF-13).
package audit

import (
	"context"

	"easycode/internal/domain/audit"
)

// Trail appends records. Called inside transaction.Runner it joins the caller's transaction,
// so the record commits with the state change and its domain event.
type Trail interface {
	Append(ctx context.Context, record audit.Record) (audit.Entry, error)
}
