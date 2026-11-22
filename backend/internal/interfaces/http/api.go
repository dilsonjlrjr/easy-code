package http

import (
	"context"

	"easycode/internal/application/idempotency"
)

// IdempotencyGuard runs POST effects at most once per Idempotency-Key (ADR-17).
type IdempotencyGuard interface {
	Run(ctx context.Context, req idempotency.Request, effect func(ctx context.Context) (idempotency.Response, error)) (idempotency.Response, error)
}

// api holds the handlers of the product routes.
type api struct {
	deps      Dependencies
	responder *Responder
	auth      authenticator
}
