package workflow

import (
	"context"
	"strings"

	"easycode/internal/application/command"
	appproject "easycode/internal/application/project"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
)

// Failure codes of the project lifecycle (openapi Problem).
const (
	CodeConfirmationRequired = "confirmation-required"
	CodeBudgetExhausted      = "budget-exhausted"
	CodeBlockingFinding      = "blocking-finding"
)

// Pause reasons of a project (projects.pause_reason, project.paused.pause_reason).
const (
	PauseManual = "manual"
	PauseBudget = "budget"
)

// Pause causes of the waves (waves.pause_cause, wave.pause_requested.cause).
const (
	WaveCauseProjectPaused   = "project_paused"
	WaveCauseBudgetExhausted = "budget_exhausted"
)

// Status is what the guards of the project lifecycle read (US-51, US-52, US-39).
type Status struct {
	Name        string
	State       string
	PauseReason string
	Balance     bool   // spend below the stop limit
	Spend       string // accumulated cost (useful and lost)
	StopLimit   string // "" when none
	Currency    string
	// ReleaseApproved: stage release approved by its last gate.release round (US-52 CA-1, CA-2).
	ReleaseApproved bool
	ActiveWaves     []string // waves in queued, running, awaiting_user, pausing or paused (US-52 CA-3)
	// BlockingFindings are the critical or high findings still open (accepted ones do not block).
	BlockingFindings []string
}

// ControlledWave is a wave changed by the project lifecycle (events of the waves).
type ControlledWave struct {
	Code, Stage, Agent, Purpose, Task, Finding, From string
	LostCost                                         string // canceled: estimated cost of the call in flight
	// Paused is true when the wave reached paused at once (nobody executed it: no step to finish).
	Paused bool
}

// Lifecycle is the persistence of the project transitions pause, resume, cancel, record_publication and
// of the budget (worker role, under the project lock taken by LockProject).
type Lifecycle interface {
	Status(ctx context.Context, projectID string) (Status, error)
	// PauseProject moves in_progress → paused with the reason and its running waves to pausing
	// (pause_by_policy with the cause); queued waves stay retained by the dispatch guard.
	PauseProject(ctx context.Context, projectID, reason, waveCause string) ([]ControlledWave, error)
	// ResumeProject moves paused → in_progress and the waves paused by the project or by the budget back to
	// running (resume_by_policy); it returns them.
	ResumeProject(ctx context.Context, projectID string) ([]ControlledWave, error)
	// ResumeHeld resumes the waves paused by the project or the budget of projects in progress again (a wave
	// that reached its checkpoint after the project was resumed). It returns them by project.
	ResumeHeld(ctx context.Context) (map[string][]ControlledWave, error)
	// CancelProject cancels the active waves (cancel_by_policy, lost cost recorded) and moves the project to
	// canceled; artifacts, findings and history stay.
	CancelProject(ctx context.Context, projectID string) ([]ControlledWave, error)
	// CompleteProject moves in_progress → completed (record_publication); it returns the RELEASE and its
	// version and the tag of the release.
	CompleteProject(ctx context.Context, projectID string) (ReleaseMark, error)
	// RecordAlert records the highest alert percentage emitted (project.budget_alerted).
	RecordAlert(ctx context.Context, projectID string, percentage int) error
	// BudgetCandidates lists the projects in progress whose spend reached an alert limit not yet emitted or
	// the stop limit (also after the lead lowered the limit, PL-67).
	BudgetCandidates(ctx context.Context) ([]string, error)
	// Alerts returns the alert percentages of the project and the last one emitted (0 when none).
	Alerts(ctx context.Context, projectID string) ([]int, int, error)
}

// ReleaseMark is the approved release that closes the project.
type ReleaseMark struct {
	Artifact string
	Version  int
	Tag      string
}

// ProjectOpener opens a project for the organization administrator without a role (US-51 RN-1).
type ProjectOpener interface {
	Open(ctx context.Context, principal identity.Principal, projectID string) (appproject.Access, error)
}

// LifecycleReader reads the guards of the lifecycle in the api.
type LifecycleReader interface {
	Status(ctx context.Context, projectID string) (Status, error)
}

// WithLifecycle enables pause, resume, cancel and conclusion of the project (US-51, US-52).
func (s *Service) WithLifecycle(reader LifecycleReader, opener ProjectOpener) *Service {
	s.lifecycle, s.opener = reader, opener
	return s
}

func ended(state string) bool {
	return state == project.StateCompleted || state == project.StateCanceled
}

func conflict(state string, actions []string) error {
	return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", state).With("available_actions", actions)
}

// authorizeLead lets the lead or the organization administrator act on the project (US-51 RN-1, CA-6).
func (s *Service) authorizeLead(ctx context.Context, principal identity.Principal, projectID string) (appproject.Access, error) {
	access, err := s.projects.Access(ctx, principal, projectID)
	if err != nil && principal.Administrator && s.opener != nil && failure.From(err).Kind == failure.KindNotFound {
		access, err = s.opener.Open(ctx, principal, projectID)
	}
	if err != nil {
		return appproject.Access{}, err
	}
	if !identity.Has(access.Roles, identity.RoleLead) && !principal.Administrator {
		return appproject.Access{}, failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	return access, nil
}

func (s *Service) enqueue(ctx context.Context, principal identity.Principal, projectID, commandType, action, key, bodyHash string) (CommandView, error) {
	var queued CommandView
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if queued, err = s.commands.Enqueue(ctx, principal, projectID, commandType, key, bodyHash); err != nil {
			return err
		}
		_, err = s.trail.Append(ctx, domainaudit.Record{
			ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
			Origin: domainaudit.OriginWeb, Action: action, Target: "project", CorrelationID: queued.ID,
		})
		return err
	})
	return queued, err
}

// Pause is pauseProject (US-51 CA-1, CA-5, CA-6). Call it in the idempotency transaction.
func (s *Service) Pause(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (CommandView, error) {
	access, err := s.authorizeLead(ctx, principal, projectID)
	if err != nil {
		return CommandView{}, err
	}
	if access.State != project.StateInProgress {
		return CommandView{}, conflict(access.State, project.Actions(access.State, access.Roles, principal.Administrator))
	}
	return s.enqueue(ctx, principal, projectID, "pause", "project.pause", key, bodyHash)
}

// Resume is resumeProject (US-51 CA-2, CA-3; US-39 CA-3): after a pause by the budget only with the stop
// limit raised above the spend.
func (s *Service) Resume(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (CommandView, error) {
	access, err := s.authorizeLead(ctx, principal, projectID)
	if err != nil {
		return CommandView{}, err
	}
	if access.State != project.StatePaused {
		return CommandView{}, conflict(access.State, project.Actions(access.State, access.Roles, principal.Administrator))
	}
	status, err := s.lifecycle.Status(ctx, projectID)
	if err != nil {
		return CommandView{}, err
	}
	if !status.Balance {
		return CommandView{}, failure.New(failure.KindConflict, CodeBudgetExhausted, nil)
	}
	return s.enqueue(ctx, principal, projectID, "resume", "project.resume", key, bodyHash)
}

// Cancel is cancelProject (US-51 CA-4, CA-5, RN-1: the confirmation is the project name typed again).
func (s *Service) Cancel(ctx context.Context, principal identity.Principal, projectID, nameConfirmation, key, bodyHash string) (CommandView, error) {
	access, err := s.authorizeLead(ctx, principal, projectID)
	if err != nil {
		return CommandView{}, err
	}
	if ended(access.State) {
		return CommandView{}, conflict(access.State, []string{})
	}
	status, err := s.lifecycle.Status(ctx, projectID)
	if err != nil {
		return CommandView{}, err
	}
	if strings.TrimSpace(nameConfirmation) != strings.TrimSpace(status.Name) {
		return CommandView{}, failure.New(failure.KindInvalidInput, CodeConfirmationRequired, nil).WithField("name_confirmation", "invalid")
	}
	return s.enqueue(ctx, principal, projectID, "cancel", "project.cancel", key, bodyHash)
}

// CompletionBlocks are the reasons that keep the project from concluding (US-52 CA-2, CA-3).
func CompletionBlocks(status Status) error {
	if !status.ReleaseApproved {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", status.State).
			With("reason", "release_not_approved").With("available_actions", []string{})
	}
	if len(status.BlockingFindings) > 0 {
		return failure.New(failure.KindConflict, CodeBlockingFinding, nil).With("findings", status.BlockingFindings)
	}
	if len(status.ActiveWaves) > 0 {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", status.State).
			With("reason", "active_wave").With("waves", status.ActiveWaves).With("available_actions", []string{})
	}
	return nil
}

// Complete is completeProject (US-52): record_publication with the Release approved, no active wave and no
// blocking finding; only the lead or an approver concludes (RN-2).
func (s *Service) Complete(ctx context.Context, principal identity.Principal, projectID, key, bodyHash string) (CommandView, error) {
	access, err := appproject.Authorize(ctx, s.projects, principal, projectID, identity.RoleLead, identity.RoleApprover)
	if err != nil {
		return CommandView{}, err
	}
	if access.State != project.StateInProgress {
		return CommandView{}, conflict(access.State, []string{})
	}
	status, err := s.lifecycle.Status(ctx, projectID)
	if err != nil {
		return CommandView{}, err
	}
	if err := CompletionBlocks(status); err != nil {
		return CommandView{}, err
	}
	return s.enqueue(ctx, principal, projectID, "record_publication", "project.record_publication", key, bodyHash)
}

// ---------------------------------------------------------------- worker side

// WithLifecycle gives the orchestrator the persistence of the project lifecycle and of the budget.
func (o *Orchestrator) WithLifecycle(store Lifecycle) *Orchestrator {
	o.lifecycle = store
	return o
}

func humanActor(cmd command.Command) map[string]any {
	return map[string]any{"type": "human", "id": cmd.RequestedBy, "origin": "web"}
}

var orchestratorActor = map[string]any{"type": "orchestrator", "id": "orchestrator"}

func waveData(w ControlledWave, from, to string, extra map[string]any) map[string]any {
	data := map[string]any{"wave": w.Code, "stage": w.Stage, "agent": w.Agent, "purpose": w.Purpose, "actor": orchestratorActor,
		"transition": map[string]any{"machine": "wave", "from_state": from, "to_state": to}}
	if w.Task != "" {
		data["task"] = w.Task
	}
	if w.Finding != "" {
		data["finding"] = w.Finding
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

func money(value, currency string) map[string]any {
	return map[string]any{"value": project.TrimDecimal(value), "currency": currency}
}

// PauseProject is the handler of the "project.pause" command (estados.yaml project: pause).
func (o *Orchestrator) PauseProject(ctx context.Context, cmd command.Command) error {
	state, err := o.store.LockProject(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	if state != project.StateInProgress {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	return o.pause(ctx, cmd.ProjectID, cmd.ID, PauseManual, WaveCauseProjectPaused, humanActor(cmd))
}

func (o *Orchestrator) pause(ctx context.Context, projectID, correlation, reason, waveCause string, actor map[string]any) error {
	waves, err := o.lifecycle.PauseProject(ctx, projectID, reason, waveCause)
	if err != nil {
		return err
	}
	emit := o.emitter(ctx, projectID, correlation)
	for _, w := range waves {
		if err := emit("br.easycode.wave.pause_requested.v1", "waves/"+w.Code, w.Code,
			waveData(w, "running", "pausing", map[string]any{"cause": waveCause})); err != nil {
			return err
		}
		if w.Paused {
			if err := emit("br.easycode.wave.paused.v1", "waves/"+w.Code, w.Code,
				waveData(w, "pausing", "paused", map[string]any{"cause": waveCause, "mode": "cooperative"})); err != nil {
				return err
			}
		}
	}
	return emit("br.easycode.project.paused.v1", "project", "", map[string]any{
		"project": projectID, "actor": actor, "pause_reason": reason,
		"transition": map[string]any{"machine": "project", "from_state": project.StateInProgress, "to_state": project.StatePaused},
	})
}

// ResumeProject is the handler of the "project.resume" command (estados.yaml project: resume).
func (o *Orchestrator) ResumeProject(ctx context.Context, cmd command.Command) error {
	state, err := o.store.LockProject(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	if state != project.StatePaused {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	status, err := o.lifecycle.Status(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	if !status.Balance {
		return failure.New(failure.KindConflict, CodeBudgetExhausted, nil)
	}
	waves, err := o.lifecycle.ResumeProject(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	emit := o.emitter(ctx, cmd.ProjectID, cmd.ID)
	for _, w := range waves {
		if err := emit("br.easycode.wave.resumed.v1", "waves/"+w.Code, w.Code, waveData(w, "paused", "running", map[string]any{"cause": "project_resumed"})); err != nil {
			return err
		}
	}
	data := map[string]any{"project": cmd.ProjectID, "actor": humanActor(cmd), "previous_pause_reason": status.PauseReason,
		"transition": map[string]any{"machine": "project", "from_state": project.StatePaused, "to_state": project.StateInProgress}}
	if status.StopLimit != "" {
		data["stop_limit"] = money(status.StopLimit, status.Currency)
	}
	return emit("br.easycode.project.resumed.v1", "project", "", data)
}

// ResumeHeld resumes, by policy, the waves that reached their checkpoint in pausing after their project was
// resumed (reconcile of the worker).
func (o *Orchestrator) ResumeHeld(ctx context.Context) (int, error) {
	if o.lifecycle == nil {
		return 0, nil
	}
	held, err := o.lifecycle.ResumeHeld(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for projectID, waves := range held {
		emit := o.emitter(ctx, projectID, newCorrelation())
		for _, w := range waves {
			n++
			if err := emit("br.easycode.wave.resumed.v1", "waves/"+w.Code, w.Code, waveData(w, "paused", "running", map[string]any{"cause": "project_resumed"})); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// CancelProject is the handler of the "project.cancel" command (estados.yaml project: cancel; US-51 CA-4).
func (o *Orchestrator) CancelProject(ctx context.Context, cmd command.Command) error {
	state, err := o.store.LockProject(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	if ended(state) {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	status, err := o.lifecycle.Status(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	waves, err := o.lifecycle.CancelProject(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	emit := o.emitter(ctx, cmd.ProjectID, cmd.ID)
	codes := []string{}
	lost := "0"
	for _, w := range waves {
		codes = append(codes, w.Code)
		extra := map[string]any{"cause": "project_canceled"}
		if w.LostCost != "" {
			extra["lost_cost"] = money(w.LostCost, status.Currency)
			lost = addDecimal(lost, w.LostCost)
		}
		if err := emit("br.easycode.wave.canceled.v1", "waves/"+w.Code, w.Code, waveData(w, w.From, "canceled", extra)); err != nil {
			return err
		}
	}
	data := map[string]any{"project": cmd.ProjectID, "actor": humanActor(cmd), "canceled_waves": codes,
		"transition": map[string]any{"machine": "project", "from_state": state, "to_state": project.StateCanceled}}
	if lost != "0" {
		data["lost_cost"] = money(lost, status.Currency)
	}
	return emit("br.easycode.project.canceled.v1", "project", "", data)
}

// CompleteProject is the handler of the "project.record_publication" command (US-52): it rechecks the
// guard under lock, concludes the project (read-only from then on) and records the release as published
// in the repository of the project (Phases 1-2: the tag).
func (o *Orchestrator) CompleteProject(ctx context.Context, cmd command.Command) error {
	state, err := o.store.LockProject(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	if state != project.StateInProgress {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	status, err := o.lifecycle.Status(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	if err := CompletionBlocks(status); err != nil {
		return err
	}
	mark, err := o.lifecycle.CompleteProject(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	emit := o.emitter(ctx, cmd.ProjectID, cmd.ID)
	if err := emit("br.easycode.release.published.v1", "artifacts/"+mark.Artifact, "", map[string]any{
		"project": cmd.ProjectID, "actor": humanActor(cmd), "release": map[string]any{"artifact": mark.Artifact, "version": mark.Version},
		"publication_reference": mark.Tag,
	}); err != nil {
		return err
	}
	return emit("br.easycode.project.completed.v1", "project", "", map[string]any{
		"project": cmd.ProjectID, "actor": humanActor(cmd), "release": mark.Artifact, "cost_total": money(status.Spend, status.Currency),
		"transition": map[string]any{"machine": "project", "from_state": project.StateInProgress, "to_state": project.StateCompleted},
	})
}
