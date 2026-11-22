// Package runtime runs agent waves (interno.md section 4, ADR-03, ADR-04): the Executor claims
// queued waves with a lease, drives the agent harness one step at a time and records every step
// (model call or tool execution) as a checkpoint, with cost, audit and outbox in one transaction,
// so a forced restart resumes from the last checkpoint (US-11, RNF-12).
package runtime

import (
	"context"
	"encoding/json"

	"easycode/internal/domain/permission"
	"easycode/internal/domain/provider"
	"easycode/internal/domain/sandbox"
)

// Step types (StepResult.Type).
const (
	StepContinue = "continue"
	StepAsk      = "ask"
	StepComplete = "complete"
	StepFail     = "fail"
)

// After says what a checkpoint follows (wave_checkpoints.after).
const (
	AfterModelCall = "model_call"
	AfterTool      = "tool"
)

// WaveContext is what the harness knows of the wave it runs.
type WaveContext struct {
	ProjectID     string
	WaveID        string // public id
	Code          string // REQ-01
	Stage         string
	Agent         string
	Attempt       int
	Instruction   string // instruction text in the pinned version
	BusinessNeed  string // untrusted input (RNF-05)
	Constraints   string // rendered technical constraints, untrusted
	Skills        string // skills by description (none in MARCO-1)
	Language      string // artifact language (RNF-21)
	Feedback      string // reason of the last rejected gate round (rework), untrusted
	WritablePaths []string
	Outputs       []string            // artifact types the stage must deliver
	Models        map[string]ModelRef // by role: the pair the wave started with (US-16 CA-4)
	// Policy is the least-privilege policy of the agent (RNF-07); nil derives a write-only policy
	// from WritablePaths (agents without sandbox of MARCO-1).
	Policy *permission.Policy
	// Base is the project repository at the start of the wave (path → text), read-only for the agent.
	Base map[string]string
	// Approved maps the path of every approved artifact to its author (US-30 CA-2, RN-1).
	Approved map[string]string
	Mode     string         // agent mode of the stage (DESENHO, MANUAL…)
	Task     string         // T of an implementation wave (one wave per task, PL-20)
	Image    string         // logical sandbox image of the project stack
	Limits   sandbox.Limits // effective limits of the commands (min of agent and project, RNF-03)
}

// ModelRef is a model of the wave pair.
type ModelRef struct {
	ID       int64
	Provider string
	Name     string
}

// Checkpoint is the agent state after a step (number 0 = the start of the attempt).
type Checkpoint struct {
	Number int
	After  string
	State  json.RawMessage // opaque to the executor
}

// QuestionItem is one numbered question with its suggested answer (US-17 CA-2, US-10).
type QuestionItem struct {
	Text       string `json:"text"`
	Suggestion string `json:"suggestion,omitempty"`
	Source     string `json:"source,omitempty"`
	Excerpt    string `json:"excerpt,omitempty"`
}

// Output is a file the agent wrote in its workspace.
type Output struct {
	Path    string
	Content []byte
}

// ToolRecord is a tool execution, without its arguments (US-09 RN-1).
type ToolRecord struct {
	Tool          string
	Allowed       bool
	RefusalReason string
	Success       bool
	Summary       string
	Execution     *Execution // execute_command in the sandbox (US-41)
}

// Execution is the evidence of a command run in the sandbox (US-41 CA-1, CA-2, CA-3).
type Execution struct {
	ID       string
	Category string
	Command  string
	Result   sandbox.ExecutionResult
	Output   []byte // full output, kept as evidence
}

// TaskFailure is a build, test or lint failure of the task of the wave (wave.task_failed).
type TaskFailure struct {
	Cause       string // build | tests | lint
	Consecutive int
}

// StepResult is the outcome of one step.
type StepResult struct {
	Type     string
	New      Checkpoint
	Call     *CallRecord // the step was a model call
	Tool     *ToolRecord // the step was a tool execution
	Question []QuestionItem
	Outputs  []Output
	Failure  string // failure cause when Type = fail
	// TaskFailed is set when the step was a failed build or test command of the task.
	TaskFailed *TaskFailure
}

// Steps is implemented by the agent harness: one unit of work per Next, never touching the database.
type Steps interface {
	Start(ctx context.Context, wave WaveContext) (Checkpoint, string, error) // checkpoint 0 and the inputs digest (RNF-15)
	Next(ctx context.Context, wave WaveContext, cp Checkpoint) (StepResult, error)
	// Answer puts the user answers into the state before the next step (US-10 CA-1).
	Answer(cp Checkpoint, answers []string) (Checkpoint, error)
	// Reject returns the validation errors of the delivered outputs to the agent (US-29 CA-1).
	Reject(cp Checkpoint, problems string) (Checkpoint, error)
	// Awaiting reports whether the state waits for the user answer.
	Awaiting(cp Checkpoint) bool
	// Corrections counts the rejected deliveries of the attempt (US-29 CA-2).
	Corrections(cp Checkpoint) int
}

// CallRecord is a model call as the runtime records it (cost by the price table, RNF-16).
type CallRecord struct {
	ID       int64 // model_calls row, opened before the call
	Role     string
	Provider string
	Model    string
	Usage    provider.Usage
	Result   string // success | error | aborted
	Error    string // error class
	Summary  string
	Duration int64 // ms
	// Filled by the ledger when the call is finished: exact cost by the price in force at the start.
	Cost              string
	PriceTableVersion string
}

// ModelCaller makes one model call in the wave scope: it opens the model_calls row before the
// call (so a crash leaves a lost, estimated call), asks easycode-gateway and returns the usage.
type ModelCaller interface {
	Call(ctx context.Context, wave WaveContext, step int, role string, req provider.Request) (provider.Response, CallRecord, error)
}
