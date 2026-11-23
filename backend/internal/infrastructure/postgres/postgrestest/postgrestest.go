// Package postgrestest gives integration tests a fresh database with every migration applied.
// It needs EASYCODE_TEST_DATABASE_URL pointing at a disposable server whose user may create
// databases and roles (the CI container's superuser); without it the test is skipped.
package postgrestest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/infrastructure/postgres"
	"easycode/migrations"
)

// EnvURL names the variable with the disposable server URL.
const EnvURL = "EASYCODE_TEST_DATABASE_URL"

// Database is a migrated database private to one test.
type Database struct {
	URL    string
	Admin  *pgxpool.Pool // connects as the server's superuser
	Logger *slog.Logger
	t      *testing.T
}

// New creates the database, applies the migrations and drops it when the test ends.
func New(t *testing.T) *Database {
	t.Helper()
	base := os.Getenv(EnvURL)
	if base == "" {
		t.Skip(EnvURL + " not set")
	}
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	name := "easycode_test_" + randomSuffix()
	server, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = server.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = server.Close(context.Background())
	})

	dbURL := withDatabase(t, base, name)
	admin, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: dbURL, MaxConns: 4}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	files, err := postgres.LoadMigrations(migrations.Files())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewMigrator(admin, logger).Apply(ctx, files); err != nil {
		t.Fatal(err)
	}
	return &Database{URL: dbURL, Admin: admin, Logger: logger, t: t}
}

// PoolAs opens a pool whose connections act as one application role (easycode_api,
// easycode_worker, easycode_retention), so tests exercise the real grants.
func (d *Database) PoolAs(role string) *pgxpool.Pool {
	d.t.Helper()
	return d.PoolOf(role, 4)
}

// PoolOf is PoolAs with maxConns connections (the load test of RNF-09 uses the size of production).
func (d *Database) PoolOf(role string, maxConns int32) *pgxpool.Pool {
	d.t.Helper()
	config, err := pgxpool.ParseConfig(d.URL)
	if err != nil {
		d.t.Fatal(err)
	}
	config.MaxConns = maxConns
	config.ConnConfig.Tracer = postgres.NewQueryTracer(d.Logger)
	postgres.SetSchemaSearchPath(config.ConnConfig)
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		d.t.Fatal(err)
	}
	d.t.Cleanup(pool.Close)
	return pool
}

// Project is a fixture project with its organization.
type Project struct {
	OrganizationID string // public ids
	ProjectID      string
}

// CreateProject inserts an organization, a user and a project with its lead.
func (d *Database) CreateProject(name string) Project {
	d.t.Helper()
	var project Project
	err := d.Admin.QueryRow(context.Background(), `
WITH org AS (
  INSERT INTO easycode.organizations (name, oidc_issuer) VALUES ($1, 'https://sso.example/' || $1) RETURNING id, public_id
), person AS (
  INSERT INTO easycode.users (organization_id, sub, display_name) SELECT id, 'sub-' || $1, 'Fixture' FROM org RETURNING id, organization_id
), project AS (
  INSERT INTO easycode.projects (organization_id, name, business_need, provider_acknowledgment_by, provider_acknowledgment_at, created_by)
  SELECT organization_id, $1, 'fixture need', id, now(), id FROM person RETURNING id, public_id
), lead AS (
  INSERT INTO easycode.project_members (project_id, user_id, role, assigned_by)
  SELECT project.id, person.id, 'lead', person.id FROM project, person
)
SELECT org.public_id::text, project.public_id::text FROM org, project`, name).Scan(&project.OrganizationID, &project.ProjectID)
	if err != nil {
		d.t.Fatal(err)
	}
	return project
}

func withDatabase(t *testing.T, base, name string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + strings.TrimPrefix(name, "/")
	return parsed.String()
}

func randomSuffix() string {
	buffer := make([]byte, 6)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
