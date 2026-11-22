// Package gateway is the worker ↔ easycode-gateway protocol (interno.md section 3): HTTP/1.1 +
// JSON over a Unix socket, with the call answer streamed as NDJSON. It holds both ends: the
// server handler run by easycode-gateway and the client used by the worker.
package gateway

import (
	"net/http"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/provider"
)

// Routes.
const (
	routeSessions = "/v1/sessions"
	routeKeyTests = "/v1/key-tests"
	routeHealth   = "/v1/health"
)

// Key test results (only valid or invalid leave the gateway, US-15).
const (
	KeyValid   = "valid"
	KeyInvalid = "invalid"
)

// CodeProviderError reports a provider failure that is not an invalid key during a key test.
const CodeProviderError = "gateway-provider-error"

// OpenSessionRequest opens a wave scope with the sealed key read from the database by the worker.
type OpenSessionRequest struct {
	appgateway.Scope
	SealedKey []byte `json:"sealed_key"`
}

// OpenSessionResponse returns the session id.
type OpenSessionResponse struct {
	ID string `json:"id"`
}

// CallLine is one NDJSON line of a call answer: chunks, then exactly one of Response, Error or Failure.
type CallLine struct {
	Chunk    *provider.Chunk    `json:"chunk,omitempty"`
	Response *provider.Response `json:"response,omitempty"`
	Error    *provider.Error    `json:"error,omitempty"`
	Failure  *Problem           `json:"failure,omitempty"`
}

// KeyTestRequest asks to validate a sealed key.
type KeyTestRequest struct {
	Provider  string `json:"provider"`
	SealedKey []byte `json:"sealed_key"`
}

// KeyTestResponse is valid or invalid with a short message (never the key).
type KeyTestResponse struct {
	Result  string `json:"result"`
	Message string `json:"message,omitempty"`
}

// HealthResponse reports the gateway status.
type HealthResponse struct {
	Status    string   `json:"status"`
	Providers []string `json:"providers"`
}

// Problem is a classified failure of the gateway itself.
type Problem struct {
	Code   string              `json:"code"`
	Status int                 `json:"status"`
	Class  provider.ErrorClass `json:"class,omitempty"`
}

func problemFor(err error) Problem {
	classified := failure.From(err)
	return Problem{Code: classified.Code, Status: statusFor(classified.Kind)}
}

func statusFor(kind failure.Kind) int {
	switch kind {
	case failure.KindInvalidInput:
		return http.StatusBadRequest
	case failure.KindNotFound:
		return http.StatusNotFound
	case failure.KindConflict:
		return http.StatusConflict
	case failure.KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func kindFor(status int) failure.Kind {
	switch status {
	case http.StatusBadRequest:
		return failure.KindInvalidInput
	case http.StatusNotFound:
		return failure.KindNotFound
	case http.StatusConflict:
		return failure.KindConflict
	case http.StatusServiceUnavailable:
		return failure.KindUnavailable
	default:
		return failure.KindInternal
	}
}

func (p Problem) asError() error {
	code := p.Code
	if code == "" {
		code = failure.CodeInternal
	}
	return failure.New(kindFor(p.Status), code, nil)
}
