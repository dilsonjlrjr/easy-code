package workflow

import (
	"context"

	"easycode/internal/domain/project"
)

// StageView is one stage of getWorkflow (StageSummary).
type StageView struct {
	ID                   string
	Name                 string
	ShortName            string
	Agent                string
	DependsOn            []string
	RequiredCapabilities []string
	State                string // "" before the start (null in the contract)
	Gate                 string
	CurrentRound         int
	GateState            string
	ReevaluationPending  bool
	ActiveWaves          int
	Cost                 project.Money
	// Type is stage or parallel_activity; ApprovedInGate is the gate of an activity (approved_in_gate).
	Type           string
	ApprovedInGate string
	// ReopenedByFinding is the ACH that reopened the stage (US-34), "" when none.
	ReopenedByFinding string
}

// View is getWorkflow: the stages of the workflow in force (US-05 CA-5, US-49 CA-1).
type View struct {
	ID      string
	Version int
	Mode    string
	Stages  []StageView
}

// Views reads the workflow of a project.
type Views interface {
	// Workflow uses the pinned copy after the start and the workflow in force before it.
	Workflow(ctx context.Context, projectID string) (View, error)
}
