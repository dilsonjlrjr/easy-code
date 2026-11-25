package specs_test

import (
	"testing"

	appgate "easycode/internal/application/gate"
	"easycode/internal/domain/governance"
	"easycode/internal/infrastructure/specs"
)

// US-31, ADR-10: gates.yaml and checagens.yaml load; every gate cites catalog checks; required checks block
// and alert checks only alert; the engine has an implementation for every automatic check of the gates of
// the workflow in force (the worker refuses to start otherwise).
func TestGovernanceCompositionIsComplete(t *testing.T) {
	composition, err := specs.Governance()
	if err != nil {
		t.Fatal(err)
	}
	if len(composition.Catalog) < 100 || len(composition.Gates) != 9 || composition.Version == "" {
		t.Fatalf("composition: %d checks, %d gates", len(composition.Catalog), len(composition.Gates))
	}
	severity := map[string]string{}
	human := 0
	for _, spec := range composition.ChecksOf("gate.requirements", nil) {
		severity[spec.ID] = spec.Severity
		if spec.Type == governance.TypeHuman {
			human++
		}
	}
	if severity["artifact.schema_valid"] != governance.SeverityBlocks || severity["finding.no_open_blocking"] != governance.SeverityBlocks ||
		severity["us.criteria_no_vague_term"] != governance.SeverityAlert || human != 6 {
		t.Fatalf("gate.requirements composition: %v (human %d)", severity, human)
	}
	promoted := composition.ChecksOf("gate.requirements", []string{"us.criteria_no_vague_term"})
	for _, spec := range promoted {
		if spec.ID == "us.criteria_no_vague_term" && spec.Severity != governance.SeverityBlocks {
			t.Fatal("promote_alert: a project makes an alert blocking")
		}
	}
	if validation := composition.Gates["gate.validation"]; !validation.RequiresHuman("validacao.review_opinion_rejected") {
		t.Fatal("PC-06: the rejected opinion takes the automatic round to a human")
	}
	for _, agents := range [][]string{specs.StandardAgents, specs.TeamAgents} {
		wf, err := specs.StandardWorkflow(agents)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := appgate.NewProgress(nil, nil, nil, nil, nil, nil, wf.Definition, composition); err != nil {
			t.Fatalf("every automatic check has an implementation: %v", err)
		}
	}
	basic, _ := specs.BasicWorkflow()
	if _, err := appgate.NewProgress(nil, nil, nil, nil, nil, nil, basic.Definition, composition); err != nil {
		t.Fatal(err)
	}
}

// ESP-12, PC-20: every check of the catalog has the name the gate and the stage exit criteria show.
func TestEveryCatalogCheckHasAName(t *testing.T) {
	composition, err := specs.Governance()
	if err != nil {
		t.Fatal(err)
	}
	for id, check := range composition.Catalog {
		if check.Name == "" || composition.NameOf(id) != check.Name {
			t.Fatalf("check %s has no catalog name", id)
		}
	}
}
