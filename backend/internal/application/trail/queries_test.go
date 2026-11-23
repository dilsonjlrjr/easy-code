package trail

import (
	"context"
	"testing"
	"time"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

type projects struct {
	appproject.Repository
	roles []identity.Role
}

func (p projects) Access(_ context.Context, _ identity.Principal, projectID string) (appproject.Access, error) {
	if projectID == "other" {
		return appproject.Access{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return appproject.Access{ProjectID: projectID, Roles: p.roles}, nil
}

type reader struct{ got Filter }

func (r *reader) ProjectTrail(_ context.Context, _ string, f Filter) (Page, error) {
	r.got = f
	return Page{}, nil
}

func (r *reader) OrganizationTrail(_ context.Context, _ string, f Filter) (Page, error) {
	r.got = f
	return Page{}, nil
}

// US-35 CA-2 and ESP-27: filters by action and period, order asc by default; approvers and the lead only,
// the organization trail for the administrator only.
func TestTrailFiltersAndRoles(t *testing.T) {
	r := &reader{}
	ctx := context.Background()
	q := NewQueries(projects{roles: []identity.Role{identity.RoleApprover}}, r)
	from, until := time.Now(), time.Now().Add(time.Hour)
	if _, err := q.ProjectTrail(ctx, identity.Principal{}, "p", Filter{Actions: []string{"gate.approve"}, From: &from, Until: &until, Limit: 10}); err != nil ||
		r.got.Order != OrderAsc {
		t.Fatalf("filter: %v %+v", err, r.got)
	}
	for _, f := range []Filter{{Order: "sideways"}, {Actions: []string{"Gate Approve"}}, {From: &until, Until: &from}} {
		if _, err := q.ProjectTrail(ctx, identity.Principal{}, "p", f); failure.From(err).Code != failure.CodeValidation {
			t.Errorf("%+v: %v", f, err)
		}
	}
	member := NewQueries(projects{roles: []identity.Role{identity.RoleMember}}, r)
	if _, err := member.ProjectTrail(ctx, identity.Principal{}, "p", Filter{}); failure.From(err).Kind != failure.KindForbidden {
		t.Fatalf("member: %v", err)
	}
	if _, err := q.ProjectTrail(ctx, identity.Principal{}, "other", Filter{}); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("RNF-04: %v", err)
	}
	if _, err := q.OrganizationTrail(ctx, identity.Principal{}, Filter{}); failure.From(err).Kind != failure.KindForbidden {
		t.Fatalf("organization trail of a non administrator: %v", err)
	}
	if _, err := q.OrganizationTrail(ctx, identity.Principal{Administrator: true}, Filter{Order: OrderDesc}); err != nil || r.got.Order != OrderDesc {
		t.Fatalf("organization trail: %v", err)
	}
}
