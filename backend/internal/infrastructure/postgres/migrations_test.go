package postgres

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsOrdersByVersionAndIgnoresOtherFiles(t *testing.T) {
	files := fstest.MapFS{
		"0010_add_index.sql":      {Data: []byte("-- migrate:no-transaction\nCREATE INDEX CONCURRENTLY i ON t (c);")},
		"0002_create_table.sql":   {Data: []byte("CREATE TABLE t (c int);")},
		"0001_initial_schema.sql": {Data: []byte("CREATE SCHEMA s;")},
		"embed.go":                {Data: []byte("package migrations")},
		"README.md":               {Data: []byte("notes")},
	}

	migrations, err := LoadMigrations(files)
	if err != nil {
		t.Fatal(err)
	}

	if len(migrations) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[1].Version != 2 || migrations[2].Version != 10 {
		t.Fatalf("wrong order: %+v", migrations)
	}
	if migrations[2].InTransaction || !migrations[0].InTransaction {
		t.Fatal("no-transaction marker not honored")
	}
	if migrations[0].Name != "initial_schema" || migrations[0].Checksum == "" {
		t.Fatalf("unexpected metadata: %+v", migrations[0])
	}
}

func TestLoadMigrationsRejectsDuplicateVersions(t *testing.T) {
	files := fstest.MapFS{
		"0001_a.sql": {Data: []byte("SELECT 1;")},
		"001_b.sql":  {Data: []byte("SELECT 2;")},
	}

	if _, err := LoadMigrations(files); err == nil {
		t.Fatal("expected duplicate version error")
	}
}
