package http

import (
	"github.com/valyala/fasthttp"

	"easycode/internal/domain/health"
)

// livenessResponse is the body of GET /api/v1/health.
type livenessResponse struct {
	Status  health.Status `json:"status"`
	Version string        `json:"version"`
	Message string        `json:"message"`
}

// readinessResponse is the body of GET /api/v1/ready when every dependency answers.
type readinessResponse struct {
	Status  health.Status     `json:"status"`
	Version string            `json:"version"`
	Message string            `json:"message"`
	Checks  map[string]string `json:"checks"`
}

// HealthHandler serves the liveness and readiness endpoints (no authentication).
type HealthHandler struct {
	useCases  HealthUseCases
	responder *Responder
}

// NewHealthHandler builds the health handlers.
func NewHealthHandler(useCases HealthUseCases, responder *Responder) *HealthHandler {
	return &HealthHandler{useCases: useCases, responder: responder}
}

// Liveness answers 200 while the process runs; it never touches dependencies.
func (h *HealthHandler) Liveness(rc *fasthttp.RequestCtx) error {
	report := h.useCases.Liveness()
	h.responder.JSON(rc, fasthttp.StatusOK, livenessResponse{
		Status:  report.Status,
		Version: report.Version,
		Message: h.responder.Message(rc, "health.alive", nil),
	})
	return nil
}

// Readiness answers 200 when every dependency answers, or a 503 problem otherwise.
func (h *HealthHandler) Readiness(rc *fasthttp.RequestCtx) error {
	report, err := h.useCases.Readiness(RequestContext(rc))
	if err != nil {
		return err
	}
	h.responder.JSON(rc, fasthttp.StatusOK, readinessResponse{
		Status:  report.Status,
		Version: report.Version,
		Message: h.responder.Message(rc, "health.ready", nil),
		Checks:  checkSummary(report.Checks),
	})
	return nil
}

func checkSummary(checks []health.Check) map[string]string {
	summary := make(map[string]string, len(checks))
	for _, check := range checks {
		summary[string(check.Dependency)] = "ok"
		if !check.Healthy {
			summary[string(check.Dependency)] = "failing"
		}
	}
	return summary
}
