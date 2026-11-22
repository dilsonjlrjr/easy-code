package postgres_test

import (
	"context"
	"testing"

	"easycode/internal/domain/event"
	"easycode/internal/infrastructure/postgres/postgrestest"
)

const correlation = "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b30"

func gateApproved(projectID string) event.Draft {
	return event.Draft{
		Type: "br.easycode.gate.approved.v1", Subject: "gates/gate.requirements/rounds/1", ProjectID: projectID,
		CorrelationID: correlation, Data: []byte(`{"gate":"gate.requirements","round":1}`),
	}
}

func count(t *testing.T, db *postgrestest.Database, sql string) int {
	t.Helper()
	var n int
	if err := db.Admin.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
