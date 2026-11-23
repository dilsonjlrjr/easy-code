package artifact_test

import (
	"testing"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/domain/failure"
)

// US-53: the guard of the transition edit (estados.yaml).
func TestEditGuard(t *testing.T) {
	modify := appartifact.EditRequest{Operation: appartifact.EditModify, Type: "US", Artifact: "US-01", BaseVersion: 2}
	cases := []struct {
		name  string
		state appartifact.EditState
		code  string
	}{
		{"draft", appartifact.EditState{Exists: true, State: "draft", CurrentVersion: 2, StageState: "running"}, ""},
		{"in review with the round open", appartifact.EditState{Exists: true, State: "in_review", CurrentVersion: 2, StageState: "awaiting_gate", OpenRound: true}, ""},
		{"in review, round already decided", appartifact.EditState{Exists: true, State: "in_review", CurrentVersion: 2, StageState: "awaiting_gate"}, failure.CodeStateConflict},
		{"approved (CA-6)", appartifact.EditState{Exists: true, State: "approved", CurrentVersion: 2, StageState: "approved"}, failure.CodeStateConflict},
		{"wave active", appartifact.EditState{Exists: true, State: "draft", CurrentVersion: 2, StageState: "running", ActiveWaves: 1}, failure.CodeStateConflict},
		{"outdated", appartifact.EditState{Exists: true, State: "draft", CurrentVersion: 3, StageState: "running"}, appartifact.CodeOutdatedVersion},
		{"missing", appartifact.EditState{StageState: "running"}, failure.CodeNotFound},
	}
	for _, c := range cases {
		err := appartifact.EditGuard(modify, c.state)
		if got := failure.From(err).Code; (err == nil) != (c.code == "") || (err != nil && got != c.code) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	create := appartifact.EditRequest{Operation: appartifact.EditCreate, Type: "ADR"}
	if err := appartifact.EditGuard(create, appartifact.EditState{StageState: "awaiting_gate"}); err != nil {
		t.Errorf("create in a stage at its gate: %v", err)
	}
}
