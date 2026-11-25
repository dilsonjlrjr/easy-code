package runtime

import (
	"context"
	"encoding/json"

	"easycode/internal/domain/provider"
)

// Releaser is implemented by a harness that keeps work alive between steps (a Claude Code run): the
// executor calls Release when the wave leaves its loop, so nothing keeps running for a wave that paused,
// failed, waits for the user or was canceled.
type Releaser interface {
	Release(wave WaveContext)
}

// Dispatch picks the harness of a wave by its model pair (ADR-23): the Claude Code harness when a model of
// the pair is of the claude-code provider, the own harness otherwise. The agent policy is the same.
type Dispatch struct {
	own    Steps
	claude *ClaudeCodeAgent
}

// NewDispatch wraps the own harness of an agent; without claude it is the own harness alone.
func NewDispatch(own Steps, claude *ClaudeCodeAgent) Steps {
	if claude == nil {
		return own
	}
	return &Dispatch{own: own, claude: claude}
}

// UsesClaudeCode reports whether the wave runs through Claude Code.
func UsesClaudeCode(wave WaveContext) bool {
	for _, model := range wave.Models {
		if model.Provider == provider.ClaudeCode {
			return true
		}
	}
	return false
}

func (d *Dispatch) byWave(wave WaveContext) Steps {
	if UsesClaudeCode(wave) {
		return d.claude
	}
	return d.own
}

// byState picks the harness that wrote the checkpoint.
func (d *Dispatch) byState(cp Checkpoint) Steps {
	var mark struct {
		Harness string `json:"harness"`
	}
	if json.Unmarshal(cp.State, &mark) == nil && mark.Harness == HarnessClaudeCode {
		return d.claude
	}
	return d.own
}

// Start implements Steps.
func (d *Dispatch) Start(ctx context.Context, wave WaveContext) (Checkpoint, string, error) {
	return d.byWave(wave).Start(ctx, wave)
}

// Next implements Steps: the harness of the checkpoint (a pair changed mid-attempt keeps its harness).
func (d *Dispatch) Next(ctx context.Context, wave WaveContext, cp Checkpoint) (StepResult, error) {
	return d.byState(cp).Next(ctx, wave, cp)
}

// Answer implements Steps.
func (d *Dispatch) Answer(cp Checkpoint, answers []string) (Checkpoint, error) {
	return d.byState(cp).Answer(cp, answers)
}

// Reject implements Steps.
func (d *Dispatch) Reject(cp Checkpoint, problems string) (Checkpoint, error) {
	return d.byState(cp).Reject(cp, problems)
}

// Awaiting implements Steps.
func (d *Dispatch) Awaiting(cp Checkpoint) bool { return d.byState(cp).Awaiting(cp) }

// Corrections implements Steps.
func (d *Dispatch) Corrections(cp Checkpoint) int { return d.byState(cp).Corrections(cp) }

// Release implements Releaser.
func (d *Dispatch) Release(wave WaveContext) { d.claude.Release(wave) }
