// Package finding holds the rules of the finding (ACH, specs/dominio/achado.md and estados/achado.md):
// the states of its machine, which ones block a gate, the mapping between the pt-BR values of the
// artifact header and the code names of the database and the API (ADR-21, D-36), and the default
// destination of each category.
package finding

import (
	"strings"
	"time"
)

// States of the machine "finding" (findings.status).
const (
	StatusOpen          = "open"
	StatusInRework      = "in_rework"
	StatusRevalidating  = "revalidating"
	StatusClosed        = "closed"
	StatusEscalated     = "escalated"
	StatusAwaitingScope = "awaiting_scope"
	StatusAccepted      = "accepted"
	StatusDiscarded     = "discarded"
)

// Resolutions of a closed finding and results of a cycle.
const (
	ResolutionFixed       = "fixed"
	ResolutionScopeChange = "scope_change"
	ResultFixed           = "fixed"
	ResultNotFixed        = "not_fixed"
)

// Escalation decisions (finding_escalations.decision).
const (
	DecisionNewRework   = "new_rework"
	DecisionAcceptRisk  = "accept_risk"
	DecisionScopeChange = "scope_change"
)

// CycleLimit is the number of rework cycles before a finding escalates (PROJETO 6.3, RN-1 of US-32).
const CycleLimit = 3

// Active reports whether the finding still asks for work or a decision (it counts for the gate).
func Active(status string) bool {
	switch status {
	case StatusOpen, StatusInRework, StatusRevalidating, StatusEscalated, StatusAwaitingScope:
		return true
	}
	return false
}

// Blocking reports whether a finding blocks the gate of its origin stage and the Release (critical or
// high and active, achado.md section 7).
func Blocking(severity, status string) bool {
	return (severity == "critical" || severity == "high") && Active(status)
}

var severities = map[string]string{"critico": "critical", "alto": "high", "medio": "medium", "baixo": "low"}

var categories = map[string]string{
	"defeito": "defect", "requisito_ambiguo": "ambiguous_requirement", "vulnerabilidade": "vulnerability",
	"desvio_arquitetura": "architecture_deviation", "inconsistencia_dados": "data_inconsistency",
	"inconsistencia_design": "design_inconsistency", "divida_tecnica": "technical_debt", "lacuna_contrato": "contract_gap",
}

var stages = map[string]string{
	"requisitos": "requirements", "arquitetura": "architecture", "dados": "data", "design": "design", "validacao": "validation",
	"implementacao": "implementation", "qa": "qa", "seguranca": "security", "release": "release", "qa-plano": "qa-plan",
	"seguranca-ameacas": "security-threats",
}

var statuses = map[string]string{
	"aberto": StatusOpen, "em_retrabalho": StatusInRework, "revalidando": StatusRevalidating, "fechado": StatusClosed,
	"escalado": StatusEscalated, "aguardando_escopo": StatusAwaitingScope, "aceito": StatusAccepted, "descartado": StatusDiscarded,
}

func lookup(table map[string]string, value string) string {
	if code, ok := table[value]; ok {
		return code
	}
	for _, code := range table {
		if code == value {
			return value
		}
	}
	return ""
}

func reverse(table map[string]string, code string) string {
	for header, c := range table {
		if c == code {
			return header
		}
	}
	return ""
}

// Severity maps the header value (alto) to the code name (high); a code name maps to itself.
func Severity(value string) string { return lookup(severities, value) }

// Category maps the header value (defeito) to the code name (defect).
func Category(value string) string { return lookup(categories, value) }

// Stage maps a stage of the header (implementacao, qa-plano) to the workflow id (implementation, qa-plan).
func Stage(value string) string { return lookup(stages, value) }

// Status maps the header situation (aberto) to the machine state (open).
func Status(value string) string { return lookup(statuses, value) }

// HeaderStage is the header value of a workflow stage id (implementation → implementacao).
func HeaderStage(stage string) string { return reverse(stages, stage) }

// HeaderStatus is the header situation of a machine state (open → aberto).
func HeaderStatus(status string) string { return reverse(statuses, status) }

// HeaderSeverity is the header severity of a code name (high → alto).
func HeaderSeverity(severity string) string { return reverse(severities, severity) }

// SeverityRank orders severities, critical first.
func SeverityRank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	}
	return 3
}

// Cycle is one rework and revalidation cycle (finding_cycles).
type Cycle struct {
	Number            int
	ReworkWave        string
	ReworkAgent       string
	CommitOrVersion   string
	RevalidationWave  string
	RevalidationAgent string
	Result            string // fixed | not_fixed | "" while running
}

// Escalation is one escalation and the human decision on it (finding_escalations).
type Escalation struct {
	Number           int
	At               time.Time
	Reason           string
	Decision         string // new_rework | accept_risk | scope_change | ""
	AuthorizedBy     string // subject of the deciding user
	AuthorizedByName string // display name
	DecidedAt        *time.Time
	ModifiedBy       string
	StartingPoint    []string
	Justification    string
}

// Finding is a recorded finding with its history.
type Finding struct {
	ProjectID         string
	Code              string
	Title             string
	Severity          string
	Category          string
	Status            string
	OriginStage       string
	DestinationStage  string
	Author            string // agent of the origin wave: the one who revalidates
	OriginWave        string
	Affected          []string
	Cycles            []Cycle
	Escalations       []Escalation
	Resolution        string
	ModifiedArtifacts []string
	AcceptedBy        string // subject
	AcceptedByName    string
	AcceptedAt        *time.Time
	Justification     string
	ValidUntil        *time.Time
	DiscardReason     string
	DuplicateOf       string
	OpenedAt          time.Time
	Version           int
	Header            map[string]any
	Body              string
}

// Blocking reports whether the finding blocks the gate of its origin stage.
func (f Finding) Blocking() bool { return Blocking(f.Severity, f.Status) }

// AcceptanceExpired reports whether an accepted finding has its validity in the past (PL-68).
func (f Finding) AcceptanceExpired(now time.Time) bool {
	if f.Status != StatusAccepted || f.ValidUntil == nil {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return f.ValidUntil.Before(today)
}

// LastCycle returns the last cycle, or nil.
func (f Finding) LastCycle() *Cycle {
	if len(f.Cycles) == 0 {
		return nil
	}
	return &f.Cycles[len(f.Cycles)-1]
}

// ExtraRounds counts the human authorizations of a round beyond the cycle limit.
func (f Finding) ExtraRounds() int {
	n := 0
	for _, e := range f.Escalations {
		if e.Decision == DecisionNewRework && e.AuthorizedBy != "" {
			n++
		}
	}
	return n
}

// ArtifactOf is the artifact id of an affected reference: US-03/CA-2 → US-03; a file path → "".
func ArtifactOf(reference string) string {
	if strings.Contains(reference, ".") || strings.HasPrefix(reference, "docs/") || strings.HasPrefix(reference, "app/") {
		return ""
	}
	id, _, _ := strings.Cut(reference, "/")
	id, _, _ = strings.Cut(id, ":")
	return id
}

// WithoutEvidence reports whether the evidence is only a template placeholder or too short to point to
// anything (US-32 CA-3: a finding without evidence is discarded; the schema already refuses an empty one).
func WithoutEvidence(evidence string) bool {
	text := strings.TrimSpace(evidence)
	if len([]rune(text)) < 10 {
		return true
	}
	return strings.HasPrefix(text, "<") && strings.HasSuffix(text, ">") && !strings.Contains(text[1:len(text)-1], "<")
}

// JustifiedDestination reports whether the body has a non-empty "Justificativa do destino" section.
func JustifiedDestination(body string) bool {
	_, after, ok := strings.Cut(body, "## Justificativa do destino")
	if !ok {
		return false
	}
	section, _, _ := strings.Cut(after, "\n## ")
	section = strings.TrimSpace(section)
	return section != "" && !strings.HasPrefix(section, "<")
}
