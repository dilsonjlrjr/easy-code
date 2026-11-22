// Package traceability answers the traceability queries of a project (US-28, specs/dominio/rastreabilidade.md):
// the graph of links from an artifact (US → T, ADR, screens, CT, commits, tests and findings) and the
// coverage matrix US × CT.
package traceability

import (
	"context"
	"sort"
	"strings"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	domainfinding "easycode/internal/domain/finding"
	"easycode/internal/domain/identity"
)

// Record is the current version of an artifact as the queries read it.
type Record struct {
	Code, Type, State, Title string
	Header                   map[string]any
}

// Source reads the current artifacts of a project.
type Source interface {
	Records(ctx context.Context, projectID string) ([]Record, error)
}

// Node is a vertex of the graph: an artifact, a commit or a test (file::name).
type Node struct {
	ID, Type, State, Title string
	Exists                 bool
}

// Edge links two nodes by an edge of the table of rastreabilidade.md (E-NN).
type Edge struct {
	From, To, Rule string
}

// Graph is getArtifactTraceability.
type Graph struct {
	Root  string
	Nodes []Node
	Edges []Edge
}

// Criterion is a row of the coverage matrix.
type Criterion struct {
	ID  string
	CTs []CTResult
}

// CTResult is a CT covering a criterion with its last result.
type CTResult struct {
	CT     string
	Result string // passed | failed | not_executed
}

// Coverage is one US of the matrix.
type Coverage struct {
	US       string
	Priority string
	Status   string // covered | gap
	Criteria []Criterion
}

// Service answers the queries.
type Service struct {
	projects appproject.Repository
	source   Source
}

// NewService builds the service.
func NewService(projects appproject.Repository, source Source) *Service {
	return &Service{projects: projects, source: source}
}

type link struct {
	to, rule string
}

func list(v any) []any {
	items, _ := v.([]any)
	return items
}

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

// links extracts the typed edges of a header (section 1 of rastreabilidade.md).
func links(r Record) []link {
	h := r.Header
	var out []link
	add := func(rule string, values ...any) {
		for _, v := range values {
			if s := text(v); s != "" {
				out = append(out, link{to: s, rule: rule})
			}
		}
	}
	for _, v := range list(h["vinculos"]) {
		add("E-01", v)
	}
	for _, item := range list(h["historico"]) {
		add("E-03", object(item)["achado"])
	}
	switch r.Type {
	case "US":
		add("E-04", list(h["rnf"])...)
	case "RNF":
		add("E-05", list(h["us_afetadas"])...)
	case "BACKLOG":
		for _, item := range list(h["historias"]) {
			add("E-06", object(item)["us"])
		}
		for _, item := range list(h["adiadas"]) {
			add("E-07", object(item)["us"])
		}
		for _, item := range list(h["rnf"]) {
			add("E-08", object(item)["rnf"])
		}
	case "ARQ":
		add("E-09", list(object(h["contexto"])["rnf_criticos"])...)
		for _, item := range list(h["componentes"]) {
			add("E-10", list(object(item)["atende"])...)
		}
		add("E-11", list(h["decisoes"])...)
	case "ADR":
		add("E-13", list(h["motivada_por"])...)
		add("E-14", h["substitui"])
	case "T":
		for _, item := range list(h["cobre"]) {
			add("E-15", object(item)["us"])
		}
		add("E-16", list(h["segue"])...)
		add("E-17", list(h["depende_de"])...)
		add("E-18", list(h["cts"])...)
		record := object(h["registro_dev"])
		for _, sha := range list(record["commits"]) {
			add("E-20", "commit:"+text(sha))
		}
		for _, test := range list(record["testes"]) {
			add("E-21", "test:"+text(test))
		}
	case "DIC":
		for _, item := range list(h["tabelas"]) {
			add("E-61", list(object(item)["origem"])...)
		}
	case "PERSIST":
		for _, item := range list(h["desempenho"]) {
			add("E-26", object(item)["rnf"])
		}
	case "TELAS":
		for _, item := range list(h["telas"]) {
			add("E-27", list(object(item)["us"])...)
		}
		add("E-29", list(h["lacunas_contrato"])...)
	case "VALIDACAO":
		for _, item := range list(h["conferencias"]) {
			add("E-30", object(item)["achado"])
		}
		for _, item := range list(h["artefatos_conferidos"]) {
			add("E-56", object(item)["id"])
		}
	case "CT":
		covers := object(h["cobre"])
		add("E-31", covers["us"], covers["rnf"])
		run := object(h["execucao"])
		if test := text(run["teste"]); test != "" {
			add("E-32", "test:"+test)
		}
		add("E-33", run["achado"])
	case "AMEACAS":
		for _, item := range list(h["ameacas"]) {
			add("E-35", object(item)["achado"])
		}
	case "ACH":
		for _, ref := range list(h["artefatos_afetados"]) {
			add("E-38", domainfinding.ArtifactOf(text(ref))) // a file path is not an artifact node
		}
	case "REL-QA":
		for _, item := range list(h["matriz"]) {
			row := object(item)
			add("E-41", row["us"])
			add("E-42", row["ct"])
			add("E-43", row["achado"])
		}
		add("E-45", list(h["achados_abertos"])...)
	case "REL-SEG":
		for _, item := range list(h["varreduras"]) {
			add("E-46", list(object(item)["achados"])...)
		}
	}
	return out
}

// Graph is getArtifactTraceability: the nodes reachable from the artifact within depth, following the
// edges out of it (output), into it (input) or both; a link to an unknown id is a node with exists false.
func (s *Service) Graph(ctx context.Context, principal identity.Principal, projectID, root string, depth int, direction string) (Graph, error) {
	if _, err := appproject.Authorize(ctx, s.projects, principal, projectID); err != nil {
		return Graph{}, err
	}
	if depth == 0 {
		depth = 3
	}
	if depth < 1 || depth > 5 {
		return Graph{}, failure.Invalid(failure.FieldError{Field: "depth", Message: "range"})
	}
	switch direction {
	case "":
		direction = "both"
	case "both", "output", "input":
	default:
		return Graph{}, failure.Invalid(failure.FieldError{Field: "direction", Message: "enum"})
	}
	records, err := s.source.Records(ctx, projectID)
	if err != nil {
		return Graph{}, err
	}
	byCode := map[string]Record{}
	for _, r := range records {
		byCode[r.Code] = r
	}
	if _, ok := byCode[root]; !ok {
		return Graph{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	outgoing := map[string][]link{}
	incoming := map[string][]link{}
	for _, r := range records {
		for _, l := range links(r) {
			outgoing[r.Code] = append(outgoing[r.Code], l)
			incoming[l.to] = append(incoming[l.to], link{to: r.Code, rule: l.rule})
		}
	}
	graph := Graph{Root: root}
	seen := map[string]bool{root: true}
	edges := map[Edge]bool{}
	frontier := []string{root}
	for level := 0; level < depth && len(frontier) > 0; level++ {
		var next []string
		for _, id := range frontier {
			if direction != "input" {
				for _, l := range outgoing[id] {
					edges[Edge{From: id, To: l.to, Rule: l.rule}] = true
					if !seen[l.to] {
						seen[l.to] = true
						next = append(next, l.to)
					}
				}
			}
			if direction != "output" {
				for _, l := range incoming[id] {
					edges[Edge{From: l.to, To: id, Rule: l.rule}] = true
					if !seen[l.to] {
						seen[l.to] = true
						next = append(next, l.to)
					}
				}
			}
		}
		frontier = next
	}
	for id := range seen {
		node := Node{ID: id}
		switch {
		case strings.HasPrefix(id, "commit:"):
			node.ID, node.Type, node.Exists = strings.TrimPrefix(id, "commit:"), "commit", true
		case strings.HasPrefix(id, "test:"):
			node.ID, node.Type, node.Exists = strings.TrimPrefix(id, "test:"), "test", true
		default:
			if r, ok := byCode[id]; ok {
				node.Type, node.State, node.Title, node.Exists = r.Type, r.State, r.Title, true
			} else {
				node.Type = typeOf(id)
			}
		}
		graph.Nodes = append(graph.Nodes, node)
	}
	for e := range edges {
		e.From, e.To = strings.TrimPrefix(strings.TrimPrefix(e.From, "commit:"), "test:"), strings.TrimPrefix(strings.TrimPrefix(e.To, "commit:"), "test:")
		graph.Edges = append(graph.Edges, e)
	}
	sort.Slice(graph.Nodes, func(i, j int) bool {
		return graph.Nodes[i].Type+graph.Nodes[i].ID < graph.Nodes[j].Type+graph.Nodes[j].ID
	})
	sort.Slice(graph.Edges, func(i, j int) bool {
		return graph.Edges[i].From+graph.Edges[i].To+graph.Edges[i].Rule < graph.Edges[j].From+graph.Edges[j].To+graph.Edges[j].Rule
	})
	return graph, nil
}

// typeOf derives the type of an artifact id (US-03 → US, REL-QA-01 → REL-QA).
func typeOf(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		return id[:i]
	}
	return id
}

var priorities = map[string]bool{"must": true, "should": true, "could": true, "wont": true}

// Coverage is getCoverageMatrix (US-28 CA-2, US-22 CA-2): each US of the priorities (must by default)
// covered by passing CT for every criterion, or a gap.
func (s *Service) Coverage(ctx context.Context, principal identity.Principal, projectID string, filter []string) ([]Coverage, error) {
	if _, err := appproject.Authorize(ctx, s.projects, principal, projectID); err != nil {
		return nil, err
	}
	if len(filter) == 0 {
		filter = []string{"must"}
	}
	wanted := map[string]bool{}
	for _, p := range filter {
		if !priorities[p] {
			return nil, failure.Invalid(failure.FieldError{Field: "priority", Message: "enum"})
		}
		wanted[p] = true
	}
	records, err := s.source.Records(ctx, projectID)
	if err != nil {
		return nil, err
	}
	results := map[string][]CTResult{} // US/CA → CT
	for _, r := range records {
		if r.Type != "CT" || r.State == "replaced" {
			continue
		}
		covers := object(r.Header["cobre"])
		us, ca := text(covers["us"]), text(covers["criterio"])
		if us == "" {
			continue
		}
		result := map[string]string{"passou": "passed", "falhou": "failed"}[text(object(r.Header["execucao"])["resultado"])]
		if result == "" {
			result = "not_executed"
		}
		results[us+"/"+ca] = append(results[us+"/"+ca], CTResult{CT: r.Code, Result: result})
	}
	out := []Coverage{}
	for _, r := range records {
		priority := text(r.Header["prioridade"])
		if r.Type != "US" || r.State == "replaced" || !wanted[priority] {
			continue
		}
		row := Coverage{US: r.Code, Priority: priority, Status: "covered", Criteria: []Criterion{}}
		for _, item := range list(r.Header["criterios_aceite"]) {
			id := text(object(item)["id"])
			cts := results[r.Code+"/"+id]
			if cts == nil {
				cts = []CTResult{}
			}
			passed := false
			for _, ct := range cts {
				passed = passed || ct.Result == "passed"
			}
			if !passed {
				row.Status = "gap"
			}
			row.Criteria = append(row.Criteria, Criterion{ID: id, CTs: cts})
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].US) != len(out[j].US) {
			return len(out[i].US) < len(out[j].US)
		}
		return out[i].US < out[j].US
	})
	return out, nil
}
