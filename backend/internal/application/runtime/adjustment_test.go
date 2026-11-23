package runtime_test

import (
	"strings"
	"testing"

	"easycode/internal/application/runtime"
)

// US-25 CA-2: the adjustment of the project comes after the default instruction and never replaces it.
func TestWithAdjustmentAppendsTheProjectText(t *testing.T) {
	if got := runtime.WithAdjustment("base", "  "); got != "base" {
		t.Fatalf("no adjustment: %q", got)
	}
	got := runtime.WithAdjustment("base", " Use the room vocabulary of the company. ")
	if !strings.HasPrefix(got, "base\n\n## Project adjustment") || !strings.HasSuffix(got, "Use the room vocabulary of the company.\n") {
		t.Fatalf("adjusted: %q", got)
	}
}
