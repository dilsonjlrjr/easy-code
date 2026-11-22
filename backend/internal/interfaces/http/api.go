package http

import (
	"context"
	"encoding/json"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/idempotency"
	"easycode/internal/domain/identity"
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

// created is what an idempotent effect answers: status, body and the Location of the result.
type created struct {
	status   int
	body     any
	location string
}

// idempotent runs effect once per Idempotency-Key and writes its (possibly replayed) answer.
func (a *api) idempotent(rc *fasthttp.RequestCtx, principal identity.Principal, operation string, effect func(ctx context.Context) (created, error)) error {
	req, err := idempotencyRequest(rc, principal, operation)
	if err != nil {
		return err
	}
	resp, err := a.deps.Idempotency.Run(RequestContext(rc), req, func(ctx context.Context) (idempotency.Response, error) {
		result, err := effect(ctx)
		if err != nil {
			return idempotency.Response{}, err
		}
		encoded, err := json.Marshal(result.body)
		if err != nil {
			return idempotency.Response{}, err
		}
		return idempotency.Response{Status: result.status, Body: encoded, ResourceID: result.location}, nil
	})
	if err != nil {
		return err
	}
	if resp.ResourceID != "" {
		rc.Response.Header.Set(headerLocation, resp.ResourceID)
	}
	a.responder.Raw(rc, resp.Status, resp.Body)
	return nil
}
