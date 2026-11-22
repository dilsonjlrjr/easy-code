package audit

import (
	"errors"
	"testing"
)

const project = "3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14"

func valid() Record {
	return Record{ProjectID: project, Actor: Actor{Type: ActorHuman, ID: "sub-1"}, Origin: OriginWeb, Action: "gate.approve", Target: "gates/gate.data/rounds/2"}
}

func TestValidRecords(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	orchestrator := Record{OrganizationID: project, Actor: Actor{Type: ActorOrchestrator, ID: "orchestrator"}, Policy: "policy.decision_record", Action: "wave.pause_now", Target: "waves/DBA-01", LostCost: "0.4127"}
	if err := orchestrator.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidRecords(t *testing.T) {
	cases := map[string]func(r *Record){
		"no scope":             func(r *Record) { r.ProjectID = "" },
		"human without origin": func(r *Record) { r.Origin = "" },
		"agent with origin":    func(r *Record) { r.Actor.Type = ActorAgent },
		"unknown actor":        func(r *Record) { r.Actor.Type = "robot" },
		"policy on human":      func(r *Record) { r.Policy = "policy.x" },
		"bad action":           func(r *Record) { r.Action = "Approve" },
		"blank target":         func(r *Record) { r.Target = "  " },
		"float lost cost":      func(r *Record) { r.LostCost = "1e3" },
		"empty actor id":       func(r *Record) { r.Actor.ID = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			record := valid()
			mutate(&record)
			if err := record.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}
