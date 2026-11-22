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

// Development organization of the marco1 profile (ADR-12): fixed issuer, never a real Keycloak realm.
const (
	DevOrganizationIssuer = "urn:easycode:dev"
	DevOrganizationName   = "Easy Code (desenvolvimento)"
)

// Users reads people and organizations for sessions and authorization.
type Users struct {
	pool *pgxpool.Pool
}

var _ session.Users = Users{}

// NewUsers builds the adapter.
func NewUsers(pool *pgxpool.Pool) Users { return Users{pool: pool} }

const selectPrincipal = `
SELECT u.sub, u.display_name, o.public_id::text, o.name, u.administrator
  FROM easycode.users u JOIN easycode.organizations o ON o.id = u.organization_id
 WHERE u.active AND `

// Principal implements session.Users.
func (u Users) Principal(ctx context.Context, organizationID, subject string) (identity.Principal, error) {
	return u.scanPrincipal(conn(ctx, u.pool).QueryRow(WithOperation(ctx, "users.principal"),
		selectPrincipal+"o.public_id = $1::uuid AND u.sub = $2", organizationID, subject))
}

// DevPrincipal implements session.Users.
func (u Users) DevPrincipal(ctx context.Context, subject string) (identity.Principal, error) {
	return u.scanPrincipal(conn(ctx, u.pool).QueryRow(WithOperation(ctx, "users.dev_principal"),
		selectPrincipal+"o.oidc_issuer = $1 AND u.sub = $2", DevOrganizationIssuer, subject))
}

func (Users) scanPrincipal(row pgx.Row) (identity.Principal, error) {
	var p identity.Principal
	err := row.Scan(&p.Subject, &p.Name, &p.OrganizationID, &p.OrganizationName, &p.Administrator)
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

// SeedDevIdentity creates the development organization and its "dev" user, administrator of the
// organization (marco1 profile, ADR-12). It runs with the migration role, which owns the tables;
// running it again only refreshes the display name.
func SeedDevIdentity(ctx context.Context, pool *pgxpool.Pool, subject, name string) error {
	return NewTxRunner(pool).InTx(ctx, func(ctx context.Context) error {
		q := conn(ctx, pool)
		var organizationID int64
		err := q.QueryRow(WithOperation(ctx, "seed.dev_organization"), `
INSERT INTO easycode.organizations (name, oidc_issuer) VALUES ($1, $2)
ON CONFLICT (oidc_issuer) DO UPDATE SET name = excluded.name
RETURNING id`, DevOrganizationName, DevOrganizationIssuer).Scan(&organizationID)
		if err != nil {
			return fmt.Errorf("seed dev organization: %w", classify(err))
		}
		_, err = q.Exec(WithOperation(ctx, "seed.dev_user"), `
INSERT INTO easycode.users (organization_id, sub, display_name, administrator) VALUES ($1, $2, $3, true)
ON CONFLICT (organization_id, sub) DO UPDATE SET display_name = excluded.display_name, administrator = true, active = true`,
			organizationID, subject, name)
		if err != nil {
			return fmt.Errorf("seed dev user: %w", classify(err))
		}
		return nil
	})
}
