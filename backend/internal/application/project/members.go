package project

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"easycode/internal/application/audit"
	"easycode/internal/application/event"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
)

// Failure codes of the member rules (openapi Problem; the database rules map to the same codes).
const (
	CodeLastApprover       = "last-approver"
	CodeLeadRequired       = "lead-required"
	CodeSeparationOfDuties = "separation-of-duties"
)

// ActionAccessAsAdministrator is recorded once per session and project when the administrator
// opens a project without a role in it (US-02 CA-6, RNF-13).
const ActionAccessAsAdministrator = "project.access_as_administrator"

// Member is a person of the project with the roles.
type Member struct {
	User       UserRef
	Roles      []identity.Role
	AssignedBy UserRef
	AssignedAt time.Time
}

// MemberStore keeps the roles of the people in the projects (project_members).
type MemberStore interface {
	// AdministratorAccess returns the access of an administrator of the project's organization
	// without a role in it (roles empty), else not-found.
	AdministratorAccess(ctx context.Context, principal identity.Principal, projectID string) (Access, error)
	// AccessRecordedSince reports whether the administrator access was recorded since a moment.
	AccessRecordedSince(ctx context.Context, principal identity.Principal, projectID string, since time.Time) (bool, error)
	Members(ctx context.Context, projectID string) ([]Member, error)
	// Roles returns the roles of a person in the project (empty when none).
	Roles(ctx context.Context, projectID, subject string) ([]identity.Role, error)
	// Lead returns the subject of the lead of the project.
	Lead(ctx context.Context, projectID string) (string, error)
	// EnsurePerson makes sure the person is a user of the organization: known people are kept as
	// they are; a new one is recorded with the display name of the directory. A deactivated person
	// is not-found.
	EnsurePerson(ctx context.Context, organizationID, subject, name string) error
	// KnownPerson reports whether the person is an active user of the organization.
	KnownPerson(ctx context.Context, organizationID, subject string) (bool, error)
	// SetRoles replaces the roles of the person in the project (assigned by principal).
	SetRoles(ctx context.Context, principal identity.Principal, projectID, subject string, roles []identity.Role) error
}

// Directory finds people of the organization in the SSO (searchUsers, US-02).
type Directory interface {
	Search(ctx context.Context, text string, first, limit int) ([]UserRef, error)
	// Person returns the display name of a person of the SSO; found is false when unknown.
	Person(ctx context.Context, subject string) (string, bool, error)
}

// MemberService implements the role use cases of US-02.
type MemberService struct {
	tx        transaction.Runner
	projects  Repository
	members   MemberStore
	directory Directory
	outbox    event.Outbox
	trail     audit.Trail
}

// NewMemberService builds the service. directory may be nil (no searchUsers; only people who
// already signed in can receive a role).
func NewMemberService(tx transaction.Runner, projects Repository, members MemberStore, directory Directory, outbox event.Outbox, trail audit.Trail) *MemberService {
	return &MemberService{tx: tx, projects: projects, members: members, directory: directory, outbox: outbox, trail: trail}
}

// Open authorizes a read of the project: a role in it, or the organization administrator, whose
// first access in the session is recorded (US-02 CA-6). Without either it is not-found (CA-3).
func (s *MemberService) Open(ctx context.Context, principal identity.Principal, projectID string) (Access, error) {
	access, err := s.projects.Access(ctx, principal, projectID)
	if err == nil || !principal.Administrator || failure.From(err).Kind != failure.KindNotFound {
		return access, err
	}
	access, err = s.members.AdministratorAccess(ctx, principal, projectID)
	if err != nil {
		return Access{}, err
	}
	return access, s.recordAdministratorAccess(ctx, principal, projectID)
}

func (s *MemberService) recordAdministratorAccess(ctx context.Context, principal identity.Principal, projectID string) error {
	since := time.Now().Add(-identity.SessionLifetime)
	if current, ok := identity.SessionFrom(ctx); ok {
		since = current.IssuedAt
	}
	recorded, err := s.members.AccessRecordedSince(ctx, principal, projectID, since)
	if err != nil || recorded {
		return err
	}
	_, err = s.trail.Append(ctx, domainaudit.Record{
		ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
		Origin: domainaudit.OriginWeb, Action: ActionAccessAsAdministrator, Target: "project",
	})
	return err
}

// List is listMembers.
func (s *MemberService) List(ctx context.Context, principal identity.Principal, projectID string) ([]Member, error) {
	if _, err := s.Open(ctx, principal, projectID); err != nil {
		return nil, err
	}
	return s.members.Members(ctx, projectID)
}

// Search is searchUsers: only a lead of some project or the administrator (openapi x-roles).
func (s *MemberService) Search(ctx context.Context, principal identity.Principal, lead bool, text string, first, limit int) ([]UserRef, error) {
	if !lead && !principal.Administrator {
		return nil, failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	text = strings.TrimSpace(text)
	if n := len([]rune(text)); n < 2 || n > 100 {
		return nil, failure.Invalid(failure.FieldError{Field: "search", Message: "invalid"})
	}
	if s.directory == nil {
		return nil, failure.New(failure.KindUnavailable, failure.CodeServiceUnavailable, nil)
	}
	return s.directory.Search(ctx, text, first, limit)
}

var assignableStates = []string{project.StateCreated, project.StateInProgress, project.StatePaused}

// SetRoles is setMemberRoles (US-02 CA-1, CA-4, CA-5, CA-7; RN-2, RN-3). The lead gives approver
// and member; only the organization administrator gives lead, which transfers it (the previous
// lead loses it in the same transaction) and never to themselves.
func (s *MemberService) SetRoles(ctx context.Context, principal identity.Principal, projectID, subject string, roles []identity.Role) (Member, error) {
	if err := validRoles(roles); err != nil {
		return Member{}, err
	}
	access, err := s.Open(ctx, principal, projectID)
	if err != nil {
		return Member{}, err
	}
	current, err := s.members.Roles(ctx, projectID, subject)
	if err != nil {
		return Member{}, err
	}
	lead := identity.Has(access.Roles, identity.RoleLead)
	wantsLead := slices.Contains(roles, identity.RoleLead)
	transfer := wantsLead && !identity.Has(current, identity.RoleLead)
	switch {
	case !lead && !principal.Administrator,
		transfer && !principal.Administrator, // only the administrator gives lead (RN-3)
		!lead && !transfer:                   // the administrator without a role only transfers the lead role
		return Member{}, failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	case transfer && subject == principal.Subject:
		return Member{}, failure.New(failure.KindConflict, CodeSeparationOfDuties, nil).With("reason", "admin_self_lead")
	case !slices.Contains(assignableStates, access.State):
		return Member{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", access.State).With("available_actions", []string{})
	}
	if err := s.ensurePerson(ctx, principal.OrganizationID, subject); err != nil {
		return Member{}, err
	}
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		previous, err := s.members.Roles(ctx, projectID, subject)
		if err != nil {
			return err
		}
		if identity.Has(previous, identity.RoleLead) && !wantsLead {
			return failure.New(failure.KindConflict, CodeLeadRequired, nil)
		}
		if wantsLead && !identity.Has(previous, identity.RoleLead) {
			if err := s.transferLead(ctx, principal, projectID); err != nil {
				return err
			}
		}
		return s.apply(ctx, principal, projectID, subject, previous, roles)
	})
	if err != nil {
		return Member{}, err
	}
	return s.member(ctx, projectID, subject)
}

// transferLead takes the lead role from the current lead (US-02 RN-3).
func (s *MemberService) transferLead(ctx context.Context, principal identity.Principal, projectID string) error {
	current, err := s.members.Lead(ctx, projectID)
	if err != nil {
		return err
	}
	previous, err := s.members.Roles(ctx, projectID, current)
	if err != nil {
		return err
	}
	remaining := slices.DeleteFunc(slices.Clone(previous), func(r identity.Role) bool { return r == identity.RoleLead })
	return s.apply(ctx, principal, projectID, current, previous, remaining)
}

// apply writes the roles, the audit record (who, to whom, roles, when; CA-1, CA-7) and the
// member.role_assigned event that notifies the lead and the person (CA-7, PL-52).
func (s *MemberService) apply(ctx context.Context, principal identity.Principal, projectID, subject string, previous, roles []identity.Role) error {
	if err := s.members.SetRoles(ctx, principal, projectID, subject, roles); err != nil {
		return err
	}
	data, err := json.Marshal(map[string]any{
		"project": projectID, "member": subject, "roles": roleNames(roles), "previous_roles": roleNames(previous),
		"actor": map[string]any{"type": "human", "id": principal.Subject, "origin": "web"},
	})
	if err != nil {
		return err
	}
	envelope, err := s.outbox.Append(ctx, domainevent.Draft{Type: "br.easycode.member.role_assigned.v1", Subject: "members/" + subject, ProjectID: projectID,
		CorrelationID: uuid.NewString(), Data: data})
	if err != nil {
		return err
	}
	action := "member.assign"
	if len(roles) == 0 {
		action = "member.remove"
	}
	_, err = s.trail.Append(ctx, domainaudit.Record{
		ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject}, Origin: domainaudit.OriginWeb,
		Action: action, Target: "members/" + subject, Decision: strings.Join(roleNames(roles), ","), EventID: envelope.ID,
	})
	return err
}

// Remove is removeMember (lead only; the lead and the last approver stay, US-02 CA-4, RN-3).
func (s *MemberService) Remove(ctx context.Context, principal identity.Principal, projectID, subject string) error {
	access, err := Authorize(ctx, s.projects, principal, projectID, identity.RoleLead)
	if err != nil {
		return err
	}
	if !slices.Contains(assignableStates, access.State) {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", access.State).With("available_actions", []string{})
	}
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		previous, err := s.members.Roles(ctx, projectID, subject)
		if err != nil {
			return err
		}
		switch {
		case len(previous) == 0:
			return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
		case identity.Has(previous, identity.RoleLead):
			return failure.New(failure.KindConflict, CodeLeadRequired, nil)
		}
		return s.apply(ctx, principal, projectID, subject, previous, nil)
	})
}

func (s *MemberService) ensurePerson(ctx context.Context, organizationID, subject string) error {
	known, err := s.members.KnownPerson(ctx, organizationID, subject)
	if err != nil || known {
		return err
	}
	if s.directory == nil {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	name, found, err := s.directory.Person(ctx, subject)
	if err != nil {
		return err
	}
	if !found {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return s.members.EnsurePerson(ctx, organizationID, subject, name)
}

func (s *MemberService) member(ctx context.Context, projectID, subject string) (Member, error) {
	members, err := s.members.Members(ctx, projectID)
	if err != nil {
		return Member{}, err
	}
	for _, m := range members {
		if m.User.Subject == subject {
			return m, nil
		}
	}
	return Member{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
}

func validRoles(roles []identity.Role) error {
	if len(roles) == 0 {
		return failure.Invalid(failure.FieldError{Field: "roles", Message: "required"})
	}
	seen := map[identity.Role]bool{}
	for _, r := range roles {
		if r != identity.RoleLead && r != identity.RoleApprover && r != identity.RoleMember {
			return failure.Invalid(failure.FieldError{Field: "roles", Message: "enum"})
		}
		if seen[r] {
			return failure.Invalid(failure.FieldError{Field: "roles", Message: "unique"})
		}
		seen[r] = true
	}
	return nil
}

func roleNames(roles []identity.Role) []string {
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, string(r))
	}
	slices.Sort(names)
	return names
}
