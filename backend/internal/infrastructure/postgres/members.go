package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Members implements appproject.MemberStore over project_members (US-02).
type Members struct {
	pool *pgxpool.Pool
}

var _ appproject.MemberStore = Members{}

// NewMembers builds the adapter.
func NewMembers(pool *pgxpool.Pool) Members { return Members{pool: pool} }

// AdministratorAccess implements appproject.MemberStore.
func (m Members) AdministratorAccess(ctx context.Context, p identity.Principal, projectID string) (appproject.Access, error) {
	access := appproject.Access{ProjectID: projectID, Roles: []identity.Role{}}
	err := conn(ctx, m.pool).QueryRow(WithOperation(ctx, "projects.administrator_access"), `
SELECT pr.state FROM easycode.projects pr JOIN easycode.organizations o ON o.id = pr.organization_id
  JOIN easycode.users u ON u.organization_id = o.id AND u.sub = $2 AND u.active AND u.administrator
 WHERE o.public_id = $1::uuid AND pr.public_id = $3::uuid AND pr.deletion_requested_at IS NULL`,
		p.OrganizationID, p.Subject, projectID).Scan(&access.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return access, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return access, fmt.Errorf("administrator access: %w", classify(err))
	}
	return access, nil
}

// AccessRecordedSince implements appproject.MemberStore.
func (m Members) AccessRecordedSince(ctx context.Context, p identity.Principal, projectID string, since time.Time) (bool, error) {
	var recorded bool
	err := conn(ctx, m.pool).QueryRow(WithOperation(ctx, "audit.administrator_access"), `
SELECT EXISTS (SELECT 1 FROM easycode.audit_records a JOIN easycode.projects pr ON pr.id = a.project_id
                WHERE pr.public_id = $1::uuid AND a.action = $2 AND a.actor_id = $3 AND a.recorded_at >= $4)`,
		projectID, appproject.ActionAccessAsAdministrator, p.Subject, since).Scan(&recorded)
	if err != nil {
		return false, fmt.Errorf("administrator access recorded: %w", classify(err))
	}
	return recorded, nil
}

// Members implements appproject.MemberStore: one item per person, roles sorted, the latest assignment.
func (m Members) Members(ctx context.Context, projectID string) ([]appproject.Member, error) {
	rows, err := conn(ctx, m.pool).Query(WithOperation(ctx, "project_members.list"), `
SELECT u.sub, u.display_name, array_agg(pm.role ORDER BY pm.role),
       (array_agg(ab.sub ORDER BY pm.assigned_at DESC))[1], (array_agg(ab.display_name ORDER BY pm.assigned_at DESC))[1], max(pm.assigned_at)
  FROM easycode.project_members pm
  JOIN easycode.projects pr ON pr.id = pm.project_id
  JOIN easycode.users u ON u.id = pm.user_id
  JOIN easycode.users ab ON ab.id = pm.assigned_by
 WHERE pr.public_id = $1::uuid
 GROUP BY u.id, u.sub, u.display_name
 ORDER BY u.display_name, u.sub`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appproject.Member, error) {
		var member appproject.Member
		var roles []string
		err := row.Scan(&member.User.Subject, &member.User.Name, &roles, &member.AssignedBy.Subject, &member.AssignedBy.Name, &member.AssignedAt)
		for _, r := range roles {
			member.Roles = append(member.Roles, identity.Role(r))
		}
		return member, err
	})
}

// Roles implements appproject.MemberStore.
func (m Members) Roles(ctx context.Context, projectID, subject string) ([]identity.Role, error) {
	var roles []string
	err := conn(ctx, m.pool).QueryRow(WithOperation(ctx, "project_members.roles"), `
SELECT coalesce(array_agg(pm.role ORDER BY pm.role) FILTER (WHERE pm.role IS NOT NULL), '{}')
  FROM easycode.projects pr
  JOIN easycode.users u ON u.organization_id = pr.organization_id AND u.sub = $2
  LEFT JOIN easycode.project_members pm ON pm.project_id = pr.id AND pm.user_id = u.id
 WHERE pr.public_id = $1::uuid`, projectID, subject).Scan(&roles)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("member roles: %w", classify(err))
	}
	out := make([]identity.Role, 0, len(roles))
	for _, r := range roles {
		out = append(out, identity.Role(r))
	}
	return out, nil
}

// Lead implements appproject.MemberStore.
func (m Members) Lead(ctx context.Context, projectID string) (string, error) {
	var subject string
	err := conn(ctx, m.pool).QueryRow(WithOperation(ctx, "project_members.lead"), `
SELECT u.sub FROM easycode.project_members pm JOIN easycode.projects pr ON pr.id = pm.project_id
  JOIN easycode.users u ON u.id = pm.user_id WHERE pr.public_id = $1::uuid AND pm.role = 'lead'`, projectID).Scan(&subject)
	if err != nil {
		return "", fmt.Errorf("project lead: %w", classify(err))
	}
	return subject, nil
}

// KnownPerson implements appproject.MemberStore.
func (m Members) KnownPerson(ctx context.Context, organizationID, subject string) (bool, error) {
	var known bool
	err := conn(ctx, m.pool).QueryRow(WithOperation(ctx, "users.known"), `
SELECT EXISTS (SELECT 1 FROM easycode.users u JOIN easycode.organizations o ON o.id = u.organization_id
                WHERE o.public_id = $1::uuid AND u.sub = $2 AND u.active)`, organizationID, subject).Scan(&known)
	if err != nil {
		return false, fmt.Errorf("known person: %w", classify(err))
	}
	return known, nil
}

// EnsurePerson implements appproject.MemberStore.
func (m Members) EnsurePerson(ctx context.Context, organizationID, subject, name string) error {
	tag, err := conn(ctx, m.pool).Exec(WithOperation(ctx, "users.ensure"), `
INSERT INTO easycode.users (organization_id, sub, display_name)
SELECT o.id, $2, $3 FROM easycode.organizations o WHERE o.public_id = $1::uuid
ON CONFLICT (organization_id, sub) DO NOTHING`, organizationID, subject, name)
	if err != nil {
		return fmt.Errorf("ensure person: %w", classify(err))
	}
	if tag.RowsAffected() == 0 {
		known, err := m.KnownPerson(ctx, organizationID, subject)
		if err != nil {
			return err
		}
		if !known {
			return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
		}
	}
	return nil
}

// SetRoles implements appproject.MemberStore: roles left out are deleted, new ones inserted with
// who assigned them; the database rules (one lead, at least one approver) run at commit.
func (m Members) SetRoles(ctx context.Context, p identity.Principal, projectID, subject string, roles []identity.Role) error {
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, string(r))
	}
	q := conn(ctx, m.pool)
	_, err := q.Exec(WithOperation(ctx, "project_members.delete_roles"), `
DELETE FROM easycode.project_members pm USING easycode.projects pr, easycode.users u
 WHERE pm.project_id = pr.id AND pm.user_id = u.id AND pr.public_id = $1::uuid
   AND u.organization_id = pr.organization_id AND u.sub = $2 AND NOT (pm.role = ANY ($3::text[]))`, projectID, subject, names)
	if err != nil {
		return fmt.Errorf("delete roles: %w", classify(err))
	}
	if len(names) == 0 {
		return nil
	}
	_, err = q.Exec(WithOperation(ctx, "project_members.insert_roles"), `
INSERT INTO easycode.project_members (project_id, user_id, role, assigned_by)
SELECT pr.id, u.id, r.role, `+principalUserIDSQLAt(4)+`
  FROM easycode.projects pr
  JOIN easycode.users u ON u.organization_id = pr.organization_id AND u.sub = $2 AND u.active
  CROSS JOIN unnest($3::text[]) AS r(role)
 WHERE pr.public_id = $1::uuid
ON CONFLICT (project_id, user_id, role) DO NOTHING`, projectID, subject, names, p.OrganizationID, p.Subject)
	if err != nil {
		return fmt.Errorf("insert roles: %w", classify(err))
	}
	return nil
}
