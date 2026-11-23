package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/session"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Users reads and records people and organizations for sessions and authorization.
type Users struct {
	pool *pgxpool.Pool
}

var _ session.Users = Users{}

// NewUsers builds the adapter.
func NewUsers(pool *pgxpool.Pool) Users { return Users{pool: pool} }

const selectPrincipal = `
SELECT u.sub, u.display_name, o.public_id::text, o.name, o.currency, u.administrator
  FROM easycode.users u JOIN easycode.organizations o ON o.id = u.organization_id
 WHERE u.active AND `

// Principal implements session.Users.
func (u Users) Principal(ctx context.Context, organizationID, subject string) (identity.Principal, error) {
	return u.scanPrincipal(conn(ctx, u.pool).QueryRow(WithOperation(ctx, "users.principal"),
		selectPrincipal+"o.public_id = $1::uuid AND u.sub = $2", organizationID, subject))
}

// SignIn implements session.Users: the organization of the issuer must exist; a deactivated
// person stays out (account-without-access, US-01 CA-2, D-31); otherwise the display name and the
// administrator role come from the ID token of this login.
func (u Users) SignIn(ctx context.Context, issuer string, claims session.Claims) (identity.Principal, error) {
	var principal identity.Principal
	err := NewTxRunner(u.pool).InTx(ctx, func(ctx context.Context) error {
		q := conn(ctx, u.pool)
		var organizationID int64
		err := q.QueryRow(WithOperation(ctx, "organizations.by_issuer"),
			"SELECT id FROM easycode.organizations WHERE oidc_issuer = $1", issuer).Scan(&organizationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
		}
		if err != nil {
			return fmt.Errorf("organization of issuer: %w", classify(err))
		}
		var active bool
		err = q.QueryRow(WithOperation(ctx, "users.state"),
			"SELECT active FROM easycode.users WHERE organization_id = $1 AND sub = $2", organizationID, claims.Subject).Scan(&active)
		switch {
		case err == nil && !active:
			return failure.New(failure.KindForbidden, session.CodeAccountWithoutAccess, nil)
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("user state: %w", classify(err))
		}
		_, err = q.Exec(WithOperation(ctx, "users.sign_in"), `
INSERT INTO easycode.users (organization_id, sub, display_name, administrator) VALUES ($1, $2, $3, $4)
ON CONFLICT (organization_id, sub) DO UPDATE SET display_name = excluded.display_name, administrator = excluded.administrator`,
			organizationID, claims.Subject, claims.Name, claims.Administrator)
		if err != nil {
			return fmt.Errorf("sign in: %w", classify(err))
		}
		principal, err = u.scanPrincipal(q.QueryRow(WithOperation(ctx, "users.principal"),
			selectPrincipal+"o.id = $1 AND u.sub = $2", organizationID, claims.Subject))
		return err
	})
	return principal, err
}

func (Users) scanPrincipal(row pgx.Row) (identity.Principal, error) {
	var p identity.Principal
	err := row.Scan(&p.Subject, &p.Name, &p.OrganizationID, &p.OrganizationName, &p.OrganizationCurrency, &p.Administrator)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Principal{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return identity.Principal{}, fmt.Errorf("read principal: %w", classify(err))
	}
	return p, nil
}

// LeadInAnyProject implements session.Users.
func (u Users) LeadInAnyProject(ctx context.Context, p identity.Principal) (bool, error) {
	var lead bool
	err := conn(ctx, u.pool).QueryRow(WithOperation(ctx, "users.lead_in_any_project"), `
SELECT EXISTS (SELECT 1 FROM easycode.project_members m JOIN easycode.projects p ON p.id = m.project_id
                 WHERE m.user_id = `+principalUserIDSQL+` AND m.role = 'lead' AND p.deletion_requested_at IS NULL)`,
		p.OrganizationID, p.Subject).Scan(&lead)
	if err != nil {
		return false, fmt.Errorf("lead in any project: %w", classify(err))
	}
	return lead, nil
}

// principalUserIDSQL resolves the principal of parameters $1 (organization public id) and $2
// (sub) to users.id.
const principalUserIDSQL = `(SELECT uu.id FROM easycode.users uu JOIN easycode.organizations oo ON oo.id = uu.organization_id
 WHERE oo.public_id = $1::uuid AND uu.sub = $2)`

// EnsureOrganization creates the organization of an OIDC issuer when it does not exist yet (the
// realm of the deployment, ADR-12). It runs with the migration role at deploy time; an existing
// organization keeps its name and settings.
func EnsureOrganization(ctx context.Context, pool *pgxpool.Pool, issuer, name string) error {
	_, err := pool.Exec(WithOperation(ctx, "organizations.ensure"), `
INSERT INTO easycode.organizations (name, oidc_issuer) VALUES ($1, $2) ON CONFLICT (oidc_issuer) DO NOTHING`, name, issuer)
	if err != nil {
		return fmt.Errorf("ensure organization: %w", classify(err))
	}
	return nil
}
