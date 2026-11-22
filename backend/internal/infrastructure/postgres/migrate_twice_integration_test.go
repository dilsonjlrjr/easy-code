package postgres_test

import (
	"context"
	"testing"

	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/migrations"
)

// The migrate service runs on every deploy: a second run over the real migrations applies nothing
// (it used to re-apply 0001 because schema_migrations was resolved through the easycode search_path).
func TestRealMigrationsApplyOnlyOnce(t *testing.T) {
	db := postgrestest.New(t)
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: db.URL, MaxConns: 1}, db.Logger)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	files, err := postgres.LoadMigrations(migrations.Files())
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := postgres.NewMigrator(pool, db.Logger).Apply(ctx, files); err != nil || applied != 0 {
		t.Fatalf("second run applied %d: %v", applied, err)
	}
	var unpinned int
	if err := db.Admin.QueryRow(ctx, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
 WHERE n.nspname = 'easycode' AND p.prokind = 'f'
   AND NOT EXISTS (SELECT 1 FROM unnest(coalesce(p.proconfig, '{}')) c WHERE c LIKE 'search_path=%')`).Scan(&unpinned); err != nil || unpinned != 0 {
		t.Fatalf("every function of the schema pins its search_path (0006): %d unpinned, %v", unpinned, err)
	}
}
