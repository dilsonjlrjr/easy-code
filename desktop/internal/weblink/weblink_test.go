package weblink

import (
	"errors"
	"testing"
)

const server = "https://easycode.example/"
const project = "0193a2b4-0000-7000-8000-000000000001"

func TestURL(t *testing.T) {
	cases := map[string]Target{
		"https://easycode.example/":                                                          {Kind: KindHome},
		"https://easycode.example/pending":                                                   {Kind: KindPending},
		"https://easycode.example/account/local-client":                                      {Kind: KindAccountLocalClient},
		"https://easycode.example/projects/" + project:                                       {Kind: KindProject, ProjectID: project},
		"https://easycode.example/projects/" + project + "/configuration":                    {Kind: KindProjectConfiguration, ProjectID: project},
		"https://easycode.example/projects/" + project + "/waves/REQ-01":                     {Kind: KindWave, ProjectID: project, WaveID: "REQ-01"},
		"https://easycode.example/projects/" + project + "/gates/gate.requirements/rounds/2": {Kind: KindGateRound, ProjectID: project, GateID: "gate.requirements", Round: 2},
		"https://easycode.example/projects/" + project + "/findings/ACH-07#decision":         {Kind: KindFinding, ProjectID: project, FindingID: "ACH-07"},
	}
	for want, target := range cases {
		got, err := URL(server, target)
		if err != nil || got != want {
			t.Errorf("URL(%+v) = %q, %v; want %q", target, got, err, want)
		}
	}
}

func TestURLRefusesUnexpectedTargets(t *testing.T) {
	for _, target := range []Target{
		{Kind: "settings"},
		{Kind: KindProject},
		{Kind: KindProject, ProjectID: "../settings"},
		{Kind: KindProject, ProjectID: "a/b"},
		{Kind: KindWave, ProjectID: project, WaveID: "https://evil.example"},
		{Kind: KindGateRound, ProjectID: project, GateID: "gate.requirements"},
		{Kind: KindFinding, ProjectID: project, FindingID: "ACH 07"},
	} {
		if _, err := URL(server, target); !errors.Is(err, ErrInvalidTarget) {
			t.Errorf("URL(%+v) accepted", target)
		}
	}
}

func TestForPendingItem(t *testing.T) {
	cases := []struct {
		itemType, targetType, targetID string
		want                           Target
	}{
		{"gate", "gate_round", "gate.requirements/2", Target{Kind: KindGateRound, ProjectID: project, GateID: "gate.requirements", Round: 2}},
		{"question", "question", "REQ-01/PERG-1", Target{Kind: KindWave, ProjectID: project, WaveID: "REQ-01"}},
		{"finding_escalated", "finding", "ACH-07", Target{Kind: KindFinding, ProjectID: project, FindingID: "ACH-07"}},
		{"budget", "project", project, Target{Kind: KindProjectConfiguration, ProjectID: project}},
		{"gate", "gate_round", "broken", Target{Kind: KindProject, ProjectID: project}},
	}
	for _, tc := range cases {
		if got := ForPendingItem(tc.itemType, project, tc.targetType, tc.targetID); got != tc.want {
			t.Errorf("ForPendingItem(%s, %s) = %+v, want %+v", tc.itemType, tc.targetID, got, tc.want)
		}
	}
}
