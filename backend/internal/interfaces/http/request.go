package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/idempotency"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// decodeJSON reads a JSON object body strictly: unknown members are a validation error.
func decodeJSON(rc *fasthttp.RequestCtx, target any) error {
	body := rc.Request.Body()
	if len(bytes.TrimSpace(body)) == 0 {
		return failure.Invalid(failure.FieldError{Field: "body", Message: "required"})
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return failure.Invalid(failure.FieldError{Field: fieldOf(err), Message: "invalid"})
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return failure.Invalid(failure.FieldError{Field: "body", Message: "invalid"})
	}
	return nil
}

func fieldOf(err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return typeErr.Field
	}
	if message := err.Error(); strings.HasPrefix(message, "json: unknown field ") {
		return strings.Trim(strings.TrimPrefix(message, "json: unknown field "), `"`)
	}
	return "body"
}

// Header names of the contract.
const (
	headerIdempotencyKey = "Idempotency-Key"
	headerIfMatch        = "If-Match"
	headerETag           = "ETag"
	headerLocation       = "Location"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// idempotencyRequest reads the Idempotency-Key header (428 without it).
func idempotencyRequest(rc *fasthttp.RequestCtx, principal identity.Principal, operation string) (idempotency.Request, error) {
	key := strings.ToLower(strings.TrimSpace(string(rc.Request.Header.Peek(headerIdempotencyKey))))
	if key == "" {
		return idempotency.Request{}, failure.New(failure.KindInvalidInput, codeRequiredPrecondition, nil)
	}
	if !uuidPattern.MatchString(key) {
		return idempotency.Request{}, failure.Invalid(failure.FieldError{Field: headerIdempotencyKey, Message: "uuid"})
	}
	return idempotency.Request{
		Principal: principal, Key: key, Operation: operation, BodyHash: idempotency.HashBody(rc.Request.Body()),
	}, nil
}

// ifMatch reads the If-Match header as the opaque version of an ETag (428 without it).
func ifMatch(rc *fasthttp.RequestCtx) (string, error) {
	raw := strings.TrimSpace(string(rc.Request.Header.Peek(headerIfMatch)))
	if raw == "" {
		return "", failure.New(failure.KindInvalidInput, codeRequiredPrecondition, nil)
	}
	return strings.Trim(strings.TrimPrefix(raw, "W/"), `"`), nil
}

func setETag(rc *fasthttp.RequestCtx, version string) {
	rc.Response.Header.Set(headerETag, `"`+version+`"`)
}

// pathUUID reads a UUID path parameter; anything else is not found (opaque ids, RNF-04).
func pathUUID(rc *fasthttp.RequestCtx, name string) (string, error) {
	value := strings.ToLower(PathParam(rc, name))
	if !uuidPattern.MatchString(value) {
		return "", failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return value, nil
}

// queryLimit reads the page size (1–100, default 20).
func queryLimit(rc *fasthttp.RequestCtx) (int, error) {
	raw := string(rc.QueryArgs().Peek("limit"))
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		return 0, failure.Invalid(failure.FieldError{Field: "limit", Message: "range"})
	}
	return limit, nil
}

// queryList reads a repeatable query parameter (style form, explode).
func queryList(rc *fasthttp.RequestCtx, name string) []string {
	values := []string{}
	for _, value := range rc.QueryArgs().PeekMulti(name) {
		if text := strings.TrimSpace(string(value)); text != "" {
			values = append(values, text)
		}
	}
	return values
}
