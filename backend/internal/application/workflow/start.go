// Package workflow holds the workflow use cases: start (US-05) in the api, the start command and
// the stage and wave instantiation in the worker (orchestrator), and the workflow view (US-49 basics).
package workflow

import (
	"context"
	"encoding/json"
	"time"

	"easycode/internal/application/audit"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
	domainworkflow "easycode/internal/domain/workflow"
)

// What can be missing to start (Problem.missing), in the contract order.
const (
	MissingProviderKey  = "provider_key"
	MissingModelPair    = "model_pair"
	MissingCurrentPrice = "current_price"
	MissingBudget       = "budget"
	MissingBusinessNeed = "business_need"
)

// CommandView is a queued command (Command of the contract).
type CommandView struct {
	ID          string
	Type        string
	TargetType  string
	TargetID    string
	State       string
	RequestedBy appproject.UserRef
	RequestedAt time.Time
	CompletedAt *time.Time
	Refusal     string // problem code when refused
}

// Readiness checks what a project needs before it starts (US-05 CA-2, US-37 CA-2).
type Readiness interface {
	Missing(ctx context.Context, projectID string, at time.Time) ([]string, error)
}

// Commands queues and reads project commands.
type Commands interface {
	// Enqueue records a project command for the worker and wakes it, in the caller's transaction.
	Enqueue(ctx context.Context, principal identity.Principal, projectID, commandType, idempotencyKey, bodyHash string) (CommandView, error)
	Get(ctx context.Context, projectID, commandID string) (CommandView, error)
}

// Service is the api side of the workflow.
type Service struct {
	tx        transaction.Runner
	projects  appproject.Repository
	readiness Readiness
	commands  Commands
	trail     audit.Trail
	views     Views
	now       func() time.Time
}

// NewService builds the service.
func NewService(tx transaction.Runner, projects appproject.Repository, readiness Readiness, commands Commands, views Views, trail audit.Trail) *Service {
	return &Service{tx: tx, projects: projects, readiness: readiness, commands: commands, views: views, trail: trail, now: time.Now}
}

// Start is startWorkflow: guard, then the start command for the orchestrator (202). Call it in
// the idempotency transaction; key and hash are the request's (commands.idempotency_key, body_hash).
func (s *Service) Start(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (CommandView, error) {
	access, err := appproject.Authorize(ctx, s.projects, principal, projectID)
	if err != nil {
		return CommandView{}, err
	}
	if access.State != project.StateCreated {
		return CommandView{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).
			With("current_state", access.State).With("available_actions", []string{})
	}
	missing, err := s.readiness.Missing(ctx, projectID, s.now())
	if err != nil {
		return CommandView{}, err
	}
	if len(missing) > 0 {
		return CommandView{}, failure.New(failure.KindConflict, project.CodeIncompleteConfig, nil).With("missing", missing)
	}
	var queued CommandView
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if queued, err = s.commands.Enqueue(ctx, principal, projectID, "start", key, bodyHash); err != nil {
			return err
		}
		_, err = s.trail.Append(ctx, domainaudit.Record{
			ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
			Origin: domainaudit.OriginWeb, Action: "project.start", Target: "project", CorrelationID: queued.ID,
		})
		return err
	})
	return queued, err
}

// Command is getCommand.
func (s *Service) Command(ctx context.Context, principal identity.Principal, projectID, commandID string) (CommandView, error) {
	if _, err := appproject.Authorize(ctx, s.projects, principal, projectID); err != nil {
		return CommandView{}, err
	}
	return s.commands.Get(ctx, projectID, commandID)
}

// Workflow is getWorkflow.
func (s *Service) Workflow(ctx context.Context, principal identity.Principal, projectID string) (View, error) {
	if _, err := appproject.Authorize(ctx, s.projects, principal, projectID); err != nil {
		return View{}, err
	}
	return s.views.Workflow(ctx, projectID)
}

// WorkflowAsAdministrator is getWorkflow for the organization administrator without a role, after
// the member service authorized and recorded the access (US-02 CA-6): stage states only.
func (s *Service) WorkflowAsAdministrator(ctx context.Context, projectID string) (View, error) {
	return s.views.Workflow(ctx, projectID)
}

// ---------------------------------------------------------------- worker side

// ResolvedPair is the model pair a wave starts with (US-16 CA-1, CA-3, CA-4).
type ResolvedPair struct {
	Reasoning, Execution ModelID
	FromStage            bool
}

// ModelID identifies a model row.
type ModelID struct {
	ID       int64
	Provider string
	Name     string
}

// NewWave is a wave the orchestrator queues.
type NewWave struct {
	ProjectID     string
	Stage         string
	Agent         string
	Pair          ResolvedPair
	Configuration AgentConfiguration
}

// AgentConfiguration is the default configuration of the stage agent (agent_configurations v1).
type AgentConfiguration struct {
	Agent              string
	InstructionVersion string
	InstructionHash    string
	Tools              json.RawMessage
	Limits             json.RawMessage
}

// QueuedWave is a created wave.
type QueuedWave struct {
	Code string
	ID   string // public id
}

// Orchestration is what the orchestrator writes when the workflow starts.
type Orchestration interface {
	// LockProject locks the project row and returns its state.
	LockProject(ctx context.Context, projectID string) (string, error)
	// StartProject pins the workflow copy, moves the project to in_progress and inserts the
	// stages, all blocked (machine stage starts in blocked).
	StartProject(ctx context.Context, projectID string, definition domainworkflow.Definition, definitionJSON []byte, hash string) error
	// SetStageState applies a stage transition.
	SetStageState(ctx context.Context, projectID, stage, state string) error
	// ResolvePair returns the stage pair, else the project pair.
	ResolvePair(ctx context.Context, projectID, stage string) (ResolvedPair, error)
	// QueueWave creates the next wave of the stage in queued with its agent configuration and models.
	QueueWave(ctx context.Context, wave NewWave) (QueuedWave, error)
	// StageStates returns the state of each stage of the project.
	StageStates(ctx context.Context, projectID string) (map[string]string, error)
}

// AgentDefaults gives the default configuration of an agent.
type AgentDefaults func(agent string) (AgentConfiguration, error)
