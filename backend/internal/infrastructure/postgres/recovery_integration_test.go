package postgres_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"easycode/internal/application/runtime"
	appsandbox "easycode/internal/application/sandbox"
	"easycode/internal/infrastructure/docker"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/migrations"
)

// recoveryTables are compared row by row between the source and the restored database.
var recoveryTables = []string{"easycode.organizations", "easycode.users", "easycode.projects", "easycode.project_members",
	"easycode.audit_records", "easycode.events", "easycode.state_transitions", "public.schema_migrations"}

func snapshot(ctx context.Context, t *testing.T, conn interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range recoveryTables {
		var rows int
		var sum *string
		if err := conn.QueryRow(ctx, "SELECT count(*), md5(string_agg(x::text, '|' ORDER BY x::text)) FROM "+table+" x").Scan(&rows, &sum); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		digest := ""
		if sum != nil {
			digest = *sum
		}
		out[table] = fmt.Sprintf("%d rows, md5 %s", rows, digest)
	}
	return out
}

// TestRecoveryFromBackup is the restore test of RNF-11 (RPO 24 h, RTO 4 h, restore tested; PERSIST.md backup):
// the scripts of project/infra/backup dump the database (pg_dump, custom format) and copy the project
// repositories; after the loss of both, a new database and an empty repos folder are restored from the run
// and hold the same rows, the same migration state and the same git history, and the platform roles read them.
func TestRecoveryFromBackup(t *testing.T) {
	socket, scripts := os.Getenv("EASYCODE_TEST_DOCKER_SOCKET"), os.Getenv("EASYCODE_TEST_BACKUP_SCRIPTS")
	if socket == "" || scripts == "" {
		t.Skip("needs EASYCODE_TEST_DOCKER_SOCKET and EASYCODE_TEST_BACKUP_SCRIPTS (make test-integration)")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	db := postgrestest.New(t)
	ctx := context.Background()
	project := db.CreateProject("recovery")
	trail := postgres.NewAuditTrail(db.PoolAs("easycode_api"))
	for i := 0; i < 3; i++ {
		if _, err := trail.Append(ctx, humanRecord(project.ProjectID)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := postgres.NewOutbox(db.PoolAs("easycode_api")).Append(ctx, gateApproved(project.ProjectID)); err != nil {
		t.Fatal(err)
	}
	reposDir := t.TempDir()
	repos := git.New(reposDir)
	head, err := repos.Commit(ctx, project.ProjectID, runtime.Commit{Wave: "REQ-01", Agent: "requirements-agent",
		Message: "REQ-01: requisitos\n\nEasycode-Wave: REQ-01\n", Files: map[string][]byte{"docs/requisitos/VISAO.md": []byte("# Visão\n")}})
	if err != nil {
		t.Fatal(err)
	}
	// The files volume: an attachment of the business need (US-03).
	filesDir := t.TempDir()
	attachment := filepath.Join("attachments", project.ProjectID, "0192f0aa-0000-7000-8000-000000000001")
	if err := os.MkdirAll(filepath.Join(filesDir, filepath.Dir(attachment)), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, attachment), []byte("# Ata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(ctx, t, db.Admin)

	parsed, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	container, source := parsed.Hostname(), strings.TrimPrefix(parsed.Path, "/")
	password, _ := parsed.User.Password()
	client := docker.New(socket)
	backupScript, err := os.ReadFile(filepath.Join(scripts, "backup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	restoreScript, err := os.ReadFile(filepath.Join(scripts, "restore.sh"))
	if err != nil {
		t.Fatal(err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	destination := "/tmp/rnf11-" + hex.EncodeToString(suffix)
	pgEnv := func(database string) []string {
		return []string{"PGHOST=127.0.0.1", "PGUSER=" + parsed.User.Username(), "PGPASSWORD=" + password, "PGDATABASE=" + database}
	}
	inContainer := func(script []byte, env []string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		code, err := client.Exec(ctx, container, appsandbox.ExecSpec{Cmd: append([]string{"sh", "-s", "--"}, args...), Env: env}, bytes.NewReader(script), &out)
		if err != nil || code != 0 {
			t.Fatalf("script in %s: %d %v\n%s", container, code, err, out.String())
		}
		return strings.TrimSpace(out.String())
	}
	local := func(script string, env []string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "sh", append([]string{filepath.Join(scripts, script)}, args...)...)
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", script, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	lastLine := func(text string) string {
		lines := strings.Split(text, "\n")
		return lines[len(lines)-1]
	}

	// Backup: the database in the PostgreSQL container (its pg_dump), the repositories here.
	started := time.Now()
	run := lastLine(inContainer(backupScript, pgEnv(source), destination))
	reposBackup := t.TempDir()
	reposRun := lastLine(local("backup.sh", []string{"EASYCODE_REPOS_DIR=" + reposDir, "EASYCODE_FILES_DIR=" + filesDir}, reposBackup))
	t.Cleanup(func() {
		_, _ = client.Exec(context.Background(), container, appsandbox.ExecSpec{Cmd: []string{"rm", "-rf", destination}}, nil, &bytes.Buffer{})
	})

	// Loss: a new empty database and an empty repos folder; restore from the run.
	restored := "easycode_restore_" + hex.EncodeToString(suffix)
	server, err := pgx.Connect(ctx, strings.Replace(db.URL, "/"+source, "/postgres", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(ctx)
	if _, err := server.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{restored}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = server.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{restored}.Sanitize()+" WITH (FORCE)")
	})
	inContainer(restoreScript, pgEnv(restored), run)
	newRepos := filepath.Join(t.TempDir(), "repos")
	newFiles := filepath.Join(t.TempDir(), "files")
	local("restore.sh", []string{"EASYCODE_REPOS_DIR=" + newRepos, "EASYCODE_FILES_DIR=" + newFiles}, reposRun)
	elapsed := time.Since(started)

	target, err := pgx.Connect(ctx, strings.Replace(db.URL, "/"+source, "/"+restored, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(ctx)
	after := snapshot(ctx, t, target)
	for table, digest := range before {
		if after[table] != digest {
			t.Fatalf("RNF-11: %s differs after the restore: %s, want %s", table, after[table], digest)
		}
	}
	// The restored database is at the same migration: the migrator has nothing to apply.
	pool, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: strings.Replace(db.URL, "/"+source, "/"+restored, 1), MaxConns: 2}, db.Logger)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	files, err := postgres.LoadMigrations(migrations.Files())
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := postgres.NewMigrator(pool, db.Logger).Apply(ctx, files); err != nil || applied != 0 {
		t.Fatalf("RNF-11: migrations after the restore: %d %v", applied, err)
	}
	// The grants came back: the api role reads the restored rows.
	var name string
	if _, err := target.Exec(ctx, "SET ROLE easycode_api"); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, "SELECT name FROM easycode.projects WHERE public_id = $1::uuid", project.ProjectID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "recovery" {
		t.Fatalf("RNF-11: project after the restore: %q", name)
	}
	restoredHead, err := git.New(newRepos).Head(ctx, project.ProjectID)
	if err != nil || restoredHead.SHA != head || restoredHead.Wave != "REQ-01" {
		t.Fatalf("RNF-11: repository after the restore: %+v %v (want %s)", restoredHead, err, head)
	}
	if content, err := os.ReadFile(filepath.Join(newFiles, attachment)); err != nil || string(content) != "# Ata\n" {
		t.Fatalf("RNF-11: files volume after the restore: %q %v", content, err)
	}
	if elapsed > 4*time.Hour {
		t.Fatalf("RNF-11: RTO %s", elapsed)
	}
	t.Logf("RNF-11: backup and restore in %s", elapsed)
}
