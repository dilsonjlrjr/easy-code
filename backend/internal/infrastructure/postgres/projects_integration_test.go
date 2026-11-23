package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
)

type projectFixture struct {
	db      *postgrestest.Database
	dev     identity.Principal
	service *appproject.Service
	repo    postgres.Projects
}

func newProjectFixture(t *testing.T) projectFixture {
	t.Helper()
	db := postgrestest.New(t)
	api := db.PoolAs("easycode_api")
	dev := seedDev(t, db)
	repo := postgres.NewProjects(api, "workflow-basico", 3)
	service := appproject.NewService(postgres.NewTxRunner(api), repo, postgres.NewOutbox(api), postgres.NewAuditTrail(api), "workflow-basico", 1)
	return projectFixture{db: db, dev: dev, service: service, repo: repo}
}

func roomBooking() project.NewProject {
	return project.NewProject{
		Name: "Reserva de salas", BusinessNeed: "Precisamos reservar salas pelo navegador, sem conflito de horário.", Acknowledgement: true,
		Budget:      &project.Budget{StopLimit: project.Money{Value: "200.00", Currency: "BRL"}},
		Constraints: []project.Constraint{{Category: "database", Description: "PostgreSQL 16 já operado."}},
	}
}

// US-03 CA-1, RN-2, RN-4: the project starts in created, with the default workflow, no stages yet,
// its creator as lead and approver, the acknowledgment recorded and constraints version 1.
func TestCreateProjectInCreatedWithCreatorRoles(t *testing.T) {
	f := newProjectFixture(t)
	ctx := context.Background()
	view, err := f.service.Create(ctx, f.dev, roomBooking())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != project.StateCreated || view.Workflow != "workflow-basico" || view.Summary.StagesTotal != 3 || view.Summary.ApprovedStages != 0 {
		t.Fatalf("CA-1: unexpected view %+v", view)
	}
	if !identity.Has(view.MyRoles, identity.RoleLead) || !identity.Has(view.MyRoles, identity.RoleApprover) || view.Lead.Subject != "dev" {
		t.Fatalf("creator must be lead and approver: %+v", view.MyRoles)
	}
	if view.AcknowledgedBy.Subject != "dev" || view.AcknowledgedAt.IsZero() {
		t.Fatal("RN-2: acknowledgment recorded with user and date")
	}
	if fmt.Sprint(view.AvailableActions) != "[start cancel]" || view.Summary.StopLimit == nil || view.Summary.StopLimit.Value != "200" {
		t.Fatalf("actions and budget: %+v", view)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.stages"); n != 0 {
		t.Fatal("CA-1: stages exist only after the workflow starts")
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.project_constraints WHERE version = 1 AND items @> '[{\"id\":\"RESTR-01\"}]'"); n != 1 {
		t.Fatal("RN-4: constraints version 1 with RESTR-01")
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.project.created.v1' AND envelope->'data'->'budget'->'stop_limit'->>'value' = '200'"); n != 1 {
		t.Fatal("project.created event in the outbox")
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'project.create'"); n != 1 {
		t.Fatal("RNF-13: project.create audited")
	}
	page, err := f.service.List(ctx, f.dev, appproject.Filter{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != view.ID || page.CountByState[project.StateCreated] != 1 {
		t.Fatalf("CA-1: the project is in my list: %+v %v", page, err)
	}
	filtered, err := f.service.List(ctx, f.dev, appproject.Filter{Limit: 20, States: []string{project.StateInProgress}})
	if err != nil || len(filtered.Items) != 0 || filtered.CountByState[project.StateCreated] != 1 {
		t.Fatalf("state filter: %+v %v", filtered, err)
	}
}

// US-03 CA-2, CA-3: empty need or missing acknowledgment creates nothing.
func TestCreateProjectRefusesEmptyNeedAndMissingAcknowledgment(t *testing.T) {
	f := newProjectFixture(t)
	input := roomBooking()
	input.BusinessNeed = ""
	input.Acknowledgement = false
	err := failure.From(func() error { _, err := f.service.Create(context.Background(), f.dev, input); return err }())
	if err.Code != failure.CodeValidation || len(err.Fields) != 2 {
		t.Fatalf("want two field errors, got %+v", err)
	}
	if n := count(t, f.db, "SELECT count(*) FROM easycode.projects"); n != 0 {
		t.Fatal("nothing created")
	}
}

// US-03 RN-1: the name is unique in the organization, ignoring case.
func TestCreateProjectRefusesDuplicateName(t *testing.T) {
	f := newProjectFixture(t)
	ctx := context.Background()
	if _, err := f.service.Create(ctx, f.dev, roomBooking()); err != nil {
		t.Fatal(err)
	}
	again := roomBooking()
	again.Name = strings.ToUpper(again.Name)
	if _, err := f.service.Create(ctx, f.dev, again); failure.From(err).Code != project.CodeDuplicateName {
		t.Fatalf("want duplicate-name, got %v", err)
	}
}

// RNF-04: a person without a role in the project gets not found.
func TestProjectOfAnotherOrganizationIsNotFound(t *testing.T) {
	f := newProjectFixture(t)
	other := f.db.CreateProject("foreign")
	if _, err := f.service.Get(context.Background(), f.dev, other.ProjectID); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("want not found, got %v", err)
	}
}
