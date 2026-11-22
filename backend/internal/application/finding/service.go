// Package finding is the finding machine (ACH, specs/dominio/estados/achado.md) in the worker: it
// records the ACH an agent wrote, starts the rework wave in the destination stage (reopening the stage
// and the approved artifact when the destination is not the internal cycle of the implementation,
// US-34), sends the corrected work to the revalidation of the author, closes, cycles or escalates the
// finding (US-32), and applies the effects of the human decisions the api records (US-33).
package finding

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"easycode/internal/application/artifact"
	"easycode/internal/application/event"
	"easycode/internal/application/runtime"
	"easycode/internal/application/transaction"
	appwave "easycode/internal/application/wave"
	domainartifact "easycode/internal/domain/artifact"
	domainevent "easycode/internal/domain/event"
	domainfinding "easycode/internal/domain/finding"
	domainworkflow "easycode/internal/domain/workflow"
)

// NewFinding is the row of a recorded ACH.
type NewFinding struct {
	ProjectID, Code, OriginWave, OriginStage, Destination, Severity, Category string
	Affected                                                                  []string
}

// Waiting is a stage returned for findings, or a finding whose human decision has an effect pending.
type Waiting struct {
	ProjectID string
	Stage     string
	Finding   string
}

// Store is the persistence of the finding machine (worker role).
type Store interface {
	Insert(ctx context.Context, f NewFinding) error
	// Get locks and returns a finding (nil when the code is not a finding).
	Get(ctx context.Context, projectID, code string) (*domainfinding.Finding, error)
	SetStatus(ctx context.Context, projectID, code, status, resolution, discardReason string) error
	StartCycle(ctx context.Context, projectID, code string, number int, reworkWave string) error
	// ReplaceRework points the running cycle to a new rework wave (internal cycle not green).
	ReplaceRework(ctx context.Context, projectID, code string, number int, reworkWave string) error
	SendToRevalidation(ctx context.Context, projectID, code string, number int, commit, revalidationWave string) error
	RecordResult(ctx context.Context, projectID, code string, number int, result string) error
	Escalate(ctx context.Context, projectID, code, reason string) error
	SetModifiedArtifacts(ctx context.Context, projectID, code string, artifacts []string) error
	// WaveFinding returns the ACH of a rework or revalidation wave ("" when none).
	WaveFinding(ctx context.Context, projectID, wave string) (string, error)
	// ReworkWaves counts the rework waves of a finding.
	ReworkWaves(ctx context.Context, projectID, code string) (int, error)
	StageState(ctx context.Context, projectID, stage string) (string, error)
	// ReopenStage applies approved → reopened with the finding; InvalidateRound marks the approved (or
	// active) round of the stage invalidated and returns its number (0 when none).
	ReopenStage(ctx context.Context, projectID, stage, finding string) error
	InvalidateRound(ctx context.Context, projectID, stage string, states []string) (int, error)
	// BlockStage moves the stage to blocked and asks its running waves to pause (pause_by_policy).
	BlockStage(ctx context.Context, projectID, stage string) ([]string, error)
	SetReevaluation(ctx context.Context, projectID, stage string, pending bool) error
	// ActiveWaves lists the rework and revalidation waves of a finding not finished.
	ActiveWaves(ctx context.Context, projectID, code string) ([]string, error)
	// Pending lists the effects of human decisions not applied yet: findings in_rework without a running
	// cycle (extra round authorized), accepted or discarded findings with active waves, findings
	// awaiting_scope whose requirements stage is still approved; and stages returned for findings with
	// no active wave.
	Pending(ctx context.Context) (extra, closed, scope []Waiting, returned []Waiting, err error)
	// RoundOutputs are the artifacts approved in a round.
	RoundOutputs(ctx context.Context, projectID, stage string, round int) ([]string, error)
	// AwaitingScope lists the findings awaiting a scope change decided before the round opened.
	AwaitingScope(ctx context.Context, projectID, stage string, round int) ([]string, error)
}

// Flow queues the waves of the findings.
type Flow interface {
	QueueWave(ctx context.Context, projectID, stageID, agent, purpose, finding, correlation string) (string, error)
}

// Gate is the gate side the findings drive.
type Gate interface {
	FindingOpened(ctx context.Context, projectID, originStage, finding, correlation string) error
	InternalCycle(ctx context.Context, projectID, wave string) ([]string, []map[string]any, error)
	ResumeReturned(ctx context.Context, projectID, stageID, correlation string) ([]string, error)
}

// Artifacts reads and reopens artifacts.
type Artifacts interface {
	Current(ctx context.Context, projectID, code string) (*artifact.Current, error)
	Reopen(ctx context.Context, projectID, code, waveCode, agent, finding string) (int, error)
}

// Canceler cancels a wave by policy (cancel_by_policy).
type Canceler interface {
	Cancel(ctx context.Context, projectID, waveCode string) (appwave.Canceled, error)
}

// Service is the worker side of the finding machine.
type Service struct {
	tx         transaction.Runner
	store      Store
	artifacts  Artifacts
	flow       Flow
	gate       Gate
	waves      Canceler
	outbox     event.Outbox
	definition domainworkflow.Definition
	now        func() time.Time
}

// NewService builds the service.
func NewService(tx transaction.Runner, store Store, artifacts Artifacts, flow Flow, gate Gate, waves Canceler, outbox event.Outbox,
	definition domainworkflow.Definition) *Service {
	return &Service{tx: tx, store: store, artifacts: artifacts, flow: flow, gate: gate, waves: waves, outbox: outbox, definition: definition, now: time.Now}
}

var orchestrator = map[string]any{"type": "orchestrator", "id": "orchestrator"}

// maxInternalRetries bounds the rework waves of one cycle whose internal checks (build, tests, lint)
// were not green.
const maxInternalRetries = 3

func (s *Service) emit(ctx context.Context, projectID, correlation, eventType, subject string, data map[string]any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.outbox.Append(ctx, domainevent.Draft{Type: eventType, Subject: subject, ProjectID: projectID, CorrelationID: correlation, Data: encoded})
	return err
}

func transition(machine, from, to string) map[string]any {
	var fromState any = from
	if from == "" {
		fromState = nil
	}
	return map[string]any{"machine": machine, "from_state": fromState, "to_state": to}
}

func base(f *domainfinding.Finding, from, to string) map[string]any {
	return map[string]any{"finding": f.Code, "severity": f.Severity, "category": f.Category, "destination_stage": f.DestinationStage,
		"origin_stage": f.OriginStage, "author": f.Author, "actor": orchestrator, "transition": transition("finding", from, to)}
}

func with(data map[string]any, extra map[string]any) map[string]any {
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// WaveCompleted implements runtime.FindingProgress, in the transaction of the wave completion.
func (s *Service) WaveCompleted(ctx context.Context, o runtime.WaveOutcome) error {
	for _, code := range o.Findings {
		if err := s.register(ctx, o, code); err != nil {
			return err
		}
	}
	switch o.Purpose {
	case runtime.PurposeRework:
		return s.reworkCompleted(ctx, o)
	case runtime.PurposeRevalidation:
		return s.revalidated(ctx, o)
	case runtime.PurposeReevaluation:
		if err := s.store.SetReevaluation(ctx, o.ProjectID, o.Stage, false); err != nil {
			return err
		}
		return s.emit(ctx, o.ProjectID, o.Correlation, "br.easycode.stage.reevaluated.v1", "stages/"+o.Stage, map[string]any{
			"stage": o.Stage, "wave": o.Wave, "actor": orchestrator, "version_inputs": []any{},
			"transition": transition("stage", domainworkflow.StageApproved, domainworkflow.StageApproved),
		})
	}
	return nil
}

// register records an ACH written by a wave (finding.opened): discarded when without evidence (US-32
// CA-3), else the rework starts (CA-1).
func (s *Service) register(ctx context.Context, o runtime.WaveOutcome, code string) error {
	if existing, err := s.store.Get(ctx, o.ProjectID, code); err != nil || existing != nil {
		return err // an ACH rewritten by its author is the same finding
	}
	current, err := s.artifacts.Current(ctx, o.ProjectID, code)
	if err != nil || current == nil {
		return err
	}
	h := current.Header
	text := func(key string) string { v, _ := h[key].(string); return v }
	var affected []string
	if items, ok := h["artefatos_afetados"].([]any); ok {
		for _, item := range items {
			if ref, ok := item.(string); ok && strings.TrimSpace(ref) != "" {
				affected = append(affected, ref)
			}
		}
	}
	nf := NewFinding{ProjectID: o.ProjectID, Code: code, OriginWave: o.Wave, OriginStage: o.Stage, Severity: domainfinding.Severity(text("severidade")),
		Category: domainfinding.Category(text("categoria")), Destination: domainfinding.Stage(text("etapa_destino")), Affected: affected}
	if err := s.store.Insert(ctx, nf); err != nil {
		return err
	}
	f, err := s.store.Get(ctx, o.ProjectID, code)
	if err != nil || f == nil {
		return err
	}
	refs := make([]map[string]any, 0, len(affected))
	for _, ref := range affected {
		refs = append(refs, map[string]any{"reference": ref})
	}
	if err := s.emit(ctx, o.ProjectID, o.Correlation, "br.easycode.finding.opened.v1", "findings/"+code, with(base(f, "", domainfinding.StatusOpen), map[string]any{
		"actor": map[string]any{"type": "agent", "id": o.Agent}, "affected_artifacts": refs, "wave": o.Wave, "blocking": f.Blocking(),
	})); err != nil {
		return err
	}
	if domainfinding.WithoutEvidence(text("evidencia")) {
		return s.discard(ctx, f, "sem evidência", o.Correlation)
	}
	if f.Blocking() {
		if err := s.gate.FindingOpened(ctx, o.ProjectID, f.OriginStage, f.Code, o.Correlation); err != nil {
			return err
		}
	}
	return s.startRework(ctx, f, 1, o.Correlation)
}

func (s *Service) discard(ctx context.Context, f *domainfinding.Finding, reason, correlation string) error {
	if err := s.store.SetStatus(ctx, f.ProjectID, f.Code, domainfinding.StatusDiscarded, "", reason); err != nil {
		return err
	}
	return s.emit(ctx, f.ProjectID, correlation, "br.easycode.finding.discarded.v1", "findings/"+f.Code, with(base(f, f.Status, domainfinding.StatusDiscarded),
		map[string]any{"reason": "no_evidence", "canceled_waves": []string{}}))
}

// reworkStage is the stage (or activity) whose agent corrects the finding: the destination, or, for the
// validation, the producer of the affected artifact (CT: QA plan; AMEACAS: threat model).
func (s *Service) reworkStage(f *domainfinding.Finding) string {
	if f.DestinationStage == "validation" {
		for _, ref := range f.Affected {
			switch domainfinding.ArtifactOf(ref) {
			case "AMEACAS":
				if _, ok := s.definition.Activity("security-threats"); ok {
					return "security-threats"
				}
			default:
				if strings.HasPrefix(domainfinding.ArtifactOf(ref), "CT-") {
					if _, ok := s.definition.Activity("qa-plan"); ok {
						return "qa-plan"
					}
				}
			}
		}
	}
	return f.DestinationStage
}

// startRework is start_rework (cycle 1) or a new cycle after not_fixed: the rework wave of the
// destination agent; outside the internal cycle, the approved affected artifacts and the destination
// stage are reopened and the dependent stages blocked or marked for reevaluation (US-34).
func (s *Service) startRework(ctx context.Context, f *domainfinding.Finding, number int, correlation string) error {
	stage := s.reworkStage(f)
	agent := s.definition.AgentOf(stage)
	if agent == "" {
		return nil // destination out of the workflow in force: the finding waits for a human decision
	}
	internal := s.definition.Returns.Internal(f.DestinationStage)
	wave, err := s.flow.QueueWave(ctx, f.ProjectID, stage, agent, runtime.PurposeRework, f.Code, correlation)
	if err != nil {
		return err
	}
	if err := s.store.StartCycle(ctx, f.ProjectID, f.Code, number, wave); err != nil {
		return err
	}
	from := f.Status
	if err := s.store.SetStatus(ctx, f.ProjectID, f.Code, domainfinding.StatusInRework, "", ""); err != nil {
		return err
	}
	if err := s.emit(ctx, f.ProjectID, correlation, "br.easycode.finding.rework_started.v1", "findings/"+f.Code, with(base(f, from, domainfinding.StatusInRework),
		map[string]any{"cycle": number, "rework_wave": wave, "internal_cycle": internal, "extra_round_authorized": number > domainfinding.CycleLimit})); err != nil {
		return err
	}
	if internal {
		return nil
	}
	return s.reopen(ctx, f, agent, wave, correlation)
}

// reopen takes the approved affected artifacts back to draft in a new version and reopens their stage.
func (s *Service) reopen(ctx context.Context, f *domainfinding.Finding, agent, wave, correlation string) error {
	projectID := f.ProjectID
	var reopened []map[string]any
	for _, ref := range f.Affected {
		code := domainfinding.ArtifactOf(ref)
		if code == "" {
			continue
		}
		current, err := s.artifacts.Current(ctx, projectID, code)
		if err != nil {
			return err
		}
		if current == nil || current.State != domainartifact.StateApproved || ownerOf(current.Type) != f.DestinationStage {
			continue
		}
		version, err := s.artifacts.Reopen(ctx, projectID, code, wave, agent, f.Code)
		if err != nil {
			return err
		}
		reopened = append(reopened, map[string]any{"artifact": code, "version": version})
		if err := s.emit(ctx, projectID, correlation, "br.easycode.artifact.reopened.v1", "artifacts/"+code, map[string]any{
			"artifact": code, "type": current.Type, "version": version, "previous_version": current.Version, "stage": f.DestinationStage,
			"finding": f.Code, "actor": orchestrator, "transition": transition("artifact", domainartifact.StateApproved, domainartifact.StateDraft),
		}); err != nil {
			return err
		}
	}
	state, err := s.store.StageState(ctx, projectID, f.DestinationStage)
	if err != nil || state != domainworkflow.StageApproved {
		return err
	}
	if err := s.store.ReopenStage(ctx, projectID, f.DestinationStage, f.Code); err != nil {
		return err
	}
	destination, _ := s.definition.Stage(f.DestinationStage)
	round, err := s.store.InvalidateRound(ctx, projectID, f.DestinationStage, []string{"approved"})
	if err != nil {
		return err
	}
	if round > 0 {
		if err := s.emit(ctx, projectID, correlation, "br.easycode.gate.invalidated.v1", "gates/"+destination.Gate+"/rounds/"+itoa(round), map[string]any{
			"gate": destination.Gate, "round": round, "stage": f.DestinationStage, "cause": "stage_reopened", "actor": orchestrator,
			"transition": transition("gate", "approved", "invalidated"),
		}); err != nil {
			return err
		}
	}
	if reopened == nil {
		reopened = []map[string]any{}
	}
	if err := s.emit(ctx, projectID, correlation, "br.easycode.stage.reopened.v1", "stages/"+f.DestinationStage, map[string]any{
		"stage": f.DestinationStage, "finding": f.Code, "reopened_artifacts": reopened, "gate": destination.Gate, "invalidated_round": round,
		"actor": orchestrator, "transition": transition("stage", domainworkflow.StageApproved, domainworkflow.StageReopened),
	}); err != nil {
		return err
	}
	return s.affectDependents(ctx, f, correlation)
}

// affectDependents blocks the dependents of the reopened stage that are not approved (their running
// waves pause by policy) and marks the approved ones for reevaluation; the stage that emitted the
// finding is not blocked (US-34 CA-3).
func (s *Service) affectDependents(ctx context.Context, f *domainfinding.Finding, correlation string) error {
	projectID := f.ProjectID
	emitter := map[string]bool{f.OriginStage: true}
	for _, dependent := range s.definition.Dependents(f.DestinationStage) {
		if emitter[dependent] {
			continue
		}
		state, err := s.store.StageState(ctx, projectID, dependent)
		if err != nil {
			return err
		}
		_, isActivity := s.definition.Activity(dependent)
		machine := "stage"
		if isActivity {
			machine = "activity"
		}
		switch state {
		case domainworkflow.StageApproved:
			if err := s.store.SetReevaluation(ctx, projectID, dependent, true); err != nil {
				return err
			}
			if err := s.emit(ctx, projectID, correlation, "br.easycode.stage.reevaluation_pending.v1", "stages/"+dependent, map[string]any{
				"stage": dependent, "stage_reopened": f.DestinationStage, "finding": f.Code, "actor": orchestrator,
				"transition": transition("stage", domainworkflow.StageApproved, domainworkflow.StageApproved),
			}); err != nil {
				return err
			}
		case domainworkflow.StageReady, domainworkflow.StageRunning, domainworkflow.StageAwaitingGate, domainworkflow.StageReturned,
			domainworkflow.StageReopened, domainworkflow.ActivityCompleted:
			if _, err := s.store.InvalidateRound(ctx, projectID, dependent, []string{"evaluating", "awaiting_human"}); err != nil {
				return err
			}
			paused, err := s.store.BlockStage(ctx, projectID, dependent)
			if err != nil {
				return err
			}
			if paused == nil {
				paused = []string{}
			}
			if err := s.emit(ctx, projectID, correlation, "br.easycode.stage.blocked.v1", "stages/"+dependent, map[string]any{
				"stage": dependent, "stage_reopened": f.DestinationStage, "finding": f.Code, "paused_waves": paused, "actor": orchestrator,
				"transition": transition(machine, state, domainworkflow.StageBlocked),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func ownerOf(artifactType string) string { return domainartifact.OwnerStage(artifactType) }

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// reworkCompleted is in_rework → revalidating on the completion of the rework wave: with destination
// implementation the internal cycle checks (build, tests and lint green) must pass first (US-32 CA-5).
func (s *Service) reworkCompleted(ctx context.Context, o runtime.WaveOutcome) error {
	code, err := s.store.WaveFinding(ctx, o.ProjectID, o.Wave)
	if err != nil || code == "" {
		return err
	}
	f, err := s.store.Get(ctx, o.ProjectID, code)
	if err != nil || f == nil || f.Status != domainfinding.StatusInRework {
		return err
	}
	cycle := f.LastCycle()
	if cycle == nil || cycle.ReworkWave != o.Wave {
		return nil
	}
	if s.definition.Returns.Internal(f.DestinationStage) {
		failed, _, err := s.gate.InternalCycle(ctx, o.ProjectID, o.Wave)
		if err != nil {
			return err
		}
		if len(failed) > 0 {
			waves, err := s.store.ReworkWaves(ctx, o.ProjectID, f.Code)
			if err != nil {
				return err
			}
			if waves >= len(f.Cycles)+maxInternalRetries {
				// The machine only escalates from revalidating: after the retries the finding stays in_rework,
				// blocking, for a human decision (accept the risk or discard).
				return nil
			}
			stage := s.reworkStage(f)
			wave, err := s.flow.QueueWave(ctx, o.ProjectID, stage, s.definition.AgentOf(stage), runtime.PurposeRework, f.Code, o.Correlation)
			if err != nil {
				return err
			}
			return s.store.ReplaceRework(ctx, o.ProjectID, f.Code, cycle.Number, wave)
		}
	}
	version := o.Commit
	if version == "" {
		version = "onda " + o.Wave
	}
	revalidation, err := s.flow.QueueWave(ctx, o.ProjectID, f.OriginStage, f.Author, runtime.PurposeRevalidation, f.Code, o.Correlation)
	if err != nil {
		return err
	}
	if err := s.store.SendToRevalidation(ctx, o.ProjectID, f.Code, cycle.Number, version, revalidation); err != nil {
		return err
	}
	if err := s.store.SetStatus(ctx, o.ProjectID, f.Code, domainfinding.StatusRevalidating, "", ""); err != nil {
		return err
	}
	return s.emit(ctx, o.ProjectID, o.Correlation, "br.easycode.finding.revalidation_started.v1", "findings/"+f.Code,
		with(base(f, domainfinding.StatusInRework, domainfinding.StatusRevalidating), map[string]any{"cycle": cycle.Number, "revalidation_wave": revalidation,
			"commit_or_version": version}))
}

// revalidated is record_revalidation: fixed closes the finding; not_fixed starts the next cycle or,
// past the limit (3 plus the extra rounds a human authorized), escalates (US-32 CA-2, CA-4).
func (s *Service) revalidated(ctx context.Context, o runtime.WaveOutcome) error {
	code, err := s.store.WaveFinding(ctx, o.ProjectID, o.Wave)
	if err != nil || code == "" {
		return err
	}
	f, err := s.store.Get(ctx, o.ProjectID, code)
	if err != nil || f == nil || f.Status != domainfinding.StatusRevalidating {
		return err
	}
	cycle := f.LastCycle()
	if cycle == nil || cycle.RevalidationWave != o.Wave {
		return nil
	}
	result := domainfinding.ResultNotFixed
	if o.Revalidation != nil && o.Revalidation.Result == domainfinding.ResultFixed {
		result = domainfinding.ResultFixed
	}
	if err := s.store.RecordResult(ctx, o.ProjectID, f.Code, cycle.Number, result); err != nil {
		return err
	}
	f.Cycles[len(f.Cycles)-1].Result = result
	if result == domainfinding.ResultFixed {
		if err := s.store.SetStatus(ctx, o.ProjectID, f.Code, domainfinding.StatusClosed, domainfinding.ResolutionFixed, ""); err != nil {
			return err
		}
		if err := s.emit(ctx, o.ProjectID, o.Correlation, "br.easycode.finding.closed.v1", "findings/"+f.Code,
			with(base(f, domainfinding.StatusRevalidating, domainfinding.StatusClosed), map[string]any{"actor": map[string]any{"type": "agent", "id": o.Agent},
				"resolution": domainfinding.ResolutionFixed, "cycles": len(f.Cycles)})); err != nil {
			return err
		}
		_, err := s.gate.ResumeReturned(ctx, o.ProjectID, s.definition.GateStage(f.OriginStage), o.Correlation)
		return err
	}
	if len(f.Cycles) < domainfinding.CycleLimit+f.ExtraRounds() {
		return s.startRework(ctx, f, len(f.Cycles)+1, o.Correlation)
	}
	return s.escalate(ctx, f, "3 ciclos sem correção", o.Correlation)
}

func (s *Service) escalate(ctx context.Context, f *domainfinding.Finding, reason, correlation string) error {
	if err := s.store.Escalate(ctx, f.ProjectID, f.Code, reason); err != nil {
		return err
	}
	from := f.Status
	if err := s.store.SetStatus(ctx, f.ProjectID, f.Code, domainfinding.StatusEscalated, "", ""); err != nil {
		return err
	}
	return s.emit(ctx, f.ProjectID, correlation, "br.easycode.finding.escalated.v1", "findings/"+f.Code,
		with(base(f, from, domainfinding.StatusEscalated), map[string]any{"cycles": len(f.Cycles), "reason": reason}))
}

// RoundApproved implements gate.RoundHook: a gate.requirements approved after a scope change decision
// closes the finding with resolution scope_change and the requirements artifacts it approved (PD-06).
func (s *Service) RoundApproved(ctx context.Context, projectID, stage string, round int, correlation string) error {
	if stage != "requirements" {
		return nil
	}
	codes, err := s.store.AwaitingScope(ctx, projectID, stage, round)
	if err != nil || len(codes) == 0 {
		return err
	}
	outputs, err := s.store.RoundOutputs(ctx, projectID, stage, round)
	if err != nil {
		return err
	}
	for _, code := range codes {
		f, err := s.store.Get(ctx, projectID, code)
		if err != nil || f == nil {
			return err
		}
		if err := s.store.SetModifiedArtifacts(ctx, projectID, code, outputs); err != nil {
			return err
		}
		if err := s.store.SetStatus(ctx, projectID, code, domainfinding.StatusClosed, domainfinding.ResolutionScopeChange, ""); err != nil {
			return err
		}
		if err := s.emit(ctx, projectID, correlation, "br.easycode.finding.closed.v1", "findings/"+code,
			with(base(f, domainfinding.StatusAwaitingScope, domainfinding.StatusClosed), map[string]any{"resolution": domainfinding.ResolutionScopeChange,
				"cycles": len(f.Cycles), "modified_artifacts": outputs})); err != nil {
			return err
		}
	}
	return nil
}

// Reconcile applies the effects of the human decisions recorded by the api (extra round, risk accepted,
// discard, scope change) and redoes the stages returned for findings that no longer block them.
// It is idempotent: each effect changes the state the query reads.
func (s *Service) Reconcile(ctx context.Context) (int, error) {
	extra, closed, scope, returned, err := s.store.Pending(ctx)
	if err != nil {
		return 0, err
	}
	applied := 0
	run := func(effect func(ctx context.Context, correlation string) error) error {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		correlation := id.String()
		if err := s.tx.InTx(ctx, func(ctx context.Context) error { return effect(ctx, correlation) }); err != nil {
			return err
		}
		applied++
		return nil
	}
	for _, w := range extra {
		if err := run(func(ctx context.Context, correlation string) error {
			f, err := s.store.Get(ctx, w.ProjectID, w.Finding)
			if err != nil || f == nil || f.Status != domainfinding.StatusInRework {
				return err
			}
			return s.startRework(ctx, f, len(f.Cycles)+1, correlation)
		}); err != nil {
			return applied, err
		}
	}
	for _, w := range closed {
		if err := run(func(ctx context.Context, correlation string) error {
			waves, err := s.store.ActiveWaves(ctx, w.ProjectID, w.Finding)
			if err != nil {
				return err
			}
			for _, code := range waves {
				canceled, err := s.waves.Cancel(ctx, w.ProjectID, code)
				if err != nil {
					return err
				}
				if err := s.emit(ctx, w.ProjectID, correlation, "br.easycode.wave.canceled.v1", "waves/"+code, map[string]any{
					"wave": code, "stage": canceled.Stage, "agent": canceled.Agent, "purpose": canceled.Purpose, "cause": "finding_closed", "actor": orchestrator,
					"transition": transition("wave", canceled.From, "canceled"),
				}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return applied, err
		}
	}
	for _, w := range scope {
		if err := run(func(ctx context.Context, correlation string) error { return s.scopeChange(ctx, w, correlation) }); err != nil {
			return applied, err
		}
	}
	for _, w := range returned {
		if err := run(func(ctx context.Context, correlation string) error {
			_, err := s.gate.ResumeReturned(ctx, w.ProjectID, w.Stage, correlation)
			return err
		}); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

// scopeChange reopens the requirements stage and the starting point artifacts of the decision; with
// requirements-agent a rework wave of Requisitos changes them (decide_scope_change, PD-06).
func (s *Service) scopeChange(ctx context.Context, w Waiting, correlation string) error {
	f, err := s.store.Get(ctx, w.ProjectID, w.Finding)
	if err != nil || f == nil || f.Status != domainfinding.StatusAwaitingScope || len(f.Escalations) == 0 {
		return err
	}
	decision := f.Escalations[len(f.Escalations)-1]
	stage := "requirements"
	agent := s.definition.AgentOf(stage)
	wave := ""
	if decision.ModifiedBy == "requirements-agent" {
		if wave, err = s.flow.QueueWave(ctx, w.ProjectID, stage, agent, runtime.PurposeRework, f.Code, correlation); err != nil {
			return err
		}
	}
	scoped := *f
	scoped.Affected, scoped.DestinationStage = decision.StartingPoint, stage
	if wave == "" {
		// human edition (US-53): the stage and the artifacts reopen; the edits arrive by editArtifact
		return s.store.ReopenStage(ctx, w.ProjectID, stage, f.Code)
	}
	return s.reopen(ctx, &scoped, agent, wave, correlation)
}
