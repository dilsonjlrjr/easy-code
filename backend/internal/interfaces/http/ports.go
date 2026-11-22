// Package http exposes the application over HTTP with fasthttp: routing, middleware,
// problem details (ADR-17) and handlers.
package http

import (
	"context"

	"easycode/internal/domain/health"
)

// Translator resolves user-facing messages. The i18n adapter implements it.
type Translator interface {
	Negotiate(acceptLanguage string) string
	Translate(language, key string, params map[string]string) string
}

// HealthUseCases are the health use cases the handlers call.
type HealthUseCases interface {
	Liveness() health.Report
	Readiness(ctx context.Context) (health.Report, error)
}
