package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/install"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/identity"
)

// InstallDatabase implements install.DatabaseAdmin (US-55, ADR-22). The administrator connections
// have no query tracer: role statements carry generated passwords, which must never reach a log.
type InstallDatabase struct {
	Migrations fs.FS
	Logger     *slog.Logger
}

var _ install.DatabaseAdmin = InstallDatabase{}

// URL implements install.DatabaseAdmin.
func (InstallDatabase) URL(database install.Database, user, password string) string {
	return databaseURL(database, database.Name, user, password)
}

func databaseURL(database install.Database, name, user, password string) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     net.JoinHostPort(database.Host, strconv.Itoa(database.Port)),
		Path:     "/" + name,
		RawQuery: url.Values{"sslmode": {database.SSLMode}}.Encode(),
	}
	return u.String()
}

// connectAdmin opens one untraced connection as the administrator to the database name.
func connectAdmin(ctx context.Context, database install.Database, name string) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, databaseURL(database, name, database.AdminUser, database.AdminPassword))
	if err != nil {
		return nil, connectionProblem(err)
	}
	return conn, nil
}

// connectionProblem turns a connection failure into a field the wizard shows, without the URL.
func connectionProblem(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "28P01", "28000":
			return install.Invalids{{Field: "database.admin_password", Reason: "auth_failed"}}
		case "3D000":
			return install.Invalids{{Field: "database.name", Reason: "not_found"}}
		}
		return fmt.Errorf("database answered %s", pgErr.Code)
	}
	if strings.Contains(err.Error(), "SSL") || strings.Contains(err.Error(), "tls") {
		return install.Invalids{{Field: "database.ssl_mode", Reason: "tls_failed"}}
	}
	return install.Invalids{{Field: "database.host", Reason: "unreachable"}}
}

// Probe implements install.DatabaseAdmin: it only reads.
func (d InstallDatabase) Probe(ctx context.Context, database install.Database) (install.DatabaseProbe, error) {
	conn, err := connectAdmin(ctx, database, "postgres")
	if err != nil {
		return install.DatabaseProbe{}, err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	var probe install.DatabaseProbe
	var superuser, createDB, createRole bool
	err = conn.QueryRow(ctx, `
SELECT current_setting('server_version'), r.rolsuper, r.rolcreatedb, r.rolcreaterole,
       EXISTS (SELECT 1 FROM pg_database WHERE datname = $1),
       COALESCE((SELECT min(d.datname) FROM pg_database d JOIN pg_roles o ON o.oid = d.datdba
                  WHERE o.rolname = $2 AND d.datname <> $1), '')
  FROM pg_roles r WHERE r.rolname = current_user`, database.Name, install.MigratorLogin).
		Scan(&probe.ServerVersion, &superuser, &createDB, &createRole, &probe.Exists, &probe.OtherPlatformDatabase)
	if err != nil {
		return install.DatabaseProbe{}, fmt.Errorf("read the server: %w", err)
	}
	probe.CanCreate = superuser || (createDB && createRole)
	if !probe.Exists {
		return probe, nil
	}
	target, err := connectAdmin(ctx, database, database.Name)
	if err != nil {
		return install.DatabaseProbe{}, err
	}
	defer target.Close(context.WithoutCancel(ctx))
	err = target.QueryRow(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL OR to_regclass('easycode.organizations') IS NOT NULL`).
		Scan(&probe.HasPlatformTables)
	if err != nil {
		return install.DatabaseProbe{}, fmt.Errorf("read the database: %w", err)
	}
	return probe, nil
}

// processRoles are the NOLOGIN roles of migration 0001, created here first so the migration does not
// need CREATEROLE.
var processRoles = []string{"easycode_migration", "easycode_api", "easycode_worker", "easycode_retention", "easycode_backup"}

// Prepare implements install.DatabaseAdmin: roles, login users and the database owned by the
// migrator. Every statement is idempotent; a password is rewritten on a retry.
func (d InstallDatabase) Prepare(ctx context.Context, database install.Database, logins install.Logins) error {
	conn, err := connectAdmin(ctx, database, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	var superuser bool
	if err := conn.QueryRow(ctx, "SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&superuser); err != nil {
		return fmt.Errorf("read the administrator: %w", err)
	}
	for _, role := range processRoles {
		if err := ensureRole(ctx, conn, role, "", ""); err != nil {
			return err
		}
	}
	for _, login := range []struct{ name, role, password string }{
		{install.MigratorLogin, "easycode_migration", logins.Migrator},
		{install.APILogin, "easycode_api", logins.API},
		{install.WorkerLogin, "easycode_worker", logins.Worker},
	} {
		if err := ensureRole(ctx, conn, login.name, login.role, login.password); err != nil {
			return err
		}
	}
	if !superuser {
		// A non-superuser may only give a database to a role it can SET ROLE to.
		if _, err := conn.Exec(ctx, "GRANT "+pgx.Identifier{install.MigratorLogin}.Sanitize()+" TO CURRENT_USER"); err != nil {
			return fmt.Errorf("grant the migrator to the administrator: %w", statementProblem(err))
		}
	}
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", database.Name).Scan(&exists); err != nil {
		return fmt.Errorf("read the database: %w", err)
	}
	name, owner := pgx.Identifier{database.Name}.Sanitize(), pgx.Identifier{install.MigratorLogin}.Sanitize()
	statement := "CREATE DATABASE " + name + " OWNER " + owner + " ENCODING 'UTF8'"
	if exists {
		statement = "ALTER DATABASE " + name + " OWNER TO " + owner
	}
	if _, err := conn.Exec(ctx, statement); err != nil {
		return fmt.Errorf("create the database: %w", statementProblem(err))
	}
	return nil
}

// ensureRole creates role (or updates it) with LOGIN and the password when password is set, and makes
// it a member of memberOf when set.
func ensureRole(ctx context.Context, conn *pgx.Conn, role, memberOf, password string) error {
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil {
		return fmt.Errorf("read role %s: %w", role, err)
	}
	name := pgx.Identifier{role}.Sanitize()
	options := " NOLOGIN"
	if password != "" {
		options = " LOGIN PASSWORD " + quoteLiteral(password)
	}
	statement := "CREATE ROLE " + name + options
	if exists {
		statement = "ALTER ROLE " + name + options
	}
	if _, err := conn.Exec(ctx, statement); err != nil {
		return fmt.Errorf("role %s: %w", role, statementProblem(err))
	}
	if memberOf != "" {
		if _, err := conn.Exec(ctx, "GRANT "+pgx.Identifier{memberOf}.Sanitize()+" TO "+name); err != nil {
			return fmt.Errorf("grant %s to %s: %w", memberOf, role, statementProblem(err))
		}
	}
	return nil
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// statementProblem keeps the SQLSTATE and the server message, never the statement (which may hold a
// password).
func statementProblem(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return fmt.Errorf("%s (%s)", pgErr.Message, pgErr.Code)
	}
	return errors.New("statement failed")
}

// Migrate implements install.DatabaseAdmin: the embedded migrations as the migrator.
func (d InstallDatabase) Migrate(ctx context.Context, database install.Database, logins install.Logins) (int, error) {
	files, err := LoadMigrations(d.Migrations)
	if err != nil {
		return 0, err
	}
	pool, err := NewPool(ctx, PoolSettings{URL: d.URL(database, install.MigratorLogin, logins.Migrator), MaxConns: 1}, d.Logger)
	if err != nil {
		return 0, errors.New("connect as the migrator failed")
	}
	defer pool.Close()
	if _, err := NewMigrator(pool, d.Logger).Apply(ctx, files); err != nil {
		return 0, err
	}
	var version int
	if err := pool.QueryRow(ctx, "SELECT coalesce(max(version), 0) FROM public.schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("read the schema version: %w", err)
	}
	return version, nil
}

// Installation writes the records of the install into the migrated database (as the migrator).
type Installation struct {
	pool *pgxpool.Pool
}

// NewInstallation builds the adapter.
func NewInstallation(pool *pgxpool.Pool) Installation { return Installation{pool: pool} }

// Register records the organization, the administrator, the person of step 6 and the audit record
// platform.install, in one transaction (ER-31-11). A retry updates the same rows.
func (i Installation) Register(ctx context.Context, registration install.Registration) (identity.Principal, error) {
	var principal identity.Principal
	err := NewTxRunner(i.pool).InTx(ctx, func(ctx context.Context) error {
		q := conn(ctx, i.pool)
		var organizationID int64
		org := registration.Organization
		err := q.QueryRow(WithOperation(ctx, "install.organization"), `
INSERT INTO easycode.organizations (name, oidc_issuer, currency) VALUES ($1, $2, $3)
ON CONFLICT (oidc_issuer) DO UPDATE SET name = excluded.name, currency = excluded.currency
RETURNING id, public_id::text`, strings.TrimSpace(org.Name), registration.Issuer, org.Currency).Scan(&organizationID, &principal.OrganizationID)
		if err != nil {
			return fmt.Errorf("register the organization: %w", classify(err))
		}
		people := []struct {
			user          install.DirectoryUser
			administrator bool
		}{{registration.Administrator, true}}
		if registration.User != nil {
			people = append(people, struct {
				user          install.DirectoryUser
				administrator bool
			}{*registration.User, false})
		}
		for _, person := range people {
			name := strings.TrimSpace(person.user.Name)
			if name == "" {
				name = person.user.Username
			}
			_, err := q.Exec(WithOperation(ctx, "install.user"), `
INSERT INTO easycode.users (organization_id, sub, display_name, administrator) VALUES ($1, $2, $3, $4)
ON CONFLICT (organization_id, sub) DO UPDATE SET display_name = excluded.display_name, administrator = excluded.administrator`,
				organizationID, person.user.ID, name, person.administrator)
			if err != nil {
				return fmt.Errorf("register a person: %w", classify(err))
			}
		}
		principal.Subject = registration.Administrator.ID
		principal.Name = registration.Administrator.Name
		principal.OrganizationName = org.Name
		principal.OrganizationCurrency = org.Currency
		principal.Administrator = true
		var recorded bool
		if err := q.QueryRow(WithOperation(ctx, "install.audit_exists"), `
SELECT EXISTS (SELECT 1 FROM easycode.audit_records WHERE action = 'platform.install')`).Scan(&recorded); err != nil {
			return fmt.Errorf("read the audit trail: %w", err)
		}
		if recorded {
			return nil
		}
		_, err = NewAuditTrail(i.pool).Append(ctx, domainaudit.Record{
			OrganizationID: principal.OrganizationID,
			Actor:          domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
			Origin:         domainaudit.OriginWeb, Action: "platform.install", Target: "platform",
			Decision: "installed",
		})
		return err
	})
	return principal, err
}

// ImportReceived reports whether an import with the key already exists (a retry of the skill step).
func (i Installation) ImportReceived(ctx context.Context, principal identity.Principal, key string) (bool, error) {
	var exists bool
	err := i.pool.QueryRow(WithOperation(ctx, "install.skill_import_exists"), `
SELECT EXISTS (SELECT 1 FROM easycode.skill_imports
 WHERE idempotency_key = $2 AND organization_id = (SELECT id FROM easycode.organizations WHERE public_id = $1::uuid))`,
		principal.OrganizationID, key).Scan(&exists)
	return exists, err
}

// Lock writes the row of platform_installation (US-55 RN-1).
func (i Installation) Lock(ctx context.Context, installation install.Installation) error {
	_, err := i.pool.Exec(WithOperation(ctx, "install.lock"), `
INSERT INTO easycode.platform_installation (installed_at, sso_mode, realm, administrator_sub, installer_version)
VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO NOTHING`,
		installation.InstalledAt, installation.Mode, installation.Realm, installation.AdministratorSub, installation.Version)
	if err != nil {
		return fmt.Errorf("write the installation lock: %w", classify(err))
	}
	return nil
}

// Principal reads the administrator as the platform sees them (for the skill import of the install).
func (i Installation) Principal(ctx context.Context, issuer, subject string) (identity.Principal, error) {
	var organizationID string
	err := i.pool.QueryRow(WithOperation(ctx, "install.organization_id"),
		"SELECT public_id::text FROM easycode.organizations WHERE oidc_issuer = $1", issuer).Scan(&organizationID)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("read the organization: %w", classify(err))
	}
	return NewUsers(i.pool).Principal(ctx, organizationID, subject)
}
