package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/valyala/fasthttp"

	"easycode/internal/domain/failure"
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
