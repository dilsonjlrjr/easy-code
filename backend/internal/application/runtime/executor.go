package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"easycode/internal/application/artifact"
	"easycode/internal/application/event"
	"easycode/internal/application/transaction"
	domainartifact "easycode/internal/domain/artifact"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/permission"
	"easycode/internal/domain/provider"
	"easycode/internal/domain/sandbox"
	"easycode/internal/domain/workflow"
)

// ArtifactStore validates and records the outputs (US-29) and submits them for review.
type ArtifactStore interface {
	Parse(content []byte) (domainartifact.Document, string, error)
	StoreVersion(ctx context.Context, in artifact.Input) (artifact.Stored, error)
	SubmitForReview(ctx context.Context, projectID, waveCode, agent string, stored artifact.Stored, correlation string) error
	// Current returns the current version of an artifact (nil when it does not exist).
	Current(ctx context.Context, projectID, code string) (*artifact.Current, error)
	// UpdateTracking merges tracking fields into the header of the current version (no new version).
	UpdateTracking(ctx context.Context, projectID, code string, fields map[string]any) error
}

// FindingProgress applies the finding effects of a completed wave (findings it wrote, rework and
// revalidation cycles, reevaluation; US-32, US-34).
type FindingProgress interface {
	WaveCompleted(ctx context.Context, outcome WaveOutcome) error
}

// WaveOutcome is what a completed wave delivered, for the finding machine.
type WaveOutcome struct {
	ProjectID, Wave, Stage, Agent, Purpose, Commit, Correlation string
	Findings                                                    []string // ACH written by the wave
	Revalidation                                                *Revalidation
}

// trackingFields are the fields an agent may change in an approved artifact (estados.yaml tracking_fields).
var trackingFields = map[string][]string{
	"T": {"situacao", "justificativa", "registro_dev"}, "CT": {"execucao"}, "AMEACAS": {"conferencia"},
}

var freeFields = []string{"estado", "substituido_por", "revisado_por"}

// StageProgress moves the stage after a wave of it completes (submit_to_gate and the gate round).
type StageProgress interface {
	WaveCompleted(ctx context.Context, projectID, stage, correlation string) error
}

// Agent is a runnable agent: its harness, its least-privilege policy and its agent mode per stage.
type Agent struct {
	Steps         Steps
	WritablePaths []string           // used when Policy is nil (write-only agents)
	Policy        *permission.Policy // tools, paths and command categories (RNF-07)
	Mode          string             // mode when Modes has no entry for the stage
	Modes         map[string]string  // stage → agent mode (DESENHO in architecture, VALIDACAO in validation…)
}

// ModeIn is the agent mode in a stage.
func (a Agent) ModeIn(stage string) string {
	if mode, ok := a.Modes[stage]; ok {
		return mode
	}
	return a.Mode
}

// Repository is the git repository of the project (US-42): the base the agents read and the commit of
// every completed wave (D-17), with the artifacts in docs/ and the code in app/.
type Repository interface {
	Files(ctx context.Context, projectID string) (map[string]string, error)
	Commit(ctx context.Context, projectID string, commit Commit) (string, error)
}

// Commit is the content of one wave commit.
type Commit struct {
	Wave    string
	Agent   string
	Message string
	Files   map[string][]byte
}

// Settings tune the executor.
type Settings struct {
	Worker         string
	Lease          time.Duration // lease length; renewed during each step
	MaxCorrections int           // rejected deliveries before the wave fails (US-29 CA-2)
	RetryBackoff   time.Duration // first backoff of a transient provider error
	Poll           time.Duration
	// Control is how often a running step checks that its lease still holds, so a cancel aborts the
	// call or command in flight within RNF-10 (5 s p95); zero means one second.
	Control       time.Duration
	Image         string         // logical sandbox image of the projects (stack of the Phase 1)
	ProjectLimits sandbox.Limits // project limits of RNF-03, combined with the agent ceiling (effective_limits)
}

// Executor runs the waves of the registered agents (ADR-04 Phase 1: PostgreSQL lease and fencing).
type Executor struct {
	tx           transaction.Runner
	ledger       Ledger
	agents       map[string]Agent
	instructions InstructionSource
	outbox       event.Outbox
	telemetry    event.TelemetryEmitter
	artifacts    ArtifactStore
	progress     StageProgress
	outputs      func(stage string) []string
	settings     Settings
	logger       *slog.Logger
	now          func() time.Time
	repository   Repository
	attachments  func(stage string) []string
	// attachmentSpecs gives, per stage, the attachment outputs with the artifact type they belong to, to
	// record the attachments of every version (Artifact.attachments, PL-24).
	attachmentSpecs func(stage string) []domainartifact.AttachmentSpec
	sources         SourceReader
	findings        FindingProgress
	budget          Budget
}

// WithBudget makes the executor apply the project budget after every recorded cost (US-39, RNF-16).
func (e *Executor) WithBudget(budget Budget) *Executor {
	e.budget = budget
	return e
}

// checkBudget runs the budget policy in its own transaction (it locks the project row, so it never runs
// inside the step transaction that holds the wave row). A failure is only logged: the reconcile of the
// worker checks the budgets again.
func (e *Executor) checkBudget(ctx context.Context, c Claimed) {
	if e.budget == nil {
		return
	}
	err := e.tx.InTx(ctx, func(ctx context.Context) error { return e.budget.CheckBudget(ctx, c.ProjectID, correlation(c)) })
	if err != nil && ctx.Err() == nil {
		e.logger.WarnContext(ctx, "budget check failed", slog.String("wave", c.Code), slog.String("error", failure.From(err).Code))
	}
}

// hold stops a wave whose next model call was refused (project paused or budget exhausted, RNF-16): the
// budget policy runs, the wave pauses at its last checkpoint when it was asked to, else the lease is freed
// and the dispatch guard keeps it until the project runs again.
func (e *Executor) hold(ctx context.Context, c Claimed) error {
	e.checkBudget(ctx, c)
	paused, err := e.pauseIfAsked(ctx, c)
	if err != nil || paused {
		return err
	}
	return e.ledger.Release(context.WithoutCancel(ctx), c.Lease)
}

// WithFindings makes the executor apply the finding effects of every completed wave.
func (e *Executor) WithFindings(findings FindingProgress) *Executor {
	e.findings = findings
	return e
}

// WithRepository makes the executor read the project repository as the wave base and commit every
// completed wave (US-42, D-17). attachments gives, per stage, the output paths that are files of an
// artifact and not artifacts themselves (openapi.yaml, estrutura.md).
func (e *Executor) WithRepository(repository Repository, attachments func(stage string) []string) *Executor {
	e.repository, e.attachments = repository, attachments
	return e
}

// WithAttachmentSpecs makes the executor record, with every artifact version, the other files of the
// artifact folder after the wave (openapi.yaml, estrutura.md, prototipo/; PL-24).
func (e *Executor) WithAttachmentSpecs(specs func(stage string) []domainartifact.AttachmentSpec) *Executor {
	e.attachmentSpecs = specs
	return e
}

// WithSources makes the executor give the attachments of the business need to the Requisitos waves (US-03).
func (e *Executor) WithSources(sources SourceReader) *Executor {
	e.sources = sources
	return e
}

// maxSourceBytes bounds the text of one attachment given to the agent.
const maxSourceBytes = 512 << 10

// sourceFiles reads the attachments of the business need; a binary or too large file goes by name only.
func (e *Executor) sourceFiles(ctx context.Context, attachments []SourceAttachment) ([]SourceFile, error) {
	if e.sources == nil || len(attachments) == 0 {
		return nil, nil
	}
	files := make([]SourceFile, 0, len(attachments))
	for _, a := range attachments {
		content, err := e.sources.Read(ctx, a.Path)
		if err != nil {
			return nil, err
		}
		file := SourceFile{Name: a.FileName, MediaType: a.MediaType}
		if len(content) > maxSourceBytes || !utf8.Valid(content) {
			file.Binary = true
		} else {
			file.Text = string(content)
		}
		files = append(files, file)
	}
	return files, nil
}

// versionFiles is the repository after the wave: the base and the outputs of the wave over it.
func versionFiles(wave WaveContext, outputs []Output) map[string][]byte {
	files := make(map[string][]byte, len(wave.Base)+len(outputs))
	for p, content := range wave.Base {
		files[p] = []byte(content)
	}
	for _, output := range outputs {
		files[output.Path] = output.Content
	}
	return files
}

// NewExecutor builds the executor. outputs gives the artifact types each stage must deliver.
func NewExecutor(tx transaction.Runner, ledger Ledger, agents map[string]Agent, instructions InstructionSource, outbox event.Outbox,
	telemetry event.TelemetryEmitter, artifacts ArtifactStore, progress StageProgress, outputs func(string) []string, settings Settings, logger *slog.Logger) *Executor {
	return &Executor{tx: tx, ledger: ledger, agents: agents, instructions: instructions, outbox: outbox, telemetry: telemetry,
		artifacts: artifacts, progress: progress, outputs: outputs, settings: settings, logger: logger, now: time.Now}
}

func (e *Executor) agentNames() []string {
	names := make([]string, 0, len(e.agents))
	for name := range e.agents {
		names = append(names, name)
	}
	return names
}

// Run claims and runs waves until ctx ends; wake shortens the wait between claims.
func (e *Executor) Run(ctx context.Context, wake <-chan struct{}) {
	for ctx.Err() == nil {
		ran, err := e.RunOnce(ctx)
		if errors.Is(err, ErrLeaseLost) {
			e.logger.InfoContext(ctx, "wave stopped: lease lost (canceled or taken over)")
		} else if err != nil && ctx.Err() == nil {
			e.logger.ErrorContext(ctx, "wave execution failed", slog.String("error", err.Error()))
		}
		if ran {
			continue
		}
		select {
		case <-ctx.Done():
		case <-wake:
		case <-time.After(e.settings.Poll):
		}
	}
}

// RunOnce claims one wave and runs it until it waits, ends or loses the lease. It reports
// whether a wave was claimed.
func (e *Executor) RunOnce(ctx context.Context) (bool, error) {
	var claimed *Claimed
	err := e.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if claimed, err = e.ledger.Claim(ctx, e.settings.Worker, e.agentNames(), e.settings.Lease); err != nil || claimed == nil || claimed.EstimateExceeded {
			return err
		}
		return e.claimedEvents(ctx, *claimed)
	})
	if err != nil || claimed == nil {
		return false, err
	}
	if claimed.EstimateExceeded {
		// policy.project_budget stop_by_estimate (US-40): the project is paused by the budget in its own
		// transaction (it locks the project row) and the wave stays queued.
		if e.budget == nil {
			return false, nil
		}
		c := *claimed
		err := e.tx.InTx(ctx, func(ctx context.Context) error {
			return e.budget.StopByEstimate(ctx, c.ProjectID, c.Estimate, correlation(c))
		})
		return err == nil, err
	}
	return true, e.execute(ctx, *claimed)
}

func (e *Executor) base(c Claimed) map[string]any {
	return map[string]any{"wave": c.Code, "stage": c.Stage, "agent": c.Agent, "purpose": c.Purpose}
}

var orchestratorActor = map[string]any{"type": "orchestrator", "id": "orchestrator"}

func with(base map[string]any, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func (e *Executor) emit(ctx context.Context, c Claimed, eventType, subject string, data map[string]any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = e.outbox.Append(ctx, domainevent.Draft{
		Type: eventType, Subject: subject, ProjectID: c.ProjectID, WaveID: c.Code, CorrelationID: correlation(c), Data: encoded,
	})
	return err
}

// correlation is stable per wave attempt: a UUID v8-like id derived from the wave public id.
func correlation(c Claimed) string {
	sum := sha256.Sum256([]byte(c.PublicID + fmt.Sprintf(":%d", c.Attempt)))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x80
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func (e *Executor) claimedEvents(ctx context.Context, c Claimed) error {
	waveSubject := "waves/" + c.Code
	switch {
	case c.WasQueued || !c.Crashed:
		// queued → running (dispatch), or a wave resumed by policy (paused → running, its stage unblocked)
		if c.WasQueued {
			if err := e.emit(ctx, c, "br.easycode.wave.started.v1", waveSubject, with(e.base(c), map[string]any{
				"actor": orchestratorActor, "attempt": c.Attempt,
				"transition": map[string]any{"machine": "wave", "from_state": "queued", "to_state": "running"},
			})); err != nil {
				return err
			}
		}
		// Only the waves that redo the stage work move it (revalidation and reevaluation waves run beside it);
		// an activity runs again from completed (estados.yaml, machine activity).
		moves := c.Purpose == "" || c.Purpose == PurposeExecution || c.Purpose == PurposeRework || c.Purpose == PurposeEditReview
		// The review wave of a human edit also takes the stage back from awaiting_gate (the edit invalidated the round, US-53).
		reviewing := c.Purpose == PurposeEditReview && c.StageState == "awaiting_gate"
		if moves && (reviewing || c.StageState == "ready" || c.StageState == "returned" || c.StageState == "reopened" || c.StageState == "completed") {
			if err := e.ledger.SetStageState(ctx, c.ProjectID, c.Stage, "running"); err != nil {
				return err
			}
			return e.emit(ctx, c, "br.easycode.stage.started.v1", "stages/"+c.Stage, map[string]any{
				"stage": c.Stage, "wave": c.Code, "actor": orchestratorActor,
				"transition": map[string]any{"machine": "stage", "from_state": c.StageState, "to_state": "running"},
			})
		}
	case c.Crashed:
		data := with(e.base(c), map[string]any{
			"actor": orchestratorActor, "checkpoint": c.CurrentStep, "lease_epoch": c.Epoch, "redone_call": c.RedoneCall,
			"transition": map[string]any{"machine": "wave", "from_state": "running", "to_state": "running"},
		})
		if c.LostCost != "" {
			data["lost_cost"] = money(c.LostCost, "")
			data["estimated_lost_cost"] = true
		}
		return e.emit(ctx, c, "br.easycode.wave.resumed_after_crash.v1", waveSubject, data)
	}
	return nil
}

// WithAdjustment adds the adjustment of the project to the default instruction of the agent (US-25 CA-2): it
// comes after the instruction and never replaces it; the instruction hash pins only the default text.
func WithAdjustment(instruction, adjustment string) string {
	if strings.TrimSpace(adjustment) == "" {
		return instruction
	}
	return instruction + "\n\n## Project adjustment (set by the project lead)\n\n" + strings.TrimSpace(adjustment) + "\n"
}

func money(value, currency string) map[string]any {
	if currency == "" {
		currency = "BRL"
	}
	return map[string]any{"value": value, "currency": currency}
}

func (e *Executor) execute(ctx context.Context, c Claimed) error {
	agent, ok := e.agents[c.Agent]
	if !ok {
		return e.fail(ctx, c, CauseUnrecoverable)
	}
	inputs, err := e.ledger.Inputs(ctx, c)
	if err != nil {
		return err
	}
	text, hash, err := e.instructions(c.Agent)
	if err != nil || hash != inputs.InstructionHash {
		e.logger.ErrorContext(ctx, "instruction differs from the pinned agent configuration", slog.String("wave", c.Code))
		return e.fail(ctx, c, CauseUnrecoverable)
	}
	wave := WaveContext{
		ProjectID: c.ProjectID, WaveID: c.PublicID, Code: c.Code, Stage: c.Stage, Agent: c.Agent, Attempt: c.Attempt,
		Instruction: WithAdjustment(text, inputs.Adjustment), BusinessNeed: inputs.BusinessNeed, Constraints: inputs.Constraints, Language: inputs.Language,
		WritablePaths: agent.WritablePaths, Outputs: e.outputs(c.Stage), Models: inputs.Models, Feedback: inputs.Feedback,
		Policy: agent.Policy, Approved: inputs.Approved, Mode: agent.ModeIn(c.Stage), Task: inputs.Task, Image: e.settings.Image,
		Opinions: inputs.Opinions, Skills: inputs.Skills, SkillFiles: inputs.SkillFiles, Purpose: c.Purpose, Finding: inputs.Finding,
		Edit: inputs.Edit,
	}
	if wave.Purpose == "" {
		wave.Purpose = PurposeExecution
	}
	if wave.Sources, err = e.sourceFiles(ctx, inputs.Attachments); err != nil {
		return err
	}
	if wave.Reviewing() || wave.Purpose != PurposeExecution {
		// the delivery of a participant, rework, revalidation, reevaluation or edit review wave is not the full stage output
		// (an edit review delivers the edited artifact, or nothing when the agent refuses the edit, US-53 CA-2)
		wave.Outputs = nil
	}
	if agent.Policy != nil {
		wave.Limits = agent.Policy.Ceiling.Min(e.settings.ProjectLimits)
	}
	if e.repository != nil {
		if wave.Base, err = e.repository.Files(ctx, c.ProjectID); err != nil {
			return err
		}
	}
	cp, err := e.ledger.LastCheckpoint(ctx, c.Lease, c.Attempt)
	if err != nil {
		return err
	}
	if cp == nil {
		start, digest, err := agent.Steps.Start(ctx, wave)
		if err != nil {
			return err
		}
		if err := e.ledger.SetInputsDigest(ctx, c.Lease, digest); err != nil {
			return err
		}
		cp = &start
	}
	if agent.Steps.Awaiting(*cp) {
		answer, err := e.ledger.LatestAnswer(ctx, c.Lease)
		if err != nil {
			return err
		}
		if answer == nil {
			return e.fail(ctx, c, CauseUnrecoverable)
		}
		answered, err := agent.Steps.Answer(*cp, answer.Answers)
		if err != nil {
			return err
		}
		cp = &answered
	}
	return e.loop(ctx, c, agent, wave, *cp)
}

func (e *Executor) loop(ctx context.Context, c Claimed, agent Agent, wave WaveContext, cp Checkpoint) error {
	for {
		started := e.now()
		stepCtx, stop := context.WithCancel(ctx)
		renewed := e.renew(stepCtx, c.Lease, stop)
		result, stepErr := agent.Steps.Next(stepCtx, wave, cp)
		stop()
		if lost := <-renewed; lost {
			return ErrLeaseLost
		}
		duration := e.now().Sub(started).Milliseconds()
		if stepErr != nil {
			if ctx.Err() != nil {
				_ = e.ledger.Release(context.WithoutCancel(ctx), c.Lease)
				return ctx.Err()
			}
			if errors.Is(stepErr, ErrDispatchHeld) {
				return e.hold(ctx, c)
			}
			retry, err := e.stepFailed(ctx, c, result.Call, stepErr, duration)
			if err != nil || !retry {
				return err
			}
			continue
		}
		write := StepWrite{Checkpointed: true, Checkpoint: result.New, StateHash: hash(result.New.State), Call: result.Call, Tool: result.Tool, DurationMs: duration,
			Task: wave.Task, TaskFailed: result.TaskFailed}
		switch result.Type {
		case StepContinue:
			if err := e.saveStep(ctx, c, write, nil); err != nil {
				return err
			}
			if paused, err := e.pauseIfAsked(ctx, c); err != nil || paused {
				return err
			}
			cp = result.New
		case StepAsk:
			return e.saveStep(ctx, c, write, func(ctx context.Context) error {
				code, err := e.ledger.OpenQuestion(ctx, c.Lease, result.Question)
				if err != nil {
					return err
				}
				return e.emit(ctx, c, "br.easycode.wave.question_opened.v1", "waves/"+c.Code, with(e.base(c), map[string]any{
					"actor": map[string]any{"type": "agent", "id": c.Agent}, "question": code, "items": len(result.Question),
					"transition": map[string]any{"machine": "wave", "from_state": "running", "to_state": "awaiting_user"},
				}))
			})
		case StepComplete:
			next, done, err := e.deliver(ctx, c, agent, wave, write, result)
			if err != nil || done {
				return err
			}
			cp = next
		default:
			cause := result.Failure
			if cause == "" {
				cause = CauseUnrecoverable
			}
			// A failure without a new step (limit reached before any call) only closes the call row.
			write.Checkpointed = result.New.After != ""
			if err := e.saveStep(ctx, c, write, nil); err != nil {
				return err
			}
			return e.fail(ctx, c, cause)
		}
	}
}

// renew keeps the lease alive while a step runs and checks that it still holds; when the lease is lost
// (wave canceled, US-14, or taken over) it aborts the step in flight (model call or sandbox command,
// RNF-10) and reports true.
func (e *Executor) renew(ctx context.Context, lease Lease, abort context.CancelFunc) <-chan bool {
	lost := make(chan bool, 1)
	control := e.settings.Control
	if control <= 0 {
		control = time.Second
	}
	go func() {
		ticker := time.NewTicker(e.settings.Lease / 3)
		defer ticker.Stop()
		check := time.NewTicker(control)
		defer check.Stop()
		for {
			select {
			case <-ctx.Done():
				lost <- false
				return
			case <-ticker.C:
				if err := e.ledger.Renew(context.WithoutCancel(ctx), lease, e.settings.Lease); errors.Is(err, ErrLeaseLost) {
					abort()
					lost <- true
					return
				}
			case <-check.C:
				if held, err := e.ledger.Held(context.WithoutCancel(ctx), lease); err == nil && !held {
					abort()
					lost <- true
					return
				}
			}
		}
	}()
	return lost
}

func hash(state []byte) string {
	sum := sha256.Sum256(state)
	return hex.EncodeToString(sum[:])
}

// saveStep writes the step (and the extra effect) in one transaction, then emits the telemetry.
func (e *Executor) saveStep(ctx context.Context, c Claimed, write StepWrite, extra func(ctx context.Context) error) error {
	var totals Totals
	err := e.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if totals, err = e.ledger.SaveStep(ctx, c.Lease, c.Attempt, write); err != nil {
			return err
		}
		if write.Checkpointed {
			if err := e.emit(ctx, c, "br.easycode.wave.checkpoint_written.v1", "waves/"+c.Code, with(e.base(c), map[string]any{
				"actor": orchestratorActor, "checkpoint": write.Checkpoint.Number, "after": write.Checkpoint.After,
				"accumulated_tokens": tokens(totals.Usage), "accumulated_cost": money(totals.UsefulCost, totals.Currency),
				"transition": map[string]any{"machine": "wave", "from_state": "running", "to_state": "running"},
			})); err != nil {
				return err
			}
		}
		if write.Tool != nil {
			data := with(e.base(c), map[string]any{"actor": map[string]any{"type": "agent", "id": c.Agent}, "tool": write.Tool.Tool, "summary": clip(write.Tool.Summary, 120)})
			if write.Task != "" {
				data["task"] = write.Task
			}
			data["result"] = "allowed"
			if !write.Tool.Allowed {
				data["result"], data["refusal_reason"] = "refused", write.Tool.RefusalReason
			}
			if err := e.emit(ctx, c, "br.easycode.wave.tool_called.v1", "waves/"+c.Code, data); err != nil {
				return err
			}
		}
		if failed := write.TaskFailed; failed != nil && write.Task != "" {
			if err := e.emit(ctx, c, "br.easycode.wave.task_failed.v1", "waves/"+c.Code, with(e.base(c), map[string]any{
				"actor": orchestratorActor, "task": write.Task, "cause": failed.Cause, "consecutive_failures": failed.Consecutive,
			})); err != nil {
				return err
			}
		}
		if extra != nil {
			return extra(ctx)
		}
		return nil
	})
	if err == nil && write.Call != nil {
		e.callTelemetry(ctx, c, *write.Call, totals.Currency)
		if write.Call.ID != 0 {
			e.checkBudget(ctx, c)
		}
	}
	return err
}

func clip(text string, limit int) string {
	if text == "" {
		return "executou ferramenta"
	}
	if len([]rune(text)) > limit {
		return string([]rune(text)[:limit])
	}
	return text
}

func tokens(u provider.Usage) map[string]any {
	return map[string]any{"input": u.Input, "output": u.Output, "read_cache": u.CacheRead, "write_cache": u.CacheWrite}
}

// callTelemetry is best effort (cost.model_call_recorded; the true cost is in the checkpoint).
func (e *Executor) callTelemetry(ctx context.Context, c Claimed, call CallRecord, currency string) {
	if e.telemetry == nil {
		return
	}
	data, _ := json.Marshal(with(e.base(c), map[string]any{
		"provider": call.Provider, "model": call.Model, "role": call.Role, "tokens": tokens(call.Usage),
		"cost": money(call.Cost, currency), "price_table_version": call.PriceTableVersion, "duration_ms": call.Duration, "result": call.Result,
	}))
	if _, err := e.telemetry.Emit(ctx, domainevent.Draft{Type: "br.easycode.cost.model_call_recorded.v1", Subject: "waves/" + c.Code,
		ProjectID: c.ProjectID, WaveID: c.Code, CorrelationID: correlation(c), Data: data}); err != nil {
		e.logger.WarnContext(ctx, "cost telemetry not sent", slog.String("error", err.Error()))
	}
}

// stepFailed handles a failed step: transient provider errors are retried with backoff up to
// max_retries (the aborted call cost is lost cost); others fail the wave.
func (e *Executor) stepFailed(ctx context.Context, c Claimed, call *CallRecord, stepErr error, duration int64) (bool, error) {
	var providerErr *provider.Error
	if call != nil {
		if err := e.saveStep(ctx, c, StepWrite{Call: call, DurationMs: duration}, nil); err != nil {
			return false, err
		}
	}
	if errors.As(stepErr, &providerErr) && (providerErr.Class == provider.ErrorTransient || providerErr.Class == provider.ErrorRateLimit) {
		var used, maximum int
		err := e.tx.InTx(ctx, func(ctx context.Context) error {
			var err error
			if used, maximum, err = e.ledger.ScheduleRetry(ctx, c.Lease); err != nil || used > maximum {
				return err
			}
			return e.emit(ctx, c, "br.easycode.wave.retry_scheduled.v1", "waves/"+c.Code, with(e.base(c), map[string]any{
				"actor": orchestratorActor, "retry": used, "max_retries": maximum, "error_class": string(providerErr.Class),
				"transition": map[string]any{"machine": "wave", "from_state": "running", "to_state": "running"},
			}))
		})
		if err != nil {
			return false, err
		}
		if used > maximum {
			return false, e.fail(ctx, c, CauseRetriesExhausted)
		}
		wait := e.settings.RetryBackoff * time.Duration(1<<min(used-1, 5))
		if providerErr.RetryAfter > wait {
			wait = providerErr.RetryAfter
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(wait):
		}
		return true, nil
	}
	if errors.Is(stepErr, ErrLeaseLost) {
		return false, stepErr
	}
	e.logger.WarnContext(ctx, "wave step failed", slog.String("wave", c.Code), slog.String("error", failure.From(stepErr).Code))
	return false, e.fail(ctx, c, CauseUnrecoverable)
}

func (e *Executor) fail(ctx context.Context, c Claimed, cause string) error {
	return e.tx.InTx(ctx, func(ctx context.Context) error {
		totals, err := e.ledger.Fail(ctx, c.Lease, cause)
		if err != nil {
			return err
		}
		return e.emit(ctx, c, "br.easycode.wave.failed.v1", "waves/"+c.Code, with(e.base(c), map[string]any{
			"actor": orchestratorActor, "cause": cause, "tokens": tokens(totals.Usage),
			"cost": money(totals.UsefulCost, totals.Currency), "lost_cost": money(totals.LostCost, totals.Currency),
			"transition": map[string]any{"machine": "wave", "from_state": "running", "to_state": "failed"},
		}))
	})
}

// schemaProblem is one error of a delivery (Wave.artifact_errors).
type schemaProblem struct {
	Artifact string `json:"artifact"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

// pauseIfAsked stops the wave at the checkpoint when it was asked to pause, by the user (US-12 CA-1) or by
// policy (project paused, budget exhausted, a reopened dependency of the stage, US-34 CA-1): pausing →
// paused (checkpoint_reached), nothing lost.
func (e *Executor) pauseIfAsked(ctx context.Context, c Claimed) (bool, error) {
	var paused bool
	err := e.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		var cause string
		if cause, paused, err = e.ledger.Pause(ctx, c.Lease); err != nil || !paused {
			return err
		}
		return e.emit(ctx, c, "br.easycode.wave.paused.v1", "waves/"+c.Code, with(e.base(c), map[string]any{
			"actor": orchestratorActor, "cause": cause, "mode": "cooperative",
			"transition": map[string]any{"machine": "wave", "from_state": "pausing", "to_state": "paused"},
		}))
	})
	return paused, err
}

// tracked is a write that only changes the tracking fields of an approved artifact.
type tracked struct {
	code   string
	fields map[string]any
}

// splitTracking separates the writes on approved artifacts: allowed when only their tracking fields
// changed (the T of the Dev, CT.execucao of the QA, AMEACAS.conferencia of the Segurança), refused
// otherwise; the other outputs are new versions.
func (e *Executor) splitTracking(ctx context.Context, c Claimed, wave WaveContext, outputs []Output) ([]Output, []tracked, []schemaProblem, error) {
	var versions []Output
	var updates []tracked
	var problems []schemaProblem
	for _, output := range outputs {
		if _, approved := wave.Approved[output.Path]; !approved {
			versions = append(versions, output)
			continue
		}
		document, artifactType, err := e.artifacts.Parse(output.Content)
		if err != nil {
			versions = append(versions, output) // validate reports the schema errors
			continue
		}
		code, _ := document.Header["id"].(string)
		current, err := e.artifacts.Current(ctx, c.ProjectID, code)
		if err != nil {
			return nil, nil, nil, err
		}
		fields := trackingFields[artifactType]
		if current == nil || len(fields) == 0 {
			problems = append(problems, schemaProblem{Artifact: output.Path, Message: "approved artifact: only a finding reopens it"})
			continue
		}
		strip := func(header map[string]any) string {
			copied := map[string]any{}
			for k, v := range header {
				copied[k] = v
			}
			for _, k := range append(append([]string{}, fields...), freeFields...) {
				delete(copied, k)
			}
			encoded, _ := json.Marshal(copied)
			return string(encoded)
		}
		if strip(document.Header) != strip(current.Header) || strings.TrimSpace(document.Body) != strings.TrimSpace(current.Body) {
			problems = append(problems, schemaProblem{Artifact: output.Path, Message: "approved artifact: only the tracking fields " + strings.Join(fields, ", ") + " may change"})
			continue
		}
		update := tracked{code: code, fields: map[string]any{}}
		for _, k := range fields {
			if v, ok := document.Header[k]; ok {
				update.fields[k] = v
			}
		}
		updates = append(updates, update)
	}
	return versions, updates, problems, nil
}

// deliver validates the outputs (artifact.schema_valid, guard of wave: complete) and either
// completes the wave or returns the errors to the agent, failing after the correction limit.
func (e *Executor) deliver(ctx context.Context, c Claimed, agent Agent, wave WaveContext, write StepWrite, result StepResult) (Checkpoint, bool, error) {
	artifacts, updates, problems, err := e.splitTracking(ctx, c, wave, e.artifactOutputs(c.Stage, result.Outputs))
	if err != nil {
		return Checkpoint{}, true, err
	}
	problems = append(problems, e.validate(wave, artifacts)...)
	if len(problems) > 0 {
		evidence, _ := json.Marshal(problems)
		if agent.Steps.Corrections(result.New) >= e.settings.MaxCorrections {
			err := e.saveStep(ctx, c, write, func(ctx context.Context) error {
				return e.ledger.RecordWaveCheck(ctx, c.Lease, "artifact.schema_valid", false, evidence)
			})
			if err != nil {
				return Checkpoint{}, true, err
			}
			return Checkpoint{}, true, e.fail(ctx, c, CauseUnrecoverable)
		}
		var text strings.Builder
		for _, p := range problems {
			fmt.Fprintf(&text, "- %s %s: %s\n", p.Artifact, p.Path, p.Message)
		}
		rejected, err := agent.Steps.Reject(result.New, text.String())
		if err != nil {
			return Checkpoint{}, true, err
		}
		write.Checkpoint, write.StateHash = rejected, hash(rejected.State)
		return rejected, false, e.saveStep(ctx, c, write, nil)
	}
	var sha string
	if e.repository != nil && len(result.Outputs) > 0 {
		files := make(map[string][]byte, len(result.Outputs))
		for _, output := range result.Outputs {
			files[output.Path] = output.Content
		}
		if sha, err = e.repository.Commit(ctx, c.ProjectID, Commit{Wave: c.Code, Agent: c.Agent, Message: commitMessage(c, wave), Files: files}); err != nil {
			return Checkpoint{}, true, err
		}
	}
	return Checkpoint{}, true, e.saveStep(ctx, c, write, func(ctx context.Context) error {
		refs := []map[string]any{}
		var versions []CommittedVersion
		findings := []string{}
		for _, update := range updates {
			if err := e.artifacts.UpdateTracking(ctx, c.ProjectID, update.code, update.fields); err != nil {
				return err
			}
		}
		var tree map[string][]byte
		var specs []domainartifact.AttachmentSpec
		if e.attachmentSpecs != nil {
			if specs = e.attachmentSpecs(c.Stage); len(specs) > 0 {
				tree = versionFiles(wave, result.Outputs)
			}
		}
		for _, output := range artifacts {
			var attachments []domainartifact.Attachment
			if tree != nil {
				if _, artifactType, err := e.artifacts.Parse(output.Content); err == nil {
					attachments = domainartifact.VersionAttachments(artifactType, output.Path, specs, tree)
				}
			}
			stored, err := e.artifacts.StoreVersion(ctx, artifact.Input{ProjectID: c.ProjectID, WaveCode: c.Code, Agent: c.Agent,
				Path: output.Path, Content: output.Content, CorrelationID: correlation(c), Attachments: attachments})
			if err != nil {
				return err
			}
			if err := e.artifacts.SubmitForReview(ctx, c.ProjectID, c.Code, c.Agent, stored, correlation(c)); err != nil {
				return err
			}
			refs = append(refs, map[string]any{"artifact": stored.Code, "version": stored.Version})
			versions = append(versions, CommittedVersion{Code: stored.Code, Version: stored.Version})
			if stored.Type == "ACH" {
				findings = append(findings, stored.Code)
			}
		}
		if err := e.ledger.RecordWaveCheck(ctx, c.Lease, "artifact.schema_valid", true, nil); err != nil {
			return err
		}
		if result.Opinion != nil {
			evidence, _ := json.Marshal(result.Opinion)
			if err := e.ledger.RecordWaveCheck(ctx, c.Lease, CheckReviewOpinion, true, evidence); err != nil {
				return err
			}
		}
		if sha != "" {
			if err := e.ledger.RecordCommit(ctx, c.Lease, sha, versions); err != nil {
				return err
			}
			if wave.Task != "" {
				if err := e.recordTask(ctx, c.ProjectID, wave.Task, sha); err != nil {
					return err
				}
			}
		}
		totals, executionMs, cycleMs, err := e.ledger.Complete(ctx, c.Lease)
		if err != nil {
			return err
		}
		completed := with(e.base(c), map[string]any{
			"actor": orchestratorActor, "tokens": tokens(totals.Usage), "cost": money(totals.UsefulCost, totals.Currency),
			"lost_cost": money(totals.LostCost, totals.Currency), "execution_duration_ms": executionMs, "cycle_duration_ms": cycleMs,
			"artifacts": refs, "findings": findings,
			"transition": map[string]any{"machine": "wave", "from_state": "running", "to_state": "completed"},
		})
		if wave.Task != "" {
			completed["task"] = wave.Task
		}
		if err := e.emit(ctx, c, "br.easycode.wave.completed.v1", "waves/"+c.Code, completed); err != nil {
			return err
		}
		if e.findings != nil {
			if err := e.findings.WaveCompleted(ctx, WaveOutcome{ProjectID: c.ProjectID, Wave: c.Code, Stage: c.Stage, Agent: c.Agent, Purpose: wave.Purpose,
				Commit: sha, Correlation: correlation(c), Findings: findings, Revalidation: result.Revalidation}); err != nil {
				return err
			}
		}
		return e.progress.WaveCompleted(ctx, c.ProjectID, c.Stage, correlation(c))
	})
}

// recordTask fills the Dev record of the task of the wave (T tracking fields, t.dev_record_present):
// the wave commit enters registro_dev.commits and the task is concluded, unless the Dev justified it.
func (e *Executor) recordTask(ctx context.Context, projectID, task, sha string) error {
	current, err := e.artifacts.Current(ctx, projectID, task)
	if err != nil || current == nil {
		return err
	}
	record, _ := current.Header["registro_dev"].(map[string]any)
	if record == nil {
		record = map[string]any{}
	}
	commits, _ := record["commits"].([]any)
	record["commits"] = append(commits, sha)
	fields := map[string]any{"registro_dev": record}
	if situation, _ := current.Header["situacao"].(string); situation != "justificada" {
		fields["situacao"] = "concluida"
	}
	return e.artifacts.UpdateTracking(ctx, projectID, task, fields)
}

// CheckReviewOpinion is the wave record of the opinion of a participant (evidence: the opinion), read
// by the lead's wave and by validacao.review_opinions_recorded ("registro de ondas").
const CheckReviewOpinion = "validacao.review_opinion"

func (e *Executor) validate(wave WaveContext, outputs []Output) []schemaProblem {
	var problems []schemaProblem
	present := map[string]bool{}
	seen := map[string]string{}
	for _, output := range outputs {
		document, artifactType, err := e.artifacts.Parse(output.Content)
		if err != nil {
			classified := failure.From(err)
			errorsList, _ := classified.Extensions["schema_errors"].([]domainartifact.SchemaError)
			if len(errorsList) == 0 {
				errorsList = []domainartifact.SchemaError{{Message: classified.Code}}
			}
			for _, item := range errorsList {
				problems = append(problems, schemaProblem{Artifact: output.Path, Path: item.Path, Message: item.Message})
			}
			continue
		}
		code, _ := document.Header["id"].(string)
		if previous, dup := seen[code]; dup {
			problems = append(problems, schemaProblem{Artifact: output.Path, Path: "/id", Message: "id " + code + " also used by " + previous})
		}
		seen[code] = output.Path
		present[artifactType] = true
		if artifactType == "VALIDACAO" {
			problems = append(problems, consolidated(output.Path, document.Header, wave.Opinions)...)
		}
	}
	for _, required := range wave.Outputs {
		if !present[required] {
			problems = append(problems, schemaProblem{Artifact: required, Message: "required output missing: " + required})
		}
	}
	return problems
}

// consolidated checks that VALIDACAO.pareceres carries the opinion each participant gave in its wave,
// unchanged (template VALIDACAO: one wave per participant, consolidated by the orchestrator).
func consolidated(path string, header map[string]any, opinions []workflow.ReviewOpinion) []schemaProblem {
	var problems []schemaProblem
	written := map[string]map[string]any{}
	list, _ := header["pareceres"].([]any)
	for _, item := range list {
		if entry, ok := item.(map[string]any); ok {
			name, _ := entry["participante"].(string)
			written[name] = entry
		}
	}
	for _, opinion := range opinions {
		entry, ok := written[opinion.Participant]
		given, _ := entry["parecer"].(string)
		var reservations []string
		raw, _ := entry["ressalvas"].([]any)
		for _, r := range raw {
			text, _ := r.(string)
			reservations = append(reservations, text)
		}
		if !ok || given != opinion.Opinion || strings.Join(reservations, "\n") != strings.Join(opinion.Reservations, "\n") {
			encoded, _ := json.Marshal(opinion)
			problems = append(problems, schemaProblem{Artifact: path, Path: "/pareceres",
				Message: "parecer de " + opinion.Participant + " difere do registrado na onda " + opinion.Wave + ": " + string(encoded)})
		}
	}
	return problems
}

// artifactOutputs keeps the outputs that are artifacts (docs/**.md with a header): code in app/ and
// the attachments of an artifact (openapi.yaml, estrutura.md) only go to the repository.
func (e *Executor) artifactOutputs(stage string, outputs []Output) []Output {
	if e.attachments == nil {
		return outputs
	}
	attached := e.attachments(stage)
	var artifacts []Output
	for _, output := range outputs {
		if !strings.HasPrefix(output.Path, "docs/") || !strings.HasSuffix(output.Path, ".md") {
			continue
		}
		attachment := false
		for _, a := range attached {
			attachment = attachment || output.Path == a || (strings.HasSuffix(a, "/") && strings.HasPrefix(output.Path, a))
		}
		if !attachment {
			artifacts = append(artifacts, output)
		}
	}
	return artifacts
}

// commitMessage cites the wave and, in the implementation, the task (US-19 CA-1).
func commitMessage(c Claimed, wave WaveContext) string {
	subject := c.Code + ": " + map[string]string{"requirements": "requisitos", "architecture": "arquitetura", "data": "modelagem de dados",
		"design": "design", "validation": "validação técnica", "implementation": "implementação"}[c.Stage]
	if wave.Task != "" {
		subject = wave.Task + ": implementação da tarefa (" + c.Code + ")"
	}
	trailer := "\n\nEasycode-Wave: " + c.Code + "\nEasycode-Agent: " + c.Agent
	if wave.Task != "" {
		trailer += "\nEasycode-Task: " + wave.Task
	}
	return subject + trailer + "\n"
}
