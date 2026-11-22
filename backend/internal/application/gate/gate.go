// Package gate is the minimal governance engine of MARCO-1 (ADR-10, interno.md section 5): after
// the last wave of a stage the orchestrator submits it to its gate, opens a round, runs the only
// automatic check of the marco (artifact.schema_valid) and waits for the human approver; the api
// applies approve and reject synchronously, and the worker reacts (stage approved and the next
// stages unblocked, US-05 CA-4; or stage returned with a rework wave).
package gate

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"easycode/internal/application/artifact"
	"easycode/internal/application/event"
	"easycode/internal/application/transaction"
	domainartifact "easycode/internal/domain/artifact"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	domainworkflow "easycode/internal/domain/workflow"
)

// Automatic checks of the gates (checagens.yaml). gate.requirements runs artifact.schema_valid
// (MARCO-1); MARCO-3 adds the traceability of the tasks to gate.architecture and the build and test
// evidence of the sandbox to gate.implementation; MARCO-4 adds the model references of gate.data, the
// screen ↔ story links of gate.design (US-21) and the opinions and verdict of gate.validation (US-20).
// The other checks of gates.yaml come with US-31.
const (
	CheckSchemaValid        = "artifact.schema_valid"
	CheckTasksCoverUS       = "traceability.t_points_to_us"
	CheckBuildGreen         = "code.build_green"
	CheckTestsGreen         = "tests.executed_green"
	CheckModelReferences    = "persist.model_references"
	CheckInterfaceUSScreen  = "traceability.interface_us_in_screen"
	CheckCriteriaConsistent = "telas.criteria_consistent"
	CheckOpinionsRecorded   = "validacao.review_opinions_recorded"
	CheckVerdictUnblocked   = "validacao.verdict_unblocked"
	categoryBuild           = "build"
	categoryTests           = "tests"
	stageArchitecture       = "architecture"
	stageData               = "data"
	stageDesign             = "design"
	stageValidation         = "validation"
	stageImplementation     = "implementation"
)

// Execution is the last sandbox execution of a category in the stage (evidence of US-41).
type Execution struct {
	Wave        string
	ID          string
	ExitCode    *int
	EndReason   string
	TestsTotal  *int
	TestsFailed *int
}

// Round states (machine gate).
const (
	StateEvaluating    = "evaluating"
	StateAwaitingHuman = "awaiting_human"
	StateApproved      = "approved"
	StateRejected      = "rejected"
)

// Decided is a round already decided whose stage still waits for the effect.
type Decided struct {
	ProjectID string
	Stage     string
	Round     int
	State     string
}

// Store is the gate persistence used by the worker.
type Store interface {
	// ActiveWaves counts the waves of the stage not completed (queued to paused, or failed).
	ActiveWaves(ctx context.Context, projectID, stage string) (int, error)
	// OpenRound creates the next round in evaluating with the outputs and returns its number.
	OpenRound(ctx context.Context, projectID, stage string, outputs []artifact.Stored) (int, error)
	RecordRoundCheck(ctx context.Context, projectID, stage string, round int, check string, passed bool, evidence json.RawMessage) error
	SetRoundState(ctx context.Context, projectID, stage string, round int, state string) error
	// Decided lists stages in awaiting_gate whose last round is approved or rejected.
	Decided(ctx context.Context) ([]Decided, error)
	// LastExecution returns the last execution of the category in the last completed wave of the
	// stage, or nil when the wave ran none.
	LastExecution(ctx context.Context, projectID, stage, category string) (*Execution, error)
	// ReviewOpinions returns, by agent, the opinion recorded by the last completed wave of each agent
	// in the stage (participants of the validation, "registro de ondas").
	ReviewOpinions(ctx context.Context, projectID, stage string) (map[string]domainworkflow.ReviewOpinion, error)
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

// Progress is the worker side of the gate.
type Progress struct {
	tx         transaction.Runner
	store      Store
	artifacts  Artifacts
	validator  HeaderValidator
	flow       StageFlow
	outbox     event.Outbox
	definition domainworkflow.Definition
}

// NewProgress builds the gate progress.
func NewProgress(tx transaction.Runner, store Store, artifacts Artifacts, validator HeaderValidator, flow StageFlow, outbox event.Outbox, definition domainworkflow.Definition) *Progress {
	return &Progress{tx: tx, store: store, artifacts: artifacts, validator: validator, flow: flow, outbox: outbox, definition: definition}
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

// WaveCompleted implements runtime.StageProgress: when every wave of the stage is done and the
// required outputs are in review, the stage goes to its gate (US-17 CA-3) and the round runs its
// checks; with all of them passing it waits for the human approval (gate.requirements: human).
func (p *Progress) WaveCompleted(ctx context.Context, projectID, stageID, correlation string) error {
	stage, ok := p.definition.Stage(stageID)
	if !ok {
		return nil
	}
	active, err := p.store.ActiveWaves(ctx, projectID, stageID)
	if err != nil || active > 0 {
		return err
	}
	outputs, err := p.artifacts.InReview(ctx, projectID, stageID)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	refs := make([]map[string]any, 0, len(outputs))
	for _, output := range outputs {
		present[output.Type] = true
		refs = append(refs, map[string]any{"artifact": output.Code, "version": output.Version})
	}
	for _, output := range stage.Outputs {
		if output.Required && output.Type != "" && !present[output.Type] {
			return nil // guard of submit_to_gate: required outputs present and in review
		}
	}
	round, err := p.store.OpenRound(ctx, projectID, stageID, outputs)
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
	subject := roundSubject(stage.Gate, round)
	if err := p.emit(ctx, projectID, correlation, "br.easycode.gate.opened.v1", subject, map[string]any{
		"gate": stage.Gate, "round": round, "stage": stageID, "approval_mode": "human", "outputs": refs, "actor": orchestrator,
		"transition": transition("gate", "", StateEvaluating),
	}); err != nil {
		return err
	}
	return p.evaluate(ctx, projectID, stage, round, outputs, correlation)
}

func roundSubject(gate string, round int) string {
	return "gates/" + gate + "/rounds/" + itoa(round)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

type checkOutcome struct {
	check    string
	evidence []map[string]any
}

// evaluate runs the automatic checks of the gate and applies complete_checks.
func (p *Progress) evaluate(ctx context.Context, projectID string, stage domainworkflow.Stage, round int, outputs []artifact.Stored, correlation string) error {
	headers := map[string]map[string]any{}
	schema := checkOutcome{check: CheckSchemaValid}
	for _, output := range outputs {
		header, err := p.artifacts.Header(ctx, projectID, output.Code)
		if err != nil {
			return err
		}
		headers[output.Code] = header
		problems, err := p.validator.Validate(output.Type, header)
		if err != nil {
			return err
		}
		for _, problem := range problems {
			schema.evidence = append(schema.evidence, map[string]any{"reference": output.Code, "path": problem.Path, "message": problem.Message})
		}
	}
	outcomes := []checkOutcome{schema}
	switch stage.ID {
	case stageArchitecture:
		tasks, err := p.tasksCoverStories(ctx, projectID, outputs, headers)
		if err != nil {
			return err
		}
		outcomes = append(outcomes, tasks)
	case stageData:
		outcomes = append(outcomes, modelReferences(outputs, headers))
	case stageDesign:
		links, err := p.screensLinkStories(ctx, projectID, outputs, headers)
		if err != nil {
			return err
		}
		outcomes = append(outcomes, links, criteriaConsistent(outputs, headers))
	case stageValidation:
		recorded, err := p.opinionsRecorded(ctx, projectID, stage, outputs, headers)
		if err != nil {
			return err
		}
		outcomes = append(outcomes, recorded, verdictUnblocked(outputs, headers))
	case stageImplementation:
		for _, c := range []struct{ check, category string }{{CheckBuildGreen, categoryBuild}, {CheckTestsGreen, categoryTests}} {
			outcome, err := p.executionGreen(ctx, projectID, stage.ID, c.check, c.category)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, outcome)
		}
	}
	var failed []string
	for _, outcome := range outcomes {
		passed := len(outcome.evidence) == 0
		var raw json.RawMessage
		if !passed {
			failed = append(failed, outcome.check)
			raw, _ = json.Marshal(outcome.evidence)
		}
		if err := p.store.RecordRoundCheck(ctx, projectID, stage.ID, round, outcome.check, passed, raw); err != nil {
			return err
		}
	}
	subject := roundSubject(stage.Gate, round)
	if len(failed) > 0 {
		if err := p.store.SetRoundState(ctx, projectID, stage.ID, round, StateRejected); err != nil {
			return err
		}
		return p.emit(ctx, projectID, correlation, "br.easycode.gate.rejected.v1", subject, map[string]any{
			"gate": stage.Gate, "round": round, "stage": stage.ID, "actor": orchestrator, "failed_checks": failed,
			"blocking_findings": []string{}, "transition": transition("gate", StateEvaluating, StateRejected),
		})
	}
	if err := p.store.SetRoundState(ctx, projectID, stage.ID, round, StateAwaitingHuman); err != nil {
		return err
	}
	return p.emit(ctx, projectID, correlation, "br.easycode.gate.awaiting_approval.v1", subject, map[string]any{
		"gate": stage.Gate, "round": round, "stage": stage.ID, "actor": orchestrator, "ok_checks": len(outcomes), "notices": 0,
		"non_blocking_findings": []string{}, "transition": transition("gate", StateEvaluating, StateAwaitingHuman),
	})
}

// tasksCoverStories is traceability.t_points_to_us (RAS-08, US-18 CA-1): every functional T covers
// at least one US, and each US and criterion it cites exists in the project.
func (p *Progress) tasksCoverStories(ctx context.Context, projectID string, outputs []artifact.Stored, headers map[string]map[string]any) (checkOutcome, error) {
	outcome := checkOutcome{check: CheckTasksCoverUS}
	stories := map[string]map[string]bool{}
	for _, output := range outputs {
		if output.Type != "T" {
			continue
		}
		header := headers[output.Code]
		nature, _ := header["natureza"].(string)
		covers, _ := header["cobre"].([]any)
		if len(covers) == 0 && (nature == "" || nature == "funcional") {
			outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/cobre", "message": "functional task covers no US"})
		}
		for _, item := range covers {
			entry, _ := item.(map[string]any)
			us, _ := entry["us"].(string)
			criteria, known := stories[us]
			if !known {
				story, err := p.artifacts.Header(ctx, projectID, us)
				if err != nil && failure.From(err).Kind != failure.KindNotFound {
					return outcome, err
				}
				criteria = map[string]bool{}
				if story != nil {
					list, _ := story["criterios_aceite"].([]any)
					for _, ca := range list {
						if m, ok := ca.(map[string]any); ok {
							id, _ := m["id"].(string)
							criteria[id] = true
						}
					}
				}
				stories[us] = criteria
			}
			if len(criteria) == 0 {
				outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/cobre", "message": "unknown US " + us})
				continue
			}
			cited, _ := entry["criterios"].([]any)
			for _, ca := range cited {
				if id, _ := ca.(string); !criteria[id] {
					outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/cobre", "message": us + " has no " + id})
				}
			}
		}
	}
	return outcome, nil
}

func list(header map[string]any, field string) []any {
	items, _ := header[field].([]any)
	return items
}

func text(value any) string {
	s, _ := value.(string)
	return s
}

// modelReferences is persist.model_references (RAS-14): every index of PERSIST points to a table of DIC
// and to columns of it.
func modelReferences(outputs []artifact.Stored, headers map[string]map[string]any) checkOutcome {
	outcome := checkOutcome{check: CheckModelReferences}
	tables := map[string]map[string]bool{}
	for _, output := range outputs {
		if output.Type != "DIC" {
			continue
		}
		for _, item := range list(headers[output.Code], "tabelas") {
			table, _ := item.(map[string]any)
			columns := map[string]bool{}
			for _, c := range list(table, "colunas") {
				column, _ := c.(map[string]any)
				columns[text(column["nome"])] = true
			}
			tables[text(table["nome"])] = columns
		}
	}
	for _, output := range outputs {
		if output.Type != "PERSIST" {
			continue
		}
		for _, item := range list(headers[output.Code], "indices") {
			index, _ := item.(map[string]any)
			name, table := text(index["nome"]), text(index["tabela"])
			columns, known := tables[table]
			if !known {
				outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/indices", "message": "index " + name + " on unknown table " + table})
				continue
			}
			for _, c := range list(index, "colunas") {
				if column := text(c); !columns[column] {
					outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/indices", "message": "index " + name + " on unknown column " + table + "." + column})
				}
			}
		}
	}
	return outcome
}

// screensLinkStories is traceability.interface_us_in_screen (RAS-17, US-21 CA-1, CA-2, CA-3): every US
// with tem_interface true (and not wont) is in at least one screen; a US without interface is not
// required; a screen whose stories do not exist in the project is pointed out as orphan.
func (p *Progress) screensLinkStories(ctx context.Context, projectID string, outputs []artifact.Stored, headers map[string]map[string]any) (checkOutcome, error) {
	outcome := checkOutcome{check: CheckInterfaceUSScreen}
	stories, err := p.artifacts.Headers(ctx, projectID, "US")
	if err != nil {
		return outcome, err
	}
	covered := map[string]bool{}
	for _, output := range outputs {
		if output.Type != "TELAS" {
			continue
		}
		for _, item := range list(headers[output.Code], "telas") {
			screen, _ := item.(map[string]any)
			id := text(screen["id"])
			known := 0
			for _, us := range list(screen, "us") {
				code := text(us)
				if _, ok := stories[code]; ok {
					covered[code] = true
					known++
				} else {
					outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code + "/" + id, "path": "/telas", "message": "screen cites unknown US " + code})
				}
			}
			if known == 0 {
				outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code + "/" + id, "path": "/telas", "message": "orphan screen: no US of the project"})
			}
		}
	}
	for code, story := range stories {
		if story["tem_interface"] == true && text(story["prioridade"]) != "wont" && !covered[code] {
			outcome.evidence = append(outcome.evidence, map[string]any{"reference": code, "path": "/tem_interface", "message": "US with interface in no screen"})
		}
	}
	sortEvidence(outcome.evidence)
	return outcome, nil
}

// criteriaConsistent is telas.criteria_consistent (RAS-18): the US of every criterion of a screen is a
// US of the same screen.
func criteriaConsistent(outputs []artifact.Stored, headers map[string]map[string]any) checkOutcome {
	outcome := checkOutcome{check: CheckCriteriaConsistent}
	for _, output := range outputs {
		if output.Type != "TELAS" {
			continue
		}
		for _, item := range list(headers[output.Code], "telas") {
			screen, _ := item.(map[string]any)
			own := map[string]bool{}
			for _, us := range list(screen, "us") {
				own[text(us)] = true
			}
			for _, c := range list(screen, "criterios") {
				criterion := text(c)
				if us, _, _ := strings.Cut(criterion, "/"); !own[us] {
					outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code + "/" + text(screen["id"]), "path": "/telas",
						"message": "criterion " + criterion + " of a US outside the screen"})
				}
			}
		}
	}
	return outcome
}

// opinionsRecorded is validacao.review_opinions_recorded: one opinion of each participant (arquiteto,
// dba, designer), each recorded in a completed wave of the matching agent and copied unchanged.
func (p *Progress) opinionsRecorded(ctx context.Context, projectID string, stage domainworkflow.Stage, outputs []artifact.Stored, headers map[string]map[string]any) (checkOutcome, error) {
	outcome := checkOutcome{check: CheckOpinionsRecorded}
	recorded, err := p.store.ReviewOpinions(ctx, projectID, stage.ID)
	if err != nil {
		return outcome, err
	}
	for _, output := range outputs {
		if output.Type != "VALIDACAO" {
			continue
		}
		given := map[string]map[string]any{}
		for _, item := range list(headers[output.Code], "pareceres") {
			entry, _ := item.(map[string]any)
			name := text(entry["participante"])
			if _, dup := given[name]; dup {
				outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/pareceres", "message": "repeated opinion of " + name})
			}
			given[name] = entry
		}
		for _, reviewer := range stage.Reviewers() {
			name := domainworkflow.ReviewerName(reviewer.Agent)
			opinion, ok := recorded[reviewer.Agent]
			entry, written := given[name]
			switch {
			case !ok:
				outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/pareceres", "message": "no completed wave of " + reviewer.Agent + " with its opinion"})
			case !written || text(entry["parecer"]) != opinion.Opinion:
				outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/pareceres", "message": "opinion of " + name + " differs from wave " + opinion.Wave})
			}
		}
		if _, ok := given[domainworkflow.ReviewerName(stage.Agent)]; !ok {
			outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/pareceres", "message": "no opinion of the lead"})
		}
	}
	return outcome, nil
}

// verdictUnblocked is validacao.verdict_unblocked: the VALIDACAO of the round has veredito liberado
// (a rejection by the DBA forces devolvido in the schema, US-20 RN-2).
func verdictUnblocked(outputs []artifact.Stored, headers map[string]map[string]any) checkOutcome {
	outcome := checkOutcome{check: CheckVerdictUnblocked}
	for _, output := range outputs {
		if output.Type == "VALIDACAO" && text(headers[output.Code]["veredito"]) != "liberado" {
			outcome.evidence = append(outcome.evidence, map[string]any{"reference": output.Code, "path": "/veredito", "message": "validation returned"})
		}
	}
	return outcome
}

func sortEvidence(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool { return text(items[i]["reference"]) < text(items[j]["reference"]) })
}

// executionGreen is code.build_green and tests.executed_green: the last command of the category in the
// last wave of the stage ran in the sandbox with exit code 0 and, for tests, at least one test run and
// none failed (US-19 CA-1, US-41).
func (p *Progress) executionGreen(ctx context.Context, projectID, stage, check, category string) (checkOutcome, error) {
	outcome := checkOutcome{check: check}
	last, err := p.store.LastExecution(ctx, projectID, stage, category)
	if err != nil {
		return outcome, err
	}
	switch {
	case last == nil:
		outcome.evidence = []map[string]any{{"message": category + " not executed in the sandbox"}}
	case last.ExitCode == nil || *last.ExitCode != 0:
		outcome.evidence = []map[string]any{{"reference": last.Wave, "execution": last.ID, "end_reason": last.EndReason, "message": category + " not green"}}
	case category == categoryTests && (last.TestsTotal == nil || *last.TestsTotal == 0 || (last.TestsFailed != nil && *last.TestsFailed > 0)):
		outcome.evidence = []map[string]any{{"reference": last.Wave, "execution": last.ID, "message": "no test executed or a test failed"}}
	}
	return outcome, nil
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
	return p.flow.UnblockReady(ctx, d.ProjectID, correlation)
}

func (p *Progress) rejected(ctx context.Context, d Decided, correlation string) error {
	stage, _ := p.definition.Stage(d.Stage)
	outputs, err := p.artifacts.InReview(ctx, d.ProjectID, d.Stage)
	if err != nil {
		return err
	}
	if err := p.flow.SetStageState(ctx, d.ProjectID, d.Stage, "returned"); err != nil {
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
	waves, err := p.flow.QueueRework(ctx, d.ProjectID, d.Stage, correlation)
	if err != nil {
		return err
	}
	return p.emit(ctx, d.ProjectID, correlation, "br.easycode.stage.returned.v1", "stages/"+d.Stage, map[string]any{
		"stage": d.Stage, "gate": stage.Gate, "round": d.Round, "rework_waves": waves, "actor": orchestrator,
		"transition": transition("stage", domainworkflow.StageAwaitingGate, "returned"),
	})
}

// roundCorrelation is stable per round, so the effects of a decision share it.
func roundCorrelation(d Decided) string {
	return deterministicUUID(d.ProjectID + "/" + d.Stage + "/" + itoa(d.Round))
}
