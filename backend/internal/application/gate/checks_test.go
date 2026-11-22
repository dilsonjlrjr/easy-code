package gate

import (
	"context"
	"strings"
	"testing"
)

// dic.model_consistent and code.migrations_consistent_with_ddl read tables and columns of a DDL.
func TestDDLTables(t *testing.T) {
	tables := ddlTables(`CREATE TABLE IF NOT EXISTS easycode.reservas (
  id bigint GENERATED ALWAYS AS IDENTITY,
  sala text NOT NULL,
  CONSTRAINT reservas_pkey PRIMARY KEY (id)
);`)
	if len(tables) != 1 || !tables["reservas"]["id"] || !tables["reservas"]["sala"] || tables["reservas"]["constraint"] {
		t.Fatalf("tables: %v", tables)
	}
	var o outcome
	compareTables(&o, "DIC", "/tabelas", map[string]map[string]bool{"reservas": {"id": true, "autor": true}}, tables, "esquema.sql")
	if len(o.evidence) != 2 {
		t.Fatalf("differences: %v", o.evidence)
	}
}

// t.dependencies_no_cycle (RAS-10) and traceability.existing_references (RAS-01, US-28 CA-3).
func TestTaskCycleAndUnknownLinks(t *testing.T) {
	e := &evaluation{byCode: map[string]*ArtifactRecord{}}
	e.all = []ArtifactRecord{
		{Code: "T-01", Type: "T", State: "in_review", Header: map[string]any{"depende_de": []any{"T-02"}, "vinculos": []any{"US-99", "US-01/CA-3"}}},
		{Code: "T-02", Type: "T", State: "in_review", Header: map[string]any{"depende_de": []any{"T-01"}}},
		{Code: "US-01", Type: "US", State: "approved", Header: map[string]any{"criterios_aceite": []any{map[string]any{"id": "CA-1"}}}},
	}
	for i := range e.all {
		e.byCode[e.all[i].Code] = &e.all[i]
	}
	cycle, _ := dependenciesNoCycle(context.Background(), e)
	if cycle.passed() || !strings.Contains(cycle.evidence[0]["message"].(string), "T-01 -> T-02 -> T-01") {
		t.Fatalf("cycle: %v", cycle.evidence)
	}
	e.outputs = nil
	links := outcome{}
	for _, r := range []*ArtifactRecord{e.byCode["T-01"]} {
		for _, ref := range references(r.Header) {
			code, item := splitReference(ref.value)
			target, ok := e.byCode[code]
			if !ok || (item != "" && !itemExists(target.Header, item)) {
				links.fail(r.Code, ref.path, ref.value)
			}
		}
	}
	if len(links.evidence) != 2 {
		t.Fatalf("links: %v", links.evidence)
	}
}
