package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/provider"
)

// HeaderProblem marks an answer of the forward route written by the gateway itself (a Problem), as
// opposed to the upstream answer passed through unchanged.
const HeaderProblem = "Easycode-Problem"

// CodeBodyTooLarge reports a forwarded body above maxBodyBytes.
const CodeBodyTooLarge = "gateway-body-too-large"

// forwardedHeaders are the only request headers that pass from the agent to the provider.
var forwardedHeaders = []string{"anthropic-version", "anthropic-beta"}

// forward passes one raw provider request of an agent session (ADR-23). The upstream status,
// content type, request id and retry-after pass back with the body streamed as it arrives.
func (s *Server) forward(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := s.sessions.Session(id)
	if err != nil {
		s.forwardProblem(w, r, err)
		return
	}
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(
		attribute.String("easycode.project_id", scope.ProjectID), attribute.String("easycode.wave_id", scope.WaveID),
		attribute.String("gen_ai.system", scope.Provider), attribute.String("gen_ai.request.model", scope.Model),
		attribute.String("easycode.forward.path", r.PathValue("path")))

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			w.Header().Set(HeaderProblem, "1")
			writeJSON(w, http.StatusRequestEntityTooLarge, Problem{Code: CodeBodyTooLarge, Status: http.StatusRequestEntityTooLarge})
			return
		}
		w.Header().Set(HeaderProblem, "1")
		writeJSON(w, http.StatusBadRequest, Problem{Code: appgateway.CodeInvalidBody, Status: http.StatusBadRequest})
		return
	}
	response, err := s.sessions.Forward(r.Context(), id, appgateway.ForwardRequest{
		Path: r.PathValue("path"), RawQuery: r.URL.RawQuery,
		Version: r.Header.Get("anthropic-version"), Beta: r.Header.Get("anthropic-beta"), Body: body,
	})
	if err != nil {
		s.forwardProblem(w, r, err)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.Status >= http.StatusBadRequest {
		class := statusClass(response.Status)
		span.SetAttributes(attribute.String("error.type", string(class)))
		span.SetStatus(codes.Error, string(class))
		// Only the class and the status: the body may carry provider text and is passed unchanged.
		s.logger.WarnContext(r.Context(), "gateway forward refused upstream",
			slog.String("class", string(class)), slog.Int("upstream_status", response.Status))
	}
	for name, value := range map[string]string{"Content-Type": response.ContentType, "Request-Id": response.RequestID, "Retry-After": response.RetryAfter} {
		if value != "" {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(response.Status)
	stream(w, response.Body)
}

// stream copies the body and flushes after every read, so server-sent events reach the agent live.
func stream(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
	buffer := make([]byte, 32<<10)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// forwardProblem writes a gateway failure marked with HeaderProblem. A provider failure before any
// answer (unreachable, aborted) is 502 with its class.
func (s *Server) forwardProblem(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set(HeaderProblem, "1")
	var providerErr *provider.Error
	if errors.As(err, &providerErr) {
		span := trace.SpanFromContext(r.Context())
		span.SetAttributes(attribute.String("error.type", string(providerErr.Class)))
		span.SetStatus(codes.Error, string(providerErr.Class))
		s.logger.WarnContext(r.Context(), "gateway forward failed", slog.String("class", string(providerErr.Class)))
		writeJSON(w, http.StatusBadGateway, Problem{Code: CodeProviderError, Status: http.StatusBadGateway, Class: providerErr.Class})
		return
	}
	s.problem(w, r, err)
}

// statusClass classifies an upstream error status for logs and spans only.
func statusClass(status int) provider.ErrorClass {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return provider.ErrorAuthentication
	case status == http.StatusTooManyRequests:
		return provider.ErrorRateLimit
	case status == http.StatusRequestEntityTooLarge:
		return provider.ErrorContextExceeded
	case status >= http.StatusInternalServerError || status == http.StatusRequestTimeout:
		return provider.ErrorTransient
	default:
		return provider.ErrorRequest
	}
}

// Forward sends one raw provider request in the session scope (ADR-23) and returns the upstream answer
// unchanged (status, content type, body streamed). path is "v1/messages" or "v1/messages/count_tokens"
// (the query string, e.g. "beta=true", goes in rawQuery). Only anthropic-version and anthropic-beta headers pass.
// A failure of the gateway itself (unknown session, path or model out of scope, provider unreachable)
// is returned as an error, not as a response: failure codes or *provider.Error, as Call does.
func (c *Client) Forward(ctx context.Context, sessionID, path, rawQuery string, header http.Header, body []byte) (*http.Response, error) {
	target := "http://easycode-gateway" + routeSessions + "/" + url.PathEscape(sessionID) + "/forward/" + strings.TrimLeft(path, "/")
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	for _, name := range forwardedHeaders {
		if value := header.Get(name); value != "" {
			request.Header.Set(name, value)
		}
	}
	c.propagator.Inject(ctx, propagation.HeaderCarrier(request.Header))
	resp, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted"}
		}
		return nil, &provider.Error{Class: provider.ErrorTransient, Message: "gateway unreachable"}
	}
	if resp.Header.Get(HeaderProblem) != "" {
		defer func() { _ = resp.Body.Close() }()
		return nil, readProblem(resp)
	}
	return resp, nil
}
