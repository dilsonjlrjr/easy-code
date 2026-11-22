package gate

import (
	"context"
	"path"
	"sort"
	"strings"

	domainartifact "easycode/internal/domain/artifact"
	domainfinding "easycode/internal/domain/finding"
	domainworkflow "easycode/internal/domain/workflow"
)

// Check ids implemented by the engine (checagens.yaml). Every automatic check of the gates of the
// workflow in force has an entry in registry; NewProgress refuses to start otherwise (ADR-10).
const (
	CheckSchemaValid        = "artifact.schema_valid"
	CheckTasksCoverUS       = "traceability.t_points_to_us"
	CheckBuildGreen         = "code.build_green"
	CheckTestsGreen         = "tests.executed_green"
	CheckLintClean          = "code.lint_clean"
	CheckModelReferences    = "persist.model_references"
	CheckInterfaceUSScreen  = "traceability.interface_us_in_screen"
	CheckCriteriaConsistent = "telas.criteria_consistent"
	CheckOpinionsRecorded   = "validacao.review_opinions_recorded"
	CheckVerdictUnblocked   = "validacao.verdict_unblocked"
	CheckInternalCycleGreen = "finding.internal_cycle_green"
	categoryBuild           = "build"
	categoryTests           = "tests"
	categoryLint            = "lint"
	categoryValidateDDL     = "validate_ddl"
	categoryScanner         = "scanner"
)

var registry = map[string]checkFunc{}

func init() {
	for id, f := range map[string]checkFunc{
		CheckSchemaValid:                            schemaValid,
		"artifact.history_has_current_version":      historyHasCurrentVersion,
		"artifact.ids_unique":                       idsUnique,
		"artifact.author_consistent":                authorConsistent,
		"artifact.no_foreign_approved_modification": noForeignApprovedModification,
		"stage.required_outputs_present":            requiredOutputsPresent,
		"stage.inputs_approved":                     inputsApproved,
		"wave.limits_respected":                     limitsRespected,
		"wave.record_complete":                      recordComplete,
		"project.history_preserved":                 historyPreserved,
		"traceability.existing_references":          existingReferences,
		"traceability.no_reference_to_replaced":     noReferenceToReplaced,
		"traceability.replacement_reciprocal":       replacementReciprocal,
		"traceability.rework_recorded":              reworkRecorded,
		CheckFindingsBlocking:                       noOpenBlocking,
		"finding.no_open_medium_low":                noOpenMediumLow,
		"finding.destination_consistent":            destinationConsistent,
		"finding.design_points_to_screens":          designPointsToScreens,
		"finding.cycles_consistent":                 cyclesConsistent,
		CheckInternalCycleGreen:                     internalCycleGreen,
		"finding.escalation_consistent":             escalationConsistent,
		"finding.extra_round_authorized":            extraRoundAuthorized,
		"finding.closing_consistent":                closingConsistent,
		"finding.human_acceptance":                  humanAcceptance,
		"finding.discard_consistent":                discardConsistent,
	} {
		registry[id] = f
	}
}

func schemaValid(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, r := range e.outputRecords() {
		problems, err := e.p.validator.Validate(r.Type, r.Header)
		if err != nil {
			return o, err
		}
		for _, problem := range problems {
			o.fail(r.Code, problem.Path, problem.Message)
		}
	}
	return o, nil
}

// historyHasCurrentVersion: the history goes from 1 to the current version without gap and the
// version born from a finding rework cites the finding.
func historyHasCurrentVersion(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, r := range e.outputRecords() {
		seen := map[int]bool{}
		last, achado := 0, ""
		for _, item := range list(r.Header, "historico") {
			entry := object(item)
			v := number(entry["versao"])
			if seen[v] || v <= last {
				o.fail(r.Code, "/historico", "repeated or decreasing version "+itoa(v))
			}
			seen[v], last = true, v
			if v == r.Version {
				achado = text(entry["achado"])
			}
		}
		if last != r.Version {
			o.fail(r.Code, "/historico", "last entry is not the current version "+itoa(r.Version))
		}
		for v := 1; v <= r.Version; v++ {
			if !seen[v] {
				o.fail(r.Code, "/historico", "missing version "+itoa(v))
				break
			}
		}
		if r.Finding != "" && achado != r.Finding {
			o.fail(r.Code, "/historico", "rework version without the finding "+r.Finding)
		}
	}
	return o, nil
}

var innerLists = []string{"criterios_aceite", "regras_negocio", "suposicoes", "riscos", "ameacas", "telas", "conferencias"}

// fixedNames are the types with a fixed id and file (template name).
var fixedNames = map[string]bool{"VISAO": true, "ARQ": true, "DIC": true, "PERSIST": true, "AMEACAS": true, "BACKLOG": true, "TELAS": true}

func idsUnique(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, r := range e.outputRecords() {
		for _, field := range innerLists {
			seen := map[string]bool{}
			for _, item := range list(r.Header, field) {
				id := text(object(item)["id"])
				if id == "" {
					continue
				}
				if seen[id] {
					o.fail(r.Code, "/"+field, "repeated internal id "+id)
				}
				seen[id] = true
			}
		}
		base := path.Base(r.Path)
		if fixedNames[r.Type] {
			if !strings.EqualFold(base, r.Code+".md") {
				o.fail(r.Code, r.Path, "file name differs from the template name "+r.Code+".md")
			}
		} else if !strings.HasPrefix(base, r.Code+".") && !strings.HasPrefix(base, r.Code+"-") {
			o.fail(r.Code, r.Path, "file name differs from the id")
		}
	}
	return o, nil
}

// typeAuthors is the author agent of each type (artefatos/README.md) and the stage or activity that
// produces it.
var typeAuthors = map[string][2]string{
	"VISAO": {"requirements-agent", "requirements"}, "US": {"requirements-agent", "requirements"}, "RNF": {"requirements-agent", "requirements"},
	"BACKLOG": {"requirements-agent", "requirements"}, "ARQ": {"architect-agent", "architecture"}, "ADR": {"architect-agent", "architecture"},
	"T": {"architect-agent", "architecture"}, "DIC": {"dba-agent", "data"}, "PERSIST": {"dba-agent", "data"}, "TELAS": {"designer-agent", "design"},
	"VALIDACAO": {"architect-agent", "validation"}, "CT": {"qa-agent", "qa-plan"}, "AMEACAS": {"security-agent", "security-threats"},
	"REL-QA": {"qa-agent", "qa"}, "REL-SEG": {"security-agent", "security"},
}

var headerAuthors = map[string]string{"orquestrador": "orchestrator", "humano": "human"}

func authorConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, r := range e.outputRecords() {
		expected, known := typeAuthors[r.Type]
		header := text(r.Header["autor"])
		if mapped, ok := headerAuthors[header]; ok {
			header = mapped
		}
		switch {
		case r.Author == "human":
			if text(r.Header["revisado_por"]) != expected[0] || r.WavePurpose != "edit_review" {
				o.fail(r.Code, "/revisado_por", "human edit without the review of "+expected[0])
			}
		case known && r.Author != expected[0]:
			o.fail(r.Code, "/autor", "author "+r.Author+" is not "+expected[0])
		case r.Type == "ACH" && r.Author == "requirements-agent":
			o.fail(r.Code, "/autor", "the requirements agent does not emit findings")
		}
		if header != r.Author {
			o.fail(r.Code, "/autor", "header author "+header+" differs from the wave agent "+r.Author)
		}
		if r.WaveState != "completed" {
			o.fail(r.Code, "/onda", "wave "+r.WaveCode+" is not completed")
		}
		if known && r.WaveStage != expected[1] && r.WaveStage != e.p.definition.GateStage(expected[1]) {
			o.fail(r.Code, "/onda", "wave "+r.WaveCode+" is of stage "+r.WaveStage)
		}
	}
	return o, nil
}

// noForeignApprovedModification: every approved artifact has its current version approved by a round
// (a change after the approval only comes through a reopening, which takes it back to draft).
func noForeignApprovedModification(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, a := range e.all {
		if a.State == domainartifact.StateApproved && !a.ApprovedByRound {
			o.fail(a.Code, "", "approved artifact changed after its approval (version "+itoa(a.Version)+")")
		}
	}
	return o, nil
}

func requiredOutputsPresent(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	present := map[string]bool{}
	for _, a := range e.all {
		if a.State == domainartifact.StateInReview || (a.State == domainartifact.StateApproved && a.ApprovedByRound) {
			present[a.Type] = true
		}
	}
	for _, required := range e.p.requiredTypes(e.stage) {
		if !present[required] {
			o.fail(required, "", "required output missing or not in review: "+required)
		}
	}
	files, err := e.repositoryFiles(ctx)
	if err != nil {
		return o, err
	}
	required := attachments(e)
	if files == nil {
		if len(required) > 0 && len(o.evidence) == 0 {
			return notice("", "attachments not verified: project repository not available"), nil
		}
		return o, nil
	}
	for _, p := range required {
		if !fileExists(files, p) {
			o.fail("", p, "required attachment missing in the repository")
		}
	}
	return o, nil
}

// attachments are the required attachment paths of the stage and of its activities.
func attachments(e *evaluation) []string {
	var out []string
	add := func(outputs []domainworkflow.Output) {
		for _, o := range outputs {
			if o.Required && o.Attachment && !strings.Contains(o.Path, "NN") && !strings.Contains(o.Path, "<") {
				out = append(out, o.Path)
			}
		}
	}
	add(e.stage.Outputs)
	for _, a := range e.p.definition.ActivitiesFor(e.stage.ID) {
		add(a.Outputs)
	}
	return out
}

func fileExists(files map[string]string, p string) bool {
	if _, ok := files[p]; ok {
		return true
	}
	if strings.HasSuffix(p, "/") {
		for f := range files {
			if strings.HasPrefix(f, p) {
				return true
			}
		}
	}
	return false
}

// reports are the numbered reports whose latest one is the input of the next stage.
var reports = map[string]bool{"REL-QA": true, "REL-SEG": true, "VALIDACAO": true}

// inputsApproved: every input type of the stage (produced in the workflow in force) has current
// artifacts, all approved; the stage is not waiting for a reevaluation.
func inputsApproved(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	if e.record.ReevaluationPending {
		o.fail(e.stage.ID, "", "stage waits for a reevaluation")
	}
	for _, input := range e.stage.Inputs {
		if _, isStage := e.p.definition.Stage(input.Origin); !isStage {
			if _, isActivity := e.p.definition.Activity(input.Origin); !isActivity {
				continue // origin out of the workflow in force
			}
		}
		if _, isActivity := e.p.definition.Activity(input.Origin); isActivity && e.p.definition.GateStage(input.Origin) == e.stage.ID {
			continue // output of an activity approved in this very gate
		}
		list := e.current(input.Type)
		if reports[input.Type] && len(list) > 0 {
			list = list[len(list)-1:] // a numbered report: the input is the latest execution (REL-QA-02 supersedes REL-QA-01)
		}
		if len(list) == 0 {
			o.fail(input.Type, "", "input "+input.Type+" missing")
		}
		for _, a := range list {
			if a.State != domainartifact.StateApproved {
				o.fail(a.Code, "", "input "+a.Code+" is "+a.State)
			}
		}
	}
	return o, nil
}

func limitsRespected(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	waves, err := e.roundWaves(ctx)
	if err != nil {
		return o, err
	}
	for _, w := range waves {
		if w.LimitExceeded > 0 {
			o.fail(w.Code, "", "execution ended by a resource limit of the agent")
		}
		if w.FailureCause == "limit_violation" {
			o.fail(w.Code, "", "wave failed by a limit violation")
		}
	}
	return o, nil
}

func recordComplete(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	waves, err := e.roundWaves(ctx)
	if err != nil {
		return o, err
	}
	for _, w := range waves {
		if w.State == "completed" && (w.Models == 0 || w.Calls == 0 || !w.FinalCheckpoint) {
			o.fail(w.Code, "", "wave record without model, calls or final checkpoint")
		}
	}
	return o, nil
}

func historyPreserved(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	if e.p.repository == nil {
		return notice("", "history not verified: project repository not available"), nil
	}
	commits, err := e.p.store.RecordedCommits(ctx, e.projectID)
	if err != nil {
		return o, err
	}
	for _, sha := range commits {
		ok, err := e.p.repository.Reachable(ctx, e.projectID, sha)
		if err != nil {
			return o, err
		}
		if !ok {
			o.fail(sha, "", "recorded commit no longer reachable")
		}
	}
	return o, nil
}

// existingReferences (RAS-01, US-28 CA-3): every id a header points to exists, and an item
// (US-03/CA-2) exists in the artifact it names.
func existingReferences(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, r := range e.outputRecords() {
		for _, ref := range references(r.Header) {
			code, item := splitReference(ref.value)
			target, ok := e.byCode[code]
			switch {
			case !ok:
				o.fail(r.Code, ref.path, "link to unknown id "+ref.value)
			case item != "" && !itemExists(target.Header, item):
				o.fail(r.Code, ref.path, "link to unknown item "+ref.value)
			}
		}
	}
	sortEvidence(o.evidence)
	return o, nil
}

func noReferenceToReplaced(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, r := range e.outputRecords() {
		for _, ref := range references(r.Header) {
			code, _ := splitReference(ref.value)
			if target, ok := e.byCode[code]; ok && target.State == domainartifact.StateReplaced && ref.path != "/substituido_por" && ref.path != "/substitui" {
				o.fail(r.Code, ref.path, "link to replaced artifact "+code)
			}
		}
	}
	return o, nil
}

func replacementReciprocal(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, a := range e.all {
		if a.State == domainartifact.StateReplaced {
			if target, ok := e.byCode[a.ReplacedBy]; !ok || target.Type != a.Type {
				o.fail(a.Code, "/substituido_por", "replaced by an artifact of another type or unknown")
			}
		}
		if a.Type == "ADR" {
			if old := text(a.Header["substitui"]); old != "" {
				if target, ok := e.byCode[old]; ok && a.State == domainartifact.StateApproved &&
					(target.State != domainartifact.StateReplaced || target.ReplacedBy != a.Code) {
					o.fail(a.Code, "/substitui", old+" is not replaced by "+a.Code)
				}
			}
		}
	}
	return o, nil
}

func (e *evaluation) finding(code string) *domainfinding.Finding {
	for i := range e.findings {
		if e.findings[i].Code == code {
			return &e.findings[i]
		}
	}
	return nil
}

func affects(f *domainfinding.Finding, code string) bool {
	for _, list := range [][]string{f.Affected, f.ModifiedArtifacts} {
		for _, ref := range list {
			if c, _ := splitReference(ref); c == code {
				return true
			}
		}
	}
	return false
}

func reworkRecorded(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, r := range e.outputRecords() {
		cited := map[string]bool{}
		for _, item := range list(r.Header, "historico") {
			code := text(object(item)["achado"])
			if code == "" {
				continue
			}
			cited[code] = true
			if f := e.finding(code); f == nil || !affects(f, r.Code) {
				o.fail(r.Code, "/historico", "history cites "+code+", which does not point to the artifact")
			}
		}
		if r.Finding != "" && !cited[r.Finding] {
			o.fail(r.Code, "/historico", "reopened by "+r.Finding+" without the finding in the history")
		}
	}
	return o, nil
}

// scoped are the findings of the gate scope.
func (e *evaluation) scoped() []domainfinding.Finding {
	var out []domainfinding.Finding
	for _, f := range e.findings {
		if e.inScope(f) {
			out = append(out, f)
		}
	}
	return out
}

func noOpenBlocking(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if f.Blocking() {
			o.fail(f.Code, "/situacao", f.Severity+" finding "+f.Status)
		}
	}
	return o, nil
}

func noOpenMediumLow(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if domainfinding.Active(f.Status) && !f.Blocking() {
			o.fail(f.Code, "/situacao", f.Severity+" finding "+f.Status)
		}
	}
	return o, nil
}

// DestinationProblems implements finding.destination_consistent for one finding (guard of start_rework).
func DestinationProblems(f domainfinding.Finding, defaults map[string]string, ownerOf func(code string) string) []string {
	var problems []string
	if defaults[f.Category] != f.DestinationStage && !domainfinding.JustifiedDestination(f.Body) {
		problems = append(problems, "destination "+f.DestinationStage+" differs from the default of "+f.Category+" without justification")
	}
	if f.DestinationStage != "implementation" {
		for _, ref := range f.Affected {
			code := domainfinding.ArtifactOf(ref)
			if code == "" {
				continue
			}
			if owner := ownerOf(code); owner != "" && owner != f.DestinationStage {
				problems = append(problems, "affected "+code+" is owned by "+owner)
			}
		}
	}
	if f.Category == "design_inconsistency" && f.DestinationStage == "design" {
		screens := false
		for _, ref := range f.Affected {
			screens = screens || ref == "TELAS" || strings.HasPrefix(ref, "TELAS/")
		}
		if !screens {
			problems = append(problems, "design finding without TELAS in the affected artifacts")
		}
	}
	return problems
}

func destinationConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if f.Status == domainfinding.StatusDiscarded {
			continue
		}
		for _, problem := range DestinationProblems(f, e.p.definition.Returns.DefaultByCategory, domainartifactOwner) {
			if !strings.HasPrefix(problem, "design finding") {
				o.fail(f.Code, "/etapa_destino", problem)
			}
		}
	}
	return o, nil
}

func domainartifactOwner(code string) string {
	t := code
	if i := strings.LastIndex(code, "-"); i > 0 && !fixedNames[code] {
		t = code[:i]
	}
	return domainartifact.OwnerStage(t)
}

func designPointsToScreens(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		for _, problem := range DestinationProblems(f, e.p.definition.Returns.DefaultByCategory, func(string) string { return "" }) {
			if strings.HasPrefix(problem, "design finding") {
				o.fail(f.Code, "/artefatos_afetados", problem)
			}
		}
	}
	return o, nil
}

func cyclesConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		agent := e.p.definition.AgentOf(f.DestinationStage)
		for i, c := range f.Cycles {
			if c.Number != i+1 {
				o.fail(f.Code, "/ciclos", "cycle out of order")
			}
			if i < len(f.Cycles)-1 && (c.Result == "" || c.CommitOrVersion == "") {
				o.fail(f.Code, "/ciclos", "earlier cycle "+itoa(c.Number)+" without result")
			}
			if agent != "" && c.ReworkAgent != agent {
				o.fail(f.Code, "/ciclos", "rework wave "+c.ReworkWave+" is not of "+agent)
			}
			if c.RevalidationWave != "" && c.RevalidationAgent != f.Author {
				o.fail(f.Code, "/ciclos", "revalidation wave "+c.RevalidationWave+" is not of the author "+f.Author)
			}
		}
	}
	return o, nil
}

// internalCycleGreen reads the internal cycle checks recorded on the rework waves (finding.InternalCycle).
func internalCycleGreen(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if f.DestinationStage != "implementation" {
			continue
		}
		for _, c := range f.Cycles {
			if c.RevalidationWave == "" {
				continue
			}
			failed, _, err := e.p.InternalCycle(ctx, e.projectID, c.ReworkWave)
			if err != nil {
				return o, err
			}
			if len(failed) > 0 {
				o.fail(f.Code, "/ciclos", "cycle "+itoa(c.Number)+" went to revalidation without "+strings.Join(failed, ", "))
			}
		}
	}
	return o, nil
}

func escalationConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if f.Status != domainfinding.StatusEscalated {
			continue
		}
		notFixed := 0
		for _, c := range f.Cycles {
			if c.Result == domainfinding.ResultNotFixed {
				notFixed++
			}
		}
		last := f.LastCycle()
		if len(f.Escalations) == 0 || notFixed < domainfinding.CycleLimit || last == nil || last.Result != domainfinding.ResultNotFixed {
			o.fail(f.Code, "/escalonamentos", "escalated without the not fixed cycles")
		}
	}
	return o, nil
}

func extraRoundAuthorized(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if len(f.Cycles) > domainfinding.CycleLimit+f.ExtraRounds() {
			o.fail(f.Code, "/ciclos", "cycle beyond the limit without human authorization")
		}
	}
	return o, nil
}

func closingConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if f.Status != domainfinding.StatusClosed {
			continue
		}
		switch f.Resolution {
		case domainfinding.ResolutionFixed:
			last := f.LastCycle()
			if last == nil || last.Result != domainfinding.ResultFixed || last.RevalidationAgent != f.Author {
				o.fail(f.Code, "/resolucao", "fixed without the revalidation of the author")
			}
		case domainfinding.ResolutionScopeChange:
			decided := false
			for _, esc := range f.Escalations {
				decided = decided || (esc.Decision == domainfinding.DecisionScopeChange && esc.AuthorizedBy != "" && esc.ModifiedBy != "" && len(esc.StartingPoint) > 0)
			}
			if !decided || len(f.ModifiedArtifacts) == 0 {
				o.fail(f.Code, "/resolucao", "scope change without the human decision or the modified artifacts")
			}
		}
	}
	return o, nil
}

func humanAcceptance(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if f.Status == domainfinding.StatusAccepted && (f.AcceptedBy == "" || f.AcceptedAt == nil || strings.TrimSpace(f.Justification) == "") {
			o.fail(f.Code, "/aceite", "acceptance without the human, the date or the justification")
		}
	}
	return o, nil
}

func discardConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.scoped() {
		if f.Status != domainfinding.StatusDiscarded {
			continue
		}
		if strings.TrimSpace(f.DiscardReason) == "" {
			o.fail(f.Code, "/motivo_descarte", "discard without reason")
		}
		if f.DuplicateOf != "" && (f.DuplicateOf == f.Code || e.finding(f.DuplicateOf) == nil) {
			o.fail(f.Code, "/motivo_descarte", "duplicate of an unknown finding")
		}
	}
	return o, nil
}

// sorted returns the keys of a set in order.
func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
