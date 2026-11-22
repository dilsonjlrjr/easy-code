// Package wave holds the api use cases of waves: reads (US-09, US-36, US-11 CA-3) and the answer
// to an agent question (US-10), plus the worker handler that resumes the wave.
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
}

// Answers stores the answers of a question (api: question_items.answer, wave_questions.answered_*).
type Answers interface {
	Record(ctx context.Context, principal identity.Principal, projectID, waveCode, questionCode string, answers []string) error
	// EnqueueAnswer queues the answer command of the wave for the worker.
	EnqueueAnswer(ctx context.Context, principal identity.Principal, projectID, waveCode, questionCode, key, bodyHash string) (appworkflow.CommandView, error)
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
