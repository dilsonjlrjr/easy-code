package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
)

func humanRecord(projectID string) audit.Record {
	return audit.Record{
		ProjectID: projectID, Actor: audit.Actor{Type: audit.ActorHuman, ID: "sub-123"}, Origin: audit.OriginWeb,
		Action: "gate.approve", Target: "gates/gate.requirements/rounds/1", Decision: "approved",
		Justification: "looks good", References: []string{"US-01"}, CorrelationID: correlation,
	}
}

func TestAuditTrailAppendsWithGrantsOfEachRole(t *testing.T) {
	db := postgrestest.New(t)
	project := db.CreateProject("audit-roles")
	ctx := context.Background()

	entry, err := postgres.NewAuditTrail(db.PoolAs("easycode_api")).Append(ctx, humanRecord(project.ProjectID))
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID == "" || entry.RecordedAt.IsZero() {
		t.Fatalf("empty entry %+v", entry)
	}
	var organization, origin, references string
	err = db.Admin.QueryRow(ctx, `
SELECT o.public_id::text, a.origin, a.reference_list::text FROM easycode.audit_records a
  JOIN easycode.organizations o ON o.id = a.organization_id WHERE a.public_id = $1`, entry.ID).Scan(&organization, &origin, &references)
	if err != nil {
		t.Fatal(err)
	}
	if organization != project.OrganizationID || origin != "web" || references != `["US-01"]` {
		t.Fatalf("stored %s %s %s", organization, origin, references)
	}

	orchestrator := audit.Record{
		OrganizationID: project.OrganizationID, Actor: audit.Actor{Type: audit.ActorOrchestrator, ID: "orchestrator"},
		Policy: "policy.decision_record", Action: "price.correct", Target: "models/fake", LostCost: "0.4127",
	}
	if _, err := postgres.NewAuditTrail(db.PoolAs("easycode_worker")).Append(ctx, orchestrator); err != nil {
		t.Fatal(err)
	}
}

func TestAuditTrailRefusesInvalidAndUnknownReferences(t *testing.T) {
	db := postgrestest.New(t)
	project := db.CreateProject("audit-invalid")
	other := db.CreateProject("audit-other")
	trail := postgres.NewAuditTrail(db.PoolAs("easycode_api"))
	ctx := context.Background()

	noOrigin := humanRecord(project.ProjectID)
	noOrigin.Origin = ""
	if _, err := trail.Append(ctx, noOrigin); failure.From(err).Code != postgres.CodeAuditInvalid {
		t.Fatalf("expected invalid record, got %v", err)
	}
	unknown := humanRecord("00000000-0000-4000-8000-000000000000")
	if _, err := trail.Append(ctx, unknown); failure.From(err).Code != postgres.CodeAuditUnknownTarget {
		t.Fatalf("expected unknown project, got %v", err)
	}
	crossed := humanRecord(project.ProjectID)
	crossed.OrganizationID = other.OrganizationID
	if _, err := trail.Append(ctx, crossed); failure.From(err).Code != postgres.CodeAuditUnknownTarget {
		t.Fatalf("expected project outside organization to be refused, got %v", err)
	}

	session := audit.Record{
		OrganizationID: project.OrganizationID, Actor: audit.Actor{Type: audit.ActorHuman, ID: "sub-123"}, Origin: audit.OriginWeb,
		Action: "session.login", Target: "sessions/sub-123", ExternalEventID: "kc-1", RecordedAt: time.Now().Add(-time.Hour),
	}
	if _, err := trail.Append(ctx, session); err != nil {
		t.Fatal(err)
	}
	if _, err := trail.Append(ctx, session); failure.From(err).Kind != failure.KindConflict {
		t.Fatalf("re-reading the same external event should conflict, got %v", err)
	}
}

func TestAuditTrailIsAppendOnly(t *testing.T) {
	db := postgrestest.New(t)
	project := db.CreateProject("audit-append-only")
	ctx := context.Background()
	entry, err := postgres.NewAuditTrail(db.PoolAs("easycode_api")).Append(ctx, humanRecord(project.ProjectID))
	if err != nil {
		t.Fatal(err)
	}

	for _, role := range []string{"easycode_api", "easycode_worker"} {
		pool := db.PoolAs(role)
		if _, err := pool.Exec(ctx, "UPDATE easycode.audit_records SET decision = 'rejected' WHERE public_id = $1", entry.ID); sqlState(err) != "42501" {
			t.Fatalf("%s update: want 42501, got %v", role, err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM easycode.audit_records WHERE public_id = $1", entry.ID); sqlState(err) != "42501" {
			t.Fatalf("%s delete: want 42501, got %v", role, err)
		}
	}
	retention := db.PoolAs("easycode_retention")
	if _, err := retention.Exec(ctx, "UPDATE easycode.audit_records SET action = 'gate.reject' WHERE public_id = $1", entry.ID); sqlState(err) != "42501" {
		t.Fatalf("retention may only anonymize the justification, got %v", err)
	}
	if _, err := retention.Exec(ctx, "DELETE FROM easycode.audit_records WHERE public_id = $1", entry.ID); sqlState(err) != "42501" {
		t.Fatalf("retention may not delete inside audit_period, got %v", err)
	}
	if _, err := retention.Exec(ctx, "UPDATE easycode.audit_records SET justification = '[anonimizado]' WHERE public_id = $1", entry.ID); err != nil {
		t.Fatalf("anonymization should pass: %v", err)
	}
}

func TestAuditAndEventCommitOrRollBackTogether(t *testing.T) {
	db := postgrestest.New(t)
	project := db.CreateProject("audit-tx")
	pool := db.PoolAs("easycode_api")
	runner := postgres.NewTxRunner(pool)
	trail := postgres.NewAuditTrail(pool)
	outbox := postgres.NewOutbox(pool)
	ctx := context.Background()

	transition := func(ctx context.Context) error {
		envelope, err := outbox.Append(ctx, gateApproved(project.ProjectID))
		if err != nil {
			return err
		}
		record := humanRecord(project.ProjectID)
		record.EventID = envelope.ID
		_, err = trail.Append(ctx, record)
		return err
	}
	boom := errors.New("boom")
	err := runner.InTx(ctx, func(ctx context.Context) error {
		if err := transition(ctx); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected rollback error, got %v", err)
	}
	if count(t, db, "SELECT count(*) FROM easycode.audit_records")+count(t, db, "SELECT count(*) FROM easycode.events") != 0 {
		t.Fatal("rolled back transaction left rows behind")
	}

	if err := runner.InTx(ctx, transition); err != nil {
		t.Fatal(err)
	}
	linked := count(t, db, `SELECT count(*) FROM easycode.audit_records a JOIN easycode.events e ON e.id = a.event_id WHERE e.sequence = 1`)
	if linked != 1 {
		t.Fatalf("audit record not linked to its event (sequence restarts at 1 after rollback): %d", linked)
	}
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
