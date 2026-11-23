package notice

import (
	"encoding/json"
	"testing"

	"easycode-desktop/internal/weblink"
)

const project = "0193a2b4-0000-7000-8000-000000000001"

func event(kind, wave string, data map[string]any) CloudEvent {
	raw, _ := json.Marshal(data)
	return CloudEvent{ID: "e-" + kind, Type: Prefix + kind + ".v1", ProjectID: project, WaveID: wave, Data: raw}
}

func builder(followed bool) Builder {
	return Builder{
		ProjectName: func(id string) string {
			if id == project {
				return "Portal de compras"
			}
			return ""
		},
		Followed: func(string, string) bool { return followed },
	}
}

func TestActionableNoticesCarryTypeProjectAndStage(t *testing.T) {
	cases := []struct {
		ev     CloudEvent
		title  string
		body   string
		target weblink.Target
	}{
		{
			event("gate.awaiting_approval", "", map[string]any{"gate": "gate.requirements", "round": 2, "stage": "requirements"}),
			"Gate aguardando aprovação", "Projeto: Portal de compras · Etapa: Requisitos",
			weblink.Target{Kind: weblink.KindGateRound, ProjectID: project, GateID: "gate.requirements", Round: 2},
		},
		{
			event("wave.question_opened", "REQ-01", map[string]any{"wave": "REQ-01", "stage": "requirements"}),
			"Pergunta do agente aguardando resposta", "Projeto: Portal de compras · Etapa: Requisitos",
			weblink.Target{Kind: weblink.KindWave, ProjectID: project, WaveID: "REQ-01"},
		},
		{
			event("finding.escalated", "", map[string]any{"finding": "ACH-07", "origin_stage": "qa", "destination_stage": "implementation"}),
			"Achado escalado aguardando decisão", "Projeto: Portal de compras · Etapa: Implementação",
			weblink.Target{Kind: weblink.KindFinding, ProjectID: project, FindingID: "ACH-07"},
		},
		{
			event("project.budget_alerted", "", map[string]any{"project": project}),
			"Alerta de orçamento do projeto", "Projeto: Portal de compras",
			weblink.Target{Kind: weblink.KindProjectConfiguration, ProjectID: project},
		},
		{
			event("member.role_assigned", "", map[string]any{"project": project}),
			"Você recebeu um papel no projeto", "Projeto: Portal de compras",
			weblink.Target{Kind: weblink.KindProject, ProjectID: project},
		},
	}
	for _, tc := range cases {
		n, ok := builder(false).Build(tc.ev, "pt-BR")
		if !ok || n.Title != tc.title || n.Body != tc.body || n.Target != tc.target || n.OpenInApp {
			t.Errorf("%s: got %+v %v", tc.ev.Type, n, ok)
		}
		if _, err := weblink.URL("https://easycode.example", n.Target); err != nil {
			t.Errorf("%s: target without web address: %v", tc.ev.Type, err)
		}
	}
}

func TestEnglishNotice(t *testing.T) {
	n, _ := builder(false).Build(event("gate.awaiting_approval", "", map[string]any{"gate": "gate.qa", "round": 1, "stage": "qa"}), "en")
	if n.Title != "Gate awaiting approval" || n.Body != "Project: Portal de compras · Stage: QA" {
		t.Fatalf("%+v", n)
	}
}

func TestFinishedWaveNotifiesOnlyWhenFollowed(t *testing.T) {
	ev := event("wave.completed", "REQ-01", map[string]any{"wave": "REQ-01", "stage": "requirements"})
	if _, ok := builder(false).Build(ev, "pt-BR"); ok {
		t.Fatal("a wave nobody follows must not notify")
	}
	n, ok := builder(true).Build(ev, "pt-BR")
	if !ok || n.Title != "Onda concluída: REQ-01" || !n.OpenInApp || n.Target.WaveID != "REQ-01" {
		t.Fatalf("%+v %v", n, ok)
	}
}

func TestOtherEventsDoNotNotify(t *testing.T) {
	for _, kind := range []string{"wave.started", "gate.approved", "stage.approved", "project.paused"} {
		if _, ok := builder(true).Build(event(kind, "", map[string]any{}), "pt-BR"); ok {
			t.Errorf("%s notified", kind)
		}
	}
}

func TestShortType(t *testing.T) {
	if got := ShortType("br.easycode.gate.awaiting_approval.v1"); got != "gate.awaiting_approval" {
		t.Fatal(got)
	}
	if got := ShortType("easycode.gap"); got != "easycode.gap" {
		t.Fatal(got)
	}
}

func TestUnknownProjectAndStage(t *testing.T) {
	ev := event("wave.question_opened", "X-01", map[string]any{"stage": "custom"})
	ev.ProjectID = "other"
	n, _ := builder(false).Build(ev, "pt-BR")
	if n.Body != "Projeto: projeto sem nome · Etapa: custom" {
		t.Fatal(n.Body)
	}
}
