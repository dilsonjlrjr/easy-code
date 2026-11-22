// Package wave holds the api use cases of waves: reads (US-09, US-36, US-11 CA-3), the answer
// to an agent question (US-10) and the cancellation (US-14), plus the worker handlers that resume
// and cancel the wave.
package wave

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"easycode/internal/application/audit"
	"easycode/internal/application/command"
	"easycode/internal/application/event"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/transaction"
	appworkflow "easycode/internal/application/workflow"
	domainaudit "easycode/internal/domain/audit"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/provider"
	domainskill "easycode/internal/domain/skill"
)

// Cost is the exact cost of a wave or checkpoint.
type Cost struct {
	Useful, Lost, Currency string
	EstimatedLost          bool
}

// Summary is a wave in a list (WaveSummary).
type Summary struct {
	Code          string
	Stage         string
	Agent         string
	State         string
	Purpose       string
	Attempt       int
	ConfigVersion int
	CreatedAt     time.Time
	StartedAt     *time.Time
	EndedAt       *time.Time
	Cost          Cost
	Task          string // T of an implementation wave (PL-20)
}

// View is getWave.
type View struct {
	Summary
	Reasoning, Execution appproject.ModelRef
	InstructionVersion   string
	InputsDigest         string
	PriceTable           string
	Tokens               provider.Usage
	ExecutionMs, CycleMs int64
	FailureCause         string
	ArtifactErrors       json.RawMessage
	LastCheckpoint       *int
	QuestionOpen         string
	Outputs              []ExpectedOutput
	GitCommit            string               // commit of the project repository written when the wave completed (D-17)
	Skills               []domainskill.Pinned // skill versions the wave loaded (US-43 CA-2)
	PerProvider          []ProviderCost       // cost of the calls by provider (US-24 CA-3)
}

// ProviderCost is the total cost of the calls of a wave to one provider.
type ProviderCost struct {
	Provider, Total string
}

// SandboxExecution is a command run in the sandbox with its evidence (US-41).
type SandboxExecution struct {
	ID           string
	Command      string
	Category     string
	Profile      string
	Start        time.Time
	DurationMs   int64
	ExitCode     *int
	EndReason    string
	TestsTotal   *int
	TestsPassed  *int
	TestsFailed  *int
	TestsSkipped *int
	OutputStart  string
	OutputBytes  int64
	OutputSHA256 string
	ImageDigest  string
}

// ExecutionOutput is the full output of a command (gzip as stored).
type ExecutionOutput struct {
	Compressed []byte
	FileName   string
}

// ExpectedOutput is an output of the stage and its situation in the wave (PL-22).
type ExpectedOutput struct {
	Type     string
	Artifact string
	Version  int
	Status   string
}

// QuestionView is a question of the wave.
type QuestionView struct {
	Code       string
	OpenedAt   time.Time
	Items      []QuestionItem
	AnsweredBy *appproject.UserRef
	AnsweredAt *time.Time
}

// QuestionItem is one numbered item.
type QuestionItem struct {
	Number     int
	Text       string
	Suggestion string
	Source     string
	Excerpt    string
	Answer     string
}

// CheckpointView is a checkpoint (Checkpoint).
type CheckpointView struct {
	Number    int
	Attempt   int
	After     string
	WrittenAt time.Time
	Tokens    provider.Usage
	Cost      Cost
}

// CallView is a model call or tool execution (CallRecord).
type CallView struct {
	Number       int
	Type         string
	Start        time.Time
	DurationMs   int64
	Result       string
	Lost         bool
	Estimated    bool
	Attempt      int
	Provider     string
	Model        string
	Role         string
	Tokens       provider.Usage
	Cost         string
	Currency     string
	PriceVersion string
	Tool         string
	Summary      string
	Refusal      string
	Error        string
}

// Reader reads waves (api role).
type Reader interface {
	List(ctx context.Context, projectID string, stage, agent string, states []string, limit int) ([]Summary, error)
	Get(ctx context.Context, projectID, code string) (View, error)
	Questions(ctx context.Context, projectID, code string) ([]QuestionView, error)
	Checkpoints(ctx context.Context, projectID, code string) ([]CheckpointView, error)
	Calls(ctx context.Context, projectID, code, kind string) ([]CallView, error)
	SandboxExecutions(ctx context.Context, projectID, code string, limit int, after string) ([]SandboxExecution, error)
	ExecutionOutput(ctx context.Context, projectID, code, executionID string) (ExecutionOutput, error)
}

// Answers stores the answers of a question (api: question_items.answer, wave_questions.answered_*).
type Answers interface {
	Record(ctx context.Context, principal identity.Principal, projectID, waveCode, questionCode string, answers []string) error
	// EnqueueAnswer queues the answer command of the wave for the worker.
	EnqueueAnswer(ctx context.Context, principal identity.Principal, projectID, waveCode, questionCode, key, bodyHash string) (appworkflow.CommandView, error)
	// EnqueueCancel queues the cancel command of the wave for the worker (US-14).
	EnqueueCancel(ctx context.Context, principal identity.Principal, projectID, waveCode, key, bodyHash string) (appworkflow.CommandView, error)
}

// Cancelable are the states the cancel event leaves (estados.yaml wave: cancel; US-14 CA-1, CA-2, CA-4).
var Cancelable = []string{"queued", "running", "awaiting_user", "pausing", "paused", "failed"}

// CanCancel reports whether a wave in the state can be canceled (US-14 CA-3).
func CanCancel(state string) bool {
	for _, s := range Cancelable {
		if s == state {
			return true
		}
	}
	return false
}

// Service is the api side of waves.
type Service struct {
	tx       transaction.Runner
	projects appproject.Repository
	reader   Reader
	answers  Answers
	trail    audit.Trail
}

// NewService builds the service.
func NewService(tx transaction.Runner, projects appproject.Repository, reader Reader, answers Answers, trail audit.Trail) *Service {
	return &Service{tx: tx, projects: projects, reader: reader, answers: answers, trail: trail}
}

func (s *Service) access(ctx context.Context, principal identity.Principal, projectID string) error {
	_, err := appproject.Authorize(ctx, s.projects, principal, projectID)
	return err
}

// List is listWaves (created_at descending).
func (s *Service) List(ctx context.Context, principal identity.Principal, projectID, stage, agent string, states []string, limit int) ([]Summary, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return nil, err
	}
	return s.reader.List(ctx, projectID, stage, agent, states, limit)
}

// Get is getWave (US-09 CA-1, US-36 CA-1).
func (s *Service) Get(ctx context.Context, principal identity.Principal, projectID, code string) (View, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return View{}, err
	}
	return s.reader.Get(ctx, projectID, code)
}

// Questions is listWaveQuestions.
func (s *Service) Questions(ctx context.Context, principal identity.Principal, projectID, code string) ([]QuestionView, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return nil, err
	}
	return s.reader.Questions(ctx, projectID, code)
}

// Checkpoints is listCheckpoints (US-11 CA-3).
func (s *Service) Checkpoints(ctx context.Context, principal identity.Principal, projectID, code string) ([]CheckpointView, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return nil, err
	}
	return s.reader.Checkpoints(ctx, projectID, code)
}

// Calls is listWaveCalls (US-09 RN-1: metadata and short summary only; US-36).
func (s *Service) Calls(ctx context.Context, principal identity.Principal, projectID, code, kind string) ([]CallView, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return nil, err
	}
	if kind != "" && kind != "model" && kind != "tool" {
		return nil, failure.Invalid(failure.FieldError{Field: "type", Message: "enum"})
	}
	return s.reader.Calls(ctx, projectID, code, kind)
}

// SandboxExecutions is listSandboxExecutions (US-41 CA-1, CA-2, CA-3): oldest first, after the cursor.
func (s *Service) SandboxExecutions(ctx context.Context, principal identity.Principal, projectID, code string, limit int, after string) ([]SandboxExecution, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return nil, err
	}
	return s.reader.SandboxExecutions(ctx, projectID, code, limit, after)
}

// ExecutionOutput is downloadExecutionOutput (US-41 CA-1).
func (s *Service) ExecutionOutput(ctx context.Context, principal identity.Principal, projectID, code, executionID string) (ExecutionOutput, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return ExecutionOutput{}, err
	}
	return s.reader.ExecutionOutput(ctx, projectID, code, executionID)
}

// Answer is answerQuestion (US-10 CA-1, RN-1): every item answered, recorded with who, when and
// from where; the worker resumes the wave from the point where it stopped. Call it inside the
// idempotency transaction.
func (s *Service) Answer(ctx context.Context, principal identity.Principal, projectID, waveCode, questionCode string, answers map[int]string, key, bodyHash string) (appworkflow.CommandView, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return appworkflow.CommandView{}, err
	}
	current, err := s.reader.Get(ctx, projectID, waveCode)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	if current.State != "awaiting_user" || current.QuestionOpen != questionCode {
		return appworkflow.CommandView{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).
			With("current_state", current.State).With("available_actions", []string{})
	}
	questions, err := s.reader.Questions(ctx, projectID, waveCode)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	var items int
	for _, q := range questions {
		if q.Code == questionCode {
			items = len(q.Items)
		}
	}
	ordered := make([]string, 0, items)
	for number := 1; number <= items; number++ {
		text := strings.TrimSpace(answers[number])
		if text == "" || len([]rune(text)) > 10000 {
			return appworkflow.CommandView{}, failure.Invalid(failure.FieldError{Field: "answers", Message: "required"})
		}
		ordered = append(ordered, text)
	}
	if len(answers) != items {
		return appworkflow.CommandView{}, failure.Invalid(failure.FieldError{Field: "answers", Message: "invalid"})
	}
	var queued appworkflow.CommandView
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.answers.Record(ctx, principal, projectID, waveCode, questionCode, ordered); err != nil {
			return err
		}
		if queued, err = s.answers.EnqueueAnswer(ctx, principal, projectID, waveCode, questionCode, key, bodyHash); err != nil {
			return err
		}
		_, err = s.trail.Append(ctx, domainaudit.Record{
			ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject}, Origin: domainaudit.OriginWeb,
			Action: "question.answer", Target: "waves/" + waveCode, Decision: questionCode, CorrelationID: queued.ID,
		})
		return err
	})
	return queued, err
}

// Cancel is cancelWave (US-14): with the confirmation, a wave that is not completed or canceled gets
// the cancel command; the worker applies it, aborts the call in flight and records the lost cost.
// The guard here is shallow (state read now); the worker rechecks it under lock. Call it inside the
// idempotency transaction.
func (s *Service) Cancel(ctx context.Context, principal identity.Principal, projectID, waveCode string, confirm bool, key, bodyHash string) (appworkflow.CommandView, error) {
	if err := s.access(ctx, principal, projectID); err != nil {
		return appworkflow.CommandView{}, err
	}
	if !confirm {
		return appworkflow.CommandView{}, failure.Invalid(failure.FieldError{Field: "confirm", Message: "required"})
	}
	current, err := s.reader.Get(ctx, projectID, waveCode)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	if !CanCancel(current.State) {
		return appworkflow.CommandView{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).
			With("current_state", current.State).With("available_actions", []string{})
	}
	var queued appworkflow.CommandView
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if queued, err = s.answers.EnqueueCancel(ctx, principal, projectID, waveCode, key, bodyHash); err != nil {
			return err
		}
		_, err = s.trail.Append(ctx, domainaudit.Record{
			ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject}, Origin: domainaudit.OriginWeb,
			Action: "wave.cancel", Target: "waves/" + waveCode, CorrelationID: queued.ID,
		})
		return err
	})
	return queued, err
}

// Canceled is a wave the worker canceled.
type Canceled struct {
	Ref
	From     string
	Task     string
	LostCost string // cost of the call in flight, estimated (RNF-16); "" when none
	Currency string
}

// Canceler is the worker store of the cancel command.
type Canceler interface {
	// Cancel locks the wave, rechecks the guard, marks the call in flight lost (estimated cost), moves
	// the wave to canceled and frees its lease, so the worker that runs it stops at once (fencing).
	Cancel(ctx context.Context, projectID, waveCode string) (Canceled, error)
}

// CancelHandler applies the cancel command in the worker (estados.yaml wave: cancel).
type CancelHandler struct {
	store  Canceler
	outbox event.Outbox
}

// NewCancelHandler builds the handler.
func NewCancelHandler(store Canceler, outbox event.Outbox) *CancelHandler {
	return &CancelHandler{store: store, outbox: outbox}
}

// Handle implements command.Handler.
func (h *CancelHandler) Handle(ctx context.Context, cmd command.Command) error {
	var payload struct {
		Wave string `json:"wave"`
	}
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil || payload.Wave == "" {
		return failure.Invalid(failure.FieldError{Field: "payload", Message: "invalid"})
	}
	canceled, err := h.store.Cancel(ctx, cmd.ProjectID, payload.Wave)
	if err != nil {
		return err
	}
	data := map[string]any{
		"wave": canceled.Code, "stage": canceled.Stage, "agent": canceled.Agent, "purpose": canceled.Purpose, "cause": "user_request",
		"actor":      map[string]any{"type": "human", "id": cmd.RequestedBy, "origin": "web"},
		"transition": map[string]any{"machine": "wave", "from_state": canceled.From, "to_state": "canceled"},
	}
	if canceled.Task != "" {
		data["task"] = canceled.Task
	}
	if canceled.LostCost != "" {
		data["lost_cost"] = map[string]any{"value": canceled.LostCost, "currency": canceled.Currency}
	}
	encoded, _ := json.Marshal(data)
	_, err = h.outbox.Append(ctx, domainevent.Draft{Type: "br.easycode.wave.canceled.v1", Subject: "waves/" + canceled.Code,
		ProjectID: cmd.ProjectID, WaveID: canceled.Code, CorrelationID: cmd.ID, Data: encoded})
	return err
}

// Resumer is the worker store of the answer command.
type Resumer interface {
	// Resume moves the wave from awaiting_user to running (without lease, so the executor claims it).
	Resume(ctx context.Context, projectID, waveCode string) (Ref, error)
}

// Ref identifies a wave for its events.
type Ref struct {
	Code, Stage, Agent, Purpose string
}

// AnswerHandler applies the answer command in the worker (estados.yaml wave: answer).
type AnswerHandler struct {
	store  Resumer
	outbox event.Outbox
}

// NewAnswerHandler builds the handler.
func NewAnswerHandler(store Resumer, outbox event.Outbox) *AnswerHandler {
	return &AnswerHandler{store: store, outbox: outbox}
}

// Handle implements command.Handler.
func (h *AnswerHandler) Handle(ctx context.Context, cmd command.Command) error {
	var payload struct {
		Wave     string `json:"wave"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return failure.Invalid(failure.FieldError{Field: "payload", Message: "invalid"})
	}
	ref, err := h.store.Resume(ctx, cmd.ProjectID, payload.Wave)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{
		"wave": ref.Code, "stage": ref.Stage, "agent": ref.Agent, "purpose": ref.Purpose, "question": payload.Question,
		"actor":      map[string]any{"type": "human", "id": cmd.RequestedBy, "origin": "web"},
		"transition": map[string]any{"machine": "wave", "from_state": "awaiting_user", "to_state": "running"},
	})
	_, err = h.outbox.Append(ctx, domainevent.Draft{Type: "br.easycode.wave.question_answered.v1", Subject: "waves/" + ref.Code,
		ProjectID: cmd.ProjectID, WaveID: ref.Code, CorrelationID: cmd.ID, Data: data})
	return err
}

// Authorize checks a role in the project (the SSE stream and the event history, RNF-04).
func (s *Service) Authorize(ctx context.Context, principal identity.Principal, projectID string) error {
	return s.access(ctx, principal, projectID)
}
