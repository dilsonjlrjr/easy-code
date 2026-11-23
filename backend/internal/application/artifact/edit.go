package artifact

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"easycode/internal/application/audit"
	"easycode/internal/application/command"
	"easycode/internal/application/event"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/transaction"
	domainartifact "easycode/internal/domain/artifact"
	domainaudit "easycode/internal/domain/audit"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Edit operations and the types a person edits (US-53, x-human-edit-types of the common header).
const (
	EditCreate = "create"
	EditModify = "modify"
	EditDelete = "delete"
)

// HumanEditTypes are the types with human edit (PD-03).
var HumanEditTypes = []string{"VISAO", "US", "RNF", "BACKLOG", "ADR"}

// MaxEditContent is the size limit of the edited file (openapi ArtifactEdit, 413 above).
const MaxEditContent = 204800

// CodeOutdatedVersion refuses an edit made over a version that is no longer the current one.
const CodeOutdatedVersion = "outdated-version"

// EditRequest is the body of editArtifact.
type EditRequest struct {
	Operation   string
	Type        string
	Artifact    string
	BaseVersion int
	Content     string
	Reason      string
}

// EditState is what the edit store knows of the artifact and its stage when the edit is asked.
type EditState struct {
	Exists         bool
	Type           string
	State          string // draft | in_review | approved | replaced
	CurrentVersion int
	StageState     string
	ActiveWaves    int
	// OpenRound reports a round of the stage gate still waiting (evaluating or awaiting_human): an approved round
	// whose effects the worker has not applied yet does not take edits.
	OpenRound bool
}

// EditView is an edit request and its outcome (getArtifactEdit).
type EditView struct {
	ID        string
	Command   string
	Operation string
	Type      string
	Artifact  string
	State     string // pending | reviewing | applied | refused
	Wave      string
	Version   int
	Refusal   string
}

// Edit outcomes.
const (
	EditPending   = "pending"
	EditReviewing = "reviewing"
	EditApplied   = "applied"
	EditRefused   = "refused"
)

// Edits stores the edit requests (api role).
type Edits interface {
	// State reads the artifact (when code is set) and the owner stage of the type.
	State(ctx context.Context, projectID, code, stage string) (EditState, error)
	// Request records the edit and its edit_artifact command; it returns the edit and command public ids.
	Request(ctx context.Context, principal identity.Principal, projectID string, edit EditRequest, key, bodyHash string) (string, string, error)
	Get(ctx context.Context, projectID, id string) (EditView, error)
}

// Editor is the api side of US-53.
type Editor struct {
	tx       transaction.Runner
	projects appproject.Repository
	edits    Edits
	trail    audit.Trail
}

// NewEditor builds the use cases.
func NewEditor(tx transaction.Runner, projects appproject.Repository, edits Edits, trail audit.Trail) *Editor {
	return &Editor{tx: tx, projects: projects, edits: edits, trail: trail}
}

// Request is editArtifact: shallow guard here (role, type, versions, states), the full guard again in the worker;
// the edit only counts after the edit_review wave of the stage agent (RN-1). Audited as artifact.edit (RN-4).
func (e *Editor) Request(ctx context.Context, principal identity.Principal, projectID string, edit EditRequest, key, bodyHash string) (string, string, error) {
	if _, err := appproject.Authorize(ctx, e.projects, principal, projectID, identity.RoleMember, identity.RoleApprover, identity.RoleLead); err != nil {
		return "", "", err
	}
	if err := validateEdit(&edit); err != nil {
		return "", "", err
	}
	stage := domainartifact.OwnerStage(edit.Type)
	state, err := e.edits.State(ctx, projectID, edit.Artifact, stage)
	if err != nil {
		return "", "", err
	}
	if err := EditGuard(edit, state); err != nil {
		return "", "", err
	}
	var editID, commandID string
	err = e.tx.InTx(ctx, func(ctx context.Context) error {
		if editID, commandID, err = e.edits.Request(ctx, principal, projectID, edit, key, bodyHash); err != nil {
			return err
		}
		target := "artifacts/" + edit.Artifact
		if edit.Operation == EditCreate {
			target = "artifacts/" + edit.Type
		}
		_, err := e.trail.Append(ctx, domainaudit.Record{
			ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject}, Origin: domainaudit.OriginWeb,
			Action: "artifact.edit", Target: target, Decision: edit.Operation, Justification: edit.Reason, CorrelationID: commandID,
		})
		return err
	})
	return editID, commandID, err
}

// Get is getArtifactEdit (any role in the project).
func (e *Editor) Get(ctx context.Context, principal identity.Principal, projectID, id string) (EditView, error) {
	if _, err := appproject.Authorize(ctx, e.projects, principal, projectID); err != nil {
		return EditView{}, err
	}
	return e.edits.Get(ctx, projectID, id)
}

func validateEdit(edit *EditRequest) error {
	edit.Reason = strings.TrimSpace(edit.Reason)
	switch {
	case edit.Operation != EditCreate && edit.Operation != EditModify && edit.Operation != EditDelete:
		return failure.Invalid(failure.FieldError{Field: "operation", Message: "enum"})
	case len([]rune(edit.Reason)) < 3 || len([]rune(edit.Reason)) > 2000:
		return failure.Invalid(failure.FieldError{Field: "reason", Message: "length"})
	case len(edit.Content) > MaxEditContent:
		return failure.New(failure.KindInvalidInput, "too-large-package", nil).With("limit_bytes", MaxEditContent)
	case edit.Operation != EditDelete && strings.TrimSpace(edit.Content) == "":
		return failure.Invalid(failure.FieldError{Field: "content", Message: "required"})
	}
	if edit.Operation == EditCreate {
		if edit.Artifact != "" || edit.BaseVersion != 0 {
			return failure.Invalid(failure.FieldError{Field: "artifact", Message: "not_allowed"})
		}
	} else {
		if edit.Artifact == "" || edit.BaseVersion < 1 {
			return failure.Invalid(failure.FieldError{Field: "base_version", Message: "required"})
		}
		edit.Type = domainartifact.TypeOf(edit.Artifact)
	}
	if !slices.Contains(HumanEditTypes, edit.Type) {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", "type_without_human_edit").With("available_actions", []string{})
	}
	return nil
}

// EditGuard is the guard of the transition edit (estados.yaml): artifact in draft, or in review with the stage
// awaiting its gate, and no active wave in the owner stage; an approved artifact only changes after a reopening
// (CA-6). The worker applies it again before queuing the review wave.
func EditGuard(edit EditRequest, state EditState) error {
	conflict := func(current string) error {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", current).With("available_actions", []string{})
	}
	if state.StageState == "" || state.StageState == "blocked" || state.StageState == "approved" {
		return conflict(state.StageState)
	}
	if state.ActiveWaves > 0 {
		return conflict("wave_active")
	}
	if edit.Operation == EditCreate {
		if state.StageState == "awaiting_gate" || state.StageState == "running" || state.StageState == "ready" || state.StageState == "returned" ||
			state.StageState == "reopened" {
			return nil
		}
		return conflict(state.StageState)
	}
	if !state.Exists {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	switch {
	case state.State == "draft":
	case state.State == "in_review" && state.StageState == "awaiting_gate" && state.OpenRound:
	default:
		return conflict(state.State)
	}
	if state.CurrentVersion != edit.BaseVersion {
		return failure.New(failure.KindConflict, CodeOutdatedVersion, nil).With("current_version", state.CurrentVersion)
	}
	return nil
}

// PendingEdit is an edit read by the worker when it applies the edit_artifact command.
type PendingEdit struct {
	ID          string
	ProjectID   string
	Operation   string
	Type        string
	Artifact    string
	BaseVersion int
	Editor      string // sub of the person
	Wave        string // set once the review wave is queued
}

// EditWork is what the worker reads and changes to apply an edit.
type EditWork interface {
	Pending(ctx context.Context, id string) (PendingEdit, error)
	State(ctx context.Context, projectID, code, stage string) (EditState, error)
	// InvalidateOpenRound ends the open round of the stage without verdict and returns its number (0 when none)
	// and the state it had.
	InvalidateOpenRound(ctx context.Context, projectID, stage string) (int, string, error)
	LinkWave(ctx context.Context, editID, waveCode string) error
}

// ReviewQueue queues the edit_review wave of the stage agent (the orchestrator) and returns its code.
type ReviewQueue interface {
	QueueWave(ctx context.Context, projectID, stageID, agent, purpose, finding, correlation string) (string, error)
}

// Emitter appends a domain event of the project.
type Emitter func(ctx context.Context, projectID, correlation, eventType, subject string, data map[string]any) error

// EditApplier is the worker handler of edit_artifact (estados.yaml, transition edit): it checks the full guard
// again, returns an artifact in review to draft and invalidates the open round of its gate, and queues the
// edit_review wave of the stage agent, which validates and formats the edit (CA-1). artifact.edited carries no
// text of the edit.
type EditApplier struct {
	work    EditWork
	queue   ReviewQueue
	emit    Emitter
	agentOf func(stage string) string
}

// NewEditApplier builds the handler; agentOf gives the agent of a stage in the workflow in force.
func NewEditApplier(work EditWork, queue ReviewQueue, emit Emitter, agentOf func(stage string) string) *EditApplier {
	return &EditApplier{work: work, queue: queue, emit: emit, agentOf: agentOf}
}

// Apply applies the edit of the command (payload {"edit": id}); correlation is the command id.
func (a *EditApplier) Apply(ctx context.Context, editID, correlation string) error {
	edit, err := a.work.Pending(ctx, editID)
	if err != nil {
		return err
	}
	if edit.Wave != "" {
		return nil
	}
	stage := domainartifact.OwnerStage(edit.Type)
	state, err := a.work.State(ctx, edit.ProjectID, edit.Artifact, stage)
	if err != nil {
		return err
	}
	request := EditRequest{Operation: edit.Operation, Type: edit.Type, Artifact: edit.Artifact, BaseVersion: edit.BaseVersion}
	if err := EditGuard(request, state); err != nil {
		return err
	}
	orchestrator := map[string]any{"type": "orchestrator", "id": "orchestrator"}
	round, from := 0, ""
	if state.StageState == "awaiting_gate" {
		if round, from, err = a.work.InvalidateOpenRound(ctx, edit.ProjectID, stage); err != nil {
			return err
		}
		if round > 0 {
			gate := "gate." + stage
			data := map[string]any{
				"gate": gate, "round": round, "stage": stage, "cause": "human_edit", "actor": orchestrator,
				"transition": map[string]any{"machine": "gate", "from_state": from, "to_state": "invalidated"},
			}
			if edit.Artifact != "" {
				data["artifact"] = edit.Artifact
			}
			if err := a.emit(ctx, edit.ProjectID, correlation, "br.easycode.gate.invalidated.v1", "gates/"+gate+"/rounds/"+itoa(round), data); err != nil {
				return err
			}
		}
	}
	// The artifact keeps its state until the review wave writes the validated version: a refused edit leaves
	// the previous version in force (CA-2), and the stage goes back to its gate after the wave either way.
	code, err := a.queue.QueueWave(ctx, edit.ProjectID, stage, a.agentOf(stage), "edit_review", "", correlation)
	if err != nil {
		return err
	}
	if err := a.work.LinkWave(ctx, edit.ID, code); err != nil {
		return err
	}
	if edit.Operation == EditCreate {
		return nil
	}
	data := map[string]any{
		"artifact": edit.Artifact, "type": edit.Type, "version": edit.BaseVersion + 1, "previous_version": edit.BaseVersion, "stage": stage,
		"actor": map[string]any{"type": "human", "id": edit.Editor}, "review_wave": code,
		"transition": map[string]any{"machine": "artifact", "from_state": state.State, "to_state": domainartifact.StateDraft},
	}
	if round > 0 {
		data["invalidated_round"] = round
	}
	return a.emit(ctx, edit.ProjectID, correlation, "br.easycode.artifact.edited.v1", "artifacts/"+edit.Artifact, data)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

// Handle is the command handler of edit_artifact (payload {"edit": id}).
func (a *EditApplier) Handle(ctx context.Context, cmd command.Command) error {
	var payload struct {
		Edit string `json:"edit"`
	}
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil || payload.Edit == "" {
		return failure.Invalid(failure.FieldError{Field: "payload", Message: "invalid"})
	}
	return a.Apply(ctx, payload.Edit, cmd.ID)
}

// OutboxEmitter appends the events of the edit to the outbox of the project.
func OutboxEmitter(outbox event.Outbox) Emitter {
	return func(ctx context.Context, projectID, correlation, eventType, subject string, data map[string]any) error {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = outbox.Append(ctx, domainevent.Draft{Type: eventType, Subject: subject, ProjectID: projectID, CorrelationID: correlation, Data: encoded})
		return err
	}
}
