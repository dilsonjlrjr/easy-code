package http_test

import (
	"strings"
	"testing"

	appevent "easycode/internal/application/event"
	"easycode/internal/infrastructure/oidc/oidctest"
)

var (
	ana   = oidctest.User{Subject: "ana", Name: "Ana Souza"}
	bruno = oidctest.User{Subject: "bruno", Name: "Bruno Lima"}
)

func createProject(t *testing.T, l liveAPI, name string) string {
	t.Helper()
	body := strings.Replace(newProjectBody, "Reserva de salas", name, 1)
	return l.json(t, "POST", "/api/v1/projects", body, key(), 201)["id"].(string)
}

// US-02 CA-1, CA-3, CA-4, CA-7: the lead gives a role to a person of the directory, who then sees
// the project; the assignment is audited and notified; the last approver and the lead stay.
func TestLeadAssignsRolesWithAuditAndRules(t *testing.T) {
	live := newLiveAPI(t)
	live.idp.AddUser(ana) // in the SSO directory, never signed in yet
	projectID := createProject(t, live, "Projeto A")
	base := "/api/v1/projects/" + projectID

	before := live.as(t, bruno)
	for _, path := range []string{base, base + "/members", base + "/workflow"} {
		if rc := before.send("GET", path, "", nil); rc.Response.StatusCode() != 404 {
			t.Fatalf("CA-3: %s without a role must be not found, got %d", path, rc.Response.StatusCode())
		}
	}
	member := live.json(t, "PUT", base+"/members/ana", `{"roles":["approver"]}`, nil, 200)
	if member["user"].(map[string]any)["name"] != "Ana Souza" || member["assigned_by"].(map[string]any)["id"] != "dev" {
		t.Fatalf("CA-1: %v", member)
	}
	if got := live.as(t, ana).json(t, "GET", base, "", nil, 200)["my_roles"].([]any); len(got) != 1 || got[0] != "approver" {
		t.Fatalf("CA-1: Ana sees the project as approver: %v", got)
	}
	if n := count(t, live.db, `SELECT count(*) FROM easycode.audit_records WHERE action = 'member.assign' AND actor_id = 'dev'
   AND target = 'members/ana' AND decision = 'approver' AND origin = 'web'`); n != 1 {
		t.Fatal("CA-1: the assignment is in the audit trail with who and when")
	}
	if n := count(t, live.db, "SELECT count(*) FROM easycode.events WHERE type = 'br.easycode.member.role_assigned.v1' AND subject = 'members/ana'"); n != 1 {
		t.Fatal("CA-7: member.role_assigned notifies the lead and the person")
	}
	members := live.json(t, "GET", base+"/members", "", nil, 200)["items"].([]any)
	if len(members) != 2 {
		t.Fatalf("members: %v", members)
	}

	// CA-4, RN-2, RN-3: with Ana as the only other approver, dev may drop approver; then Ana cannot leave.
	live.json(t, "PUT", base+"/members/dev", `{"roles":["lead"]}`, nil, 200)
	for name, rc := range map[string]struct {
		method, path, body, problem string
		status                      int
	}{
		"last approver by roles":  {"PUT", base + "/members/ana", `{"roles":["member"]}`, "last-approver", 409},
		"last approver by remove": {"DELETE", base + "/members/ana", "", "last-approver", 409},
		"lead removed":            {"DELETE", base + "/members/dev", "", "lead-required", 409},
		"lead drops lead":         {"PUT", base + "/members/dev", `{"roles":["approver"]}`, "lead-required", 409},
		"unknown person":          {"PUT", base + "/members/nobody", `{"roles":["member"]}`, "not-found", 404},
		"no roles":                {"PUT", base + "/members/ana", `{"roles":[]}`, "validation", 400},
	} {
		got := live.send(rc.method, rc.path, rc.body, nil)
		if got.Response.StatusCode() != rc.status || decode(t, got)["type"] != "urn:easycode:problem:"+rc.problem {
			t.Errorf("%s: %d %s", name, got.Response.StatusCode(), got.Response.Body())
		}
	}
	if n := count(t, live.db, "SELECT count(*) FROM easycode.project_members WHERE role = 'approver'"); n != 1 {
		t.Fatal("the refused changes must not change the roles")
	}
	// a member does not manage roles
	live.json(t, "PUT", base+"/members/bruno", `{"roles":["member"]}`, nil, 200)
	if rc := before.send("PUT", base+"/members/bruno", `{"roles":["approver"]}`, nil); rc.Response.StatusCode() != 403 {
		t.Fatalf("a member assigning roles: %d", rc.Response.StatusCode())
	}
	if rc := live.send("DELETE", base+"/members/bruno", "", nil); rc.Response.StatusCode() != 204 {
		t.Fatalf("remove: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if n := count(t, live.db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'member.remove' AND target = 'members/bruno'"); n != 1 {
		t.Fatal("the removal is audited")
	}
}

// US-02 CA-5, CA-6, RN-3: the administrator without a role sees only the metadata (recorded once
// per session), transfers the lead (the previous lead loses it in the same operation) and never
// to themselves.
func TestAdministratorTransfersTheLead(t *testing.T) {
	live := newLiveAPI(t, liveRuntimeDeps(appevent.NewBroker(), make(chan struct{})))
	anaAPI := live.as(t, ana)
	brunoAPI := live.as(t, bruno)
	projectID := createProject(t, anaAPI, "Projeto da Ana")
	base := "/api/v1/projects/" + projectID
	anaAPI.json(t, "PUT", base+"/members/bruno", `{"roles":["approver"]}`, nil, 200)
	if rc := anaAPI.send("PUT", base+"/members/bruno", `{"roles":["lead","approver"]}`, nil); rc.Response.StatusCode() != 403 {
		t.Fatalf("RN-3: only the administrator gives lead: %d", rc.Response.StatusCode())
	}

	for range 2 {
		view := live.json(t, "GET", base, "", nil, 200)
		if view["lead"].(map[string]any)["id"] != "ana" || view["business_need"] != nil || view["summary"] != nil || len(view["my_roles"].([]any)) != 0 {
			t.Fatalf("CA-6: metadata only: %v", view)
		}
	}
	if n := count(t, live.db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'project.access_as_administrator' AND actor_id = 'dev'"); n != 1 {
		t.Fatalf("CA-6: the administrator access is recorded once per session, got %d", n)
	}
	if stages := live.json(t, "GET", base+"/workflow", "", nil, 200)["stages"].([]any); len(stages) != 3 {
		t.Fatalf("CA-6: the administrator sees the workflow states: %v", stages)
	}
	for _, path := range []string{base + "/artifacts", base + "/waves", base + "/configuration"} {
		if rc := live.send("GET", path, "", nil); rc.Response.StatusCode() != 404 {
			t.Fatalf("the administrator sees no content of the project (%s): %d", path, rc.Response.StatusCode())
		}
	}
	organization := live.json(t, "GET", "/api/v1/projects?scope=organization", "", nil, 200)["items"].([]any)
	if len(organization) != 1 || organization[0].(map[string]any)["summary"] != nil {
		t.Fatalf("scope=organization: %v", organization)
	}
	if rc := anaAPI.send("GET", "/api/v1/projects?scope=organization", "", nil); rc.Response.StatusCode() != 403 {
		t.Fatalf("scope=organization is only for the administrator: %d", rc.Response.StatusCode())
	}
	if len(live.json(t, "GET", base+"/members", "", nil, 200)["items"].([]any)) != 2 {
		t.Fatal("CA-6: the administrator lists the roles")
	}
	if rc := live.send("PUT", base+"/members/dev", `{"roles":["lead"]}`, nil); rc.Response.StatusCode() != 409 ||
		decode(t, rc)["type"] != "urn:easycode:problem:separation-of-duties" || decode(t, rc)["reason"] != "admin_self_lead" {
		t.Fatalf("the administrator never gives lead to themselves: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if rc := live.send("PUT", base+"/members/bruno", `{"roles":["member"]}`, nil); rc.Response.StatusCode() != 403 {
		t.Fatalf("the administrator only transfers the lead: %d", rc.Response.StatusCode())
	}
	live.json(t, "PUT", base+"/members/bruno", `{"roles":["lead","approver"]}`, nil, 200)
	view := brunoAPI.json(t, "GET", base, "", nil, 200)
	if view["lead"].(map[string]any)["id"] != "bruno" {
		t.Fatalf("CA-5: Bruno is the only lead: %v", view["lead"])
	}
	if roles := anaAPI.json(t, "GET", base, "", nil, 200)["my_roles"].([]any); len(roles) != 1 || roles[0] != "approver" {
		t.Fatalf("CA-5: Ana is no longer lead: %v", roles)
	}
	if n := count(t, live.db, `SELECT count(*) FROM easycode.audit_records WHERE action = 'member.assign' AND actor_id = 'dev'
   AND target IN ('members/bruno', 'members/ana')`); n != 2 {
		t.Fatalf("CA-5: the transfer is audited for both people, got %d", n)
	}
	if rc := anaAPI.send("PUT", base+"/members/bruno", `{"roles":["member"]}`, nil); rc.Response.StatusCode() != 403 {
		t.Fatalf("the previous lead no longer manages roles: %d", rc.Response.StatusCode())
	}
}

// searchUsers: only a lead of some project or the administrator; only id and name come back.
func TestSearchUsersInTheSSODirectory(t *testing.T) {
	live := newLiveAPI(t)
	live.idp.AddUser(ana)
	found := live.json(t, "GET", "/api/v1/users?search=souza", "", nil, 200)["items"].([]any)
	if len(found) != 1 || found[0].(map[string]any)["id"] != "ana" || found[0].(map[string]any)["name"] != "Ana Souza" || len(found[0].(map[string]any)) != 2 {
		t.Fatalf("search: %v", found)
	}
	if rc := live.as(t, bruno).send("GET", "/api/v1/users?search=souza", "", nil); rc.Response.StatusCode() != 403 {
		t.Fatalf("a person who leads nothing cannot search: %d", rc.Response.StatusCode())
	}
	if rc := live.send("GET", "/api/v1/users?search=a", "", nil); rc.Response.StatusCode() != 400 {
		t.Fatalf("short search: %d", rc.Response.StatusCode())
	}
}
