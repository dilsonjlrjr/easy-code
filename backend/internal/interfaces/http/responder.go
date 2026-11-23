package http

import (
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/trace"

	"easycode/internal/domain/failure"
	"easycode/internal/infrastructure/logging"
)

const (
	problemTypePrefix  = "urn:easycode:problem:"
	contentTypeJSON    = "application/json"
	contentTypeProblem = "application/problem+json"
)

// Problem is an RFC 9457 problem details body (ADR-17).
type Problem struct {
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Status   int            `json:"status"`
	Detail   string         `json:"detail,omitempty"`
	Instance string         `json:"instance,omitempty"`
	TraceID  string         `json:"trace_id,omitempty"`
	Errors   []ProblemField `json:"errors,omitempty"`
	// Extensions are the extra members of the contract (missing, current_state…), inlined.
	Extensions map[string]any `json:"-"`
}

// ProblemField is one invalid input field of a validation problem.
type ProblemField struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// MarshalJSON inlines the extension members next to the standard ones.
func (p Problem) MarshalJSON() ([]byte, error) {
	type plain Problem
	encoded, err := json.Marshal(plain(p))
	if err != nil || len(p.Extensions) == 0 {
		return encoded, err
	}
	merged := map[string]any{}
	if err := json.Unmarshal(encoded, &merged); err != nil {
		return nil, err
	}
	for name, value := range p.Extensions {
		if _, taken := merged[name]; !taken {
			merged[name] = value
		}
	}
	return json.Marshal(merged)
}

// Responder writes translated JSON bodies and problem details.
type Responder struct {
	translator Translator
	logger     *slog.Logger
}

// NewResponder builds the responder shared by handlers and the router.
func NewResponder(translator Translator, logger *slog.Logger) *Responder {
	return &Responder{translator: translator, logger: logger}
}

// Language negotiates the response language from Accept-Language (pt-BR by default).
func (r *Responder) Language(rc *fasthttp.RequestCtx) string {
	return r.translator.Negotiate(string(rc.Request.Header.Peek(fasthttp.HeaderAcceptLanguage)))
}

// Message translates a catalog key for the request language.
func (r *Responder) Message(rc *fasthttp.RequestCtx, key string, params map[string]string) string {
	return r.translator.Translate(r.Language(rc), key, params)
}

// JSON writes a JSON body with the negotiated Content-Language.
func (r *Responder) JSON(rc *fasthttp.RequestCtx, status int, body any) {
	r.write(rc, status, contentTypeJSON, body)
}

// Raw writes an already encoded JSON body (a replayed idempotent answer).
func (r *Responder) Raw(rc *fasthttp.RequestCtx, status int, body []byte) {
	rc.Response.Header.Set(fasthttp.HeaderContentLanguage, r.Language(rc))
	rc.SetStatusCode(status)
	if len(body) == 0 || string(body) == "null" {
		return
	}
	rc.SetContentType(contentTypeJSON)
	rc.SetBody(body)
}

// NoContent writes 204.
func (r *Responder) NoContent(rc *fasthttp.RequestCtx) {
	rc.SetStatusCode(fasthttp.StatusNoContent)
}

// Problem converts a typed error into translated problem details and logs it.
func (r *Responder) Problem(rc *fasthttp.RequestCtx, err error) {
	classified := failure.From(err)
	status := statusFor(classified)
	r.logFailure(rc, status, err)
	r.write(rc, status, contentTypeProblem, r.problemBody(rc, classified, status))
}

// problemBody builds the translated problem of a classified failure.
func (r *Responder) problemBody(rc *fasthttp.RequestCtx, classified *failure.Error, status int) Problem {
	params := map[string]string{"method": string(rc.Method())}
	for name, value := range classified.Extensions {
		switch v := value.(type) {
		case string:
			params[name] = v
		case int:
			params[name] = strconv.Itoa(v)
		}
	}
	problem := Problem{
		Type:       problemTypePrefix + classified.Code,
		Title:      r.Message(rc, "problem."+classified.Code+".title", params),
		Status:     status,
		Detail:     r.Message(rc, "problem."+classified.Code+".detail", params),
		Instance:   string(rc.Path()),
		TraceID:    traceID(rc),
		Extensions: classified.Extensions,
	}
	for _, field := range classified.Fields {
		problem.Errors = append(problem.Errors, ProblemField{
			Field: field.Field, Message: r.Message(rc, "field."+field.Message, nil),
		})
	}
	return problem
}

func (r *Responder) write(rc *fasthttp.RequestCtx, status int, contentType string, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		r.logger.ErrorContext(RequestContext(rc), "encode response failed", logging.ErrorAttr(err))
		rc.SetStatusCode(fasthttp.StatusInternalServerError)
		return
	}
	rc.Response.Header.Set(fasthttp.HeaderContentLanguage, r.Language(rc))
	rc.SetContentType(contentType)
	rc.SetStatusCode(status)
	rc.SetBody(encoded)
}

func (r *Responder) logFailure(rc *fasthttp.RequestCtx, status int, err error) {
	level := slog.LevelInfo
	if status >= fasthttp.StatusInternalServerError {
		level = slog.LevelError
	}
	r.logger.LogAttrs(RequestContext(rc), level, "request failed",
		slog.Int("status", status),
		slog.String("route", RouteTemplate(rc)),
		logging.ErrorAttr(err))
}

func traceID(rc *fasthttp.RequestCtx) string {
	span := trace.SpanContextFromContext(RequestContext(rc))
	if !span.IsValid() {
		return ""
	}
	return span.TraceID().String()
}

// statusByCode holds the problem types whose status is not the one of their kind.
var statusByCode = map[string]int{
	codeMethodNotAllowed:     fasthttp.StatusMethodNotAllowed,
	codeRequiredPrecondition: fasthttp.StatusPreconditionRequired,
	codeFailedPrecondition:   fasthttp.StatusPreconditionFailed,
	"too-large-package":      fasthttp.StatusRequestEntityTooLarge, // US-44 CA-3
}

// statusFor maps a failure to its HTTP status: first by code, then by kind (ADR-17).
func statusFor(classified *failure.Error) int {
	if status, ok := statusByCode[classified.Code]; ok {
		return status
	}
	switch classified.Kind {
	case failure.KindInvalidInput:
		return fasthttp.StatusBadRequest
	case failure.KindUnauthenticated:
		return fasthttp.StatusUnauthorized
	case failure.KindForbidden:
		return fasthttp.StatusForbidden
	case failure.KindNotFound:
		return fasthttp.StatusNotFound
	case failure.KindConflict:
		return fasthttp.StatusConflict
	case failure.KindUnavailable:
		return fasthttp.StatusServiceUnavailable
	default:
		return fasthttp.StatusInternalServerError
	}
}
