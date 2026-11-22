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
)

// Error is a classified failure with a stable code and an optional cause.
type Error struct {
	Kind  Kind
	Code  string
	Cause error
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

// From returns the classified failure in err's chain, or an internal failure wrapping err.
func From(err error) *Error {
	var classified *Error
	if errors.As(err, &classified) {
		return classified
	}
	return New(KindInternal, CodeInternal, err)
}
