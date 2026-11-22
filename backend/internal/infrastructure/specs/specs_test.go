package specs_test

import (
	"encoding/json"
	"fmt"
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

// US-20 CA-3, US-21: in Phase 2 the workflow in force is workflow-padrao restricted to the agents with a
// harness: Dados and Design both depend only on Arquitetura (they run in parallel), the Validação técnica
// is led by the Arquiteto with DBA and Designer as participants, and the Implementação depends on it.
func TestStandardWorkflowPutsDataAndDesignBetweenArchitectureAndImplementation(t *testing.T) {
	wf, err := specs.StandardWorkflow(specs.StandardAgents)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, stage := range wf.Definition.Stages {
		ids = append(ids, stage.ID)
	}
	if wf.Definition.ID != "workflow-padrao" || wf.Definition.Phase != 2 || fmt.Sprint(ids) != "[requirements architecture data design validation implementation]" {
		t.Fatalf("stages: %s %v", wf.Definition.ID, ids)
	}
	data, _ := wf.Definition.Stage("data")
	design, _ := wf.Definition.Stage("design")
	if data.Agent != "dba-agent" || design.Agent != "designer-agent" || fmt.Sprint(data.DependsOn) != "[architecture]" || fmt.Sprint(design.DependsOn) != "[architecture]" {
		t.Fatalf("CA-3: data and design in parallel after architecture: %+v %+v", data, design)
	}
	validation, _ := wf.Definition.Stage("validation")
	if validation.Agent != "architect-agent" || validation.AgentMode != "VALIDACAO" || fmt.Sprint(validation.Agents()) != "[dba-agent designer-agent architect-agent]" ||
		validation.ModeOf("dba-agent") != "" || !validation.IsReviewer("designer-agent") || validation.IsReviewer("architect-agent") {
		t.Fatalf("validation: %+v", validation)
	}
	implementation, _ := wf.Definition.Stage("implementation")
	if fmt.Sprint(implementation.DependsOn) != "[validation]" {
		t.Fatalf("implementation after validation: %v", implementation.DependsOn)
	}
	initial := wf.Definition.Initial()
	if initial["requirements"] != "ready" || initial["data"] != "blocked" {
		t.Fatalf("initial: %v", initial)
	}
	if len(wf.Hash) != 64 || !json.Valid(wf.JSON) {
		t.Fatal("pinned copy needs canonical JSON and hash")
	}
	// Without the DBA the data stage and the validation (which needs it) are left out.
	phase1, err := specs.StandardWorkflow([]string{"requirements-agent", "architect-agent", "developer-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(phase1.Definition.Stages) != 3 {
		t.Fatalf("stages without DBA and Designer: %+v", phase1.Definition.Stages)
	}
}

// RNF-07: DBA and Designer have explicit policies with their tool allowlists (permissoes.yaml).
func TestDBAAndDesignerPolicies(t *testing.T) {
	dba, err := specs.AgentPolicy("dba-agent")
	if err != nil {
		t.Fatal(err)
	}
	if !dba.CanWrite("docs/dados/DIC.md") || dba.CanWrite("docs/arquitetura/ARQ.md") || !dba.CanRead("docs/design/TELAS.md") || dba.CanRead("docs/design/estilo.md") {
		t.Fatalf("dba paths: %+v", dba)
	}
	if _, ok := dba.Category("validate_ddl"); !ok {
		t.Fatalf("dba categories: %+v", dba.Categories)
	}
	designer, err := specs.AgentPolicy("designer-agent")
	if err != nil {
		t.Fatal(err)
	}
	if !designer.CanWrite("docs/design/prototipo/index.html") || designer.CanWrite("docs/dados/DIC.md") || designer.CanRead("docs/dados/DIC.md") {
		t.Fatalf("designer paths: %+v", designer)
	}
	if _, ok := designer.Category("build"); ok {
		t.Fatal("designer does not build")
	}
}
