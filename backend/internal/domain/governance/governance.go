// Package governance holds the gate composition of specs/dominio/governanca (gates.yaml and the catalog
// checagens.yaml): which checks every gate runs, whether each one blocks the round or only alerts, which
// ones a human confirms, and the approval modes the gate allows (ADR-10, interno.md section 5).
package governance

import (
	"fmt"
	"sort"
)

// Check types and failure severities of the catalog (check_executions.type and severity).
const (
	TypeAutomatic  = "automatic"
	TypeHuman      = "human"
	SeverityBlocks = "blocks"
	SeverityAlert  = "alert"
)

// Approval modes of a round (gate_rounds.approval_mode).
const (
	ModeHuman     = "human"
	ModeAutomatic = "automatic"
)

// CatalogCheck is one check of checagens.yaml.
type CatalogCheck struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	FailureSeverity string `json:"failure_severity"`
}

// Checks is a pair of required (blocking) and alert lists.
type Checks struct {
	Required []string `json:"required_checks"`
	Alerts   []string `json:"alert_checks"`
}

// Approval is the approval rule of a gate (PROJETO 7.3).
type Approval struct {
	Allowed      string `json:"allowed"` // human | human_or_automatic
	Default      string `json:"default"` // human | automatic
	AlwaysHuman  bool   `json:"always_human"`
	Configurable bool   `json:"configurable_per_project"`
}

// Gate is one gate of gates.yaml.
type Gate struct {
	ID                 string   `json:"id"`
	Stage              string   `json:"stage"`
	Approves           []string `json:"approves"`
	Required           []string `json:"required_checks"`
	Alerts             []string `json:"alert_checks"`
	AlertsRequireHuman []string `json:"alerts_require_human"`
	Approval           Approval `json:"approval"`
}

// InternalCycle is the check set of a finding rework that opens no round (implementation).
type InternalCycle struct {
	Stage    string   `json:"stage"`
	Required []string `json:"required_checks"`
	Alerts   []string `json:"alert_checks"`
}

// Composition is the loaded governance: catalog, common checks and gates.
type Composition struct {
	Catalog       map[string]CatalogCheck
	Common        Checks
	Gates         map[string]Gate
	InternalCycle []InternalCycle
	Version       string // hash of the YAML files (Report.GovernanceVersion)
}

// Spec is one check a gate runs, with the severity it has in that gate.
type Spec struct {
	ID       string
	Type     string // automatic | human
	Severity string // blocks | alert
}

// Validate checks that every id the gates cite is in the catalog.
func (c Composition) Validate() error {
	ids := append(append([]string{}, c.Common.Required...), c.Common.Alerts...)
	for _, g := range c.Gates {
		ids = append(append(ids, g.Required...), g.Alerts...)
	}
	for _, cycle := range c.InternalCycle {
		ids = append(append(ids, cycle.Required...), cycle.Alerts...)
	}
	for _, id := range ids {
		if _, ok := c.Catalog[id]; !ok {
			return fmt.Errorf("gates.yaml cites %s, which is not in checagens.yaml", id)
		}
	}
	return nil
}

// GateOfStage returns the gate of a stage.
func (c Composition) GateOfStage(stage string) (Gate, bool) {
	g, ok := c.Gates["gate."+stage]
	return g, ok
}

// ChecksOf lists the checks of a gate: the common ones and the gate ones, required first, without
// repetition (a check that is required anywhere blocks). promoted are alerts the project made blocking
// (configuration_per_project.promote_alert); a required check is never demoted.
func (c Composition) ChecksOf(gate string, promoted []string) []Spec {
	g := c.Gates[gate]
	blocks := map[string]bool{}
	for _, id := range promoted {
		blocks[id] = true
	}
	var order []string
	seen := map[string]bool{}
	add := func(ids []string, required bool) {
		for _, id := range ids {
			if required {
				blocks[id] = true
			}
			if !seen[id] {
				seen[id] = true
				order = append(order, id)
			}
		}
	}
	add(c.Common.Required, true)
	add(g.Required, true)
	add(c.Common.Alerts, false)
	add(g.Alerts, false)
	specs := make([]Spec, 0, len(order))
	for _, id := range order {
		severity := SeverityAlert
		if blocks[id] {
			severity = SeverityBlocks
		}
		specs = append(specs, Spec{ID: id, Type: c.Catalog[id].Type, Severity: severity})
	}
	sort.SliceStable(specs, func(i, j int) bool { return specs[i].Severity == SeverityBlocks && specs[j].Severity != SeverityBlocks })
	return specs
}

// RequiresHuman reports whether a failed alert takes an automatic round to awaiting_human (PC-06).
func (g Gate) RequiresHuman(check string) bool {
	for _, id := range g.AlertsRequireHuman {
		if id == check {
			return true
		}
	}
	return false
}
