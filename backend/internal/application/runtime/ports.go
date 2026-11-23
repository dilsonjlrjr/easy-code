package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"easycode/internal/domain/provider"
	"easycode/internal/domain/workflow"
)

// ErrLeaseLost means another worker owns the wave now (fencing): stop without writing.
var ErrLeaseLost = errors.New("wave lease lost")

// ErrDispatchHeld means no model call may start: the project is not in progress or its spend reached the
// stop limit (policy.project_budget, RNF-16). The wave stops at its last checkpoint.
var ErrDispatchHeld = errors.New("project paused or budget exhausted: no new model call")

// Budget applies policy.project_budget after each recorded cost (US-39 CA-1, CA-2): alerts, and the
// project paused by the budget with its running waves asked to pause at their next checkpoint.
type Budget interface {
	CheckBudget(ctx context.Context, projectID, correlation string) error
	// StopByEstimate pauses the project by the budget when the estimate of its next wave exceeds the balance
	// (policy.project_budget stop_by_estimate, US-40): nothing is dispatched.
	StopByEstimate(ctx context.Context, projectID, estimate, correlation string) error
}

// Lease identifies the execution right of one worker over a wave.
type Lease struct {
	WaveID int64 // internal id, only between the executor and its store
	Owner  string
	Epoch  int64
}

// Claimed is a wave taken by this worker.
type Claimed struct {
	Lease
	PublicID    string
	Code        string
	ProjectID   string
	Stage       string
	Agent       string
	Purpose     string
	Attempt     int
	WasQueued   bool // queued → running (dispatch)
	Crashed     bool // running with an expired lease (resume_after_crash)
	CurrentStep int
	StageState  string
	LostCost    string // estimated cost of the call in flight when the worker fell (RNF-16)
	RedoneCall  bool
	ReasoningID int64
	ExecutionID int64
	// EstimateExceeded: the estimate of the queued wave exceeds the balance of the budget; the wave was not
	// dispatched (stays queued) and Estimate is the value that exceeded it (US-40, stop_by_estimate).
	EstimateExceeded bool
	Estimate         string
}

// Inputs are the wave inputs read from the project (pinned versions, RNF-15).
type Inputs struct {
	BusinessNeed       string
	Constraints        string
	Language           string
	InstructionVersion string
	InstructionHash    string
	Models             map[string]ModelRef
	Feedback           string            // reason of the last rejected gate round of the stage (rework)
	Task               string            // T of an implementation wave (PL-20)
	Approved           map[string]string // path → author of every approved artifact (US-30 CA-2)
	// Opinions are the opinions recorded by the participant waves of the stage (lead of the validation).
	Opinions []workflow.ReviewOpinion
	Skills   string // skills of the agent in the project, by description (US-43 RN-1)
	// SkillFiles are the files of those skills (skills/<name>/<file>), read on demand (US-43 RN-1).
	SkillFiles map[string]string
	// Finding is the ACH of a rework or revalidation wave.
	Finding *FindingBrief
}

// Totals are the accumulated tokens and costs of the wave (exact decimals, RNF-16).
type Totals struct {
	Usage      provider.Usage
	UsefulCost string
	LostCost   string
	Currency   string
}

// StepWrite is what one step persists. Without Checkpointed only the call row is finished
// (a failed call keeps the previous checkpoint).
type StepWrite struct {
	Checkpointed bool
	Checkpoint   Checkpoint
	StateHash    string
	Call         *CallRecord
	Tool         *ToolRecord
	DurationMs   int64
	Task         string       // T of the wave, for the events
	TaskFailed   *TaskFailure // failed build or test of the task (wave.task_failed)
}

// QuestionAnswer is an answered question waiting to be applied to the agent state.
type QuestionAnswer struct {
	Code    string
	Answers []string
}

// Ledger is the persistence of the executor (waves, checkpoints, calls, tools, questions).
// Every method that takes a lease is fenced: it fails with ErrLeaseLost when the lease changed.
type Ledger interface {
	// Claim takes the oldest runnable wave of the agents and its lease, in the caller's transaction.
	Claim(ctx context.Context, worker string, agents []string, lease time.Duration) (*Claimed, error)
	Renew(ctx context.Context, lease Lease, duration time.Duration) error
	// Held reports whether the lease is still valid (read only): false once the wave was canceled or
	// taken over, so the step in flight is aborted (RNF-10).
	Held(ctx context.Context, lease Lease) (bool, error)
	Inputs(ctx context.Context, claimed Claimed) (Inputs, error)
	LastCheckpoint(ctx context.Context, lease Lease, attempt int) (*Checkpoint, error)
	// LatestAnswer returns the last answered question of the wave, or nil.
	LatestAnswer(ctx context.Context, lease Lease) (*QuestionAnswer, error)
	SaveStep(ctx context.Context, lease Lease, attempt int, step StepWrite) (Totals, error)
	SetInputsDigest(ctx context.Context, lease Lease, digest string) error
	OpenQuestion(ctx context.Context, lease Lease, items []QuestionItem) (string, error)
	Complete(ctx context.Context, lease Lease) (Totals, int64, int64, error) // totals, execution ms, cycle ms
	Fail(ctx context.Context, lease Lease, cause string) (Totals, error)
	ScheduleRetry(ctx context.Context, lease Lease) (int, int, error) // retries used, max
	RecordWaveCheck(ctx context.Context, lease Lease, check string, passed bool, evidence json.RawMessage) error
	// RecordCommit stores the commit of the wave and of the artifact versions it wrote (US-42 CA-2).
	RecordCommit(ctx context.Context, lease Lease, sha string, versions []CommittedVersion) error
	// Release frees the lease without changing the state (worker shutting down).
	Release(ctx context.Context, lease Lease) error
	// Pause applies checkpoint_reached to a wave in pausing (asked by the user, US-12 CA-1, or by policy:
	// project paused, budget exhausted, a dependency of its stage reopened, US-34 CA-1): the wave goes to
	// paused and the lease is freed; it returns the pause cause, false when the wave is not pausing.
	Pause(ctx context.Context, lease Lease) (string, bool, error)
	SetStageState(ctx context.Context, projectID, stage, state string) error
}

// InstructionSource returns the instruction text of an agent and its hash.
type InstructionSource func(agent string) (text, hash string, err error)

// CommittedVersion is an artifact version written by a wave commit.
type CommittedVersion struct {
	Code    string
	Version int
}
