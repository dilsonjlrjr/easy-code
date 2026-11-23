package gate

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"easycode/internal/application/artifact"
	apprepository "easycode/internal/application/repository"
	domainartifact "easycode/internal/domain/artifact"
	domainfinding "easycode/internal/domain/finding"
	domainworkflow "easycode/internal/domain/workflow"
)

// shaPattern is a full commit id (SHA-1 or SHA-256).
var shaPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// ReleaseStage is the stage of the orchestrator that consolidates the delivery (workflow-padrao).
const ReleaseStage = "release"

// ReleaseCode is the RELEASE of the delivery: one per project in the MVP (template RELEASE).
const ReleaseCode = "RELEASE-01"

// ReleasePath is where the RELEASE lives in the project repository.
const ReleasePath = "docs/release/" + ReleaseCode + ".md"

// releaseStages are the stages whose gates the RELEASE lists (release.schema.json, gates and custos),
// with the header name of each one.
var releaseStages = []struct{ id, name string }{
	{"requirements", "requisitos"}, {"architecture", "arquitetura"}, {"data", "dados"}, {"design", "design"},
	{"validation", "validacao"}, {"implementation", "implementacao"}, {"qa", "qa"}, {"security", "seguranca"},
}

// Approval is the decision of the last approved round of a stage.
type Approval struct {
	Round int
	By    string // display name of the approver; the template value for the automatic mode otherwise
	At    time.Time
}

// StageCost is the cost and time of the waves of a stage and of its activities (RELEASE.custos).
type StageCost struct {
	Stage        string
	Waves        int
	Rework       int
	Tokens       int64
	Cost         string
	ExecutionMin float64
	CycleMin     float64
}

// ReleaseStore reads what the RELEASE consolidates beyond the artifacts and the findings.
type ReleaseStore interface {
	// StageApprovals returns, by stage, the decision of the last round when it is approved.
	StageApprovals(ctx context.Context, projectID string) (map[string]Approval, error)
	// StageCosts returns the cost of each stage (its activities included), in workflow order of the ids given.
	StageCosts(ctx context.Context, projectID string, stages map[string][]string) ([]StageCost, error)
	// ProjectActiveWaves lists the waves of the project in queued, running, awaiting_user, pausing or paused.
	ProjectActiveWaves(ctx context.Context, projectID string) ([]string, error)
	// Delivery returns the project name, the address of its repository, the accumulated cost and lost cost.
	Delivery(ctx context.Context, projectID string) (Delivery, error)
	// ReleaseArtifact returns the id of the RELEASE of the project and the tag of its current header
	// (entrega.tag); "" before the orchestrator writes it.
	ReleaseArtifact(ctx context.Context, projectID string) (string, string, error)
}

// Delivery is the project side of the RELEASE.
type Delivery struct {
	Name, Repository, Cost, Lost, Currency string
}

// ReleaseRepository is the git side of the Release: the delivered commit, the history and the mark.
type ReleaseRepository interface {
	// HeadCommit is the last commit of the main branch ("" when none).
	HeadCommit(ctx context.Context, projectID string) (string, error)
	// IsAncestor reports whether sha is in the history of commit.
	IsAncestor(ctx context.Context, projectID, sha, commit string) (bool, error)
	// Mark commits the approved RELEASE file on main and creates the tag on that commit; repeating the
	// mark of a tag that exists returns its commit (US-08 CA-3).
	Mark(ctx context.Context, projectID string, mark ReleaseMark) (string, error)
	// Marks lists the tags of the repository (getRepository.marks).
	Marks(ctx context.Context, projectID string) ([]Mark, error)
}

// ReleaseMark is the approved RELEASE file and its tag.
type ReleaseMark struct {
	Tag     string
	Path    string
	Content []byte
	Message string
}

// Mark is a tag of the project repository.
type Mark = apprepository.Mark

// Renderer writes an artifact file (YAML front matter and Markdown body).
type Renderer interface {
	Render(header map[string]any, body string) ([]byte, error)
}

// Tracker updates the header of the current version of an artifact without a new version.
type Tracker interface {
	Current(ctx context.Context, projectID, code string) (*artifact.Current, error)
	UpdateTracking(ctx context.Context, projectID, code string, fields map[string]any) error
}

type releaseParts struct {
	store    ReleaseStore
	repo     ReleaseRepository
	renderer Renderer
	tracker  Tracker
}

// WithRelease enables the Release stage: its checks, the RELEASE of the orchestrator and the mark of the
// repository when gate.release is approved (US-08).
func (p *Progress) WithRelease(store ReleaseStore, repo ReleaseRepository, renderer Renderer, tracker Tracker) *Progress {
	p.release = &releaseParts{store: store, repo: repo, renderer: renderer, tracker: tracker}
	return p
}

// ReleaseGate is the gate of one stage in the release facts.
type ReleaseGate struct {
	Stage, Name, State string
	Reevaluation       bool
	Approval           *Approval
}

// ReleaseFacts are the facts of the project the RELEASE consolidates (and the release checks compare).
type ReleaseFacts struct {
	Gates                   []ReleaseGate
	RelQA, RelSeg, Validate *ArtifactRecord
	Accepted, Active        []domainfinding.Finding
	MustTotal, MustComplete int
	Costs                   []StageCost
	ActiveWaves             []string
	Project, Repository     string
	Head                    string
	Cost, Lost, Currency    string
	Tests                   struct{ Total, Passed, Failed int }
}

// releaseFacts loads the facts over a loaded evaluation.
func (e *evaluation) releaseFacts(ctx context.Context) (ReleaseFacts, error) {
	var f ReleaseFacts
	r := e.p.release
	if r == nil {
		return f, fmt.Errorf("release stage without its store")
	}
	approvals, err := r.store.StageApprovals(ctx, e.projectID)
	if err != nil {
		return f, err
	}
	stages := map[string][]string{}
	for _, s := range releaseStages {
		record, err := e.p.store.Stage(ctx, e.projectID, s.id)
		if err != nil {
			return f, err
		}
		gate := ReleaseGate{Stage: s.id, Name: s.name, State: record.State, Reevaluation: record.ReevaluationPending}
		if a, ok := approvals[s.id]; ok {
			gate.Approval = &a
		}
		f.Gates = append(f.Gates, gate)
		ids := []string{s.id}
		for _, a := range e.p.definition.ActivitiesFor(s.id) {
			ids = append(ids, a.ID)
		}
		stages[s.id] = ids
	}
	f.RelQA, f.RelSeg, f.Validate = e.latest("REL-QA"), e.latest("REL-SEG"), e.latest("VALIDACAO")
	for _, finding := range e.findings {
		switch {
		case finding.Status == domainfinding.StatusAccepted:
			f.Accepted = append(f.Accepted, finding)
		case domainfinding.Active(finding.Status):
			f.Active = append(f.Active, finding)
		}
	}
	f.MustTotal, f.MustComplete = e.mustCompletion()
	if f.Costs, err = r.store.StageCosts(ctx, e.projectID, stages); err != nil {
		return f, err
	}
	if f.ActiveWaves, err = r.store.ProjectActiveWaves(ctx, e.projectID); err != nil {
		return f, err
	}
	delivery, err := r.store.Delivery(ctx, e.projectID)
	if err != nil {
		return f, err
	}
	f.Project, f.Repository, f.Cost, f.Lost, f.Currency = delivery.Name, delivery.Repository, delivery.Cost, delivery.Lost, delivery.Currency
	if f.Head, err = r.repo.HeadCommit(ctx, e.projectID); err != nil {
		return f, err
	}
	for _, ct := range e.current("CT") {
		f.Tests.Total++
		switch text(execution(ct)["resultado"]) {
		case "passou":
			f.Tests.Passed++
		case "falhou":
			f.Tests.Failed++
		}
	}
	return f, nil
}

// mustCompletion counts the current Must stories and the complete ones: a completed task covers it, every
// criterion has a passing CT and no active critical or high finding affects it (traceability.us_must_complete).
func (e *evaluation) mustCompletion() (int, int) {
	done := map[string]bool{}
	for _, t := range e.current("T") {
		if text(t.Header["situacao"]) != "concluida" {
			continue
		}
		for _, item := range list(t.Header, "cobre") {
			done[text(object(item)["us"])] = true
		}
	}
	coverage, _ := e.ctCoverage()
	total, complete := 0, 0
	for _, us := range e.mustStories() {
		total++
		ok := done[us.Code]
		for _, ca := range criteria(us.Header) {
			passing := false
			for _, ct := range coverage[us.Code][ca] {
				passing = passing || text(execution(ct)["resultado"]) == "passou"
			}
			ok = ok && passing
		}
		for i := range e.findings {
			if e.findings[i].Blocking() && affects(&e.findings[i], us.Code) {
				ok = false
			}
		}
		if ok {
			complete++
		}
	}
	return total, complete
}

func codesOf(findings []domainfinding.Finding) []string {
	codes := []string{}
	for _, f := range findings {
		codes = append(codes, f.Code)
	}
	sort.Slice(codes, func(i, j int) bool { return codeLess(codes[i], codes[j]) })
	return codes
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

// ReleaseTag is the mark of the delivery in the project repository (US-08 CA-3).
func ReleaseTag(code string) string { return strings.ToLower(code) }

func decimal(value string) float64 {
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0
	}
	f, _ := r.Float64()
	return f
}

// header is the RELEASE header of the facts (release.schema.json; template RELEASE).
func (f ReleaseFacts) header() map[string]any {
	gates := []any{}
	for _, g := range f.Gates {
		item := map[string]any{"etapa": g.Name, "aprovado_por": "-", "quando": "1970-01-01"}
		if g.Approval != nil {
			item["aprovado_por"], item["quando"] = g.Approval.By, day(g.Approval.At)
		}
		gates = append(gates, item)
	}
	quality := map[string]any{}
	if f.RelQA != nil {
		quality["rel_qa"] = f.RelQA.Code
	}
	if f.RelSeg != nil {
		quality["rel_seg"] = f.RelSeg.Code
	}
	costs := []any{}
	for _, c := range f.Costs {
		costs = append(costs, map[string]any{"etapa": stageName(c.Stage), "ondas": c.Waves, "retrabalho": c.Rework, "tokens": c.Tokens,
			"custo": decimal(c.Cost), "tempo_execucao_min": round2(c.ExecutionMin), "tempo_ciclo_min": round2(c.CycleMin)})
	}
	header := map[string]any{
		"gates": gates, "qualidade": quality, "riscos_aceitos": anyList(codesOf(f.Accepted)), "achados_abertos": anyList(codesOf(f.Active)),
		"rastreabilidade": map[string]any{"us_must_total": f.MustTotal, "us_must_completas": f.MustComplete},
		"entrega":         map[string]any{"repositorio": f.Repository, "commit": dash(f.Head), "tag": ReleaseTag(ReleaseCode)},
		"custos":          costs, "situacao": "rascunho",
	}
	if f.Validate != nil {
		header["validacao"] = f.Validate.Code
	}
	return header
}

func floatOf(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case interface{ Float64() (float64, error) }:
		f, _ := v.Float64()
		return f
	}
	return 0
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func anyList(items []string) []any {
	out := make([]any, 0, len(items))
	for _, s := range items {
		out = append(out, s)
	}
	return out
}

func stageName(id string) string {
	for _, s := range releaseStages {
		if s.id == id {
			return s.name
		}
	}
	return id
}

// body is the Markdown of the RELEASE, with the accepted risks and who accepted them (US-33 CA-4).
func (f ReleaseFacts) body(project string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Release 01 — %s\n\n## Gates\n\n", project)
	for _, g := range f.Gates {
		if g.Approval != nil {
			fmt.Fprintf(&b, "- %s: aprovado por %s em %s (rodada %d)\n", g.Name, g.Approval.By, day(g.Approval.At), g.Approval.Round)
		} else {
			fmt.Fprintf(&b, "- %s: %s\n", g.Name, g.State)
		}
	}
	b.WriteString("\n## Qualidade e segurança\n\n")
	if f.RelQA != nil {
		fmt.Fprintf(&b, "- QA: %s, veredito %s; %d CT, %d passaram, %d falharam\n", f.RelQA.Code, text(f.RelQA.Header["veredito"]), f.Tests.Total, f.Tests.Passed, f.Tests.Failed)
	}
	if f.RelSeg != nil {
		fmt.Fprintf(&b, "- Segurança: %s, veredito %s\n", f.RelSeg.Code, text(f.RelSeg.Header["veredito"]))
	}
	if len(f.Accepted) == 0 {
		b.WriteString("- Riscos aceitos: nenhum\n")
	}
	for _, a := range f.Accepted {
		by := a.AcceptedByName
		if by == "" {
			by = a.AcceptedBy
		}
		when, until := "", "sem prazo"
		if a.AcceptedAt != nil {
			when = day(*a.AcceptedAt)
		}
		if a.ValidUntil != nil {
			until = "válido até " + day(*a.ValidUntil)
		}
		fmt.Fprintf(&b, "- Risco aceito %s (%s): aceito por %s em %s, %s. Justificativa: %s\n", a.Code, a.Severity, by, when, until,
			strings.ReplaceAll(strings.TrimSpace(a.Justification), "\n", " "))
	}
	if len(f.Active) > 0 {
		fmt.Fprintf(&b, "- Achados abertos: %s\n", strings.Join(codesOf(f.Active), ", "))
	}
	fmt.Fprintf(&b, "\n## Rastreabilidade\n\n- US Must completas: %d de %d\n", f.MustComplete, f.MustTotal)
	fmt.Fprintf(&b, "\n## Entrega\n\n- Repositório: %s\n- Commit: %s\n- Marcação: %s (criada na aprovação)\n", f.Repository, dash(f.Head), ReleaseTag(ReleaseCode))
	fmt.Fprintf(&b, "\n## Custo e tempo\n\n- Custo total: %s %s (perdido %s %s)\n", trimZeros(f.Cost), f.Currency, trimZeros(f.Lost), f.Currency)
	for _, c := range f.Costs {
		fmt.Fprintf(&b, "- %s: %d ondas (%d de retrabalho), %d tokens, %s %s\n", stageName(c.Stage), c.Waves, c.Rework, c.Tokens, trimZeros(c.Cost), f.Currency)
	}
	b.WriteString("\n## Aprovação humana\n\nRegistrada no cabeçalho, em `aprovacao` e `situacao`: o gate.release é sempre humano.\n")
	return b.String()
}

func trimZeros(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	return strings.TrimSuffix(strings.TrimRight(value, "0"), ".")
}

// ReleaseFile builds the next version of the RELEASE written by the release wave of the orchestrator from
// the current facts of the project (template RELEASE: "o orquestrador preenche a partir dos artefatos").
func (p *Progress) ReleaseFile(ctx context.Context, projectID, wave string) (string, []byte, error) {
	stage, _ := p.definition.Stage(ReleaseStage)
	e := &evaluation{p: p, projectID: projectID, stage: stage}
	if err := e.load(ctx); err != nil {
		return "", nil, err
	}
	facts, err := e.releaseFacts(ctx)
	if err != nil {
		return "", nil, err
	}
	header, projectName := facts.header(), facts.Project
	version, history := 1, []any{}
	if current, err := p.release.tracker.Current(ctx, projectID, ReleaseCode); err != nil {
		return "", nil, err
	} else if current != nil {
		version = current.Version + 1
		history = list(current.Header, "historico")
	}
	description := "consolidação da entrega"
	if version > 1 {
		description = "nova consolidação depois do gate.release reprovado ou de mudança no projeto"
	}
	history = append(history, map[string]any{"versao": version, "data": day(p.now()), "onda": wave, "descricao": description})
	for k, v := range map[string]any{"id": ReleaseCode, "tipo": "RELEASE", "titulo": "Release 01 — " + clipTitle(projectName), "autor": "orquestrador",
		"onda": wave, "versao": version, "estado": "rascunho", "vinculos": vinculos(facts), "historico": history} {
		header[k] = v
	}
	content, err := p.release.renderer.Render(header, facts.body(projectName))
	return ReleasePath, content, err
}

func clipTitle(name string) string {
	if runes := []rune(name); len(runes) > 120 {
		return string(runes[:120])
	}
	return name
}

func vinculos(f ReleaseFacts) []any {
	var out []string
	for _, r := range []*ArtifactRecord{f.Validate, f.RelQA, f.RelSeg} {
		if r != nil {
			out = append(out, r.Code)
		}
	}
	return anyList(out)
}

// recordRelease is the effect of an approved gate.release (US-08 CA-3; decisions_always_human
// release_approval: the decision goes to RELEASE.aprovacao): the approval is written in the RELEASE, the file
// is committed on main and the release tag marks that commit.
func (p *Progress) recordRelease(ctx context.Context, projectID string) error {
	if p.release == nil {
		return nil
	}
	current, err := p.release.tracker.Current(ctx, projectID, ReleaseCode)
	if err != nil || current == nil {
		return err
	}
	approvals, err := p.release.store.StageApprovals(ctx, projectID)
	if err != nil {
		return err
	}
	approval, ok := approvals[ReleaseStage]
	if !ok {
		return fmt.Errorf("gate.release approved without its decision")
	}
	fields := map[string]any{"situacao": "aprovado", "aprovacao": map[string]any{"por": approval.By, "quando": day(approval.At), "decisao": "aprovado"}}
	header := map[string]any{}
	for k, v := range current.Header {
		header[k] = v
	}
	for k, v := range fields {
		header[k] = v
	}
	header["estado"] = "aprovado"
	content, err := p.release.renderer.Render(header, current.Body) // the body of the version is immutable

	if err != nil {
		return err
	}
	tag := text(object(current.Header["entrega"])["tag"])
	if tag == "" {
		tag = ReleaseTag(ReleaseCode)
	}
	message := "release: " + ReleaseCode + " aprovado\n\nEasycode-Release: " + ReleaseCode + "\nEasycode-Tag: " + tag + "\n"
	if _, err := p.release.repo.Mark(ctx, projectID, ReleaseMark{Tag: tag, Path: current.Path, Content: content, Message: message}); err != nil {
		return err
	}
	return p.release.tracker.UpdateTracking(ctx, projectID, ReleaseCode, fields)
}

// ---------------------------------------------------------------- release checks

func init() {
	for id, f := range map[string]checkFunc{
		"release.gates_approved":                    releaseGatesApproved,
		"release.current_quality":                   releaseCurrentQuality,
		"release.accepted_risks_complete":           releaseAcceptedRisks,
		"release.no_active_wave":                    releaseNoActiveWave,
		"release.costs_consistent":                  releaseCostsConsistent,
		"traceability.us_must_complete":             releaseMustComplete,
		"traceability.release_scope_consistent":     releaseScopeConsistent,
		"traceability.release_commit_contains_work": releaseCommitContainsWork,
		"traceability.release_open_findings_listed": releaseOpenFindingsListed,
	} {
		registry[id] = f
	}
}

// releaseOf returns the RELEASE of the round and the facts (nil when the round has no RELEASE: no item).
func (e *evaluation) releaseOf(ctx context.Context) (*ArtifactRecord, *ReleaseFacts, error) {
	var r *ArtifactRecord
	for _, record := range e.outputRecords() {
		if record.Type == "RELEASE" {
			r = record
		}
	}
	if r == nil || e.p.release == nil {
		return nil, nil, nil
	}
	facts, err := e.releaseFacts(ctx)
	return r, &facts, err
}

func releaseGatesApproved(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	r, f, err := e.releaseOf(ctx)
	if r == nil || err != nil {
		return o, err
	}
	listed := map[string]map[string]any{}
	for _, item := range list(r.Header, "gates") {
		g := object(item)
		listed[text(g["etapa"])] = g
	}
	for _, g := range f.Gates {
		item, ok := listed[g.Name]
		switch {
		case !ok:
			o.fail(r.Code, "/gates", "stage "+g.Name+" missing")
		case g.State != domainworkflow.StageApproved || g.Approval == nil:
			o.fail(r.Code, "/gates", "stage "+g.Name+" is "+g.State)
		case g.Reevaluation:
			o.fail(r.Code, "/gates", "stage "+g.Name+" waits for reevaluation")
		case text(item["aprovado_por"]) != g.Approval.By || text(item["quando"]) != day(g.Approval.At):
			o.fail(r.Code, "/gates", "approval of "+g.Name+" differs from its round "+itoa(g.Approval.Round))
		}
	}
	return o, nil
}

func releaseCurrentQuality(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	r, f, err := e.releaseOf(ctx)
	if r == nil || err != nil {
		return o, err
	}
	quality := object(r.Header["qualidade"])
	for _, c := range []struct {
		field  string
		report *ArtifactRecord
	}{{"rel_qa", f.RelQA}, {"rel_seg", f.RelSeg}} {
		switch {
		case c.report == nil:
			o.fail(r.Code, "/qualidade/"+c.field, "no report in the project")
		case text(quality[c.field]) != c.report.Code:
			o.fail(r.Code, "/qualidade/"+c.field, text(quality[c.field])+" is not the latest report "+c.report.Code)
		case c.report.State != domainartifact.StateApproved || text(c.report.Header["veredito"]) != "aprovado":
			o.fail(c.report.Code, "/veredito", "report not approved with verdict aprovado")
		}
	}
	return o, nil
}

func sameSet(listed []string, expected []string) (missing, extra []string) {
	for _, code := range expected {
		if !slices.Contains(listed, code) {
			missing = append(missing, code)
		}
	}
	for _, code := range listed {
		if !slices.Contains(expected, code) {
			extra = append(extra, code)
		}
	}
	return missing, extra
}

func releaseAcceptedRisks(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	r, f, err := e.releaseOf(ctx)
	if r == nil || err != nil {
		return o, err
	}
	missing, extra := sameSet(textList(list(r.Header, "riscos_aceitos")), codesOf(f.Accepted))
	for _, code := range missing {
		o.fail(code, "/riscos_aceitos", "accepted finding not listed")
	}
	for _, code := range extra {
		o.fail(code, "/riscos_aceitos", "listed finding is not accepted")
	}
	now := e.p.now()
	for _, a := range f.Accepted {
		if a.AcceptedBy == "" || a.AcceptedAt == nil || strings.TrimSpace(a.Justification) == "" {
			o.fail(a.Code, "/aceite", "acceptance incomplete")
		}
		if a.AcceptanceExpired(now) {
			o.fail(a.Code, "/aceite/valido_ate", "acceptance expired on "+day(*a.ValidUntil))
		}
	}
	return o, nil
}

func releaseNoActiveWave(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	if e.p.release == nil {
		return o, nil
	}
	waves, err := e.p.release.store.ProjectActiveWaves(ctx, e.projectID)
	for _, w := range waves {
		o.fail(w, "", "active wave")
	}
	return o, err
}

func releaseCostsConsistent(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	r, f, err := e.releaseOf(ctx)
	if r == nil || err != nil {
		return o, err
	}
	listed := map[string]map[string]any{}
	for _, item := range list(r.Header, "custos") {
		c := object(item)
		listed[text(c["etapa"])] = c
	}
	for _, c := range f.Costs {
		item, ok := listed[stageName(c.Stage)]
		if !ok {
			o.fail(r.Code, "/custos", "stage "+stageName(c.Stage)+" missing")
			continue
		}
		cost := floatOf(item["custo"])
		if number(item["ondas"]) != c.Waves || number(item["retrabalho"]) != c.Rework || int64(number(item["tokens"])) != c.Tokens ||
			fmt.Sprintf("%.4f", cost) != fmt.Sprintf("%.4f", decimal(c.Cost)) {
			o.fail(r.Code, "/custos", "cost of "+stageName(c.Stage)+" differs from the wave record")
		}
	}
	return o, nil
}

func releaseMustComplete(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	r, f, err := e.releaseOf(ctx)
	if r == nil || err != nil {
		return o, err
	}
	trace := object(r.Header["rastreabilidade"])
	total, complete := number(trace["us_must_total"]), number(trace["us_must_completas"])
	if total != f.MustTotal || complete != f.MustComplete {
		o.fail(r.Code, "/rastreabilidade", fmt.Sprintf("Must stories %d/%d, counted %d/%d", complete, total, f.MustComplete, f.MustTotal))
	}
	if f.MustComplete != f.MustTotal {
		o.fail(r.Code, "/rastreabilidade", fmt.Sprintf("%d Must stories incomplete", f.MustTotal-f.MustComplete))
	}
	return o, nil
}

func releaseScopeConsistent(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	r, f, err := e.releaseOf(ctx)
	if r == nil || err != nil {
		return o, err
	}
	if stories := textList(list(r.Header, "historias")); len(stories) > 0 {
		scope := map[string]bool{}
		if backlog := e.latest("BACKLOG"); backlog != nil {
			for _, item := range list(backlog.Header, "historias") {
				scope[text(object(item)["us"])] = true
			}
		}
		for _, us := range stories {
			if !scope[us] {
				o.fail(us, "/historias", "story out of the backlog scope")
			}
		}
		for _, us := range e.mustStories() {
			if !slices.Contains(stories, us.Code) {
				o.fail(us.Code, "/historias", "Must story missing")
			}
		}
	}
	if validation := text(r.Header["validacao"]); validation != "" {
		v := f.Validate
		switch {
		case v == nil || v.Code != validation:
			o.fail(r.Code, "/validacao", validation+" is not the latest VALIDACAO")
		case v.State != domainartifact.StateApproved || text(v.Header["veredito"]) != "liberado":
			o.fail(v.Code, "/veredito", "VALIDACAO not approved with verdict liberado")
		}
	}
	return o, nil
}

func releaseCommitContainsWork(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	r, _, err := e.releaseOf(ctx)
	if r == nil || err != nil {
		return o, err
	}
	delivered := text(object(r.Header["entrega"])["commit"])
	var work []struct{ ref, sha string }
	for _, t := range e.current("T") {
		if text(t.Header["situacao"]) == "concluida" {
			for _, sha := range textList(list(object(t.Header["registro_dev"]), "commits")) {
				work = append(work, struct{ ref, sha string }{t.Code, sha})
			}
		}
	}
	for _, finding := range e.findings {
		if finding.Status != domainfinding.StatusClosed {
			continue
		}
		for _, c := range finding.Cycles {
			if shaPattern.MatchString(c.CommitOrVersion) {
				work = append(work, struct{ ref, sha string }{finding.Code, c.CommitOrVersion})
			}
		}
	}
	for _, w := range work {
		ok, err := e.p.release.repo.IsAncestor(ctx, e.projectID, w.sha, delivered)
		if err != nil {
			return o, err
		}
		if !ok {
			o.fail(w.ref, "/entrega/commit", "commit "+w.sha+" is not in the delivered history")
		}
	}
	return o, nil
}

func releaseOpenFindingsListed(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	r, f, err := e.releaseOf(ctx)
	if r == nil || err != nil {
		return o, err
	}
	missing, extra := sameSet(textList(list(r.Header, "achados_abertos")), codesOf(f.Active))
	for _, code := range missing {
		o.fail(code, "/achados_abertos", "active finding not listed")
	}
	for _, code := range extra {
		o.fail(code, "/achados_abertos", "listed finding is not active")
	}
	return o, nil
}
