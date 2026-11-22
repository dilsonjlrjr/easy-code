package specs_test

import (
	"encoding/json"
	"testing"

	"easycode/internal/infrastructure/specs"
)

// US-05 CA-5, RN-1: Phase 1 runs Requisitos → Arquitetura → Implementação, total from the workflow.
func TestBasicWorkflowHasThePhase1Stages(t *testing.T) {
	wf, err := specs.BasicWorkflow()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, stage := range wf.Definition.Stages {
		ids = append(ids, stage.ID)
	}
	if wf.Definition.ID != "workflow-basico" || wf.Definition.Phase != 1 || len(ids) != 3 || ids[0] != "requirements" || ids[2] != "implementation" {
		t.Fatalf("unexpected workflow %s %v", wf.Definition.ID, ids)
	}
	if len(wf.Hash) != 64 || !json.Valid(wf.JSON) {
		t.Fatal("pinned copy needs canonical JSON and hash")
	}
}

func TestDefaultRequirementsAgentComesFromPermissions(t *testing.T) {
	agent, err := specs.DefaultAgent("requirements-agent")
	if err != nil {
		t.Fatal(err)
	}
	var tools map[string]any
	if err := json.Unmarshal(agent.Tools, &tools); err != nil || tools["write_file"] == nil {
		t.Fatalf("tools: %s %v", agent.Tools, err)
	}
	if agent.InstructionPath != "agentes/instrucoes/requisitos.md" || len(agent.InstructionHash) != 64 || len(agent.Instruction) == 0 {
		t.Fatalf("instruction: %+v", agent.InstructionPath)
	}
}

func TestYAMLDatesStayText(t *testing.T) {
	encoded, err := specs.YAMLToJSON([]byte("data: 2026-11-19\nversao: 2\nok: true\n"))
	if err != nil || string(encoded) != `{"data":"2026-11-19","ok":true,"versao":2}` {
		t.Fatalf("got %s %v", encoded, err)
	}
}
