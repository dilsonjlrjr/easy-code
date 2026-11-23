package http_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/application/retention"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
)

// US-04: only a completed or canceled project is deleted, by its lead, with the name typed again (CA-1, CA-2);
// it leaves every list and access at once, the organization trail records who, when and the id without content
// (CA-3), and the retention job destroys the repository and anonymizes the personal data (RN-1, RNF-20).
func TestDeleteProjectAnonymizesIt(t *testing.T) {
	live := newLiveAPI(t)
	ctx := context.Background()
	projectID := createProject(t, live, "Projeto encerrado")
	base := "/api/v1/projects/" + projectID
	deletion := `{"name_confirmation":"Projeto encerrado"}`

	// CA-2: created, in progress or paused is refused with "cancel first".
	rc := live.send("POST", base+"/deletion", deletion, key())
	if rc.Response.StatusCode() != 409 || decode(t, rc)["type"] != "urn:easycode:problem:project-not-ended" {
		t.Fatalf("CA-2: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	member := live.as(t, oidctest.User{Subject: "carla", Name: "Carla"})
	live.json(t, "PUT", base+"/members/carla", `{"roles":["approver"]}`, nil, 200)
	if _, err := live.db.Admin.Exec(ctx, `UPDATE easycode.projects SET state = 'canceled', finished_at = now() WHERE public_id = $1::uuid`, projectID); err != nil {
		t.Fatal(err)
	}
	// Personal data of the project: a free human text in its trail.
	api := live.db.PoolOf("easycode_api", 2)
	if _, err := postgres.NewAuditTrail(api).Append(ctx, domainaudit.Record{
		ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: "dev"}, Origin: domainaudit.OriginWeb,
		Action: "project.cancel", Target: "project", Justification: "Cancelado a pedido de Fulano da Silva",
	}); err != nil {
		t.Fatal(err)
	}
	// Only the lead deletes; the name must match.
	if rc := member.send("POST", base+"/deletion", deletion, key()); rc.Response.StatusCode() != 403 {
		t.Fatalf("approver deletes: %d", rc.Response.StatusCode())
	}
	if rc := live.send("POST", base+"/deletion", `{"name_confirmation":"Outro nome"}`, key()); rc.Response.StatusCode() != 400 ||
		decode(t, rc)["type"] != "urn:easycode:problem:confirmation-required" {
		t.Fatalf("wrong name: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	// CA-1: deleted at once for everybody.
	live.json(t, "POST", base+"/deletion", deletion, key(), 202)
	for _, who := range []liveAPI{live, member} {
		if rc := who.send("GET", base, "", nil); rc.Response.StatusCode() != 404 {
			t.Fatalf("deleted project still answers: %d", rc.Response.StatusCode())
		}
		if items := who.json(t, "GET", "/api/v1/projects", "", nil, 200)["items"].([]any); len(items) != 0 {
			t.Fatalf("deleted project still listed: %v", items)
		}
	}
	// CA-3: the organization trail keeps who, when and the id, without content.
	if n := count(t, live.db, `SELECT count(*) FROM easycode.audit_records WHERE action = 'project.delete' AND project_id IS NULL
	   AND target = 'projects/`+projectID+`' AND actor_id = 'dev' AND justification IS NULL`); n != 1 {
		t.Fatalf("CA-3 audit: %d", n)
	}

	// RN-1: the retention job destroys the repository and anonymizes the personal data, once.
	repos := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repos, projectID+".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	worker := live.db.PoolOf("easycode_worker", 2)
	job := retention.NewJob(postgres.NewTxRunner(worker), postgres.NewRetention(worker), git.New(repos), postgres.NewOutbox(worker))
	if done, err := job.Run(ctx); err != nil || done != 1 {
		t.Fatalf("retention: %d %v", done, err)
	}
	if _, err := os.Stat(filepath.Join(repos, projectID+".git")); !os.IsNotExist(err) {
		t.Fatalf("repository kept: %v", err)
	}
	if n := count(t, live.db, `SELECT count(*) FROM easycode.audit_records a JOIN easycode.projects p ON p.id = a.project_id
	   WHERE p.public_id = '`+projectID+`' AND a.justification LIKE '%Fulano%'`); n != 0 {
		t.Fatal("personal data kept after the anonymization")
	}
	if n := count(t, live.db, `SELECT count(*) FROM easycode.projects WHERE public_id = '`+projectID+`' AND data_anonymized_at IS NOT NULL`); n != 1 {
		t.Fatal("project not marked anonymized")
	}
	if n := count(t, live.db, `SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.project.data_anonymized.v1'
	   AND envelope->'data'->>'cause' = 'deletion_requested' AND (envelope->'data'->>'anonymized_records')::int >= 1`); n != 1 {
		t.Fatal("data_anonymized event")
	}
	if done, err := job.Run(ctx); err != nil || done != 0 {
		t.Fatalf("second pass: %d %v", done, err)
	}
}
