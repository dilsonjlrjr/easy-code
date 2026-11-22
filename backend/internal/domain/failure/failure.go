// Package failure defines the typed errors shared by every layer.
// Use cases return these errors; adapters translate them to transport formats.
package failure

import (
	"errors"
	"fmt"
)

// Kind classifies a failure by what the caller can do about it.
type Kind int

// Failure kinds. The zero value is KindInternal so an unclassified error is never exposed as a client error.
const (
	KindInternal Kind = iota
	KindInvalidInput
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindUnavailable
)

// Stable codes. A code is never renamed once published (ADR-17).
const (
	CodeInternal           = "internal"
	CodeNotFound           = "not-found"
	CodeServiceUnavailable = "service-unavailable"
	CodeValidation         = "validation"
	CodeForbidden          = "forbidden"
	CodeNotAuthenticated   = "not-authenticated"
	CodeStateConflict      = "state-conflict"
)

// Error is a classified failure with a stable code and an optional cause.
type Error struct {
	Kind  Kind
	Code  string
	Cause error
	// Fields lists input problems by field (problem `errors`).
	Fields []FieldError
	// Extensions are extra problem members of the contract, such as `missing` or
	// `current_state` (openapi Problem). Values must be safe to show to the caller.
	Extensions map[string]any
}

// FieldError is one invalid input field. Message is a catalog key suffix, never user text.
type FieldError struct {
	Field   string
	Message string
}

// New builds a classified failure.
func New(kind Kind, code string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Cause: cause}
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Code
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

// WithField appends an invalid field and returns the error.
func (e *Error) WithField(field, message string) *Error {
	e.Fields = append(e.Fields, FieldError{Field: field, Message: message})
	return e
}

// With sets a problem extension member and returns the error.
func (e *Error) With(name string, value any) *Error {
	if e.Extensions == nil {
		e.Extensions = map[string]any{}
	}
	e.Extensions[name] = value
	return e
}

// Invalid builds a validation failure (code "validation") from field errors.
func Invalid(fields ...FieldError) *Error {
	return &Error{Kind: KindInvalidInput, Code: CodeValidation, Fields: fields}
}

// From returns the classified failure in err's chain, or an internal failure wrapping err.
func From(err error) *Error {
	var classified *Error
	if errors.As(err, &classified) {
		return classified
	}
	return New(KindInternal, CodeInternal, err)
}
