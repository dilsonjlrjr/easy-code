package gate

import (
	"context"
	"regexp"
	"sort"
	"strings"

	domainartifact "easycode/internal/domain/artifact"
	domainfinding "easycode/internal/domain/finding"
)

func init() {
	for id, f := range map[string]checkFunc{
		"us.acceptance_criteria_present":              acceptanceCriteriaPresent,
		"us.no_blocking_question":                     noBlockingQuestion,
		"us.criteria_no_vague_term":                   criteriaNoVagueTerm,
		"rnf.metric_and_verification":                 metricAndVerification,
		"rnf.min_categories":                          minCategories,
		"backlog.priority_consistent":                 priorityConsistent,
		"traceability.backlog_covers_us":              backlogCoversUS,
		"traceability.us_rnf_reciprocal":              usRNFReciprocal,
		"arq.openapi_valid":                           openAPIValid,
		"t.dependencies_no_cycle":                     dependenciesNoCycle,
		"traceability.us_must_covered_by_t":           mustCoveredByT,
		"traceability.ca_must_covered_by_t":           criteriaMustCoveredByT,
		"traceability.component_justified":            componentJustified,
		"traceability.adr_listed_in_arq":              adrListedInARQ,
		CheckTasksCoverUS:                             tasksCoverStories,
		"traceability.us_met_by_component":            usMetByComponent,
		"traceability.operations_in_contract":         operationsInContract,
		"dic.ddl_executes":                            ddlExecutes,
		"dic.model_consistent":                        modelConsistent,
		"dic.personal_data_marked":                    personalDataMarked,
		"dic.legal_basis_defined":                     legalBasisDefined,
		CheckModelReferences:                          modelReferences,
		"traceability.performance_rnf_in_persist":     performanceRNFInPersist,
		"telas.files_present":                         screenFilesPresent,
		CheckCriteriaConsistent:                       criteriaConsistent,
		"telas.prototype_navigable":                   prototypeNavigable,
		"telas.accessibility_verified":                accessibilityVerified,
		"telas.state_by_parameter":                    stateByParameter,
		CheckInterfaceUSScreen:                        screensLinkStories,
		"validacao.checks_complete":                   validationChecksComplete,
		CheckOpinionsRecorded:                         opinionsRecorded,
		CheckVerdictUnblocked:                         verdictUnblocked,
		"validacao.review_opinion_rejected":           reviewOpinionRejected,
		"ameacas.high_critical_handled":               threatsHandled,
		"traceability.ca_must_covered_by_ct":          criteriaMustCoveredByCT,
		"traceability.rnf_must_covered_by_ct":         rnfMustCoveredByCT,
		"traceability.t_cts_consistent":               tasksCTsConsistent,
		"traceability.ca_covered_by_ct":               criteriaCoveredByCT,
		"traceability.contract_gap_resolved":          contractGapResolved,
		"traceability.validation_coverage_consistent": validationCoverageConsistent,
		"traceability.validation_versions_current":    validationVersionsCurrent,
		CheckBuildGreen: func(ctx context.Context, e *evaluation) (outcome, error) {
			return e.executionGreen(ctx, CheckBuildGreen, categoryBuild)
		},
		CheckTestsGreen: func(ctx context.Context, e *evaluation) (outcome, error) {
			return e.executionGreen(ctx, CheckTestsGreen, categoryTests)
		},
		CheckLintClean: func(ctx context.Context, e *evaluation) (outcome, error) {
			return e.executionGreen(ctx, CheckLintClean, categoryLint)
		},
		"code.migrations_consistent_with_ddl":  migrationsConsistent,
		"code.api_contract_consistent":         apiContractConsistent,
		"t.completed_or_justified":             tasksCompleted,
		"t.dev_record_present":                 devRecordPresent,
		"traceability.file_scope_respected":    fileScopeRespected,
		"rel_qa.consistent_with_ct":            qaConsistentWithCT,
		"rel_qa.open_findings_consistent":      qaOpenFindings,
		"rel_qa.verdict_approved":              func(_ context.Context, e *evaluation) (outcome, error) { return e.verdict("REL-QA", "aprovado"), nil },
		"traceability.ca_must_with_ct_passing": mustWithCTPassing,
		"traceability.ct_execution_linked":     ctExecutionLinked,
		"ct.not_executed_justified":            ctNotExecutedJustified,
		"rel_qa.commands_reproducible":         commandsReproducible,
		"traceability.ct_test_in_t":            ctTestInT,
		"security.no_critical_risk":            noCriticalRisk,
		"rel_seg.summary_consistent":           securitySummary,
		"rel_seg.scans_executed":               scansExecuted,
		"rel_seg.verdict_approved":             func(_ context.Context, e *evaluation) (outcome, error) { return e.verdict("REL-SEG", "aprovado"), nil },
		"traceability.threats_checked":         threatsChecked,
	} {
		registry[id] = f
	}
}

// --- Requisitos

func acceptanceCriteriaPresent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, us := range e.current("US") {
		items := list(us.Header, "criterios_aceite")
		if len(items) == 0 {
			o.fail(us.Code, "/criterios_aceite", "US without acceptance criterion")
		}
		for _, item := range items {
			ca := object(item)
			for _, part := range []string{"dado", "quando", "entao"} {
				if strings.TrimSpace(text(ca[part])) == "" {
					o.fail(us.Code+"/"+text(ca["id"]), "/criterios_aceite", "empty "+part)
				}
			}
		}
	}
	return o, nil
}

func noBlockingQuestion(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, us := range e.current("US") {
		for _, item := range list(us.Header, "perguntas_abertas") {
			if object(item)["bloqueante"] == true {
				o.fail(us.Code, "/perguntas_abertas", "blocking open question")
			}
		}
	}
	return o, nil
}

var vagueTerms = []string{"rápido", "rapido", "fácil", "facil", "adequado", "intuitivo", "amigável", "amigavel"}
var digits = regexp.MustCompile(`[0-9]`)

func criteriaNoVagueTerm(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, us := range e.current("US") {
		for _, item := range list(us.Header, "criterios_aceite") {
			ca := object(item)
			then := strings.ToLower(text(ca["entao"]))
			for _, term := range vagueTerms {
				if strings.Contains(then, term) && !digits.MatchString(then) {
					o.fail(us.Code+"/"+text(ca["id"]), "/criterios_aceite", "vague term without measurable value: "+term)
				}
			}
		}
	}
	return o, nil
}

func metricAndVerification(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, rnf := range e.current("RNF") {
		verification := object(rnf.Header["verificacao"])
		if strings.TrimSpace(text(rnf.Header["metrica"])) == "" || strings.TrimSpace(text(verification["tipo"])) == "" ||
			strings.TrimSpace(text(verification["detalhe"])) == "" {
			o.fail(rnf.Code, "/metrica", "RNF without metric or verification")
		}
	}
	return o, nil
}

// minCategories (PC-02): at least 4 categories, among them seguranca, desempenho, acessibilidade and
// privacidade (privacidade_lgpd in the RNF schema).
func minCategories(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	categories := map[string]bool{}
	for _, rnf := range e.current("RNF") {
		categories[text(rnf.Header["categoria"])] = true
	}
	if len(categories) < 4 {
		o.fail("RNF", "/categoria", "fewer than 4 categories")
	}
	for _, required := range []string{"seguranca", "desempenho", "acessibilidade", "privacidade_lgpd"} {
		if !categories[required] {
			o.fail("RNF", "/categoria", "missing category "+required)
		}
	}
	return o, nil
}

func priorityConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, backlog := range e.current("BACKLOG") {
		for field, key := range map[string]string{"historias": "us", "rnf": "rnf"} {
			for _, item := range list(backlog.Header, field) {
				entry := object(item)
				code := text(entry[key])
				if target, ok := e.byCode[code]; ok && text(target.Header["prioridade"]) != text(entry["prioridade"]) {
					o.fail(backlog.Code, "/"+field, "priority of "+code+" differs from the artifact")
				}
			}
		}
	}
	return o, nil
}

func backlogCoversUS(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, backlog := range e.current("BACKLOG") {
		count := map[string]int{}
		deferred := map[string]bool{}
		for _, item := range list(backlog.Header, "historias") {
			count[text(object(item)["us"])]++
		}
		for _, item := range list(backlog.Header, "adiadas") {
			code := text(object(item)["us"])
			count[code]++
			deferred[code] = true
		}
		for _, us := range e.current("US") {
			switch {
			case count[us.Code] != 1:
				o.fail(backlog.Code, "/historias", us.Code+" must appear exactly once")
			case (text(us.Header["prioridade"]) == "wont") != deferred[us.Code]:
				o.fail(backlog.Code, "/adiadas", us.Code+": only wont stories are deferred")
			}
		}
		rnf := map[string]int{}
		for _, item := range list(backlog.Header, "rnf") {
			rnf[text(object(item)["rnf"])]++
		}
		for _, r := range e.current("RNF") {
			if rnf[r.Code] != 1 {
				o.fail(backlog.Code, "/rnf", r.Code+" must appear exactly once")
			}
		}
	}
	return o, nil
}

func usRNFReciprocal(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, us := range e.current("US") {
		for _, code := range textList(list(us.Header, "rnf")) {
			if r, ok := e.byCode[code]; ok && !contains(textList(list(r.Header, "us_afetadas")), us.Code) {
				o.fail(us.Code, "/rnf", code+" does not list "+us.Code)
			}
		}
	}
	for _, r := range e.current("RNF") {
		for _, code := range textList(list(r.Header, "us_afetadas")) {
			if us, ok := e.byCode[code]; ok && !contains(textList(list(us.Header, "rnf")), r.Code) {
				o.fail(r.Code, "/us_afetadas", code+" does not list "+r.Code)
			}
		}
	}
	return o, nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// --- Arquitetura

// contract parses the OpenAPI file of ARQ.contratos.openapi: ok=false when the system has no API.
func (e *evaluation) contract(ctx context.Context) (operations map[string]bool, problems []string, ok bool, err error) {
	arq := e.byCode["ARQ"]
	if arq == nil {
		return nil, nil, false, nil
	}
	file := text(object(arq.Header["contratos"])["openapi"])
	if file == "" {
		return nil, nil, false, nil
	}
	files, err := e.repositoryFiles(ctx)
	if err != nil || files == nil {
		return nil, nil, files != nil, err
	}
	content, found := files[file]
	if !found {
		return nil, []string{"contract file " + file + " not found"}, true, nil
	}
	if e.p.parser == nil {
		return nil, nil, false, nil
	}
	document, err := e.p.parser.ParseHeader(content)
	if err != nil {
		return nil, []string{"contract is not valid YAML"}, true, nil
	}
	if version := text(document["openapi"]); !strings.HasPrefix(version, "3.") {
		problems = append(problems, "openapi version is not 3.x")
	}
	info := object(document["info"])
	if text(info["title"]) == "" || text(info["version"]) == "" {
		problems = append(problems, "info.title and info.version are required")
	}
	paths, isMap := document["paths"].(map[string]any)
	if !isMap {
		problems = append(problems, "paths is required")
	}
	operations = map[string]bool{}
	for route, item := range paths {
		for method, op := range object(item) {
			operation := object(op)
			if operation == nil || method == "parameters" {
				continue
			}
			if _, hasResponses := operation["responses"]; !hasResponses {
				problems = append(problems, method+" "+route+" without responses")
			}
			if id := text(operation["operationId"]); id != "" {
				operations[id] = true
			}
		}
	}
	return operations, problems, true, nil
}

func openAPIValid(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	_, problems, hasContract, err := e.contract(ctx)
	if err != nil {
		return o, err
	}
	if hasContract && e.files == nil {
		return notice("ARQ", "contract not verified: project repository not available"), nil
	}
	for _, problem := range problems {
		o.fail("ARQ", "/contratos/openapi", problem)
	}
	return o, nil
}

func dependenciesNoCycle(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	graph := map[string][]string{}
	for _, t := range e.current("T") {
		graph[t.Code] = textList(list(t.Header, "depende_de"))
	}
	state := map[string]int{}
	var visit func(code string, trail []string)
	visit = func(code string, trail []string) {
		switch state[code] {
		case 1:
			o.fail(code, "/depende_de", "cycle: "+strings.Join(append(trail, code), " -> "))
			return
		case 2:
			return
		}
		state[code] = 1
		for _, next := range graph[code] {
			visit(next, append(trail, code))
		}
		state[code] = 2
	}
	codes := make([]string, 0, len(graph))
	for code := range graph {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		visit(code, nil)
	}
	return o, nil
}

// covered returns US → set of criteria covered by the current T.
func (e *evaluation) coveredByTasks() map[string]map[string]bool {
	covered := map[string]map[string]bool{}
	for _, t := range e.current("T") {
		for _, item := range list(t.Header, "cobre") {
			entry := object(item)
			us := text(entry["us"])
			if covered[us] == nil {
				covered[us] = map[string]bool{}
			}
			for _, ca := range textList(list(entry, "criterios")) {
				covered[us][ca] = true
			}
		}
	}
	return covered
}

func mustCoveredByT(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	covered := e.coveredByTasks()
	for _, us := range e.mustStories() {
		if covered[us.Code] == nil {
			o.fail(us.Code, "", "Must story without task")
		}
	}
	return o, nil
}

func criteriaMustCoveredByT(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	covered := e.coveredByTasks()
	for _, us := range e.mustStories() {
		for _, ca := range criteria(us.Header) {
			if !covered[us.Code][ca] {
				o.fail(us.Code+"/"+ca, "", "Must criterion without task")
			}
		}
	}
	return o, nil
}

func componentJustified(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, arq := range e.current("ARQ") {
		for _, item := range list(arq.Header, "componentes") {
			component := object(item)
			serves := textList(list(component, "atende"))
			if len(serves) == 0 {
				o.fail("ARQ", "/componentes", "component "+text(component["nome"])+" serves no US or RNF")
			}
			for _, code := range serves {
				if a, ok := e.byCode[code]; !ok || a.State == domainartifact.StateReplaced {
					o.fail("ARQ", "/componentes", "component "+text(component["nome"])+" serves unknown "+code)
				}
			}
		}
	}
	return o, nil
}

func adrListedInARQ(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	arq := e.byCode["ARQ"]
	if arq == nil {
		return o, nil
	}
	listed := map[string]bool{}
	for _, code := range textList(list(arq.Header, "decisoes")) {
		listed[code] = true
		if a, ok := e.byCode[code]; !ok {
			o.fail("ARQ", "/decisoes", "unknown "+code)
		} else if a.State == domainartifact.StateReplaced {
			o.fail("ARQ", "/decisoes", "replaced "+code+" in the list")
		}
	}
	for _, adr := range e.current("ADR") {
		if !listed[adr.Code] {
			o.fail("ARQ", "/decisoes", adr.Code+" missing in ARQ.decisoes")
		}
		if old := text(adr.Header["substitui"]); old != "" && listed[old] {
			o.fail("ARQ", "/decisoes", "replaced "+old+" in the list")
		}
	}
	return o, nil
}

// tasksCoverStories is traceability.t_points_to_us (RAS-08, US-18 CA-1): every functional T covers
// stories of BACKLOG.historias and criteria that exist in them.
func tasksCoverStories(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	scope := map[string]bool{}
	backlogs := e.current("BACKLOG")
	for _, backlog := range backlogs {
		for _, item := range list(backlog.Header, "historias") {
			scope[text(object(item)["us"])] = true
		}
	}
	for _, t := range e.current("T") {
		nature := text(t.Header["natureza"])
		covers := list(t.Header, "cobre")
		if len(covers) == 0 && (nature == "" || nature == "funcional") {
			o.fail(t.Code, "/cobre", "functional task covers no US")
		}
		for _, item := range covers {
			entry := object(item)
			us := text(entry["us"])
			story, ok := e.byCode[us]
			if !ok {
				o.fail(t.Code, "/cobre", "unknown US "+us)
				continue
			}
			if len(backlogs) > 0 && !scope[us] {
				o.fail(t.Code, "/cobre", us+" is out of BACKLOG.historias")
			}
			known := map[string]bool{}
			for _, ca := range criteria(story.Header) {
				known[ca] = true
			}
			for _, ca := range textList(list(entry, "criterios")) {
				if !known[ca] {
					o.fail(t.Code, "/cobre", us+" has no "+ca)
				}
			}
		}
	}
	return o, nil
}

func usMetByComponent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	arq := e.byCode["ARQ"]
	if arq == nil {
		return o, nil
	}
	served := map[string]bool{}
	for _, item := range list(arq.Header, "componentes") {
		for _, code := range textList(list(object(item), "atende")) {
			served[code] = true
		}
	}
	needed := map[string]bool{}
	for _, us := range e.mustStories() {
		needed[us.Code] = true
	}
	for _, r := range e.current("RNF") {
		if text(r.Header["prioridade"]) == "must" {
			needed[r.Code] = true
		}
	}
	for _, code := range textList(list(object(arq.Header["contexto"]), "rnf_criticos")) {
		needed[code] = true
	}
	for _, code := range sorted(needed) {
		if !served[code] {
			o.fail("ARQ", "/componentes", code+" served by no component")
		}
	}
	return o, nil
}

func operationsInContract(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	operations, _, hasContract, err := e.contract(ctx)
	if err != nil {
		return o, err
	}
	check := func(reference, path string, ids []string) {
		for _, id := range ids {
			switch {
			case !hasContract:
				o.fail(reference, path, "operation "+id+" without API contract")
			case operations != nil && !operations[id]:
				o.fail(reference, path, "operation "+id+" not in the contract")
			}
		}
	}
	for _, t := range e.current("T") {
		check(t.Code, "/operacoes", textList(list(t.Header, "operacoes")))
	}
	for _, telas := range e.current("TELAS") {
		for _, item := range list(telas.Header, "telas") {
			screen := object(item)
			check("TELAS/"+text(screen["id"]), "/telas", textList(list(screen, "operacoes")))
		}
	}
	if hasContract && operations == nil && len(o.evidence) == 0 {
		return notice("ARQ", "operations not verified: contract not readable"), nil
	}
	return o, nil
}

// --- Dados

// ddlExecutes reads the sandbox record of the data stage: the last validate_ddl execution of its last
// wave passed. Without one (the sandbox support services, ephemeral PostgreSQL, are not available yet)
// the check passes with a notice (MARCO-3 pending item).
func ddlExecutes(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	last, err := e.p.store.LastExecution(ctx, e.projectID, e.stage.ID, categoryValidateDDL)
	if err != nil {
		return o, err
	}
	switch {
	case last == nil:
		return notice("DIC", "DDL not executed: sandbox support service (ephemeral PostgreSQL) not available"), nil
	case !last.Green():
		o.fail(last.Wave, "docs/dados/esquema.sql", "DDL failed in the sandbox (execution "+last.ID+")")
	}
	return o, nil
}

var createTable = regexp.MustCompile(`(?is)create\s+table\s+(?:if\s+not\s+exists\s+)?([a-z_][a-z0-9_.]*)\s*\((.*?)\n\s*\)\s*;`)
var columnLine = regexp.MustCompile(`(?m)^\s*([a-z_][a-z0-9_]*)\s+[a-z]`)
var notColumns = map[string]bool{"constraint": true, "primary": true, "foreign": true, "unique": true, "check": true, "exclude": true}

// ddlTables reads table and column names of a DDL (static reading, no database).
func ddlTables(sql string) map[string]map[string]bool {
	tables := map[string]map[string]bool{}
	for _, m := range createTable.FindAllStringSubmatch(sql, -1) {
		name := strings.ToLower(m[1])
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		columns := map[string]bool{}
		for _, c := range columnLine.FindAllStringSubmatch(m[2], -1) {
			if col := strings.ToLower(c[1]); !notColumns[col] {
				columns[col] = true
			}
		}
		tables[name] = columns
	}
	return tables
}

func dicTables(header map[string]any) map[string]map[string]bool {
	tables := map[string]map[string]bool{}
	for _, item := range list(header, "tabelas") {
		table := object(item)
		columns := map[string]bool{}
		for _, c := range list(table, "colunas") {
			columns[text(object(c)["nome"])] = true
		}
		tables[text(table["nome"])] = columns
	}
	return tables
}

func compareTables(o *outcome, reference, path string, want, got map[string]map[string]bool, what string) {
	for table, columns := range want {
		other, ok := got[table]
		if !ok {
			o.fail(reference, path, "table "+table+" missing in "+what)
			continue
		}
		for column := range columns {
			if !other[column] {
				o.fail(reference, path, "column "+table+"."+column+" missing in "+what)
			}
		}
		for column := range other {
			if !columns[column] {
				o.fail(reference, path, "column "+table+"."+column+" only in "+what)
			}
		}
	}
	for table := range got {
		if _, ok := want[table]; !ok {
			o.fail(reference, path, "table "+table+" only in "+what)
		}
	}
	sortEvidence(o.evidence)
}

// modelConsistent compares DIC.tabelas with the reference DDL (table and column names; the catalog of
// the database comes with dic.ddl_executes when the sandbox support services exist).
func modelConsistent(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	dic := e.byCode["DIC"]
	if dic == nil {
		return o, nil
	}
	files, err := e.repositoryFiles(ctx)
	if err != nil {
		return o, err
	}
	sql, ok := files["docs/dados/esquema.sql"]
	if files == nil || !ok {
		return notice("DIC", "model not compared: docs/dados/esquema.sql not available"), nil
	}
	compareTables(&o, "DIC", "/tabelas", dicTables(dic.Header), ddlTables(sql), "esquema.sql")
	return o, nil
}

func personalDataMarked(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, dic := range e.current("DIC") {
		marked := map[string]bool{}
		for _, item := range list(dic.Header, "tabelas") {
			table := object(item)
			for _, c := range list(table, "colunas") {
				column := object(c)
				if column["dado_pessoal"] == true {
					marked[text(table["nome"])+"."+text(column["nome"])] = true
				}
			}
		}
		listed := map[string]bool{}
		for _, item := range list(dic.Header, "dados_pessoais") {
			field := text(object(item)["campo"])
			listed[field] = true
			if !marked[field] {
				o.fail("DIC", "/dados_pessoais", field+" is not a column marked as personal data")
			}
		}
		for _, field := range sorted(marked) {
			if !listed[field] {
				o.fail("DIC", "/dados_pessoais", "personal column "+field+" missing in dados_pessoais")
			}
		}
	}
	return o, nil
}

func legalBasisDefined(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, dic := range e.current("DIC") {
		for _, item := range list(dic.Header, "dados_pessoais") {
			if entry := object(item); text(entry["base_legal"]) == "a_confirmar" {
				o.fail("DIC", "/dados_pessoais", "legal basis to confirm: "+text(entry["campo"]))
			}
		}
	}
	return o, nil
}

// modelReferences is persist.model_references (RAS-14).
func modelReferences(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	dic := e.byCode["DIC"]
	if dic == nil {
		return o, nil
	}
	tables := dicTables(dic.Header)
	for _, persist := range e.current("PERSIST") {
		for _, item := range list(persist.Header, "indices") {
			index := object(item)
			name, table := text(index["nome"]), text(index["tabela"])
			columns, known := tables[table]
			if !known {
				o.fail(persist.Code, "/indices", "index "+name+" on unknown table "+table)
				continue
			}
			for _, column := range textList(list(index, "colunas")) {
				if !columns[column] {
					o.fail(persist.Code, "/indices", "index "+name+" on unknown column "+table+"."+column)
				}
			}
		}
	}
	return o, nil
}

func performanceRNFInPersist(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	listed := map[string]bool{}
	for _, persist := range e.current("PERSIST") {
		for _, item := range list(persist.Header, "desempenho") {
			listed[text(object(item)["rnf"])] = true
		}
	}
	for _, r := range e.current("RNF") {
		if text(r.Header["categoria"]) == "desempenho" && !listed[r.Code] {
			o.fail(r.Code, "", "performance RNF without item in PERSIST.desempenho")
		}
	}
	return o, nil
}

// --- Design

func screenFilesPresent(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	files, err := e.repositoryFiles(ctx)
	if err != nil {
		return o, err
	}
	for _, telas := range e.current("TELAS") {
		if files == nil {
			return notice("TELAS", "files not verified: project repository not available"), nil
		}
		paths := []string{text(telas.Header["fluxo"]), text(telas.Header["prototipo"]), text(telas.Header["estilo"])}
		for _, item := range list(telas.Header, "telas") {
			paths = append(paths, text(object(item)["arquivo"]))
		}
		for _, p := range paths {
			if p != "" && !fileExists(files, p) && !fileExists(files, strings.TrimSuffix(p, "/")+"/") {
				o.fail("TELAS", p, "file not found")
			}
		}
	}
	return o, nil
}

// criteriaConsistent is telas.criteria_consistent (RAS-18).
func criteriaConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, telas := range e.current("TELAS") {
		for _, item := range list(telas.Header, "telas") {
			screen := object(item)
			own := map[string]bool{}
			for _, us := range textList(list(screen, "us")) {
				own[us] = true
			}
			for _, criterion := range textList(list(screen, "criterios")) {
				if us, _, _ := strings.Cut(criterion, "/"); !own[us] {
					o.fail("TELAS/"+text(screen["id"]), "/telas", "criterion "+criterion+" of a US outside the screen")
				}
			}
		}
	}
	return o, nil
}

var hrefPattern = regexp.MustCompile(`href\s*=\s*["']([^"'#?]+)`)

// prototypeNavigable checks the prototype statically (no browser in the sandbox yet): every screen
// file exists and, beyond the first, is linked from another page of the prototype.
func prototypeNavigable(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	files, err := e.repositoryFiles(ctx)
	if err != nil {
		return o, err
	}
	for _, telas := range e.current("TELAS") {
		if files == nil {
			return notice("TELAS", "prototype not verified: project repository not available"), nil
		}
		screens := list(telas.Header, "telas")
		linked := map[string]bool{}
		for p, content := range files {
			if !strings.HasSuffix(p, ".html") {
				continue
			}
			dir := p[:strings.LastIndex(p, "/")+1]
			for _, m := range hrefPattern.FindAllStringSubmatch(content, -1) {
				linked[cleanJoin(dir, m[1])] = true
			}
		}
		for i, item := range screens {
			file := text(object(item)["arquivo"])
			if _, ok := files[file]; !ok {
				o.fail("TELAS/"+text(object(item)["id"]), file, "screen file not found")
			} else if i > 0 && !linked[file] {
				o.fail("TELAS/"+text(object(item)["id"]), file, "screen not reachable from the prototype")
			}
		}
	}
	return o, nil
}

func cleanJoin(dir, ref string) string {
	parts := strings.Split(dir+ref, "/")
	var out []string
	for _, part := range parts {
		switch part {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, part)
		}
	}
	return strings.Join(out, "/")
}

func accessibilityVerified(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, telas := range e.current("TELAS") {
		if object(telas.Header["acessibilidade"])["verificado"] != true {
			o.fail("TELAS", "/acessibilidade/verificado", "accessibility not verified")
		}
	}
	return o, nil
}

// stateByParameter checks statically (PL-45) that each screen page reads the state parameter and
// declares each of its states.
func stateByParameter(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	files, err := e.repositoryFiles(ctx)
	if err != nil {
		return o, err
	}
	for _, telas := range e.current("TELAS") {
		if files == nil {
			return notice("TELAS", "states not verified: project repository not available"), nil
		}
		for _, item := range list(telas.Header, "telas") {
			screen := object(item)
			content, ok := files[text(screen["arquivo"])]
			if !ok {
				continue
			}
			if !strings.Contains(content, "state") {
				o.fail("TELAS/"+text(screen["id"]), text(screen["arquivo"]), "page ignores the state parameter")
				continue
			}
			for _, state := range textList(list(screen, "estados")) {
				if !strings.Contains(content, state) {
					o.fail("TELAS/"+text(screen["id"]), text(screen["arquivo"]), "state "+state+" not in the page")
				}
			}
		}
	}
	return o, nil
}

// screensLinkStories is traceability.interface_us_in_screen (RAS-17, US-21).
func screensLinkStories(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	covered := map[string]bool{}
	for _, telas := range e.current("TELAS") {
		for _, item := range list(telas.Header, "telas") {
			screen := object(item)
			id := text(screen["id"])
			known := 0
			for _, code := range textList(list(screen, "us")) {
				if _, ok := e.byCode[code]; ok {
					covered[code] = true
					known++
				} else {
					o.fail(telas.Code+"/"+id, "/telas", "screen cites unknown US "+code)
				}
			}
			if known == 0 {
				o.fail(telas.Code+"/"+id, "/telas", "orphan screen: no US of the project")
			}
		}
	}
	for _, us := range e.current("US") {
		if us.Header["tem_interface"] == true && text(us.Header["prioridade"]) != "wont" && !covered[us.Code] {
			o.fail(us.Code, "/tem_interface", "US with interface in no screen")
		}
	}
	sortEvidence(o.evidence)
	return o, nil
}

// --- Validação técnica

func (e *evaluation) validation() *ArtifactRecord { return e.latest("VALIDACAO") }

func validationChecksComplete(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	v := e.validation()
	if v == nil {
		return o, nil
	}
	count := map[string]int{}
	for _, item := range list(v.Header, "conferencias") {
		count[text(object(item)["par"])]++
	}
	for _, pair := range []string{"arquitetura_dados", "api_dados", "api_telas", "ameacas_arquitetura", "cobertura_us_t_ct"} {
		if count[pair] != 1 {
			o.fail(v.Code, "/conferencias", "pair "+pair+" must appear exactly once")
		}
	}
	return o, nil
}

// opinionsRecorded is validacao.review_opinions_recorded.
func opinionsRecorded(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	v := e.validation()
	if v == nil {
		return o, nil
	}
	recorded, err := e.p.store.ReviewOpinions(ctx, e.projectID, e.stage.ID)
	if err != nil {
		return o, err
	}
	given := map[string]map[string]any{}
	for _, item := range list(v.Header, "pareceres") {
		entry := object(item)
		name := text(entry["participante"])
		if _, dup := given[name]; dup {
			o.fail(v.Code, "/pareceres", "repeated opinion of "+name)
		}
		given[name] = entry
	}
	for _, reviewer := range e.stage.Reviewers() {
		name := reviewerName(reviewer.Agent)
		opinion, ok := recorded[reviewer.Agent]
		entry, written := given[name]
		switch {
		case !ok:
			o.fail(v.Code, "/pareceres", "no completed wave of "+reviewer.Agent+" with its opinion")
		case !written || text(entry["parecer"]) != opinion.Opinion:
			o.fail(v.Code, "/pareceres", "opinion of "+name+" differs from wave "+opinion.Wave)
		}
	}
	if _, ok := given[reviewerName(e.stage.Agent)]; !ok {
		o.fail(v.Code, "/pareceres", "no opinion of the lead")
	}
	return o, nil
}

func verdictUnblocked(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	if v := e.validation(); v != nil && text(v.Header["veredito"]) != "liberado" {
		o.fail(v.Code, "/veredito", "validation returned")
	}
	return o, nil
}

func reviewOpinionRejected(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	v := e.validation()
	if v == nil || text(v.Header["veredito"]) != "liberado" {
		return o, nil
	}
	for _, item := range list(v.Header, "pareceres") {
		entry := object(item)
		if text(entry["parecer"]) == "reprovado" {
			o.fail(v.Code, "/pareceres", "opinion of "+text(entry["participante"])+" rejected with the validation released")
		}
	}
	return o, nil
}

func threatsHandled(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, model := range e.current("AMEACAS") {
		for _, item := range list(model.Header, "ameacas") {
			threat := object(item)
			severity := text(threat["severidade"])
			if severity != "critico" && severity != "alto" {
				continue
			}
			id := "AMEACAS/" + text(threat["id"])
			finding := e.finding(text(threat["achado"]))
			switch text(threat["status"]) {
			case "mitigada":
				if strings.TrimSpace(text(threat["mitigacao"])) == "" || len(list(threat, "onde")) == 0 {
					o.fail(id, "/ameacas", "mitigated threat without mitigation or place")
				}
			case "achado":
				if finding == nil {
					o.fail(id, "/ameacas", "threat without the finding")
				}
			case "aceita":
				if finding == nil || finding.Status != domainfinding.StatusAccepted {
					o.fail(id, "/ameacas", "accepted threat without a finding accepted by a human")
				}
			default:
				o.fail(id, "/ameacas", "high or critical threat without treatment")
			}
		}
	}
	return o, nil
}

// ctCoverage returns US → criterion → CT codes, and RNF → CT codes, of the current CT.
func (e *evaluation) ctCoverage() (map[string]map[string][]*ArtifactRecord, map[string][]*ArtifactRecord) {
	criteria := map[string]map[string][]*ArtifactRecord{}
	rnf := map[string][]*ArtifactRecord{}
	for _, ct := range e.current("CT") {
		covers := object(ct.Header["cobre"])
		if us := text(covers["us"]); us != "" {
			if criteria[us] == nil {
				criteria[us] = map[string][]*ArtifactRecord{}
			}
			ca := text(covers["criterio"])
			criteria[us][ca] = append(criteria[us][ca], ct)
		}
		if code := text(covers["rnf"]); code != "" {
			rnf[code] = append(rnf[code], ct)
		}
	}
	return criteria, rnf
}

func criteriaMustCoveredByCT(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	if len(e.current("CT")) == 0 {
		return o, nil // target without items (no QA plan in the workflow in force)
	}
	coverage, _ := e.ctCoverage()
	for _, us := range e.mustStories() {
		for _, ca := range criteria(us.Header) {
			if len(coverage[us.Code][ca]) == 0 {
				o.fail(us.Code+"/"+ca, "", "Must criterion without CT")
			}
		}
	}
	return o, nil
}

func rnfMustCoveredByCT(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	if len(e.current("CT")) == 0 {
		return o, nil
	}
	_, coverage := e.ctCoverage()
	for _, r := range e.current("RNF") {
		kind := text(object(r.Header["verificacao"])["tipo"])
		if text(r.Header["prioridade"]) == "must" && (kind == "teste_automatizado" || kind == "medicao") && len(coverage[r.Code]) == 0 {
			o.fail(r.Code, "", "testable Must RNF without CT")
		}
	}
	return o, nil
}

func tasksCTsConsistent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, t := range e.current("T") {
		covered := map[string]bool{}
		stories := map[string]bool{}
		for _, item := range list(t.Header, "cobre") {
			entry := object(item)
			stories[text(entry["us"])] = true
			for _, ca := range textList(list(entry, "criterios")) {
				covered[text(entry["us"])+"/"+ca] = true
			}
		}
		for _, code := range textList(list(t.Header, "cts")) {
			ct, ok := e.byCode[code]
			if !ok {
				continue
			}
			covers := object(ct.Header["cobre"])
			related := covered[text(covers["us"])+"/"+text(covers["criterio"])]
			if rnf := text(covers["rnf"]); rnf != "" {
				if r, ok := e.byCode[rnf]; ok {
					for _, us := range textList(list(r.Header, "us_afetadas")) {
						related = related || stories[us]
					}
				}
			}
			if !related {
				o.fail(t.Code, "/cts", code+" does not cover what the task covers")
			}
		}
	}
	return o, nil
}

func criteriaCoveredByCT(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	if len(e.current("CT")) == 0 {
		return o, nil
	}
	coverage, _ := e.ctCoverage()
	for _, backlog := range e.current("BACKLOG") {
		for _, item := range list(backlog.Header, "historias") {
			entry := object(item)
			priority := text(entry["prioridade"])
			us, ok := e.byCode[text(entry["us"])]
			if !ok || (priority != "should" && priority != "could") {
				continue
			}
			for _, ca := range criteria(us.Header) {
				if len(coverage[us.Code][ca]) == 0 {
					o.fail(us.Code+"/"+ca, "", "criterion without CT")
				}
			}
		}
	}
	return o, nil
}

func contractGapResolved(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	listed := map[string]bool{}
	for _, telas := range e.current("TELAS") {
		for _, code := range textList(list(telas.Header, "lacunas_contrato")) {
			listed[code] = true
			f := e.finding(code)
			switch {
			case f == nil:
				o.fail("TELAS", "/lacunas_contrato", "unknown finding "+code)
			case f.Category != "contract_gap" && !domainfinding.JustifiedDestination(f.Body):
				o.fail(code, "/categoria", "contract gap of another category")
			case f.DestinationStage != "architecture":
				o.fail(code, "/etapa_destino", "contract gap not routed to the architecture")
			case domainfinding.Active(f.Status):
				o.fail(code, "/situacao", "contract gap still active")
			}
		}
	}
	for _, f := range e.findings {
		if f.Category == "contract_gap" && f.Author == "designer-agent" && f.Status != domainfinding.StatusDiscarded && !listed[f.Code] {
			o.fail(f.Code, "", "contract gap of the designer missing in TELAS.lacunas_contrato")
		}
	}
	return o, nil
}

func validationCoverageConsistent(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	v := e.validation()
	if v == nil {
		return o, nil
	}
	ok := false
	for _, item := range list(v.Header, "conferencias") {
		entry := object(item)
		ok = ok || (text(entry["par"]) == "cobertura_us_t_ct" && text(entry["resultado"]) == "ok")
	}
	if !ok {
		return o, nil
	}
	for id, f := range map[string]checkFunc{"traceability.t_points_to_us": tasksCoverStories, "traceability.us_must_covered_by_t": mustCoveredByT,
		"traceability.ca_must_covered_by_ct": criteriaMustCoveredByCT, "traceability.rnf_must_covered_by_ct": rnfMustCoveredByCT} {
		result, err := f(ctx, e)
		if err != nil {
			return o, err
		}
		if !result.passed() {
			o.fail(v.Code, "/conferencias", "cobertura_us_t_ct ok but "+id+" fails")
		}
	}
	sortEvidence(o.evidence)
	return o, nil
}

func validationVersionsCurrent(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	v := e.validation()
	if v == nil {
		return o, nil
	}
	checked := map[string]int{}
	items := list(v.Header, "artefatos_conferidos")
	if len(items) == 0 {
		o.fail(v.Code, "/artefatos_conferidos", "checked artifacts missing")
		return o, nil
	}
	for _, item := range items {
		entry := object(item)
		n := number(entry["versao"])
		checked[text(entry["id"])] = n
	}
	for _, input := range e.stage.Inputs {
		for _, a := range e.current(input.Type) {
			version, ok := checked[a.Code]
			switch {
			case !ok:
				o.fail(v.Code, "/artefatos_conferidos", a.Code+" not checked")
			case version != a.Version:
				o.fail(v.Code, "/artefatos_conferidos", a.Code+" checked in version "+itoa(version)+", current "+itoa(a.Version))
			}
		}
	}
	sortEvidence(o.evidence)
	return o, nil
}

// --- Implementação

// executionGreen is code.build_green, tests.executed_green and code.lint_clean: the last command of the
// category in the last wave of the stage ran in the sandbox with exit code 0 and, for tests, at least one
// test run and none failed (US-19 CA-1, US-41).
func (e *evaluation) executionGreen(ctx context.Context, check, category string) (outcome, error) {
	var o outcome
	last, err := e.p.store.LastExecution(ctx, e.projectID, e.stage.ID, category)
	if err != nil {
		return o, err
	}
	switch {
	case last == nil:
		o.fail("", "", category+" not executed in the sandbox")
	case !last.Green():
		o.evidence = []map[string]any{{"reference": last.Wave, "execution": last.ID, "end_reason": last.EndReason, "message": category + " not green"}}
	case category == categoryTests && !testsGreen(*last):
		o.evidence = []map[string]any{{"reference": last.Wave, "execution": last.ID, "message": "no test executed or a test failed"}}
	}
	_ = check
	return o, nil
}

func testsGreen(e Execution) bool {
	return e.TestsTotal != nil && *e.TestsTotal > 0 && (e.TestsFailed == nil || *e.TestsFailed == 0)
}

var migrationFile = regexp.MustCompile(`^app/.*migrations?/[^/]+\.sql$`)

// migrationsConsistent compares the tables of the app migrations with the approved reference DDL
// (static reading; applying them in a clean database needs the sandbox support services).
func migrationsConsistent(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	files, err := e.repositoryFiles(ctx)
	if err != nil {
		return o, err
	}
	if files == nil {
		return notice("", "migrations not verified: project repository not available"), nil
	}
	reference, ok := files["docs/dados/esquema.sql"]
	var migrations strings.Builder
	names := make([]string, 0)
	for p := range files {
		if migrationFile.MatchString(p) && !strings.Contains(p, ".down.") {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	for _, p := range names {
		migrations.WriteString(files[p] + "\n")
	}
	if !ok || len(names) == 0 {
		return notice("", "no migrations or reference DDL to compare"), nil
	}
	compareTables(&o, "app/migrations", "", ddlTables(reference), ddlTables(migrations.String()), "the migrations")
	return o, nil
}

func apiContractConsistent(ctx context.Context, e *evaluation) (outcome, error) {
	_, _, hasContract, err := e.contract(ctx)
	if err != nil || !hasContract {
		return outcome{}, err
	}
	return notice("ARQ", "contract not compared with the running application: sandbox support services not available"), nil
}

func tasksCompleted(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, t := range e.current("T") {
		switch text(t.Header["situacao"]) {
		case "concluida":
		case "justificada":
			if strings.TrimSpace(text(t.Header["justificativa"])) == "" {
				o.fail(t.Code, "/justificativa", "justified task without justification")
			}
		default:
			o.fail(t.Code, "/situacao", "task "+orDash(text(t.Header["situacao"])))
		}
	}
	return o, nil
}

func orDash(s string) string {
	if s == "" {
		return "planejada"
	}
	return s
}

func devRecordPresent(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, t := range e.current("T") {
		if text(t.Header["situacao"]) != "concluida" {
			continue
		}
		commits := textList(list(object(t.Header["registro_dev"]), "commits"))
		if len(commits) == 0 {
			o.fail(t.Code, "/registro_dev/commits", "completed task without commit")
		}
		if e.p.repository == nil {
			continue
		}
		for _, sha := range commits {
			ok, err := e.p.repository.Reachable(ctx, e.projectID, sha)
			if err != nil {
				return o, err
			}
			if !ok {
				o.fail(t.Code, "/registro_dev/commits", "commit "+sha+" not in the repository")
			}
		}
	}
	return o, nil
}

func fileScopeRespected(_ context.Context, e *evaluation) (outcome, error) {
	if len(e.current("T")) == 0 {
		return outcome{}, nil
	}
	return notice("T", "file scope not compared: changed files per commit not read yet"), nil
}

// --- QA

func (e *evaluation) verdict(artifactType, approved string) outcome {
	var o outcome
	if r := e.latest(artifactType); r != nil && text(r.Header["veredito"]) != approved {
		o.fail(r.Code, "/veredito", "verdict "+text(r.Header["veredito"]))
	}
	return o
}

func execution(ct *ArtifactRecord) map[string]any { return object(ct.Header["execucao"]) }

func qaConsistentWithCT(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	report := e.latest("REL-QA")
	if report == nil {
		return o, nil
	}
	counts := map[string]int{}
	for _, item := range list(report.Header, "matriz") {
		row := object(item)
		result := text(row["resultado"])
		counts[result]++
		if ct, ok := e.byCode[text(row["ct"])]; ok && text(execution(ct)["resultado"]) != result {
			o.fail(report.Code, "/matriz", text(row["ct"])+" is "+text(execution(ct)["resultado"])+" in the CT")
		}
	}
	summary := object(report.Header["resumo"])
	expect := func(field string, value int) {
		if n := number(summary[field]); n != value {
			o.fail(report.Code, "/resumo/"+field, field+" is "+itoa(n)+", expected "+itoa(value))
		}
	}
	expect("ct_executados", counts["passou"]+counts["falhou"]+counts["bloqueado"])
	expect("passaram", counts["passou"])
	expect("falharam", counts["falhou"])
	expect("bloqueados", counts["bloqueado"])
	total, covered := 0, 0
	coverage, _ := e.ctCoverage()
	for _, us := range e.mustStories() {
		for _, ca := range criteria(us.Header) {
			total++
			for _, ct := range coverage[us.Code][ca] {
				if text(execution(ct)["resultado"]) == "passou" {
					covered++
					break
				}
			}
		}
	}
	expect("ca_must_total", total)
	expect("ca_must_cobertos", covered)
	return o, nil
}

func qaOpenFindings(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	report := e.latest("REL-QA")
	if report == nil {
		return o, nil
	}
	listed := map[string]bool{}
	for _, code := range textList(list(report.Header, "achados_abertos")) {
		listed[code] = true
		if f := e.finding(code); f == nil || !domainfinding.Active(f.Status) {
			o.fail(report.Code, "/achados_abertos", code+" is not active")
		}
	}
	for _, f := range e.findings {
		if f.Author == "qa-agent" && domainfinding.Active(f.Status) && !listed[f.Code] {
			o.fail(report.Code, "/achados_abertos", "active "+f.Code+" missing")
		}
	}
	return o, nil
}

func mustWithCTPassing(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	if e.latest("REL-QA") == nil {
		return o, nil
	}
	coverage, _ := e.ctCoverage()
	for _, us := range e.mustStories() {
		for _, ca := range criteria(us.Header) {
			passing := false
			for _, ct := range coverage[us.Code][ca] {
				passing = passing || text(execution(ct)["resultado"]) == "passou"
			}
			if !passing {
				o.fail(us.Code+"/"+ca, "", "Must criterion without passing CT")
			}
		}
	}
	return o, nil
}

func ctExecutionLinked(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	files, err := e.repositoryFiles(ctx)
	if err != nil {
		return o, err
	}
	for _, ct := range e.current("CT") {
		run := execution(ct)
		if run == nil || text(run["onda"]) == "" {
			o.fail(ct.Code, "/execucao", "CT without execution")
			continue
		}
		if text(run["resultado"]) != "passou" {
			continue
		}
		test := text(run["teste"])
		if text(ct.Header["nivel"]) == "nao_funcional" && test == "" {
			if strings.TrimSpace(text(run["evidencia"])) == "" {
				o.fail(ct.Code, "/execucao", "non functional CT without evidence")
			}
			continue
		}
		file, name, _ := strings.Cut(test, "::")
		if files == nil {
			continue
		}
		if content, ok := files[file]; !ok || !strings.Contains(content, name) {
			o.fail(ct.Code, "/execucao/teste", "test "+test+" not in the repository")
		}
	}
	return o, nil
}

func ctNotExecutedJustified(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	report := e.latest("REL-QA")
	justified := map[string]bool{}
	if report != nil {
		for _, item := range list(report.Header, "sem_cobertura") {
			entry := object(item)
			if strings.TrimSpace(text(entry["motivo"])) != "" {
				justified[text(entry["us"])+"/"+text(entry["criterio"])] = true
			}
		}
	}
	for _, ct := range e.current("CT") {
		result := text(execution(ct)["resultado"])
		covers := object(ct.Header["cobre"])
		if (result == "nao_executado" || result == "bloqueado") && !justified[text(covers["us"])+"/"+text(covers["criterio"])] {
			o.fail(ct.Code, "/execucao", "CT "+result+" without reason in the report")
		}
	}
	return o, nil
}

func commandsReproducible(_ context.Context, e *evaluation) (outcome, error) {
	if e.latest("REL-QA") == nil {
		return outcome{}, nil
	}
	return notice("REL-QA", "commands not rerun in a clean sandbox by the gate"), nil
}

func ctTestInT(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, ct := range e.current("CT") {
		test := text(execution(ct)["teste"])
		covers := object(ct.Header["cobre"])
		if test == "" || text(covers["us"]) == "" {
			continue
		}
		file, _, _ := strings.Cut(test, "::")
		found := false
		for _, t := range e.current("T") {
			tests := textList(list(object(t.Header["registro_dev"]), "testes"))
			for _, item := range list(t.Header, "cobre") {
				entry := object(item)
				if text(entry["us"]) == text(covers["us"]) && contains(textList(list(entry, "criterios")), text(covers["criterio"])) {
					for _, recorded := range tests {
						found = found || recorded == file || strings.HasPrefix(recorded, file+"::")
					}
				}
			}
		}
		if !found {
			o.fail(ct.Code, "/execucao/teste", "test of the CT in no task covering the criterion")
		}
	}
	return o, nil
}

// --- Segurança

func securityFinding(f domainfinding.Finding) bool {
	return f.Category == "vulnerability" || f.Author == "security-agent"
}

func noCriticalRisk(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	for _, f := range e.findings {
		if securityFinding(f) && f.Blocking() {
			o.fail(f.Code, "/situacao", f.Severity+" security finding "+f.Status)
		}
	}
	for _, model := range e.current("AMEACAS") {
		for _, item := range list(model.Header, "ameacas") {
			threat := object(item)
			severity := text(threat["severidade"])
			if (severity == "critico" || severity == "alto") && text(threat["status"]) == "achado" {
				if f := e.finding(text(threat["achado"])); f != nil && domainfinding.Active(f.Status) {
					o.fail("AMEACAS/"+text(threat["id"]), "/ameacas", "threat with active finding "+f.Code)
				}
			}
		}
	}
	if report := e.latest("REL-SEG"); report != nil {
		summary := object(report.Header["resumo"])
		for _, level := range []string{"critico", "alto"} {
			if n := number(object(summary[level])["abertos"]); n > 0 {
				o.fail(report.Code, "/resumo/"+level, "open "+level+" findings in the report")
			}
		}
	}
	return o, nil
}

func securitySummary(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	report := e.latest("REL-SEG")
	if report == nil {
		return o, nil
	}
	counts := map[string]map[string]int{}
	for _, f := range e.findings {
		if f.Author != "security-agent" || f.Status == domainfinding.StatusDiscarded {
			continue
		}
		level := domainfinding.HeaderSeverity(f.Severity)
		if counts[level] == nil {
			counts[level] = map[string]int{}
		}
		switch {
		case domainfinding.Active(f.Status):
			counts[level]["abertos"]++
		case f.Status == domainfinding.StatusClosed:
			counts[level]["fechados"]++
		case f.Status == domainfinding.StatusAccepted:
			counts[level]["aceitos"]++
		}
	}
	summary := object(report.Header["resumo"])
	for _, level := range []string{"critico", "alto", "medio", "baixo"} {
		for _, field := range []string{"abertos", "fechados", "aceitos"} {
			if n := number(object(summary[level])[field]); n != counts[level][field] {
				o.fail(report.Code, "/resumo/"+level+"/"+field, "is "+itoa(n)+", the findings say "+itoa(counts[level][field]))
			}
		}
	}
	return o, nil
}

// scansExecuted: opengrep, trivy and gitleaks each with a command recorded in the sandbox executions of
// the audit wave (the wave of the REL-SEG version).
func scansExecuted(ctx context.Context, e *evaluation) (outcome, error) {
	var o outcome
	report := e.latest("REL-SEG")
	if report == nil {
		return o, nil
	}
	executions, err := e.p.store.WaveExecutions(ctx, e.projectID, report.WaveCode)
	if err != nil {
		return o, err
	}
	ran := map[string]bool{}
	for _, x := range executions {
		if x.Category == categoryScanner && x.ExitCode != nil {
			ran[x.Command] = true
		}
	}
	tools := map[string]bool{}
	for _, item := range list(report.Header, "varreduras") {
		scan := object(item)
		tool := text(scan["ferramenta"])
		tools[tool] = true
		if strings.TrimSpace(text(scan["versao"])) == "" || !ran[text(scan["comando"])] {
			o.fail(report.Code, "/varreduras", "scan of "+tool+" without execution recorded in "+report.WaveCode)
		}
	}
	for _, required := range []string{"opengrep", "trivy", "gitleaks"} {
		if !tools[required] {
			o.fail(report.Code, "/varreduras", "scan of "+required+" missing")
		}
	}
	return o, nil
}

func threatsChecked(_ context.Context, e *evaluation) (outcome, error) {
	var o outcome
	report := e.latest("REL-SEG")
	model := e.byCode["AMEACAS"]
	if report == nil || model == nil {
		return o, nil
	}
	inReport := map[string]bool{}
	for _, item := range list(report.Header, "ameacas_conferidas") {
		if id, ok := item.(string); ok {
			inReport[id] = true
			continue
		}
		entry := object(item)
		inReport[text(entry["ameaca"])] = true
		if text(entry["resultado"]) == "nao_mitigada" && len(list(entry, "achados")) == 0 {
			o.fail(report.Code, "/ameacas_conferidas", text(entry["ameaca"])+" not mitigated without finding")
		}
	}
	conference := map[string]map[string]any{}
	for _, item := range list(model.Header, "conferencia") {
		entry := object(item)
		conference[text(entry["ameaca"])] = entry
	}
	for _, item := range list(model.Header, "ameacas") {
		threat := object(item)
		id := text(threat["id"])
		if text(threat["status"]) == "mitigada" && (!inReport[id] || conference[id] == nil) {
			o.fail("AMEACAS/"+id, "/conferencia", "mitigated threat not checked")
		}
		severity := text(threat["severidade"])
		if c := conference[id]; c != nil && c["implementada"] == false && (severity == "critico" || severity == "alto") && e.finding(text(threat["achado"])) == nil {
			o.fail("AMEACAS/"+id, "/conferencia", "high threat not implemented without finding")
		}
	}
	return o, nil
}

// reviewerName is the participant name of an agent in VALIDACAO.pareceres.
func reviewerName(agent string) string {
	return map[string]string{"architect-agent": "arquiteto", "dba-agent": "dba", "designer-agent": "designer"}[agent]
}
