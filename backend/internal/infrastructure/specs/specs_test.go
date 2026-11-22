package specs_test

import (
	"encoding/json"
	"fmt"
	"strings"
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
	if wf.Definition.ID != "workflow-padrao" || wf.Definition.Phase != 2 || fmt.Sprint(ids) != "[requirements architecture data design validation implementation qa security]" {
		t.Fatalf("stages: %s %v", wf.Definition.ID, ids)
	}
	// MARCO-5 (US-22 CA-1, US-23 CA-1): the QA plan after the requirements and the threat model after the
	// architecture run as parallel activities approved in gate.validation; QA after the implementation and
	// Segurança after QA. The Release (orchestrator, MARCO-6) is not in force yet.
	plan, _ := wf.Definition.Activity("qa-plan")
	threats, _ := wf.Definition.Activity("security-threats")
	qa, _ := wf.Definition.Stage("qa")
	security, _ := wf.Definition.Stage("security")
	if plan.Agent != "qa-agent" || plan.AgentMode != "PLANO" || fmt.Sprint(plan.DependsOn) != "[requirements]" || plan.RequiredFor != "validation" ||
		threats.Agent != "security-agent" || threats.AgentMode != "AMEACAS" || fmt.Sprint(threats.DependsOn) != "[architecture]" ||
		fmt.Sprint(qa.DependsOn) != "[implementation]" || qa.AgentMode != "EXECUCAO" || fmt.Sprint(security.DependsOn) != "[qa]" || security.AgentMode != "AUDITORIA" {
		t.Fatalf("parallel activities and QA/Segurança: %+v %+v %+v %+v", plan, threats, qa, security)
	}
	if wf.Definition.GateStage("qa-plan") != "validation" || !wf.Definition.Returns.Internal("implementation") || wf.Definition.Returns.DefaultByCategory["defect"] != "implementation" {
		t.Fatalf("returns: %+v", wf.Definition.Returns)
	}
	team, err := specs.StandardWorkflow(specs.TeamAgents)
	if err != nil || len(team.Definition.Stages) != 6 || len(team.Definition.Activities) != 0 {
		t.Fatalf("MARCO-4 team without QA and Segurança: %v %+v", err, team.Definition)
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

// US-43 CA-1: the native catalog has each skill with name, description, stages and agents and an estimated size.
func TestNativeSkillsCatalog(t *testing.T) {
	skills, err := specs.NativeSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 17 {
		t.Fatalf("17 native skills, got %d", len(skills))
	}
	byName := map[string]int{}
	for i, s := range skills {
		byName[s.Name] = i
		if s.Description == "" || len(s.Agents) == 0 || len(s.Stages) == 0 || s.SizeTokens() < 1 || len(s.Hash()) != 64 || s.Files["SKILL.md"] == "" {
			t.Fatalf("incomplete skill %+v", s.Name)
		}
	}
	postgres := skills[byName["modelagem-postgresql"]]
	if fmt.Sprint(postgres.Agents) != "[dba-agent developer-agent]" || fmt.Sprint(postgres.Stages) != "[data implementation validation]" {
		t.Fatalf("agents and stages of modelagem-postgresql: %v %v", postgres.Agents, postgres.Stages)
	}
	if _, ok := byName["ux-acessibilidade"]; !ok {
		t.Fatal("design skill missing")
	}
	profile, err := specs.Profile("dba-agent")
	if err != nil || profile.Name != "DBA / Database Engineer" || !strings.HasPrefix(profile.Role, "Você é o DBA") || len(profile.Limits) == 0 || fmt.Sprint(profile.Delivery) != "[DIC PERSIST]" {
		t.Fatalf("profile: %+v %v", profile, err)
	}
}
