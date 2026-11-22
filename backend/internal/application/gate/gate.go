// Package gate is the governance engine of the worker (ADR-10, interno.md section 5): after the last
// wave of a stage the orchestrator submits it to its gate, opens a round, runs every check of the gate
// composition (gates.yaml: the common checks plus the gate ones, blocking or alert, automatic or human)
// and the blocking findings, and decides complete_checks: rejected (a blocking check failed or a critical
// or high finding is active), approved (automatic mode allowed) or awaiting_human. The api applies the
// human decisions synchronously; the worker reacts (stage approved and the next stages unblocked, or the
// stage returned with rework waves, or waiting for the findings that block it).
package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"easycode/internal/application/artifact"
	"easycode/internal/application/event"
	"easycode/internal/application/transaction"
	domainartifact "easycode/internal/domain/artifact"
	domainevent "easycode/internal/domain/event"
	domainfinding "easycode/internal/domain/finding"
	"easycode/internal/domain/governance"
	domainworkflow "easycode/internal/domain/workflow"
)

// Round states (machine gate).
const (
	StateEvaluating    = "evaluating"
	StateAwaitingHuman = "awaiting_human"
	StateApproved      = "approved"
	StateRejected      = "rejected"
	StateInvalidated   = "invalidated"
)

// Check results (check_executions.result, D-36).
const (
	ResultPassed  = "passed"
	ResultFailed  = "failed"
	ResultPending = "pending"
)

// CheckFindingsBlocking is the check whose failure means a critical or high finding is active.
const CheckFindingsBlocking = "finding.no_open_blocking"

// Execution is a sandbox execution recorded in a wave (evidence of US-41).
type Execution struct {
	Wave        string
	ID          string
	Category    string
	Command     string
	ExitCode    *int
	EndReason   string
	TestsTotal  *int
	TestsFailed *int
}

// Green reports whether the execution ended with exit code 0.
func (e Execution) Green() bool { return e.ExitCode != nil && *e.ExitCode == 0 }

// Decided is a round already decided whose stage still waits for the effect.
type Decided struct {
	ProjectID string
	Stage     string
	Round     int
	State     string
}

// ArtifactRecord is the current version of an artifact of the project, as the checks read it.
type ArtifactRecord struct {
	Code, Type, State string
	Version           int
	Path              string
	Author            string // author of the version (agent, orchestrator or human)
	WaveCode          string
	WaveStage         string
	WaveState         string
	WavePurpose       string
	Finding           string // ACH whose rework created the version (artifact_versions.finding_id)
	ReplacedBy        string
	Committed         bool
	// ApprovedByRound is true when the current version of an approved artifact is in an approved round.
	ApprovedByRound bool
	Header          map[string]any
	Body            string
}

// WaveRecord is a wave of the round window (wave.limits_respected, wave.record_complete).
type WaveRecord struct {
	Code, Stage, Agent, Purpose, State, FailureCause string
	LimitExceeded                                    int // executions ended by a resource limit
	Models, Calls                                    int
	FinalCheckpoint                                  bool
	Finished                                         bool
}

// StageRecord is the stage row and the start of the round window.
type StageRecord struct {
	State               string
	ReevaluationPending bool
	// Since is when the previous round of the stage was decided (zero for the first round): the waves of
	// the round are the ones completed after it.
	Since time.Time
}

// Settings are the project choices for a gate (gate_configurations).
type Settings struct {
	Mode     string   // configured approval mode, "" when none
	Promoted []string // alerts made blocking by the project
}

// CheckResult is one check result of a round.
type CheckResult struct {
	Check    string
	Type     string
	Severity string
	Status   string
	Evidence json.RawMessage
}

// Store is the gate persistence used by the worker; it is also the read port of the checks.
type Store interface {
	// ActiveWaves counts the waves of the stage not completed (queued to paused, or failed).
	ActiveWaves(ctx context.Context, projectID, stage string) (int, error)
	// OpenRound creates the next round in evaluating with the outputs and the approval mode.
	OpenRound(ctx context.Context, projectID, stage, mode string, outputs []artifact.Stored) (int, error)
	RecordCheck(ctx context.Context, projectID, stage string, round int, result CheckResult) error
	SetRoundState(ctx context.Context, projectID, stage string, round int, state string) error
	// Reevaluate moves an awaiting_human round back to evaluating with a new evaluation number and the
	// finding that caused it.
	Reevaluate(ctx context.Context, projectID, stage string, round int, finding string) error
	// ActiveRound returns the round in evaluating or awaiting_human of the stage (0 when none) and its state.
	ActiveRound(ctx context.Context, projectID, stage string) (int, string, error)
	// RoundMode returns the approval mode the round was opened with.
	RoundMode(ctx context.Context, projectID, stage string, round int) (string, error)
	// Decided lists stages in awaiting_gate whose last round is approved or rejected.
	Decided(ctx context.Context) ([]Decided, error)
	// LastExecution returns the last execution of the category in the last completed wave of the stage.
	LastExecution(ctx context.Context, projectID, stage, category string) (*Execution, error)
	// WaveExecutions returns the sandbox executions of a wave, in order.
	WaveExecutions(ctx context.Context, projectID, wave string) ([]Execution, error)
	// ReviewOpinions returns, by agent, the opinion of the last completed wave of each agent in the stage.
	ReviewOpinions(ctx context.Context, projectID, stage string) (map[string]domainworkflow.ReviewOpinion, error)
	ProjectArtifacts(ctx context.Context, projectID string) ([]ArtifactRecord, error)
	ProjectFindings(ctx context.Context, projectID string) ([]domainfinding.Finding, error)
	RoundWaves(ctx context.Context, projectID string, stages []string, since time.Time) ([]WaveRecord, error)
	Stage(ctx context.Context, projectID, stage string) (StageRecord, error)
	Settings(ctx context.Context, projectID, gate string) (Settings, error)
	// RecordedCommits are the commits the project recorded (waves, artifact versions, finding cycles).
	RecordedCommits(ctx context.Context, projectID string) ([]string, error)
	// LastRound returns the number and failed checks of the last round of a stage (0 when none).
	LastRound(ctx context.Context, projectID, stage string) (int, []string, error)
}

// Repository reads the project repository (files at HEAD and commits).
type Repository interface {
	Files(ctx context.Context, projectID string) (map[string]string, error)
	Reachable(ctx context.Context, projectID, sha string) (bool, error)
}

// HeaderValidator validates a stored header against the schema of its type.
type HeaderValidator interface {
	Validate(artifactType string, header map[string]any) ([]domainartifact.SchemaError, error)
}

// Artifacts is what the gate reads and moves of the artifacts.
type Artifacts interface {
	InReview(ctx context.Context, projectID, stage string) ([]artifact.Stored, error)
	SetState(ctx context.Context, projectID, code, state string) error
	Header(ctx context.Context, projectID, code string) (map[string]any, error)
	Headers(ctx context.Context, projectID, artifactType string) (map[string]map[string]any, error)
}

// StageFlow is the part of the orchestrator the gate drives.
type StageFlow interface {
	SetStageState(ctx context.Context, projectID, stage, state string) error
	UnblockReady(ctx context.Context, projectID, correlation string) error
	QueueRework(ctx context.Context, projectID, stage, correlation string) ([]string, error)
}

// RoundHook is told about approved rounds (a finding awaiting a scope change closes when the
// gate.requirements approves the change, PD-06).
type RoundHook interface {
	RoundApproved(ctx context.Context, projectID, stage string, round int, correlation string) error
}

// Progress is the worker side of the gate.
type Progress struct {
	tx          transaction.Runner
	store       Store
	artifacts   Artifacts
	validator   HeaderValidator
	flow        StageFlow
	outbox      event.Outbox
	definition  domainworkflow.Definition
	composition governance.Composition
	repository  Repository
	parser      artifact.HeaderParser
	hook        RoundHook
	now         func() time.Time
}

// NewProgress builds the gate progress. It fails when an automatic check of a gate of the workflow has
// no implementation (ADR-10: the worker does not start).
func NewProgress(tx transaction.Runner, store Store, artifacts Artifacts, validator HeaderValidator, flow StageFlow, outbox event.Outbox,
	definition domainworkflow.Definition, composition governance.Composition) (*Progress, error) {
	for _, stage := range definition.Stages {
		for _, spec := range composition.ChecksOf(stage.Gate, nil) {
			if _, ok := registry[spec.ID]; spec.Type == governance.TypeAutomatic && !ok {
				return nil, fmt.Errorf("check %s of %s has no implementation", spec.ID, stage.Gate)
			}
		}
	}
	return &Progress{tx: tx, store: store, artifacts: artifacts, validator: validator, flow: flow, outbox: outbox, definition: definition,
		composition: composition, now: time.Now}, nil
}

// WithRepository lets the checks read the project repository (files, commits) and parse its YAML
// documents (the OpenAPI contract).
func (p *Progress) WithRepository(repository Repository, parser artifact.HeaderParser) *Progress {
	p.repository, p.parser = repository, parser
	return p
}

// WithHook registers who is told about approved rounds.
func (p *Progress) WithHook(hook RoundHook) *Progress {
	p.hook = hook
	return p
}

var orchestrator = map[string]any{"type": "orchestrator", "id": "orchestrator"}

func (p *Progress) emit(ctx context.Context, projectID, correlation, eventType, subject string, data map[string]any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = p.outbox.Append(ctx, domainevent.Draft{Type: eventType, Subject: subject, ProjectID: projectID, CorrelationID: correlation, Data: encoded})
	return err
}

func transition(machine, from, to string) map[string]any {
	var fromState any = from
	if from == "" {
		fromState = nil
	}
	return map[string]any{"machine": machine, "from_state": fromState, "to_state": to}
}

// requiredTypes are the typed required outputs of a stage and of the activities approved in its gate.
func (p *Progress) requiredTypes(stage domainworkflow.Stage) []string {
	seen := map[string]bool{}
	var types []string
	add := func(outputs []domainworkflow.Output) {
		for _, o := range outputs {
			if o.Required && o.Type != "" && !o.Attachment && !seen[o.Type] {
				seen[o.Type] = true
				types = append(types, o.Type)
			}
		}
	}
	add(stage.Outputs)
	for _, a := range p.definition.ActivitiesFor(stage.ID) {
		add(a.Outputs)
	}
	return types
}

// WaveCompleted implements runtime.StageProgress: when every wave of a running stage is done, its
// required activities are completed and the required outputs are in review (or approved and current,
// in a stage reopened by a finding), the stage goes to its gate (US-17 CA-3) and the round runs its
// checks. A wave of a parallel activity completes the activity and retries the stage it is required for.
func (p *Progress) WaveCompleted(ctx context.Context, projectID, stageID, correlation string) error {
	if activity, ok := p.definition.Activity(stageID); ok {
		return p.activityCompleted(ctx, projectID, activity, correlation)
	}
	stage, ok := p.definition.Stage(stageID)
	if !ok {
		return nil
	}
	record, err := p.store.Stage(ctx, projectID, stageID)
	if err != nil || record.State != domainworkflow.StageRunning {
		return err
	}
	active, err := p.store.ActiveWaves(ctx, projectID, stageID)
	if err != nil || active > 0 {
		return err
	}
	for _, activity := range p.definition.ActivitiesFor(stageID) {
		state, err := p.store.Stage(ctx, projectID, activity.ID)
		if err != nil || state.State != domainworkflow.ActivityCompleted {
			return err // guard of submit_to_gate: required activities completed
		}
	}
	outputs, err := p.artifacts.InReview(ctx, projectID, stageID)
	if err != nil || (len(outputs) == 0 && len(p.requiredTypes(stage)) > 0) {
		return err // a typed stage goes to the gate with its outputs in review (the implementation has none: app/)
	}
	all, err := p.store.ProjectArtifacts(ctx, projectID)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, a := range all {
		if a.State == domainartifact.StateInReview || (a.State == domainartifact.StateApproved && a.ApprovedByRound) {
			present[a.Type] = true
		}
	}
	for _, required := range p.requiredTypes(stage) {
		if !present[required] {
			return nil // guard of submit_to_gate: required outputs present and in review
		}
	}
	refs := make([]map[string]any, 0, len(outputs))
	for _, output := range outputs {
		refs = append(refs, map[string]any{"artifact": output.Code, "version": output.Version})
	}
	settings, err := p.store.Settings(ctx, projectID, stage.Gate)
	if err != nil {
		return err
	}
	mode := stage.ApprovalMode(settings.Mode)
	round, err := p.store.OpenRound(ctx, projectID, stageID, mode, outputs)
	if err != nil {
		return err
	}
	if err := p.flow.SetStageState(ctx, projectID, stageID, domainworkflow.StageAwaitingGate); err != nil {
		return err
	}
	if err := p.emit(ctx, projectID, correlation, "br.easycode.stage.submitted_to_gate.v1", "stages/"+stageID, map[string]any{
		"stage": stageID, "gate": stage.Gate, "round": round, "outputs": refs, "actor": orchestrator,
		"transition": transition("stage", domainworkflow.StageRunning, domainworkflow.StageAwaitingGate),
	}); err != nil {
		return err
	}
	if err := p.emit(ctx, projectID, correlation, "br.easycode.gate.opened.v1", roundSubject(stage.Gate, round), map[string]any{
		"gate": stage.Gate, "round": round, "stage": stageID, "approval_mode": mode, "outputs": refs, "actor": orchestrator,
		"transition": transition("gate", "", StateEvaluating),
	}); err != nil {
		return err
	}
	return p.evaluate(ctx, projectID, stage, round, mode, outputs, settings, correlation)
}

// activityCompleted applies complete of the activity machine and retries the stage it is required for.
func (p *Progress) activityCompleted(ctx context.Context, projectID string, activity domainworkflow.Activity, correlation string) error {
	record, err := p.store.Stage(ctx, projectID, activity.ID)
	if err != nil || record.State != domainworkflow.StageRunning {
		return err
	}
	active, err := p.store.ActiveWaves(ctx, projectID, activity.ID)
	if err != nil || active > 0 {
		return err
	}
	all, err := p.store.ProjectArtifacts(ctx, projectID)
	if err != nil {
		return err
	}
	for _, o := range activity.Outputs {
		if !o.Required || o.Type == "" || o.Attachment {
			continue
		}
		found := false
		for _, a := range all {
			found = found || (a.Type == o.Type && (a.State == domainartifact.StateInReview || a.State == domainartifact.StateApproved))
		}
		if !found {
			return nil
		}
	}
	if err := p.flow.SetStageState(ctx, projectID, activity.ID, domainworkflow.ActivityCompleted); err != nil {
		return err
	}
	return p.WaveCompleted(ctx, projectID, activity.RequiredFor, correlation)
}

func roundSubject(gate string, round int) string {
	return "gates/" + gate + "/rounds/" + itoa(round)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// evaluate runs the checks of the gate and applies complete_checks.
func (p *Progress) evaluate(ctx context.Context, projectID string, stage domainworkflow.Stage, round int, mode string, outputs []artifact.Stored,
	settings Settings, correlation string) error {
	gate := p.composition.Gates[stage.Gate]
	e := &evaluation{p: p, projectID: projectID, stage: stage, gate: gate, round: round, outputs: outputs}
	if err := e.load(ctx); err != nil {
		return err
	}
	var failed, humanRequired []string
	okChecks, notices, humanChecks := 0, 0, 0
	for _, spec := range p.composition.ChecksOf(stage.Gate, settings.Promoted) {
		result := CheckResult{Check: spec.ID, Type: spec.Type, Severity: spec.Severity}
		if spec.Type == governance.TypeHuman {
			result.Status = ResultPending
			humanChecks++
		} else {
			outcome, err := registry[spec.ID](ctx, e)
			if err != nil {
				return fmt.Errorf("check %s: %w", spec.ID, err)
			}
			result.Status = ResultPassed
			if len(outcome.evidence) > 0 {
				result.Evidence, _ = json.Marshal(outcome.evidence)
			}
			switch {
			case !outcome.passed():
				result.Status = ResultFailed
				if spec.Severity == governance.SeverityBlocks {
					failed = append(failed, spec.ID)
				} else {
					notices++
					if gate.RequiresHuman(spec.ID) {
						humanRequired = append(humanRequired, spec.ID)
					}
				}
			case len(outcome.evidence) > 0:
				notices++
				okChecks++
			default:
				okChecks++
			}
		}
		if err := p.store.RecordCheck(ctx, projectID, stage.ID, round, result); err != nil {
			return err
		}
	}
	blocking, nonBlocking := e.scopedFindings()
	subject := roundSubject(stage.Gate, round)
	if len(failed) > 0 || len(blocking) > 0 {
		if err := p.store.SetRoundState(ctx, projectID, stage.ID, round, StateRejected); err != nil {
			return err
		}
		return p.emit(ctx, projectID, correlation, "br.easycode.gate.rejected.v1", subject, map[string]any{
			"gate": stage.Gate, "round": round, "stage": stage.ID, "actor": orchestrator, "failed_checks": orEmpty(failed),
			"blocking_findings": blocking, "transition": transition("gate", StateEvaluating, StateRejected),
		})
	}
	if mode == governance.ModeAutomatic && len(humanRequired) == 0 && humanChecks == 0 {
		if err := p.store.SetRoundState(ctx, projectID, stage.ID, round, StateApproved); err != nil {
			return err
		}
		return p.emit(ctx, projectID, correlation, "br.easycode.gate.approved.v1", subject, map[string]any{
			"gate": stage.Gate, "round": round, "stage": stage.ID, "ok_checks": okChecks, "actor": orchestrator, "approvals": 1,
			"confirmed_human_checks": []string{}, "transition": transition("gate", StateEvaluating, StateApproved),
		})
	}
	if err := p.store.SetRoundState(ctx, projectID, stage.ID, round, StateAwaitingHuman); err != nil {
		return err
	}
	return p.emit(ctx, projectID, correlation, "br.easycode.gate.awaiting_approval.v1", subject, map[string]any{
		"gate": stage.Gate, "round": round, "stage": stage.ID, "actor": orchestrator, "ok_checks": okChecks, "notices": notices,
		"non_blocking_findings": nonBlocking, "transition": transition("gate", StateEvaluating, StateAwaitingHuman),
	})
}

func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// FindingOpened implements the gate side of easycode.finding.opened (US-31 CA-3): a critical or high
// finding opened in a stage whose round waits for the human makes the round evaluate again, so the
// approval is no longer available.
func (p *Progress) FindingOpened(ctx context.Context, projectID, originStage, finding, correlation string) error {
	stageID := p.definition.GateStage(originStage)
	stage, ok := p.definition.Stage(stageID)
	if !ok {
		return nil
	}
	round, state, err := p.store.ActiveRound(ctx, projectID, stageID)
	if err != nil || round == 0 || state != StateAwaitingHuman {
		return err
	}
	if err := p.store.Reevaluate(ctx, projectID, stageID, round, finding); err != nil {
		return err
	}
	if err := p.emit(ctx, projectID, correlation, "br.easycode.gate.reevaluation_started.v1", roundSubject(stage.Gate, round), map[string]any{
		"gate": stage.Gate, "round": round, "stage": stageID, "actor": orchestrator, "cause": "finding_opened", "finding": finding,
		"transition": transition("gate", StateAwaitingHuman, StateEvaluating),
	}); err != nil {
		return err
	}
	mode, err := p.store.RoundMode(ctx, projectID, stageID, round)
	if err != nil {
		return err
	}
	settings, err := p.store.Settings(ctx, projectID, stage.Gate)
	if err != nil {
		return err
	}
	outputs, err := p.artifacts.InReview(ctx, projectID, stageID)
	if err != nil {
		return err
	}
	return p.evaluate(ctx, projectID, stage, round, mode, outputs, settings, correlation)
}

// InternalCycle runs the checks of the internal cycle of the implementation (gates.yaml internal_cycle)
// on a rework wave: build, tests and lint green in the wave (finding.internal_cycle_green), without a
// round. It returns the failed blocking checks with their evidence.
func (p *Progress) InternalCycle(ctx context.Context, projectID, wave string) ([]string, []map[string]any, error) {
	executions, err := p.store.WaveExecutions(ctx, projectID, wave)
	if err != nil {
		return nil, nil, err
	}
	last := map[string]Execution{}
	for _, e := range executions {
		last[e.Category] = e
	}
	var failed []string
	var evidence []map[string]any
	for _, c := range []struct{ check, category string }{{CheckBuildGreen, categoryBuild}, {CheckTestsGreen, categoryTests}, {CheckLintClean, categoryLint}} {
		e, ok := last[c.category]
		if !ok || !e.Green() || (c.category == categoryTests && !testsGreen(e)) {
			failed = append(failed, c.check)
			evidence = append(evidence, map[string]any{"reference": wave, "message": c.category + " not green in the rework wave"})
		}
	}
	return failed, evidence, nil
}

// Reconcile applies the effect of decided rounds (stage approved or returned). It is idempotent:
// a stage leaves awaiting_gate in the same transaction.
func (p *Progress) Reconcile(ctx context.Context) (int, error) {
	decided, err := p.store.Decided(ctx)
	if err != nil {
		return 0, err
	}
	for _, d := range decided {
		correlation := roundCorrelation(d)
		err := p.tx.InTx(ctx, func(ctx context.Context) error {
			if d.State == StateApproved {
				return p.approved(ctx, d, correlation)
			}
			return p.rejected(ctx, d, correlation)
		})
		if err != nil {
			return 0, err
		}
	}
	return len(decided), nil
}

func (p *Progress) approved(ctx context.Context, d Decided, correlation string) error {
	stage, _ := p.definition.Stage(d.Stage)
	outputs, err := p.artifacts.InReview(ctx, d.ProjectID, d.Stage)
	if err != nil {
		return err
	}
	if err := p.flow.SetStageState(ctx, d.ProjectID, d.Stage, domainworkflow.StageApproved); err != nil {
		return err
	}
	if err := p.emit(ctx, d.ProjectID, correlation, "br.easycode.stage.approved.v1", "stages/"+d.Stage, map[string]any{
		"stage": d.Stage, "gate": stage.Gate, "round": d.Round, "actor": orchestrator,
		"transition": transition("stage", domainworkflow.StageAwaitingGate, domainworkflow.StageApproved),
	}); err != nil {
		return err
	}
	for _, output := range outputs {
		if err := p.artifacts.SetState(ctx, d.ProjectID, output.Code, domainartifact.StateApproved); err != nil {
			return err
		}
		if err := p.emit(ctx, d.ProjectID, correlation, "br.easycode.artifact.approved.v1", "artifacts/"+output.Code, map[string]any{
			"artifact": output.Code, "type": output.Type, "version": output.Version, "stage": d.Stage, "gate": stage.Gate, "round": d.Round,
			"actor": orchestrator, "transition": transition("artifact", domainartifact.StateInReview, domainartifact.StateApproved),
		}); err != nil {
			return err
		}
	}
	if p.hook != nil {
		if err := p.hook.RoundApproved(ctx, d.ProjectID, d.Stage, d.Round, correlation); err != nil {
			return err
		}
	}
	return p.flow.UnblockReady(ctx, d.ProjectID, correlation)
}

func (p *Progress) rejected(ctx context.Context, d Decided, correlation string) error {
	stage, _ := p.definition.Stage(d.Stage)
	outputs, err := p.artifacts.InReview(ctx, d.ProjectID, d.Stage)
	if err != nil {
		return err
	}
	if err := p.flow.SetStageState(ctx, d.ProjectID, d.Stage, domainworkflow.StageReturned); err != nil {
		return err
	}
	for _, output := range outputs {
		if err := p.artifacts.SetState(ctx, d.ProjectID, output.Code, domainartifact.StateDraft); err != nil {
			return err
		}
		if err := p.emit(ctx, d.ProjectID, correlation, "br.easycode.artifact.returned.v1", "artifacts/"+output.Code, map[string]any{
			"artifact": output.Code, "type": output.Type, "version": output.Version, "previous_version": output.Version, "stage": d.Stage,
			"cause": "gate_rejected", "gate": stage.Gate, "round": d.Round, "actor": orchestrator,
			"transition": transition("artifact", domainartifact.StateInReview, domainartifact.StateDraft),
		}); err != nil {
			return err
		}
	}
	// Routing by finding (MARCO-5): with a blocking finding of the stage active, the rework is the
	// finding cycle in its destination stage; the stage is returned without rework waves and is redone
	// when its findings stop blocking it (ResumeReturned). The other failed checks of the round are the
	// consequence of the open findings (a REL-QA reprovado for the failed CT, for instance).
	waves := []string{}
	findings, err := p.store.ProjectFindings(ctx, d.ProjectID)
	if err != nil {
		return err
	}
	blocked := false
	for _, f := range findings {
		blocked = blocked || (f.Blocking() && p.definition.GateStage(f.OriginStage) == d.Stage)
	}
	if !blocked {
		if waves, err = p.flow.QueueRework(ctx, d.ProjectID, d.Stage, correlation); err != nil {
			return err
		}
	}
	return p.emit(ctx, d.ProjectID, correlation, "br.easycode.stage.returned.v1", "stages/"+d.Stage, map[string]any{
		"stage": d.Stage, "gate": stage.Gate, "round": d.Round, "rework_waves": waves, "actor": orchestrator,
		"transition": transition("stage", domainworkflow.StageAwaitingGate, domainworkflow.StageReturned),
	})
}

// ResumeReturned redoes a stage returned only for blocking findings once none of its findings blocks it
// anymore: the stage agents run again over the corrected work (routing by finding).
func (p *Progress) ResumeReturned(ctx context.Context, projectID, stageID, correlation string) ([]string, error) {
	record, err := p.store.Stage(ctx, projectID, stageID)
	if err != nil || record.State != domainworkflow.StageReturned {
		return nil, err
	}
	active, err := p.store.ActiveWaves(ctx, projectID, stageID)
	if err != nil || active > 0 {
		return nil, err
	}
	findings, err := p.store.ProjectFindings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, f := range findings {
		if f.Blocking() && p.definition.GateStage(f.OriginStage) == stageID {
			return nil, nil
		}
	}
	return p.flow.QueueRework(ctx, projectID, stageID, correlation)
}

// roundCorrelation is stable per round, so the effects of a decision share it.
func roundCorrelation(d Decided) string {
	return deterministicUUID(d.ProjectID + "/" + d.Stage + "/" + itoa(d.Round))
}
