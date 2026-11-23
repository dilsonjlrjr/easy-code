package postgres_test

import (
	"context"
	"testing"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/project"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/specs"
)

// US-07: the lead chooses human or automatic only for gate.validation and gate.implementation (CA-3, RN-1);
// the switch to human hardens the open round at once (CA-5) and the switch to automatic leaves it waiting
// for a human (CA-6, RN-3).
func TestApprovalModeOfTheConfigurableGates(t *testing.T) {
	f := newProjectFixture(t)
	ctx := context.Background()
	wf, err := specs.StandardWorkflow(specs.ReleaseAgents)
	if err != nil {
		t.Fatal(err)
	}
	api := f.db.PoolAs("easycode_api")
	repo := postgres.NewProjects(api, wf.Definition.ID, len(wf.Definition.Stages))
	service := appproject.NewConfigurationService(postgres.NewTxRunner(api), repo, repo, postgres.NewCatalog(api), postgres.NewAuditTrail(api), wf.Definition)
	view, err := f.service.Create(ctx, f.dev, roomBooking())
	if err != nil {
		t.Fatal(err)
	}
	config, err := service.Get(ctx, f.dev, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	// RN-1: defaults of the workflow, validation automatic and implementation human; only those two choose.
	if v, i, r := config.GateApproval["gate.validation"], config.GateApproval["gate.implementation"], config.GateApproval["gate.release"]; v.Mode != "automatic" ||
		len(v.Allowed) != 2 || i.Mode != "human" || len(i.Allowed) != 2 || r.Mode != "human" || len(r.Allowed) != 1 || !r.AlwaysHuman || v.AlwaysHuman {
		t.Fatalf("modes: validation %+v implementation %+v release %+v", v, i, r)
	}
	// CA-3: the API refuses automatic in another gate.
	for _, gate := range []string{"gate.release", "gate.requirements", "gate.security"} {
		if _, err := service.Modify(ctx, f.dev, view.ID, config.Version, appproject.Change{GateApproval: map[string]string{gate: "automatic"}}); failure.From(err).Code != project.CodeApprovalNotAllowed {
			t.Fatalf("%s automatic: %v", gate, err)
		}
	}
	// An open round of the validation gate in automatic mode (opened before the change).
	if _, err := f.db.Admin.Exec(ctx, `
INSERT INTO easycode.stages (project_id, stage, type, state)
SELECT id, 'validation', 'stage', 'blocked' FROM easycode.projects WHERE public_id = $1::uuid`, view.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Admin.Exec(ctx, `
INSERT INTO easycode.gate_rounds (project_id, stage, stage_id, number, state, approval_mode)
SELECT s.project_id, s.stage, s.id, 1, 'evaluating', 'automatic'
  FROM easycode.stages s JOIN easycode.projects p ON p.id = s.project_id WHERE p.public_id = $1::uuid AND s.stage = 'validation'`, view.ID); err != nil {
		t.Fatal(err)
	}
	roundMode := func() string {
		var mode string
		if err := f.db.Admin.QueryRow(ctx, `SELECT r.approval_mode FROM easycode.gate_rounds r JOIN easycode.projects p ON p.id = r.project_id
 WHERE p.public_id = $1::uuid AND r.stage = 'validation'`, view.ID).Scan(&mode); err != nil {
			t.Fatal(err)
		}
		return mode
	}
	config, err = service.Modify(ctx, f.dev, view.ID, config.Version, appproject.Change{GateApproval: map[string]string{"gate.validation": "human", "gate.implementation": "automatic"}})
	if err != nil || config.GateApproval["gate.validation"].Mode != "human" || config.GateApproval["gate.implementation"].Mode != "automatic" {
		t.Fatalf("modify: %+v %v", config.GateApproval, err)
	}
	if mode := roundMode(); mode != "human" {
		t.Fatalf("CA-5: the open round must wait for a human, got %s", mode)
	}
	config, err = service.Modify(ctx, f.dev, view.ID, config.Version, appproject.Change{GateApproval: map[string]string{"gate.validation": "automatic"}})
	if err != nil || config.GateApproval["gate.validation"].Mode != "automatic" {
		t.Fatalf("back to automatic: %v", err)
	}
	if mode := roundMode(); mode != "human" {
		t.Fatalf("CA-6: the open round keeps waiting for a human, got %s", mode)
	}
	// RN-4: only the lead changes the mode.
	if n := count(t, f.db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'configuration.modify'"); n != 2 {
		t.Fatalf("RNF-13: mode changes audited, got %d", n)
	}
}
