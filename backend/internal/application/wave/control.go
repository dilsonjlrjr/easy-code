package wave

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"easycode/internal/application/command"
	"easycode/internal/application/event"
	appproject "easycode/internal/application/project"
	appworkflow "easycode/internal/application/workflow"
	domainaudit "easycode/internal/domain/audit"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
)

// Human control triggers of a wave (estados.yaml wave; commands.type with target wave).
const (
	ActionPause    = "pause"
	ActionPauseNow = "pause_now"
	ActionResume   = "resume"
	ActionRestart  = "restart"
	ActionCancel   = "cancel"
	ActionAnswer   = "answer"
)

// Pause modes (PauseRequest.mode, waves.pause_mode, wave.paused.mode).
const (
	ModeCooperative = "cooperative"
	ModeImmediate   = "immediate"
)

// Causes of a pause (waves.pause_cause, wave.pause_requested.cause, wave.paused.cause).
const (
	CauseUserRequest        = "user_request"
	CauseProjectPaused      = "project_paused"
	CauseBudgetExhausted    = "budget_exhausted"
	CauseDependencyReopened = "dependency_reopened"
)

// Failure codes of the control actions (openapi Problem).
const (
	CodeConfirmationRequired = "confirmation-required"
	CodeBudgetExhausted      = "budget-exhausted"
)

// Actions are the human triggers of a wave in a state (available_actions; US-12, US-13 CA-2, US-14 CA-3).
// A project that is completed or canceled is read-only: nothing is offered (US-52 CA-4, US-51 CA-5).
func Actions(state string, projectState string) []string {
	if projectState == project.StateCompleted || projectState == project.StateCanceled {
		return []string{}
	}
	switch state {
	case "running":
		return []string{ActionPause, ActionPauseNow, ActionCancel}
	case "pausing":
		return []string{ActionPauseNow, ActionCancel}
	case "paused":
		return []string{ActionResume, ActionCancel}
	case "failed":
		return []string{ActionRestart, ActionCancel}
	case "awaiting_user":
		return []string{ActionAnswer, ActionCancel}
	case "queued":
		return []string{ActionCancel}
	}
	return []string{}
}

func stateConflict(state string, actions []string) error {
	return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", state).With("available_actions", actions)
}

// readOnly refuses any control of a wave of a project that ended (US-52 CA-4: "projeto concluído, somente leitura").
func readOnly(access appproject.Access) error {
	if access.State == project.StateCompleted || access.State == project.StateCanceled {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", access.State).With("available_actions", []string{})
	}
	return nil
}

// Controls queues the control commands of a wave (api role).
type Controls interface {
	// EnqueueControl queues a pause, pause_now, resume or restart command of the wave for the worker.
	EnqueueControl(ctx context.Context, principal identity.Principal, projectID, waveCode, commandType string, payload map[string]any, key, bodyHash string) (appworkflow.CommandView, error)
	// Balance reports whether the project still has budget (spend below the stop limit).
	Balance(ctx context.Context, projectID string) (bool, error)
}

// WithControls enables pause, resume and restart (US-12, US-13).
func (s *Service) WithControls(controls Controls) *Service {
	s.controls = controls
	return s
}

func (s *Service) control(ctx context.Context, principal identity.Principal, projectID, waveCode, commandType, action string,
	payload map[string]any, key, bodyHash string) (appworkflow.CommandView, error) {
	var queued appworkflow.CommandView
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if queued, err = s.controls.EnqueueControl(ctx, principal, projectID, waveCode, commandType, payload, key, bodyHash); err != nil {
			return err
		}
		_, err = s.trail.Append(ctx, domainaudit.Record{
			ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject}, Origin: domainaudit.OriginWeb,
			Action: action, Target: "waves/" + waveCode, CorrelationID: queued.ID,
		})
		return err
	})
	return queued, err
}

// Pause is pauseWave (US-12 CA-1, CA-2): cooperative from running (pausing until the next checkpoint,
// nothing lost) or immediate from running or pausing with the explicit confirmation that the tokens of
// the call in flight are lost and charged. The worker rechecks the guard under lock. Call it inside the
// idempotency transaction.
func (s *Service) Pause(ctx context.Context, principal identity.Principal, projectID, waveCode, mode string, confirmLost bool, key, bodyHash string) (appworkflow.CommandView, error) {
	access, err := appproject.Authorize(ctx, s.projects, principal, projectID)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	if mode != ModeCooperative && mode != ModeImmediate {
		return appworkflow.CommandView{}, failure.Invalid(failure.FieldError{Field: "mode", Message: "enum"})
	}
	if mode == ModeImmediate && !confirmLost {
		return appworkflow.CommandView{}, failure.New(failure.KindInvalidInput, CodeConfirmationRequired, nil).WithField("confirm_lost_cost", "must_be_true")
	}
	if err := readOnly(access); err != nil {
		return appworkflow.CommandView{}, err
	}
	current, err := s.reader.Get(ctx, projectID, waveCode)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	trigger, action := ActionPause, "wave.pause"
	if mode == ModeImmediate {
		trigger, action = ActionPauseNow, "wave.pause_now"
	}
	if !slices.Contains(Actions(current.State, access.State), trigger) {
		return appworkflow.CommandView{}, stateConflict(current.State, Actions(current.State, access.State))
	}
	return s.control(ctx, principal, projectID, waveCode, trigger, action, map[string]any{"wave": waveCode, "mode": mode}, key, bodyHash)
}

// Resume is resumeWave (US-12 CA-3): paused → running from the last checkpoint, with the project in
// progress, budget left and the stage not blocked.
func (s *Service) Resume(ctx context.Context, principal identity.Principal, projectID, waveCode, key, bodyHash string) (appworkflow.CommandView, error) {
	access, err := appproject.Authorize(ctx, s.projects, principal, projectID)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	if err := readOnly(access); err != nil {
		return appworkflow.CommandView{}, err
	}
	current, err := s.reader.Get(ctx, projectID, waveCode)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	if current.State != "paused" {
		return appworkflow.CommandView{}, stateConflict(current.State, Actions(current.State, access.State))
	}
	if access.State != project.StateInProgress {
		if balance, err := s.controls.Balance(ctx, projectID); err == nil && !balance {
			return appworkflow.CommandView{}, failure.New(failure.KindConflict, CodeBudgetExhausted, nil)
		}
		return appworkflow.CommandView{}, stateConflict(current.State, []string{ActionCancel})
	}
	if balance, err := s.controls.Balance(ctx, projectID); err != nil || !balance {
		if err != nil {
			return appworkflow.CommandView{}, err
		}
		return appworkflow.CommandView{}, failure.New(failure.KindConflict, CodeBudgetExhausted, nil)
	}
	return s.control(ctx, principal, projectID, waveCode, ActionResume, "wave.resume", map[string]any{"wave": waveCode}, key, bodyHash)
}

// Restart is restartWave (US-13 CA-1, CA-2): only a failed wave goes back to the queue, with the
// confirmation; the previous attempt stays readable.
func (s *Service) Restart(ctx context.Context, principal identity.Principal, projectID, waveCode string, confirm bool, key, bodyHash string) (appworkflow.CommandView, error) {
	access, err := appproject.Authorize(ctx, s.projects, principal, projectID)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	if !confirm {
		return appworkflow.CommandView{}, failure.New(failure.KindInvalidInput, CodeConfirmationRequired, nil).WithField("confirm", "must_be_true")
	}
	if err := readOnly(access); err != nil {
		return appworkflow.CommandView{}, err
	}
	current, err := s.reader.Get(ctx, projectID, waveCode)
	if err != nil {
		return appworkflow.CommandView{}, err
	}
	if current.State != "failed" {
		return appworkflow.CommandView{}, stateConflict(current.State, Actions(current.State, access.State))
	}
	return s.control(ctx, principal, projectID, waveCode, ActionRestart, "wave.restart", map[string]any{"wave": waveCode}, key, bodyHash)
}

// ---------------------------------------------------------------- worker side

// Controlled is a wave changed by a control command.
type Controlled struct {
	Ref
	From     string
	Task     string
	Finding  string
	Attempt  int
	LostCost string // cost of the call in flight aborted by pause_now; "" when none
	Currency string
}

// ControlStore applies the control commands under the wave lock (worker role). Each method rechecks the
// guard and refuses with state-conflict (or budget-exhausted) when the transition is not available.
type ControlStore interface {
	// RequestPause moves a running wave to pausing with the cause (pause, pause_by_policy).
	RequestPause(ctx context.Context, projectID, waveCode, cause string) (Controlled, error)
	// PauseNow aborts the call in flight (lost cost, estimated) and moves running or pausing to paused,
	// freeing the lease so the worker that runs it stops at once (pause_now, RNF-10).
	PauseNow(ctx context.Context, projectID, waveCode string) (Controlled, error)
	// ResumeFromPause moves a paused wave to running without lease (the executor claims it from the last
	// checkpoint), when the project is in progress, has budget and the stage is not blocked.
	ResumeFromPause(ctx context.Context, projectID, waveCode string) (Controlled, error)
	// Restart moves a failed wave to queued with a new attempt and the retry counter at zero.
	Restart(ctx context.Context, projectID, waveCode string) (Controlled, error)
}

// ControlHandler applies pause, pause_now, resume and restart in the worker (estados.yaml wave).
type ControlHandler struct {
	store  ControlStore
	outbox event.Outbox
}

// NewControlHandler builds the handler.
func NewControlHandler(store ControlStore, outbox event.Outbox) *ControlHandler {
	return &ControlHandler{store: store, outbox: outbox}
}

func wavePayload(cmd command.Command) (string, error) {
	var payload struct {
		Wave string `json:"wave"`
	}
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil || payload.Wave == "" {
		return "", failure.Invalid(failure.FieldError{Field: "payload", Message: "invalid"})
	}
	return payload.Wave, nil
}

func (h *ControlHandler) emit(ctx context.Context, cmd command.Command, eventType string, w Controlled, to string, extra map[string]any) error {
	data := map[string]any{
		"wave": w.Code, "stage": w.Stage, "agent": w.Agent, "purpose": w.Purpose,
		"actor":      map[string]any{"type": "human", "id": cmd.RequestedBy, "origin": "web"},
		"transition": map[string]any{"machine": "wave", "from_state": w.From, "to_state": to},
	}
	if w.Task != "" {
		data["task"] = w.Task
	}
	if w.Finding != "" {
		data["finding"] = w.Finding
	}
	for k, v := range extra {
		data[k] = v
	}
	encoded, _ := json.Marshal(data)
	_, err := h.outbox.Append(ctx, domainevent.Draft{Type: eventType, Subject: "waves/" + w.Code, ProjectID: cmd.ProjectID,
		WaveID: w.Code, CorrelationID: cmd.ID, Data: encoded})
	return err
}

// Pause handles "wave.pause" (cooperative: running → pausing; the executor stops at the next checkpoint).
func (h *ControlHandler) Pause(ctx context.Context, cmd command.Command) error {
	code, err := wavePayload(cmd)
	if err != nil {
		return err
	}
	w, err := h.store.RequestPause(ctx, cmd.ProjectID, code, CauseUserRequest)
	if err != nil {
		return err
	}
	return h.emit(ctx, cmd, "br.easycode.wave.pause_requested.v1", w, "pausing", map[string]any{"cause": CauseUserRequest})
}

// PauseNow handles "wave.pause_now" (running or pausing → paused, the call in flight is lost and charged).
func (h *ControlHandler) PauseNow(ctx context.Context, cmd command.Command) error {
	code, err := wavePayload(cmd)
	if err != nil {
		return err
	}
	w, err := h.store.PauseNow(ctx, cmd.ProjectID, code)
	if err != nil {
		return err
	}
	extra := map[string]any{"mode": ModeImmediate, "cause": CauseUserRequest}
	if w.LostCost != "" {
		extra["lost_cost"] = map[string]any{"value": w.LostCost, "currency": w.Currency}
	}
	return h.emit(ctx, cmd, "br.easycode.wave.paused.v1", w, "paused", extra)
}

// Resume handles "wave.resume" (paused → running from the last checkpoint).
func (h *ControlHandler) Resume(ctx context.Context, cmd command.Command) error {
	code, err := wavePayload(cmd)
	if err != nil {
		return err
	}
	w, err := h.store.ResumeFromPause(ctx, cmd.ProjectID, code)
	if err != nil {
		return err
	}
	return h.emit(ctx, cmd, "br.easycode.wave.resumed.v1", w, "running", map[string]any{"cause": CauseUserRequest})
}

// Restart handles "wave.restart" (failed → queued, new attempt; US-13 CA-1).
func (h *ControlHandler) Restart(ctx context.Context, cmd command.Command) error {
	code, err := wavePayload(cmd)
	if err != nil {
		return err
	}
	w, err := h.store.Restart(ctx, cmd.ProjectID, code)
	if err != nil {
		return err
	}
	return h.emit(ctx, cmd, "br.easycode.wave.restarted.v1", w, "queued", map[string]any{"attempt": w.Attempt})
}

// PausingLimit is how long a wave may stay in pausing before the alert suggests pausing now (US-12 CA-4,
// PL-48: 10 min by default; limit_pausing of the deployment).
var PausingLimit = 10 * time.Minute
