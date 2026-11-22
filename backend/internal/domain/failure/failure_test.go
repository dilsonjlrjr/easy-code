package failure_test

import (
	"errors"
	"fmt"
	"testing"

	"easycode/internal/domain/failure"
)

func TestFromKeepsClassifiedFailureInChain(t *testing.T) {
	cause := errors.New("connection refused")
	wrapped := fmt.Errorf("probe: %w", failure.New(failure.KindUnavailable, failure.CodeServiceUnavailable, cause))

	got := failure.From(wrapped)

	if got.Kind != failure.KindUnavailable || got.Code != failure.CodeServiceUnavailable {
		t.Fatalf("unexpected failure: %+v", got)
	}
	if !errors.Is(got, cause) {
		t.Fatal("cause chain lost")
	}
}

func TestFromClassifiesUnknownErrorAsInternal(t *testing.T) {
	got := failure.From(errors.New("boom"))

	if got.Kind != failure.KindInternal || got.Code != failure.CodeInternal {
		t.Fatalf("unexpected failure: %+v", got)
	}
}
