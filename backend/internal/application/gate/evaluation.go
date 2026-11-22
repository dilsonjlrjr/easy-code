package gate

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"easycode/internal/application/artifact"
	domainartifact "easycode/internal/domain/artifact"
	domainfinding "easycode/internal/domain/finding"
	"easycode/internal/domain/governance"
	domainworkflow "easycode/internal/domain/workflow"
)

// outcome is what a check found: no evidence = passed; evidence with failed = false is a notice of a
// passed check (something it could not verify, or an item without target).
type outcome struct {
	evidence []map[string]any
	notice   bool
}

func (o outcome) passed() bool { return len(o.evidence) == 0 || o.notice }

func (o *outcome) fail(reference, path, message string) {
	item := map[string]any{"message": message}
	if reference != "" {
		item["reference"] = reference
	}
	if path != "" {
		item["path"] = path
	}
	o.evidence = append(o.evidence, item)
}

func notice(reference, message string) outcome {
	o := outcome{notice: true}
	o.fail(reference, "", message)
	return o
}

// checkFunc implements one automatic check of checagens.yaml.
type checkFunc func(ctx context.Context, e *evaluation) (outcome, error)

// evaluation is the snapshot a round reads: the outputs, every current artifact, the findings and,
// lazily, the repository files and the waves of the round window.
type evaluation struct {
	p         *Progress
	projectID string
	stage     domainworkflow.Stage
	gate      governance.Gate
	round     int
	outputs   []artifact.Stored
	// all holds every artifact of the project, current version; byCode indexes it.
	all      []ArtifactRecord
	byCode   map[string]*ArtifactRecord
	findings []domainfinding.Finding
	record   StageRecord
	files    map[string]string
	loaded   bool
	waves    []WaveRecord
	wavesOK  bool
	// results of the round so far (traceability.validation_coverage_consistent reads them)
	results map[string]bool
}

func (e *evaluation) load(ctx context.Context) error {
	var err error
	if e.all, err = e.p.store.ProjectArtifacts(ctx, e.projectID); err != nil {
		return err
	}
	e.byCode = map[string]*ArtifactRecord{}
	for i := range e.all {
		e.byCode[e.all[i].Code] = &e.all[i]
	}
	if e.findings, err = e.p.store.ProjectFindings(ctx, e.projectID); err != nil {
		return err
	}
	if e.record, err = e.p.store.Stage(ctx, e.projectID, e.stage.ID); err != nil {
		return err
	}
	e.results = map[string]bool{}
	return nil
}

// repositoryFiles returns the files at HEAD; nil when the repository is not wired (notice).
func (e *evaluation) repositoryFiles(ctx context.Context) (map[string]string, error) {
	if e.loaded {
		return e.files, nil
	}
	e.loaded = true
	if e.p.repository == nil {
		return nil, nil
	}
	files, err := e.p.repository.Files(ctx, e.projectID)
	if err != nil {
		return nil, err
	}
	e.files = files
	return files, nil
}

// roundWaves are the waves of the stage and of its activities completed in the round window.
func (e *evaluation) roundWaves(ctx context.Context) ([]WaveRecord, error) {
	if e.wavesOK {
		return e.waves, nil
	}
	stages := []string{e.stage.ID}
	for _, a := range e.p.definition.ActivitiesFor(e.stage.ID) {
		stages = append(stages, a.ID)
	}
	waves, err := e.p.store.RoundWaves(ctx, e.projectID, stages, e.record.Since)
	if err != nil {
		return nil, err
	}
	e.waves, e.wavesOK = waves, true
	return waves, nil
}

// outputRecords are the artifacts of the round (the outputs in review).
func (e *evaluation) outputRecords() []*ArtifactRecord {
	out := make([]*ArtifactRecord, 0, len(e.outputs))
	for _, o := range e.outputs {
		if r, ok := e.byCode[o.Code]; ok {
			out = append(out, r)
		}
	}
	return out
}

// current are the artifacts of a type that are not replaced ("vigentes").
func (e *evaluation) current(artifactType string) []*ArtifactRecord {
	var out []*ArtifactRecord
	for i := range e.all {
		a := &e.all[i]
		if a.Type == artifactType && a.State != domainartifact.StateReplaced {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return codeLess(out[i].Code, out[j].Code) })
	return out
}

// latest is the current artifact of a numbered type with the highest number (the most recent REL-QA).
func (e *evaluation) latest(artifactType string) *ArtifactRecord {
	list := e.current(artifactType)
	if len(list) == 0 {
		return nil
	}
	return list[len(list)-1]
}

func codeLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// inScope reports whether a finding counts for the gate of the round: origin in the stage (an activity
// counts for the stage of approved_in_gate); the Release sees the whole project.
func (e *evaluation) inScope(f domainfinding.Finding) bool {
	return e.stage.ID == "release" || e.p.definition.GateStage(f.OriginStage) == e.stage.ID
}

// scopedFindings returns the codes of the blocking and of the other active findings of the scope.
func (e *evaluation) scopedFindings() (blocking, other []string) {
	blocking, other = []string{}, []string{}
	for _, f := range e.findings {
		if !e.inScope(f) || !domainfinding.Active(f.Status) {
			continue
		}
		if f.Blocking() {
			blocking = append(blocking, f.Code)
		} else {
			other = append(other, f.Code)
		}
	}
	return blocking, other
}

// mustStories are the current US with priority must.
func (e *evaluation) mustStories() []*ArtifactRecord {
	var out []*ArtifactRecord
	for _, us := range e.current("US") {
		if text(us.Header["prioridade"]) == "must" {
			out = append(out, us)
		}
	}
	return out
}

// criteria lists the ids of the acceptance criteria of a US header.
func criteria(header map[string]any) []string {
	var ids []string
	for _, item := range list(header, "criterios_aceite") {
		if m, ok := item.(map[string]any); ok {
			ids = append(ids, text(m["id"]))
		}
	}
	return ids
}

func list(header map[string]any, field string) []any {
	items, _ := header[field].([]any)
	return items
}

func object(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

func text(value any) string {
	s, _ := value.(string)
	return s
}

func textList(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func number(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func sortEvidence(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool { return text(items[i]["reference"]) < text(items[j]["reference"]) })
}

// referencePattern matches the ids an artifact header can point to: artifact ids and items of them.
var referencePattern = regexp.MustCompile(`^((US|RNF|ADR|T|CT|ACH|REL-QA|REL-SEG|VALIDACAO|RELEASE)-[0-9]{2,}|VISAO|ARQ|DIC|PERSIST|AMEACAS|BACKLOG|TELAS)(/((CA|RN|AM|TL|S|RK)-[0-9]+))?$`)

// linkFields are header fields that never hold a link to another artifact.
var nonLinkFields = map[string]bool{"id": true, "tipo": true, "onda": true, "autor": true, "historico": true, "titulo": true, "estado": true,
	"versao": true, "revisado_por": true, "editado_por": true}

// references walks a header and returns every link value with its JSON pointer.
func references(header map[string]any) []struct{ path, value string } {
	var out []struct{ path, value string }
	var walk func(path string, value any)
	walk = func(path string, value any) {
		switch v := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if path == "" && nonLinkFields[k] {
					continue
				}
				walk(path+"/"+k, v[k])
			}
		case []any:
			for i, item := range v {
				walk(path+"/"+itoa(i), item)
			}
		case string:
			if referencePattern.MatchString(v) {
				out = append(out, struct{ path, value string }{path, v})
			}
		}
	}
	walk("", header)
	return out
}

// itemExists reports whether an item id (CA-2, AM-3, TL-1) exists anywhere in the header lists.
func itemExists(header map[string]any, item string) bool {
	found := false
	var walk func(value any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if text(v["id"]) == item {
				found = true
			}
			for _, x := range v {
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(header)
	return found
}

// splitReference returns the artifact and the item of a reference (US-03/CA-2).
func splitReference(ref string) (string, string) {
	code, item, _ := strings.Cut(ref, "/")
	return code, item
}
