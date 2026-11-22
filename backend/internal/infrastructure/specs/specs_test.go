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

// RNF-07: every agent of the Phase 1 workflow has an explicit policy; Requisitos has no sandbox.
func TestAgentPoliciesComeFromPermissions(t *testing.T) {
	requirements, err := specs.AgentPolicy("requirements-agent")
	if err != nil {
		t.Fatal(err)
	}
	if requirements.Execute || requirements.Allows("execute_command") || !requirements.CanWrite("docs/requisitos/US-01.md") {
		t.Fatalf("requirements: %+v", requirements)
	}
	developer, err := specs.AgentPolicy("developer-agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := developer.Category("tests"); !ok || !developer.CanWrite("app/main.go") || developer.CanWrite("docs/arquitetura/adr/ADR-01.md") {
		t.Fatalf("developer: %+v", developer)
	}
	if developer.Ceiling.CPU != 4 || developer.Ceiling.Time.Minutes() != 15 {
		t.Fatalf("developer ceiling: %+v", developer.Ceiling)
	}
	architect, _ := specs.AgentPolicy("architect-agent")
	if _, ok := architect.Category("build"); ok || !architect.CanRead("docs/requisitos/VISAO.md") {
		t.Fatalf("architect: %+v", architect)
	}
}
