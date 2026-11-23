package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
)

// OrchestratorAgent is the author of the Release stage (workflow-padrao: agent orchestrator). It is not an
// AI agent: it has no instruction, no model pair and no sandbox (permissoes.yaml, orchestrator).
const OrchestratorAgent = "orchestrator"

// ReleaseBuilder writes the RELEASE of a project from its current facts: the path and the whole file.
type ReleaseBuilder func(ctx context.Context, projectID, wave string) (string, []byte, error)

// releaseState is the checkpoint state of the release wave.
type releaseState struct {
	Corrections int    `json:"corrections"`
	Problems    string `json:"problems,omitempty"`
}

// ReleaseSteps is the harness of the release wave of the orchestrator: one step builds the RELEASE from the
// approved artifacts, the findings, the gate decisions and the wave record (template RELEASE: "o orquestrador
// preenche a partir dos artefatos; ninguém edita números à mão") and delivers it, with no model call.
type ReleaseSteps struct {
	build ReleaseBuilder
}

// NewReleaseSteps builds the harness.
func NewReleaseSteps(build ReleaseBuilder) *ReleaseSteps { return &ReleaseSteps{build: build} }

func encodeRelease(number int, after string, s releaseState) (Checkpoint, error) {
	state, err := json.Marshal(s)
	return Checkpoint{Number: number, After: after, State: state}, err
}

func decodeRelease(cp Checkpoint) releaseState {
	var s releaseState
	_ = json.Unmarshal(cp.State, &s)
	return s
}

// Start implements Steps: checkpoint 0; the inputs digest is the one of the wave (no model input).
func (r *ReleaseSteps) Start(_ context.Context, wave WaveContext) (Checkpoint, string, error) {
	cp, err := encodeRelease(0, "", releaseState{})
	sum := sha256.Sum256([]byte(wave.ProjectID + "/" + wave.Code + "/release"))
	return cp, hex.EncodeToString(sum[:]), err
}

// Next implements Steps: the RELEASE is written (write_file) and delivered at once.
func (r *ReleaseSteps) Next(ctx context.Context, wave WaveContext, cp Checkpoint) (StepResult, error) {
	file, content, err := r.build(ctx, wave.ProjectID, wave.Code)
	if err != nil {
		return StepResult{}, err
	}
	state := decodeRelease(cp)
	next, err := encodeRelease(cp.Number+1, AfterTool, state)
	return StepResult{Type: StepComplete, New: next, Outputs: []Output{{Path: file, Content: content}},
		Tool: &ToolRecord{Tool: "write_file", Allowed: true, Success: true, Summary: "gravou " + path.Base(file)}}, err
}

// Answer implements Steps (the orchestrator never asks).
func (r *ReleaseSteps) Answer(cp Checkpoint, _ []string) (Checkpoint, error) { return cp, nil }

// Reject implements Steps: a RELEASE out of its schema is counted; the next step builds it again.
func (r *ReleaseSteps) Reject(cp Checkpoint, problems string) (Checkpoint, error) {
	state := decodeRelease(cp)
	state.Corrections++
	state.Problems = problems
	return encodeRelease(cp.Number, cp.After, state)
}

// Awaiting implements Steps.
func (r *ReleaseSteps) Awaiting(Checkpoint) bool { return false }

// Corrections implements Steps.
func (r *ReleaseSteps) Corrections(cp Checkpoint) int { return decodeRelease(cp).Corrections }
