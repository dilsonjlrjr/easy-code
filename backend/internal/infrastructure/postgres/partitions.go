package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PartitionMaintainer creates the monthly partitions of the partitioned tables by calling
// the database function easycode.create_monthly_partitions (idempotent; months already
// partitioned are skipped).
type PartitionMaintainer struct {
	pool *pgxpool.Pool
}

// NewPartitionMaintainer builds the maintainer over a pool opened with the worker role.
func NewPartitionMaintainer(pool *pgxpool.Pool) PartitionMaintainer {
	return PartitionMaintainer{pool: pool}
}

// EnsureMonthlyPartitions uses the function defaults (current month plus months ahead) and
// returns how many partitions it created.
func (m PartitionMaintainer) EnsureMonthlyPartitions(ctx context.Context) (int, error) {
	var created int
	err := m.pool.QueryRow(WithOperation(ctx, "partitions.create_monthly"),
		"SELECT easycode.create_monthly_partitions()").Scan(&created)
	if err != nil {
		return 0, fmt.Errorf("create monthly partitions: %w", err)
	}
	return created, nil
}
