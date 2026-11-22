package event

import (
	"errors"
	"testing"
	"time"
)

const (
	project     = "3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14"
	id          = "0193a5c8-1e02-7b4f-a6d3-9c0e5b7f2a18"
	correlation = "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b30"
)

func TestNewDomainBuildsProjectEnvelope(t *testing.T) {
	envelope, err := NewDomain(Draft{
		Type: "br.easycode.wave.completed.v1", Subject: "waves/DEV-07", ProjectID: project, WaveID: "DEV-07",
		CorrelationID: correlation, Time: time.Now(),
	}, id, 512)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Source != "/projects/"+project || envelope.DataSchema != "urn:easycode:schema:event:payload-wave:v1#/$defs/completed" {
		t.Fatalf("unexpected envelope %+v", envelope)
	}
	if string(envelope.Data) != "{}" || envelope.Sequence != 512 {
		t.Fatalf("data %s sequence %d", envelope.Data, envelope.Sequence)
	}
}

func TestNewTelemetryUsesWaveSourceWithoutSequence(t *testing.T) {
	envelope, err := NewTelemetry(Draft{
		Type: "br.easycode.cost.model_call_recorded.v1", Subject: "waves/DEV-07", ProjectID: project, WaveID: "DEV-07",
		CorrelationID: correlation, Time: time.Now(),
	}, id)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Source != "/projects/"+project+"/waves/DEV-07" || envelope.Sequence != 0 {
		t.Fatalf("unexpected envelope %+v", envelope)
	}
}

func TestEnvelopeRules(t *testing.T) {
	base := Draft{Type: "br.easycode.gate.approved.v1", Subject: "gates/gate.data/rounds/2", ProjectID: project, CorrelationID: correlation, Time: time.Now()}
	cases := map[string]func(d *Draft){
		"telemetry as domain":    func(d *Draft) { d.Type = "br.easycode.agent.progress_reported.v1" },
		"skill flow":             func(d *Draft) { d.Type = "br.easycode.skill.blocked.v1"; d.Subject = "skills/go/versions/1" },
		"bad subject":            func(d *Draft) { d.Subject = "whatever" },
		"bad correlation":        func(d *Draft) { d.CorrelationID = "x" },
		"bad wave":               func(d *Draft) { d.WaveID = "DEV-1" },
		"missing project":        func(d *Draft) { d.ProjectID = "" },
		"array data":             func(d *Draft) { d.Data = []byte(`[]`) },
		"unversioned event type": func(d *Draft) { d.Type = "br.easycode.gate.approved" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			draft := base
			mutate(&draft)
			if _, err := NewDomain(draft, id, 1); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
	if _, err := NewTelemetry(Draft{Type: "br.easycode.cost.model_call_recorded.v1", Subject: "waves/DEV-07", ProjectID: project, CorrelationID: correlation, Time: time.Now()}, id); !errors.Is(err, ErrInvalid) {
		t.Fatalf("telemetry without wave must fail, got %v", err)
	}
}
