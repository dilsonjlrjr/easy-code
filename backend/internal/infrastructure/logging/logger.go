// Package logging builds the structured JSON logger every binary uses (owner decision 3).
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// New returns a JSON logger with central redaction and trace correlation.
func New(output io.Writer, level slog.Leveler, service, version string) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level, ReplaceAttr: Redact})
	return slog.New(correlationHandler{next: handler}).With(
		slog.String("service", service),
		slog.String("version", version),
	)
}

// ErrorAttr logs an error with its message and the type of every error in its cause chain.
func ErrorAttr(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	return slog.Group("error",
		slog.String("message", err.Error()),
		slog.Any("chain", causeChain(err)),
	)
}

func causeChain(err error) []string {
	var chain []string
	pending := []error{err}
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		chain = append(chain, fmt.Sprintf("%T", current))
		pending = append(pending, unwrapAll(current)...)
	}
	return chain
}

func unwrapAll(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	if next := errors.Unwrap(err); next != nil {
		return []error{next}
	}
	return nil
}
