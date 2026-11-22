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

// Stage is a stage of the definition.
type Stage struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ShortName     string   `json:"short_name"`
	Agent         string   `json:"agent"`
	AgentMode     string   `json:"agent_mode,omitempty"`
	DependsOn     []string `json:"depends_on"`
	ProjectInputs []string `json:"project_inputs,omitempty"`
	Outputs       []Output `json:"outputs"`
	Gate          string   `json:"gate"`
}

// Definition is a workflow file (workflow-basico.yaml in Phase 1).
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
