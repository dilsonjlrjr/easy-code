package gate

import (
	"slices"
	"testing"
)

// US-06 RN-3 (pending-human-check): the problem lists the human checks still to confirm and the
// confirmed ids the round does not have.
func TestHumanChecksListsMissingAndUnknownChecks(t *testing.T) {
	checks := []CheckView{{Check: "artifact.schema_valid", Type: "automatic"}, {Check: "human.assumptions", Type: "human"}, {Check: "human.scope", Type: "human"}}
	missing, unknown := humanChecks(checks, []string{"human.scope", "human.other"})
	if !slices.Equal(missing, []string{"human.assumptions"}) || !slices.Equal(unknown, []string{"human.other"}) {
		t.Fatalf("missing %v unknown %v", missing, unknown)
	}
	if missing, unknown := humanChecks(checks[:1], nil); len(missing) != 0 || len(unknown) != 0 {
		t.Fatalf("no human check: %v %v", missing, unknown)
	}
}
