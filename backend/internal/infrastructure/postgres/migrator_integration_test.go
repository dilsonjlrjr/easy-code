package postgres

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"testing/fstest"
)

// TestMigratorAppliesInOrderOnce needs a disposable database in EASYCODE_TEST_DATABASE_URL.
func TestMigratorAppliesInOrderOnce(t *testing.T) {
	url := os.Getenv("EASYCODE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("EASYCODE_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	pool, err := NewPool(ctx, PoolSettings{URL: url, MaxConns: 2}, logger)
	if err != nil {
		t.Fatal(err)
	}
	dropProbeTables := func() {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS migrator_probe_items, migrator_probe, schema_migrations")
	}
	dropProbeTables()
	t.Cleanup(func() {
		dropProbeTables()
		pool.Close()
	})
	migrations, err := LoadMigrations(fstest.MapFS{
		"0001_create_probe.sql": {Data: []byte("CREATE TABLE migrator_probe (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY);")},
		"0002_create_items.sql": {Data: []byte("CREATE TABLE migrator_probe_items (id bigint REFERENCES migrator_probe (id));")},
		"0003_index_items.sql":  {Data: []byte("-- migrate:no-transaction\nCREATE INDEX CONCURRENTLY migrator_probe_items_id ON migrator_probe_items (id);")},
	})
	if err != nil {
		t.Fatal(err)
	}
	migrator := NewMigrator(pool, logger)

	first, err := migrator.Apply(ctx, migrations)
	if err != nil || first != 3 {
		t.Fatalf("first run applied %d, err %v", first, err)
	}
	second, err := migrator.Apply(ctx, migrations)
	if err != nil || second != 0 {
		t.Fatalf("second run applied %d, err %v", second, err)
	}

	migrations[0].Checksum = "edited"
	if _, err := migrator.Apply(ctx, migrations); err == nil {
		t.Fatal("expected an edited applied migration to be rejected")
	}
}
