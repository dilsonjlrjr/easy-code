package http_test

import (
	"strings"
	"testing"

	appevent "easycode/internal/application/event"
	"easycode/internal/infrastructure/oidc/oidctest"
	httpapi "easycode/internal/interfaces/http"
)

// pathValues fill the other path parameters of a route with plausible values.
var pathValues = map[string]string{
	"{waveId}": "REQ-01", "{questionId}": "PERG-1", "{gateId}": "gate.requirements", "{round}": "1",
	"{artifactId}": "VISAO", "{version}": "1", "{commandId}": "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b99",
	"{userId}": "ana", "{stage}": "requirements",
}

// RNF-04 (and US-02 CA-3): a person without a role in project B gets, on every route of the
// project, exactly the answer of a project that does not exist; the lists never show B.
func TestEveryProjectRouteIsIsolatedBetweenProjects(t *testing.T) {
	live := newLiveAPI(t, liveRuntimeDeps(appevent.NewBroker(), make(chan struct{})))
	anaAPI := live.as(t, oidctest.User{Subject: "ana", Name: "Ana Souza"})
	brunoAPI := live.as(t, oidctest.User{Subject: "bruno", Name: "Bruno Lima"})
	projectA := createProject(t, anaAPI, "Projeto A")
	projectB := createProject(t, brunoAPI, "Projeto B")
	const missing = "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b00"

	checked := 0
	for _, route := range httpapi.RouteTemplates(live.deps) {
		method, template, _ := strings.Cut(route, " ")
		if !strings.Contains(template, "{projectId}") {
			continue
		}
		path := template
		for param, value := range pathValues {
			path = strings.ReplaceAll(path, param, value)
		}
		headers := map[string]string{"Idempotency-Key": "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b77", "If-Match": `"1"`, "X-CSRF-Token": "x"}
		body := ""
		if method != "GET" && method != "DELETE" {
			body = "{}"
		}
		other := anaAPI.send(method, strings.ReplaceAll(path, "{projectId}", projectB), body, headers)
		absent := anaAPI.send(method, strings.ReplaceAll(path, "{projectId}", missing), body, headers)
		if other.Response.StatusCode() < 400 || other.Response.StatusCode() != absent.Response.StatusCode() ||
			decode(t, other)["type"] != decode(t, absent)["type"] {
			t.Errorf("%s: project B answers %d %s, a missing project %d %s", route, other.Response.StatusCode(), other.Response.Body(),
				absent.Response.StatusCode(), absent.Response.Body())
		}
		if rc := anaAPI.do(method, strings.ReplaceAll(path, "{projectId}", projectA), nil); rc.Response.StatusCode() != 401 {
			t.Errorf("%s without a session: %d (RNF-06)", route, rc.Response.StatusCode())
		}
		checked++
	}
	if checked < 25 {
		t.Fatalf("only %d project routes walked", checked)
	}
	for _, list := range []string{"/api/v1/projects", "/api/v1/pending-items"} {
		if strings.Contains(string(anaAPI.send("GET", list, "", nil).Response.Body()), projectB) {
			t.Errorf("%s shows project B", list)
		}
	}
	if n := anaAPI.json(t, "GET", "/api/v1/projects", "", nil, 200)["items"].([]any); len(n) != 1 {
		t.Fatalf("ana sees only project A: %v", n)
	}
}
