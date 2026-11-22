// Package postgres adapts PostgreSQL (pgx v5) to the application ports.
package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/domain/health"
)

// PoolSettings configures the connection pool.
type PoolSettings struct {
	URL      string
	MaxConns int32
}

// NewPool opens a traced connection pool and checks that the database answers.
func NewPool(ctx context.Context, settings PoolSettings, logger *slog.Logger) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(settings.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolConfig.MaxConns = settings.MaxConns
	poolConfig.ConnConfig.Tracer = NewQueryTracer(logger)

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	logger.InfoContext(ctx, "db pool created",
		slog.String("db_host", poolConfig.ConnConfig.Host),
		slog.Int("db_port", int(poolConfig.ConnConfig.Port)),
		slog.String("db_name", poolConfig.ConnConfig.Database),
		slog.String("db_user", poolConfig.ConnConfig.User),
		slog.Int("max_conns", int(poolConfig.MaxConns)),
	)
	return pool, nil
}

// DatabaseProbe reports whether PostgreSQL answers a trivial query.
type DatabaseProbe struct {
	pool *pgxpool.Pool
}

// NewDatabaseProbe builds the readiness probe for the database.
func NewDatabaseProbe(pool *pgxpool.Pool) DatabaseProbe {
	return DatabaseProbe{pool: pool}
}

// Dependency implements health.DependencyProbe.
func (DatabaseProbe) Dependency() health.Dependency { return health.DependencyDatabase }

// Probe implements health.DependencyProbe.
func (p DatabaseProbe) Probe(ctx context.Context) error {
	var one int
	return p.pool.QueryRow(WithOperation(ctx, "health.probe_database"), "SELECT 1").Scan(&one)
}
