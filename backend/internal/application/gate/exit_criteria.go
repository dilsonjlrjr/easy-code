package gate

import (
	"context"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/governance"
	"easycode/internal/domain/identity"
)

// StageCheck is one check the gate of a stage runs, as the exit criteria of the stage show it before
// any round (PENDENCIAS PC-20, PL-46: "ainda não avaliado"; getStage `gate_checks`).
type StageCheck struct {
	Check    string
	Name     string // catalog name (checagens.yaml)
	Type     string // automatic | human
	Severity string // blocks | alert, with the alerts the project promoted
}

// SettingsReader reads the project choices of a gate (gate_configurations).
type SettingsReader interface {
	Settings(ctx context.Context, projectID, gate string) (Settings, error)
}

// ExitCriteria lists the checks of the gate of a stage, from the governance composition (gates.yaml,
// checagens.yaml) and the alerts the project promoted.
type ExitCriteria struct {
	projects    appproject.Repository
	composition governance.Composition
	settings    SettingsReader
}

// NewExitCriteria builds the exit criteria reader.
func NewExitCriteria(projects appproject.Repository, composition governance.Composition, settings SettingsReader) *ExitCriteria {
	return &ExitCriteria{projects: projects, composition: composition, settings: settings}
}

// Checks returns the checks of a gate for the project, required first, in the order the rounds
// evaluate them. A gate out of the composition has no checks.
func (c *ExitCriteria) Checks(ctx context.Context, principal identity.Principal, projectID, gate string) ([]StageCheck, error) {
	if _, err := appproject.Authorize(ctx, c.projects, principal, projectID); err != nil {
		return nil, err
	}
	out := []StageCheck{}
	if _, ok := c.composition.Gates[gate]; !ok {
		return out, nil
	}
	settings, err := c.settings.Settings(ctx, projectID, gate)
	if err != nil {
		return nil, err
	}
	for _, spec := range c.composition.ChecksOf(gate, settings.Promoted) {
		out = append(out, StageCheck{Check: spec.ID, Name: c.composition.NameOf(spec.ID), Type: spec.Type, Severity: spec.Severity})
	}
	return out, nil
}

// NameOf is the catalog name of a check (ESP-12: the round shows the catalog name).
func (c *ExitCriteria) NameOf(check string) string { return c.composition.NameOf(check) }
