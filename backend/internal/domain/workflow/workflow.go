// Package workflow holds the workflow definition a project runs (specs/dominio/workflow) and the
// rules that derive stages and waves from it: which stages exist in the phase, which are ready
// when the workflow starts, and the wave code of each stage (US-05, US-49).
package workflow

import (
	"fmt"

	"easycode/internal/domain/pricing"
)

// Stage states (machine "stage" of estados.yaml).
const (
	StageBlocked      = "blocked"
	StageReady        = "ready"
	StageRunning      = "running"
	StageAwaitingGate = "awaiting_gate"
	StageApproved     = "approved"
)

// Output is an artifact a stage produces.
type Output struct {
	Type       string `json:"type,omitempty"`
	Path       string `json:"path"`
	Required   bool   `json:"required"`
	Attachment bool   `json:"attachment,omitempty"` // a file of the artifact folder, not an artifact (openapi.yaml, estrutura.md)
}

// Participant roles of a stage with several agents (the Validação técnica).
const (
	RoleLead        = "lead"
	RoleParticipant = "participant"
)

// Participant is an agent of a stage led by another one (workflow-padrao, validation): the lead
// writes the stage artifact; each participant gives its own review opinion in a wave of its own.
type Participant struct {
	Agent     string   `json:"agent"`
	Role      string   `json:"role"`
	AgentMode string   `json:"agent_mode,omitempty"`
	Inputs    []string `json:"inputs,omitempty"`
}

// Stage is a stage of the definition. In a stage with participants, Agent and AgentMode are the
// lead's.
type Stage struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	ShortName     string        `json:"short_name"`
	Agent         string        `json:"agent"`
	AgentMode     string        `json:"agent_mode,omitempty"`
	Participants  []Participant `json:"participants,omitempty"`
	DependsOn     []string      `json:"depends_on"`
	ProjectInputs []string      `json:"project_inputs,omitempty"`
	Outputs       []Output      `json:"outputs"`
	Gate          string        `json:"gate"`
}

// Reviewers are the participants that are not the lead, in the workflow order.
func (s Stage) Reviewers() []Participant {
	var out []Participant
	for _, p := range s.Participants {
		if p.Role != RoleLead {
			out = append(out, p)
		}
	}
	return out
}

// Agents are every agent that runs waves in the stage: the reviewers, then the lead or the agent.
func (s Stage) Agents() []string {
	var out []string
	for _, p := range s.Reviewers() {
		out = append(out, p.Agent)
	}
	return append(out, s.Agent)
}

// ModeOf is the agent mode of an agent in the stage: the stage mode for its agent, the participant
// mode for a reviewer.
func (s Stage) ModeOf(agent string) string {
	for _, p := range s.Reviewers() {
		if p.Agent == agent {
			return p.AgentMode
		}
	}
	if agent == s.Agent {
		return s.AgentMode
	}
	return ""
}

// IsReviewer reports whether the agent gives a review opinion in the stage (participant, not lead).
func (s Stage) IsReviewer(agent string) bool {
	for _, p := range s.Reviewers() {
		if p.Agent == agent {
			return true
		}
	}
	return false
}

// Definition is a workflow file (workflow-basico.yaml in Phase 1; in Phase 2 workflow-padrao.yaml
// restricted to the stages whose agents exist, specs.StandardWorkflow).
type Definition struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Version int     `json:"version"`
	Mode    string  `json:"mode"`
	Phase   int     `json:"fase"`
	Stages  []Stage `json:"stages"`
}

// Stage returns the stage with id.
func (d Definition) Stage(id string) (Stage, bool) {
	for _, stage := range d.Stages {
		if stage.ID == id {
			return stage, true
		}
	}
	return Stage{}, false
}

// Initial returns the state of each stage right after start: those without dependency are ready,
// the others blocked until their dependencies are approved (US-05 CA-3, US-03 CA-1).
func (d Definition) Initial() map[string]string {
	states := make(map[string]string, len(d.Stages))
	for _, stage := range d.Stages {
		if len(stage.DependsOn) == 0 {
			states[stage.ID] = StageReady
		} else {
			states[stage.ID] = StageBlocked
		}
	}
	return states
}

// Validate checks the definition is usable: known dependencies, an agent per stage, no cycle.
func (d Definition) Validate() error {
	if d.ID == "" || d.Version < 1 || len(d.Stages) == 0 {
		return fmt.Errorf("workflow %q: id, version and stages are required", d.ID)
	}
	seen := map[string]bool{}
	for _, stage := range d.Stages {
		if stage.Agent == "" || stage.Gate == "" {
			return fmt.Errorf("workflow %s: stage %s needs agent and gate", d.ID, stage.ID)
		}
		if _, ok := wavePrefixes[stage.ID]; !ok {
			return fmt.Errorf("workflow %s: unknown stage %s", d.ID, stage.ID)
		}
		for _, dependency := range stage.DependsOn {
			if !seen[dependency] {
				return fmt.Errorf("workflow %s: stage %s depends on %s, which is not before it", d.ID, stage.ID, dependency)
			}
		}
		seen[stage.ID] = true
	}
	return nil
}

var wavePrefixes = map[string]string{
	"requirements": "REQ", "architecture": "ARQ", "data": "DBA", "design": "DSG", "validation": "VAL",
	"implementation": "DEV", "qa": "QA", "qa-plan": "QA", "security": "SEG", "security-threats": "SEG", "release": "REL",
}

// WavePrefix is the wave code prefix of a stage (REQ for requirements).
func WavePrefix(stage string) string { return wavePrefixes[stage] }

// WaveCode is the code of the n-th wave of a stage (REQ-01).
func WaveCode(stage string, n int64) string { return fmt.Sprintf("%s-%02d", WavePrefix(stage), n) }

// ModelRequirement is what every stage needs from its models (policy.eligible_model defaults:
// required_capabilities [tool_use], min_context_tokens 128000).
func ModelRequirement() pricing.Requirement {
	return pricing.Requirement{Capacities: []pricing.Capacity{pricing.CapacityToolUse}, MinContextTokens: 128_000}
}

// Review opinions of the Validação técnica (VALIDACAO.pareceres).
const (
	OpinionApproved = "aprovado"
	OpinionRejected = "reprovado"
	// ModeReview is the agent mode of a participant wave: it reads the stage inputs and gives its own
	// opinion, which the orchestrator consolidates into the lead's artifact (template VALIDACAO).
	ModeReview = "PARECER"
)

// ReviewOpinion is the opinion a participant gave in its wave (US-20 RN-2).
type ReviewOpinion struct {
	Participant  string   `json:"participante"` // arquiteto | dba | designer
	Opinion      string   `json:"parecer"`      // aprovado | reprovado
	Reservations []string `json:"ressalvas"`
	Wave         string   `json:"onda,omitempty"`
}

var reviewerNames = map[string]string{"architect-agent": "arquiteto", "dba-agent": "dba", "designer-agent": "designer"}

// ReviewerName is the participant name of an agent in VALIDACAO.pareceres ("" when it has none).
func ReviewerName(agent string) string { return reviewerNames[agent] }
