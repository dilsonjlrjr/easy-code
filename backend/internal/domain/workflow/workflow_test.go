package workflow_test

import (
	"testing"

	"easycode/internal/domain/workflow"
)

func basic() workflow.Definition {
	return workflow.Definition{ID: "workflow-basico", Version: 1, Phase: 1, Stages: []workflow.Stage{
		{ID: "requirements", Agent: "requirements-agent", Gate: "gate.requirements"},
		{ID: "architecture", Agent: "architect-agent", Gate: "gate.architecture", DependsOn: []string{"requirements"}},
		{ID: "implementation", Agent: "developer-agent", Gate: "gate.implementation", DependsOn: []string{"architecture"}},
	}}
}

// US-05 CA-3: stages with a dependency not approved stay blocked; the first one is ready.
func TestInitialStatesReadyOnlyWithoutDependencies(t *testing.T) {
	states := basic().Initial()
	if states["requirements"] != workflow.StageReady || states["architecture"] != workflow.StageBlocked || states["implementation"] != workflow.StageBlocked {
		t.Fatalf("unexpected initial states %v", states)
	}
}

func TestValidateRefusesUnknownOrLateDependencies(t *testing.T) {
	if err := basic().Validate(); err != nil {
		t.Fatal(err)
	}
	broken := basic()
	broken.Stages[0].DependsOn = []string{"implementation"}
	if err := broken.Validate(); err == nil {
		t.Fatal("dependency after the stage must be refused")
	}
}

func TestWaveCode(t *testing.T) {
	if got := workflow.WaveCode("requirements", 1); got != "REQ-01" {
		t.Fatalf("got %s", got)
	}
}
