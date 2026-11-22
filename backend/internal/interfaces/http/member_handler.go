package http

import (
	"context"
	"regexp"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// MemberUseCases are the role use cases (US-02).
type MemberUseCases interface {
	List(ctx context.Context, principal identity.Principal, projectID string) ([]appproject.Member, error)
	SetRoles(ctx context.Context, principal identity.Principal, projectID, subject string, roles []identity.Role) (appproject.Member, error)
	Remove(ctx context.Context, principal identity.Principal, projectID, subject string) error
	Search(ctx context.Context, principal identity.Principal, lead bool, text string, first, limit int) ([]appproject.UserRef, error)
	Open(ctx context.Context, principal identity.Principal, projectID string) (appproject.Access, error)
}

type memberJSON struct {
	User       userRefJSON     `json:"user"`
	Roles      []identity.Role `json:"roles"`
	AssignedBy userRefJSON     `json:"assigned_by"`
	AssignedAt time.Time       `json:"assigned_at"`
}

func memberOf(m appproject.Member) memberJSON {
	roles := m.Roles
	if roles == nil {
		roles = []identity.Role{}
	}
	return memberJSON{
		User: userRefJSON{ID: m.User.Subject, Name: m.User.Name}, Roles: roles,
		AssignedBy: userRefJSON{ID: m.AssignedBy.Subject, Name: m.AssignedBy.Name}, AssignedAt: m.AssignedAt,
	}
}

// userIDPattern is the opaque id of a person (UserId of the contract, sub of the SSO).
var userIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func pathUserID(rc *fasthttp.RequestCtx) (string, error) {
	id := PathParam(rc, "userId")
	if !userIDPattern.MatchString(id) {
		return "", failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return id, nil
}

// listMembers is listMembers (US-02).
func (a *api) listMembers(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	members, err := a.deps.Members.List(RequestContext(rc), principal, projectID)
	if err != nil {
		return err
	}
	out := struct {
		Items []memberJSON `json:"items"`
	}{Items: []memberJSON{}}
	for _, m := range members {
		out.Items = append(out.Items, memberOf(m))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

// setMemberRoles is setMemberRoles (US-02 CA-1, CA-4, CA-5, CA-7).
func (a *api) setMemberRoles(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	subject, err := pathUserID(rc)
	if err != nil {
		return err
	}
	var body struct {
		Roles []identity.Role `json:"roles"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	member, err := a.deps.Members.SetRoles(RequestContext(rc), principal, projectID, subject, body.Roles)
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, memberOf(member))
	return nil
}

// removeMember is removeMember (US-02 CA-4).
func (a *api) removeMember(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	subject, err := pathUserID(rc)
	if err != nil {
		return err
	}
	if err := a.deps.Members.Remove(RequestContext(rc), principal, projectID, subject); err != nil {
		return err
	}
	a.responder.NoContent(rc)
	return nil
}

// searchUsers is searchUsers: the SSO directory by name or e-mail; only id and name come back.
func (a *api) searchUsers(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	limit, err := queryLimit(rc)
	if err != nil {
		return err
	}
	first := 0
	if cursor := string(rc.QueryArgs().Peek("cursor")); cursor != "" {
		first, err = strconv.Atoi(cursor)
		if err != nil || first < 0 {
			return failure.Invalid(failure.FieldError{Field: "cursor", Message: "invalid"})
		}
	}
	current, err := a.deps.Sessions.Current(RequestContext(rc), principal, currentSession(rc))
	if err != nil {
		return err
	}
	found, err := a.deps.Members.Search(RequestContext(rc), principal, current.LeadInAnyProject, string(rc.QueryArgs().Peek("search")), first, limit)
	if err != nil {
		return err
	}
	out := struct {
		Items      []userRefJSON `json:"items"`
		NextCursor *string       `json:"next_cursor"`
	}{Items: []userRefJSON{}}
	for _, u := range found {
		out.Items = append(out.Items, userRefJSON{ID: u.Subject, Name: u.Name})
	}
	if len(found) == limit {
		next := strconv.Itoa(first + limit)
		out.NextCursor = &next
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}
