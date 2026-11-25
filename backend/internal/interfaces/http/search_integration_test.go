package http_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	appsearch "easycode/internal/application/search"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	httpapi "easycode/internal/interfaces/http"
)

func searchDeps(d *httpapi.Dependencies, db *postgrestest.Database) {
	d.Search = appsearch.NewService(postgres.NewSearch(db.PoolAs("easycode_api")))
}

type searchGroup struct {
	items        []map[string]any
	total        float64
	projectCount float64
}

func searchFor(t *testing.T, person liveAPI, term string) map[string]searchGroup {
	t.Helper()
	body := person.json(t, "GET", "/api/v1/search?search="+url.QueryEscape(term), "", nil, 200)
	out := map[string]searchGroup{}
	for _, name := range []string{"projects", "artifacts", "findings"} {
		raw := body[name].(map[string]any)
		group := searchGroup{total: raw["total"].(float64), projectCount: raw["project_count"].(float64)}
		for _, item := range raw["items"].([]any) {
			group.items = append(group.items, item.(map[string]any))
		}
		out[name] = group
	}
	return out
}

// US-56 CA-1, CA-2, CA-6, RN-1, RN-3, RN-4 over the synthetic base: 15 projects where load-02 is approver;
// Carga 005 loses the role and Carga 002 is deleted with anonymization, so neither shows, not even in the
// counts; the groups bring project, state and id, at most 10 each with the total; the administrator sees
// only projects where they have a role; the term reaches no log.
func TestSearchFindsOnlyInProjectsWithARole(t *testing.T) {
	live := newLiveAPI(t, searchDeps)
	ids := live.db.SeedScale(postgrestest.Scale{Users: 2, Projects: 15, Waves: 28, EventsPerWave: 1, AuditPerProject: 1})
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, live.db.Admin, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM easycode.project_members m USING easycode.users u, easycode.projects p
 WHERE u.id = m.user_id AND p.id = m.project_id AND u.sub = 'load-02' AND p.name = 'Carga 005'`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE easycode.projects SET deletion_requested_at = now(), deletion_requested_by = created_by,
 data_anonymized_at = now() WHERE name = 'Carga 002'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	hidden := []string{ids[1], ids[4]} // Carga 002 (deleted), Carga 005 (no role)
	person := live.as(t, oidctest.User{Subject: "load-02", Name: "Pessoa 2"})

	projects := searchFor(t, person, "carga")["projects"]
	if projects.total != 13 || projects.projectCount != 13 || len(projects.items) != 10 {
		t.Fatalf("CA-1, RN-3: %d of %v in %v projects", len(projects.items), projects.total, projects.projectCount)
	}
	if first := projects.items[0]; first["name"] != "Carga 001" || first["state"] != "paused" || first["id"] != ids[0] {
		t.Fatalf("CA-2: project hit %v", first)
	}

	byID := searchFor(t, person, "ach-01")
	findings := byID["findings"]
	if findings.total != 12 || findings.projectCount != 12 || len(findings.items) != 10 || byID["artifacts"].total != 0 || byID["projects"].total != 0 {
		t.Fatalf("CA-2, RN-3: findings %v of %v, artifacts %v", len(findings.items), findings.total, byID["artifacts"].total)
	}
	f := findings.items[0]
	if f["id"] != "ACH-01" || f["title"] != "Achado sintético de defeito" || f["status"] != "open" || f["severity"] != "high" ||
		f["project"].(map[string]any)["name"] != "Carga 001" || f["project"].(map[string]any)["state"] != "paused" {
		t.Fatalf("CA-2: finding hit %v", f)
	}

	artifacts := searchFor(t, person, "reserva de")["artifacts"]
	if artifacts.total != 12 || len(artifacts.items) != 10 || artifacts.items[0]["id"] != "VISAO" || artifacts.items[0]["type"] != "VISAO" ||
		artifacts.items[0]["state"] != "approved" || artifacts.items[0]["title"] != "Visão da reserva de salas" {
		t.Fatalf("CA-2: artifacts %v %v", artifacts.total, artifacts.items)
	}
	if titled := searchFor(t, person, "Sintético")["findings"]; titled.total != 24 {
		t.Fatalf("CA-2: findings by title: %v", titled.total)
	}

	for _, term := range []string{"carga", "ach-01", "reserva de", "Sintético", "VISAO"} {
		raw := string(person.send("GET", "/api/v1/search?search="+url.QueryEscape(term), "", nil).Response.Body())
		for _, id := range hidden {
			if strings.Contains(raw, id) || strings.Contains(raw, "Carga 005") || strings.Contains(raw, "Carga 002") {
				t.Fatalf("CA-1, CA-6: %q shows a hidden project: %s", term, raw)
			}
		}
	}

	// LIKE wildcards in the term are literal; the administrator without a role sees nothing (RN-1).
	if wild := searchFor(t, person, "%%"); wild["projects"].total+wild["artifacts"].total+wild["findings"].total != 0 {
		t.Fatalf("wildcards: %v", wild)
	}
	if admin := searchFor(t, live, "carga"); admin["projects"].total != 0 || len(admin["projects"].items) != 0 {
		t.Fatalf("RN-1: the administrator sees %v", admin["projects"].items)
	}

	// CA-3: under 2 characters (or over 100) is a validation problem on `search`.
	for _, term := range []string{"", "a", strings.Repeat("x", 101)} {
		problem := person.json(t, "GET", "/api/v1/search?search="+url.QueryEscape(term), "", nil, 400)
		if fields, _ := problem["errors"].([]any); len(fields) != 1 || fields[0].(map[string]any)["field"] != "search" {
			t.Fatalf("CA-3 %q: %v", term, problem)
		}
	}
	if rc := live.do("GET", "/api/v1/search?search=carga", nil); rc.Response.StatusCode() != 401 {
		t.Fatalf("RNF-06: %d", rc.Response.StatusCode())
	}
	// RN-4: the term never reaches the log, not even through the query string.
	if logs := live.logText.String(); strings.Contains(strings.ToLower(logs), "carga") || strings.Contains(logs, "reserva de") ||
		strings.Contains(logs, "Sintético") {
		t.Fatal("RN-4: the search term is in the log")
	}
}
