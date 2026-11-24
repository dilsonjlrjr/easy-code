package postgres_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"easycode/internal/application/install"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/migrations"
)

// installDatabase points the installer at the disposable server of EASYCODE_TEST_DATABASE_URL, with
// a database name of its own.
func installDatabase(t *testing.T) install.Database {
	t.Helper()
	base := os.Getenv(postgrestest.EnvURL)
	if base == "" {
		t.Skip(postgrestest.EnvURL + " not set")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 5432
	}
	password, _ := u.User.Password()
	name := "easycode_install_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), base)
		if err == nil {
			_, _ = conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
			_ = conn.Close(context.Background())
		}
	})
	return install.Database{Host: u.Hostname(), Port: port, AdminUser: u.User.Username(), AdminPassword: password, Name: name, SSLMode: "disable"}
}

func TestInstallCreatesTheDatabaseMigratesAsTheMigratorAndLocks(t *testing.T) {
	ctx := context.Background()
	database := installDatabase(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	admin := postgres.InstallDatabase{Migrations: migrations.Files(), Logger: logger}

	probe, err := admin.Probe(ctx, database)
	if err != nil || !probe.CanCreate || probe.Exists {
		t.Fatalf("probe = %+v, %v", probe, err)
	}
	logins := install.Logins{Migrator: "migrator-pw-1", API: "api-pw-1", Worker: "worker-pw-1"}
	if err := admin.Prepare(ctx, database, logins); err != nil {
		t.Fatal(err)
	}
	// Idempotent: a retry with new passwords keeps working.
	logins.API = "api-pw-2"
	if err := admin.Prepare(ctx, database, logins); err != nil {
		t.Fatal(err)
	}
	version, err := admin.Migrate(ctx, database, logins)
	if err != nil || version < 17 {
		t.Fatalf("migrate = %d, %v", version, err)
	}
	if probe, err = admin.Probe(ctx, database); err != nil || !probe.HasPlatformTables {
		t.Fatalf("probe after migrate = %+v, %v", probe, err)
	}

	migrator, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: admin.URL(database, install.MigratorLogin, logins.Migrator), MaxConns: 2}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	installation := postgres.NewInstallation(migrator)
	registration := install.Registration{
		Organization:  install.Organization{Name: "Empresa", Currency: "USD", PublicOrigin: "https://easycode.example", Locale: "pt-BR"},
		Issuer:        "https://sso.example/auth/realms/easycode",
		Administrator: install.DirectoryUser{ID: "sub-admin", Username: "ana", Name: "Ana"},
		User:          &install.DirectoryUser{ID: "sub-user", Username: "bruno", Name: "Bruno"},
	}
	principal, err := installation.Register(ctx, registration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installation.Register(ctx, registration); err != nil { // a retry
		t.Fatal(err)
	}
	if !principal.Administrator || principal.OrganizationCurrency != "USD" {
		t.Fatalf("principal = %+v", principal)
	}
	read, err := installation.Principal(ctx, registration.Issuer, "sub-admin")
	if err != nil || !read.Administrator || read.OrganizationID != principal.OrganizationID {
		t.Fatalf("read principal = %+v, %v", read, err)
	}
	var audits int
	if err := migrator.QueryRow(ctx, "SELECT count(*) FROM easycode.audit_records WHERE action = 'platform.install'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit records = %d, %v", audits, err)
	}
	lock := install.Installation{InstalledAt: time.Now(), Mode: install.ModeNewRealm, Realm: "easycode", AdministratorSub: "sub-admin", Version: "test"}
	if err := installation.Lock(ctx, lock); err != nil {
		t.Fatal(err)
	}

	// The api login reaches the platform with the grants of its role, and only reads the lock.
	api, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: admin.URL(database, install.APILogin, logins.API), MaxConns: 1}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var realm string
	if err := api.QueryRow(ctx, "SELECT realm FROM easycode.platform_installation").Scan(&realm); err != nil || realm != "easycode" {
		t.Fatalf("api reads the lock: %q, %v", realm, err)
	}
	if _, err := api.Exec(ctx, "DELETE FROM easycode.platform_installation"); err == nil {
		t.Fatal("the api login deleted the lock")
	}
	if _, err := postgres.NewUsers(api).Principal(ctx, principal.OrganizationID, "sub-user"); err != nil {
		t.Fatalf("api reads the user: %v", err)
	}
}

func TestInstallProbeReportsAWrongPassword(t *testing.T) {
	database := installDatabase(t)
	database.AdminPassword = "wrong-password"
	_, err := postgres.InstallDatabase{}.Probe(context.Background(), database)
	var invalid install.Invalids
	if !errors.As(err, &invalid) || invalid[0].Reason != "auth_failed" {
		t.Fatalf("got %v", err)
	}
}
