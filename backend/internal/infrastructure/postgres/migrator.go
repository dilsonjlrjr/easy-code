package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/infrastructure/logging"
)

// migrationLockID serializes concurrent migration runs through a PostgreSQL advisory lock.
const migrationLockID int64 = 0x45617379436f6465 // "EasyCode"

const createMigrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version     bigint      PRIMARY KEY,
	name        text        NOT NULL,
	checksum    text        NOT NULL,
	applied_at  timestamptz NOT NULL DEFAULT now(),
	duration_ms bigint      NOT NULL
)`

// Migrator applies pending migrations in version order and records them in schema_migrations.
// It knows no table other than schema_migrations.
type Migrator struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewMigrator builds a migrator over a pool opened with the schema owner role.
func NewMigrator(pool *pgxpool.Pool, logger *slog.Logger) *Migrator {
	return &Migrator{pool: pool, logger: logger}
}

// Apply runs every pending migration and returns how many it applied. An applied migration
// whose file changed is an error: applied migrations are never edited.
func (m *Migrator) Apply(ctx context.Context, migrations []Migration) (int, error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if err := lock(ctx, conn.Conn()); err != nil {
		return 0, err
	}
	defer m.unlock(conn.Conn())

	applied, err := appliedChecksums(ctx, conn.Conn())
	if err != nil {
		return 0, err
	}
	return m.applyPending(ctx, conn.Conn(), migrations, applied)
}

func (m *Migrator) applyPending(ctx context.Context, conn *pgx.Conn, migrations []Migration, applied map[int64]string) (int, error) {
	count := 0
	for _, migration := range migrations {
		checksum, done := applied[migration.Version]
		if done && checksum != migration.Checksum {
			return count, fmt.Errorf("migration %d_%s changed after being applied", migration.Version, migration.Name)
		}
		if done {
			continue
		}
		if err := m.applyOne(ctx, conn, migration); err != nil {
			return count, err
		}
		count++
	}
	m.logger.InfoContext(ctx, "migrations complete", slog.Int("applied", count), slog.Int("known", len(migrations)))
	return count, nil
}

func (m *Migrator) applyOne(ctx context.Context, conn *pgx.Conn, migration Migration) error {
	started := time.Now()
	run := runInTransaction
	if !migration.InTransaction {
		run = runWithoutTransaction
	}
	if err := run(ctx, conn, migration, started); err != nil {
		m.logger.ErrorContext(ctx, "migration failed",
			slog.Int64("migration_version", migration.Version),
			slog.String("migration_name", migration.Name),
			logging.ErrorAttr(err))
		return fmt.Errorf("apply migration %d_%s: %w", migration.Version, migration.Name, err)
	}
	m.logger.InfoContext(ctx, "migration applied",
		slog.Int64("migration_version", migration.Version),
		slog.String("migration_name", migration.Name),
		slog.Bool("in_transaction", migration.InTransaction),
		slog.Float64("duration_ms", milliseconds(time.Since(started))))
	return nil
}

func runInTransaction(ctx context.Context, conn *pgx.Conn, migration Migration, started time.Time) error {
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(WithOperation(ctx, "migration.lock_timeout"), "SET LOCAL lock_timeout = '5s'"); err != nil {
			return err
		}
		if _, err := tx.Exec(WithOperation(ctx, "migration.apply"), migration.SQL); err != nil {
			return err
		}
		return record(ctx, tx, migration, started)
	})
}

func runWithoutTransaction(ctx context.Context, conn *pgx.Conn, migration Migration, started time.Time) error {
	if _, err := conn.Exec(WithOperation(ctx, "migration.apply"), migration.SQL); err != nil {
		return err
	}
	return record(ctx, conn, migration, started)
}

// executor is satisfied by both *pgx.Conn and pgx.Tx.
type executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func record(ctx context.Context, db executor, migration Migration, started time.Time) error {
	_, err := db.Exec(WithOperation(ctx, "migration.record"),
		"INSERT INTO schema_migrations (version, name, checksum, duration_ms) VALUES ($1, $2, $3, $4)",
		migration.Version, migration.Name, migration.Checksum, time.Since(started).Milliseconds())
	return err
}

func lock(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(WithOperation(ctx, "migration.lock"), "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	if _, err := conn.Exec(WithOperation(ctx, "migration.create_table"), createMigrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

func (m *Migrator) unlock(conn *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.Exec(WithOperation(ctx, "migration.unlock"), "SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
		m.logger.ErrorContext(ctx, "release migration lock failed", logging.ErrorAttr(err))
	}
}

func appliedChecksums(ctx context.Context, conn *pgx.Conn) (map[int64]string, error) {
	rows, err := conn.Query(WithOperation(ctx, "migration.list_applied"), "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	applied := map[int64]string{}
	var version int64
	var checksum string
	_, err = pgx.ForEachRow(rows, []any{&version, &checksum}, func() error {
		applied[version] = checksum
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	return applied, nil
}
