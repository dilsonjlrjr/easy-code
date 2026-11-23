// Package notice turns the CloudEvents of watchNotices into native OS notifications of easycode-desktop
// (US-46 CA-3, US-39 CA-1, PL-52, cliente-local.md section 4). The notification text is complete on its own
// (item type, project and stage), so a screen reader reads it without opening the app (RNF-19).
package notice

import (
	"encoding/json"
	"strings"

	"easycode-desktop/internal/config"
	"easycode-desktop/internal/weblink"
)

// Prefix of every domain event type.
const Prefix = "br.easycode."

// CloudEvent is the part of the envelope (specs/dominio/eventos/envelope.schema.json) the app reads.
type CloudEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	ProjectID string          `json:"projectid,omitempty"`
	WaveID    string          `json:"waveid,omitempty"`
	Sequence  *int64          `json:"sequence,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// ShortType is the type without prefix and version: br.easycode.gate.awaiting_approval.v1 → gate.awaiting_approval.
func ShortType(eventType string) string {
	short := strings.TrimPrefix(eventType, Prefix)
	if i := strings.LastIndex(short, ".v"); i > 0 {
		short = short[:i]
	}
	return short
}

// Notification is what the app shows and where a click leads.
type Notification struct {
	ID     string
	Title  string
	Body   string
	Target weblink.Target
	// OpenInApp is true when a click opens the wave view of the app instead of the web (finished wave).
	OpenInApp bool
}

type payload struct {
	Gate             string `json:"gate"`
	Round            int    `json:"round"`
	Stage            string `json:"stage"`
	Wave             string `json:"wave"`
	Finding          string `json:"finding"`
	DestinationStage string `json:"destination_stage"`
	Cause            string `json:"cause"`
}

// Kinds that always notify: what the user would see in Pending items (US-48) plus a role assigned (PL-52).
var actionable = map[string]bool{
	"gate.awaiting_approval": true,
	"wave.question_opened":   true,
	"finding.escalated":      true,
	"project.budget_alerted": true,
	"member.role_assigned":   true,
}

var finished = map[string]bool{"wave.completed": true, "wave.failed": true, "wave.canceled": true}

// Builder makes notifications in the interface language.
type Builder struct {
	// ProjectName returns the name of a project, or "" when unknown.
	ProjectName func(id string) string
	// Followed says whether the user follows a wave (its view is open or they asked to follow it).
	Followed func(projectID, waveID string) bool
}

// Build returns the notification for an event, or false when the event does not notify.
func (b Builder) Build(ev CloudEvent, locale string) (Notification, bool) {
	kind := ShortType(ev.Type)
	if !actionable[kind] && !finished[kind] {
		return Notification{}, false
	}
	var data payload
	_ = json.Unmarshal(ev.Data, &data)
	waveID := ev.WaveID
	if waveID == "" {
		waveID = data.Wave
	}
	if finished[kind] && (b.Followed == nil || !b.Followed(ev.ProjectID, waveID)) {
		return Notification{}, false
	}
	msgs := catalog(locale)
	name := ""
	if b.ProjectName != nil {
		name = b.ProjectName(ev.ProjectID)
	}
	if name == "" {
		name = msgs["unknownProject"]
	}
	stage := data.Stage
	if kind == "finding.escalated" && data.DestinationStage != "" {
		stage = data.DestinationStage
	}
	body := msgs["project"] + ": " + name
	if stage != "" {
		body += " · " + msgs["stage"] + ": " + StageLabel(stage, locale)
	}
	n := Notification{ID: ev.ID, Title: msgs[kind], Body: body}
	project := ev.ProjectID
	switch kind {
	case "gate.awaiting_approval":
		n.Target = weblink.Target{Kind: weblink.KindGateRound, ProjectID: project, GateID: data.Gate, Round: data.Round}
	case "wave.question_opened":
		n.Target = weblink.Target{Kind: weblink.KindWave, ProjectID: project, WaveID: waveID}
	case "finding.escalated":
		n.Target = weblink.Target{Kind: weblink.KindFinding, ProjectID: project, FindingID: data.Finding}
	case "project.budget_alerted":
		n.Target = weblink.Target{Kind: weblink.KindProjectConfiguration, ProjectID: project}
	case "member.role_assigned":
		n.Target = weblink.Target{Kind: weblink.KindProject, ProjectID: project}
	default: // finished wave: the app view of the wave (ESP-28 section 2)
		n.Title = msgs[kind] + ": " + waveID
		n.Target = weblink.Target{Kind: weblink.KindWave, ProjectID: project, WaveID: waveID}
		n.OpenInApp = true
	}
	return n, true
}

// StageLabel is the stage name of the web message catalog (stages.*), or the id when unknown.
func StageLabel(stage, locale string) string {
	labels := stagesPtBR
	if config.NormalizeLocale(locale) == config.LocaleEn {
		labels = stagesEn
	}
	if label, ok := labels[stage]; ok {
		return label
	}
	return stage
}

func catalog(locale string) map[string]string {
	if config.NormalizeLocale(locale) == config.LocaleEn {
		return messagesEn
	}
	return messagesPtBR
}

var messagesPtBR = map[string]string{
	"gate.awaiting_approval": "Gate aguardando aprovação",
	"wave.question_opened":   "Pergunta do agente aguardando resposta",
	"finding.escalated":      "Achado escalado aguardando decisão",
	"project.budget_alerted": "Alerta de orçamento do projeto",
	"member.role_assigned":   "Você recebeu um papel no projeto",
	"wave.completed":         "Onda concluída",
	"wave.failed":            "Onda falhou",
	"wave.canceled":          "Onda cancelada",
	"project":                "Projeto",
	"stage":                  "Etapa",
	"unknownProject":         "projeto sem nome",
}

var messagesEn = map[string]string{
	"gate.awaiting_approval": "Gate awaiting approval",
	"wave.question_opened":   "Agent question awaiting an answer",
	"finding.escalated":      "Escalated finding awaiting a decision",
	"project.budget_alerted": "Project budget alert",
	"member.role_assigned":   "You were given a role in the project",
	"wave.completed":         "Wave completed",
	"wave.failed":            "Wave failed",
	"wave.canceled":          "Wave canceled",
	"project":                "Project",
	"stage":                  "Stage",
	"unknownProject":         "unnamed project",
}

// Same labels as stages.* of the web catalog (project/frontend/src/shared/i18n/locales).
var stagesPtBR = map[string]string{
	"requirements": "Requisitos", "architecture": "Arquitetura", "data": "Modelagem de Dados", "design": "Design",
	"validation": "Validação técnica", "implementation": "Implementação", "qa": "QA", "security": "Segurança",
	"release": "Release", "qa-plan": "QA (plano)", "security-threats": "Segurança (ameaças)",
}

var stagesEn = map[string]string{
	"requirements": "Requirements", "architecture": "Architecture", "data": "Data modeling", "design": "Design",
	"validation": "Technical validation", "implementation": "Implementation", "qa": "QA", "security": "Security",
	"release": "Release", "qa-plan": "QA (plan)", "security-threats": "Security (threats)",
}
