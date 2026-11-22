package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/traceability"
)

// Traceability reads the current artifacts of a project for the traceability queries (US-28).
type Traceability struct {
	pool *pgxpool.Pool
}

var _ traceability.Source = Traceability{}

// NewTraceability builds the adapter.
func NewTraceability(pool *pgxpool.Pool) Traceability { return Traceability{pool: pool} }

// Records implements traceability.Source.
func (t Traceability) Records(ctx context.Context, projectID string) ([]traceability.Record, error) {
	rows, err := conn(ctx, t.pool).Query(WithOperation(ctx, "artifacts.trace_records"), `
SELECT ar.code, ar.type, ar.state, ar.title, v.header FROM easycode.artifacts ar JOIN easycode.projects pr ON pr.id = ar.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = ar.id AND v.version = ar.current_version
 WHERE pr.public_id = $1::uuid ORDER BY ar.type, length(ar.code), ar.code`, projectID)
	if err != nil {
		return nil, fmt.Errorf("trace records: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (traceability.Record, error) {
		var r traceability.Record
		var header []byte
		if err := row.Scan(&r.Code, &r.Type, &r.State, &r.Title, &header); err != nil {
			return r, err
		}
		return r, json.Unmarshal(header, &r.Header)
	})
}
