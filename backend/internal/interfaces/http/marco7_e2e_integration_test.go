package http_test

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	appcost "easycode/internal/application/cost"
	appgateway "easycode/internal/application/gateway"
	"easycode/internal/application/runtime"
	apptrail "easycode/internal/application/trail"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/specs"
	httpapi "easycode/internal/interfaces/http"
)

// visibilityDeps is controlDeps with the reads of MARCO-7: costs and estimate (US-38, US-40) and the audit
// trail (US-35); the comparison (US-27) and the dashboard (US-50) are routes of Artifacts and Projects.
func visibilityDeps(t *testing.T) func(*httpapi.Dependencies, *postgrestest.Database) {
	return func(d *httpapi.Dependencies, db *postgrestest.Database) {
		controlDeps(t)(d, db)
		pool := db.PoolAs("easycode_api")
		wf, _ := specs.StandardWorkflow(specs.TeamAgents)
		projects := postgres.NewProjects(pool, wf.Definition.ID, len(wf.Definition.Stages))
		d.Costs = appcost.NewService(projects, postgres.NewCosts(pool, wf.Definition), wf.Definition.StageIDs())
		d.Audit = apptrail.NewQueries(projects, postgres.NewAuditReader(pool))
	}
}

func money(t *testing.T, value any) string {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok || m["currency"] != "BRL" {
		t.Fatalf("Money: %v", value)
	}
	return m["value"].(string)
}

func sum(t *testing.T, values ...string) string {
	t.Helper()
	total := new(big.Rat)
	for _, v := range values {
		r, ok := new(big.Rat).SetString(v)
		if !ok {
			t.Fatalf("decimal %q", v)
		}
		total.Add(total, r)
	}
	return strings.TrimSuffix(strings.TrimRight(total.FloatString(8), "0"), ".")
}

// secondRequirements is the delivery of the wave after a rejected round: every file at version 2, the VISAO
// with a changed objective and one more body line.
func secondRequirements(t *testing.T) []step {
	files := map[string]string{}
	for path, content := range referenceFiles(t) {
		content = strings.NewReplacer("onda: REQ-01\nversao: 1\n", "onda: REQ-02\nversao: 2\n",
			"  descricao: criação\n", "  descricao: criação\n- versao: 2\n  data: '2026-11-23'\n  onda: REQ-02\n  descricao: objetivo detalhado no gate\n").Replace(content)
		if strings.HasSuffix(path, "VISAO.md") {
			content = strings.Replace(content, "objetivo: Reservar salas pelo navegador sem conflito de horário.",
				"objetivo: Reservar salas pelo navegador sem conflito de horário, com cancelamento pelo próprio funcionário.", 1)
			content += "\nO funcionário cancela as próprias reservas.\n"
		}
		files[path] = content
	}
	return []step{{message: provider.Message{Calls: writeAll(files, "v2")}},
		{message: provider.Message{Calls: []provider.ToolCall{call("f2", runtime.ToolFinish, map[string]any{})}}}}
}

// TestMarco7Visibility is the MARCO-7 done criterion over the reference project: the five screens of
// visibility read real data produced by the worker. US-40: the estimate before running (no history, CA-2),
// recorded at dispatch and shown beside what the wave cost (CA-3), with the history of the stage in another
// project (CA-1), and the project paused when the estimate exceeds the balance (stop_by_estimate). US-27: two
// versions of the VISAO compared field by field and line by line. US-38: costs by stage, agent, model and
// provider adding up to the waves, rework apart and cycle × execution time; the home card of the person.
// US-35: the trail in order with who, when, from where, action, target and reason, filtered, paginated and
// read only. US-50: the dashboard with only the projects of the person, filtered by state. And the MARCO-6
// gaps: organization currency, who paused the project, the last budget alert.
func TestMarco7Visibility(t *testing.T) {
	live := newLiveAPI(t, visibilityDeps(t))
	db := live.db
	opener := sealKeys(t, db, map[string]string{"anthropic": anthropicKey})
	script := newStepProvider()
	work := newControlWorker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, script)})
	registerModels(t, live, "anthropic")
	started := time.Now().Add(-time.Minute)

	// US-40 CA-2: the stage is ready and no wave of it ever ran in the organization.
	base := newProject(t, live, work.teamWorker, "Reserva visível", "anthropic", "anthropic")
	projectID := strings.TrimPrefix(base, "/api/v1/projects/")
	first := live.json(t, "GET", base+"/stages/requirements/estimate", "", nil, 200)
	basis := first["basis"].(map[string]any)
	tokens := int64(basis["input_size_tokens"].(float64))
	if first["no_history"] != true || first["performed"] != nil || tokens <= 0 || basis["considered_waves"] != float64(0) ||
		basis["historical_average"] != nil || basis["price_table_version"] == "" || money(t, basis["input_price_per_million"]) != "20" {
		t.Fatalf("US-40 CA-2: %v", first)
	}
	if want := sum(t, new(big.Rat).Mul(big.NewRat(tokens, 1), big.NewRat(20, 1_000_000)).FloatString(8)); money(t, first["estimated"]) != want {
		t.Fatalf("US-40 CA-2: size × price = %s, got %v", want, first["estimated"])
	}
	live.json(t, "GET", base+"/stages/nowhere/estimate", "", nil, 404)

	// The first delivery; the estimate is recorded at dispatch (guard of dispatch) and shown beside the cost.
	script.push(requirementsSteps(t, false)...)
	work.runAll(t)
	if w := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200); w["state"] != "completed" {
		t.Fatalf("REQ-01: %v", w["state"])
	}
	done := live.json(t, "GET", base+"/stages/requirements/estimate", "", nil, 200)
	if done["wave"] != "REQ-01" || money(t, done["estimated"]) != money(t, first["estimated"]) || money(t, done["performed"]) != "0.076" ||
		done["basis"].(map[string]any)["input_size_tokens"] != float64(tokens) {
		t.Fatalf("US-40 CA-3: estimated × performed: %v", done)
	}

	// A rejected round reruns the stage (REQ-02) and the human approves round 2.
	me := live.json(t, "GET", "/api/v1/me", "", nil, 200)
	if me["organization"].(map[string]any)["currency"] != "BRL" {
		t.Fatalf("CurrentUser.organization.currency: %v", me["organization"])
	}
	csrf := map[string]string{"X-CSRF-Token": me["csrf_token"].(string)}
	live.json(t, "POST", base+"/gates/gate.requirements/rounds/1/rejection", `{"reason":"Detalhar o objetivo da visão"}`, mergeHeaders(key(), csrf), 200)
	work.reconcile(t)
	script.push(secondRequirements(t)...)
	work.runAll(t)
	if w := live.json(t, "GET", base+"/waves/REQ-02", "", nil, 200); w["state"] != "completed" {
		t.Fatalf("REQ-02: %v %v", w["state"], w["failure_cause"])
	}
	live.approve(t, base, "gate.requirements", 2)
	work.reconcile(t)

	// US-27 CA-1: header fields and body lines between versions 1 and 2 of the VISAO.
	cmp := live.json(t, "GET", base+"/artifacts/VISAO/comparison?from=1&to=2", "", nil, 200)
	header := map[string]string{}
	for _, item := range cmp["header"].([]any) {
		c := item.(map[string]any)
		header[c["path"].(string)] = c["operation"].(string)
	}
	added, removed := 0, 0
	for _, item := range cmp["body"].([]any) {
		switch item.(map[string]any)["operation"] {
		case "added":
			added++
		case "removed":
			removed++
		}
	}
	if cmp["artifact"] != "VISAO" || cmp["from"] != float64(1) || cmp["to"] != float64(2) || header["/versao"] != "modified" ||
		header["/onda"] != "modified" || header["/objetivo"] != "modified" || header["/historico/1"] != "added" || header["/id"] != "" ||
		added < 1 || removed != 0 || len(cmp["driving_findings"].([]any)) != 0 {
		t.Fatalf("US-27 CA-1: %v (body +%d -%d)", header, added, removed)
	}
	back := live.json(t, "GET", base+"/artifacts/VISAO/comparison?from=2&to=1", "", nil, 200)
	if len(back["header"].([]any)) != len(cmp["header"].([]any)) {
		t.Fatalf("US-27 CA-1: any order of versions: %v", back["header"])
	}
	live.json(t, "GET", base+"/artifacts/VISAO/comparison?from=0&to=2", "", nil, 400)
	live.json(t, "GET", base+"/artifacts/VISAO/comparison?from=1&to=9", "", nil, 404)
	if v := live.json(t, "GET", base+"/artifacts/VISAO/versions", "", nil, 200)["items"].([]any); len(v) != 2 || v[0].(map[string]any)["finding"] != nil {
		t.Fatalf("versions: %v", v)
	}

	// US-38 CA-1: every grouping adds up to the total, which is the sum of the waves; CA-3: both times.
	waves := live.json(t, "GET", base+"/waves", "", nil, 200)["items"].([]any)
	var waveTotals []string
	for _, item := range waves {
		waveTotals = append(waveTotals, money(t, item.(map[string]any)["cost_total"]))
	}
	// ESP-06 CA-06-11: each wave of the list carries its tokens, the same as getWave.
	var listedInput float64
	for _, item := range waves {
		summary := item.(map[string]any)
		detail := live.json(t, "GET", base+"/waves/"+summary["id"].(string), "", nil, 200)
		tokens, ok := summary["tokens"].(map[string]any)
		if !ok || fmt.Sprint(tokens) != fmt.Sprint(detail["tokens"]) {
			t.Fatalf("CA-06-11: list tokens %v, wave tokens %v", summary["tokens"], detail["tokens"])
		}
		listedInput += tokens["input"].(float64)
	}
	if listedInput <= 0 {
		t.Fatalf("CA-06-11: the listed waves carry no input tokens: %v", waves)
	}
	for _, groupBy := range []string{"stage", "agent", "model", "provider"} {
		costs := live.json(t, "GET", base+"/costs?group_by="+groupBy, "", nil, 200)
		total := costs["total"].(map[string]any)
		var groups []string
		for _, g := range costs["groups"].([]any) {
			groups = append(groups, money(t, g.(map[string]any)["cost"].(map[string]any)["total"]))
		}
		if costs["grouped_by"] != groupBy || money(t, total["total"]) != sum(t, waveTotals...) || sum(t, groups...) != money(t, total["total"]) ||
			money(t, total["total"]) != "0.152" || money(t, total["rework"]) != "0" {
			t.Fatalf("US-38 CA-1 %s: groups %v, waves %v: %v", groupBy, groups, waveTotals, costs)
		}
	}
	costs := live.json(t, "GET", base+"/costs", "", nil, 200)
	requirements := costs["groups"].([]any)[0].(map[string]any)
	clock := requirements["time"].(map[string]any)
	budget := costs["budget"].(map[string]any)
	if costs["grouped_by"] != "stage" || requirements["key"] != "requirements" || requirements["waves"] != float64(2) ||
		clock["cycle_ms"].(float64) < clock["execution_ms"].(float64) || costs["rework"].(map[string]any)["waves"] != float64(0) ||
		money(t, budget["stop_limit"]) != "200" || money(t, budget["balance"]) != "199.848" || budget["used_percentage"] != 0.08 ||
		len(costs["total"].(map[string]any)["per_provider"].([]any)) != 1 {
		t.Fatalf("US-38 CA-3, US-39: %v", costs)
	}
	live.json(t, "GET", base+"/costs?group_by=week", "", nil, 400)

	// US-38, US-50: the home card of the month.
	month := live.json(t, "GET", "/api/v1/me/costs", "", nil, 200)
	largest := month["largest_spend"].([]any)
	if money(t, month["total"].(map[string]any)["total"]) != "0.152" || month["rework_percentage"] != float64(0) || len(largest) != 1 ||
		largest[0].(map[string]any)["project"] != projectID || month["from"] != time.Now().UTC().Format("2006-01")+"-01" {
		t.Fatalf("getUserCosts: %v", month)
	}
	live.json(t, "GET", "/api/v1/me/costs?from=2026-05-01&until=2026-04-01", "", nil, 400)
	live.json(t, "GET", "/api/v1/me/costs?from=2024-01-01&until=2026-01-01", "", nil, 400)

	// MARCO-6 gap: who paused the project and when (ESP-06 section 6); paused, its next waves stay queued.
	live.json(t, "POST", base+"/pause", "", key(), 202)
	work.drain(t)
	paused := live.json(t, "GET", base, "", nil, 200)
	byHuman := paused["pause"].(map[string]any)
	if paused["state"] != "paused" || byHuman["reason"] != "manual" || byHuman["origin"] != "web" ||
		byHuman["by"].(map[string]any)["user"].(map[string]any)["name"] != "Dev" || byHuman["at"] == nil || byHuman["cause"] != nil ||
		byHuman["next_wave_estimate"] != nil {
		t.Fatalf("pause record: %v", paused["pause"])
	}

	// US-40 CA-1: another project of the organization has the history of the requirements stage.
	second := newProject(t, live, work.teamWorker, "Reserva com histórico", "anthropic", "anthropic")
	history := live.json(t, "GET", second+"/stages/requirements/estimate", "", nil, 200)
	hb := history["basis"].(map[string]any)
	if history["no_history"] != false || hb["considered_waves"] != float64(2) || money(t, hb["historical_average"]) != "0.076" ||
		hb["method"] != "input_times_price_times_history" || history["performed"] != nil {
		t.Fatalf("US-40 CA-1: %v", history)
	}
	// stop_by_estimate: spend + estimate over the stop limit pauses the project before the wave runs.
	live.json(t, "PATCH", second+"/configuration", `{"budget":{"stop_limit":{"value":"0.05","currency":"BRL"}}}`, map[string]string{"If-Match": `"2"`}, 200)
	calls := script.calls
	work.runAll(t)
	stopped := live.json(t, "GET", second, "", nil, 200)
	pause, _ := stopped["pause"].(map[string]any)
	if stopped["state"] != "paused" || stopped["pause_reason"] != "budget" || pause == nil || pause["reason"] != "budget" ||
		pause["by"].(map[string]any)["type"] != "orchestrator" || pause["cause"] != "estimate_exceeds_balance" || pause["next_wave_estimate"] == nil ||
		script.calls != calls {
		t.Fatalf("US-40, policy.project_budget stop_by_estimate: %v (calls %d → %d)", stopped, calls, script.calls)
	}
	if w := live.json(t, "GET", second+"/waves/REQ-01", "", nil, 200); w["state"] != "queued" {
		t.Fatalf("stop_by_estimate: the wave stays queued: %v", w["state"])
	}
	if eventCount(t, db, "br.easycode.project.budget_exhausted.v1",
		"envelope->'data'->>'cause' = 'estimate_exceeds_balance' AND envelope->'data'->'next_wave_estimate'->>'value' <> ''") != 1 {
		t.Fatal("budget_exhausted with estimate_exceeds_balance and next_wave_estimate")
	}

	// US-35 CA-1: the trail of the project in order with who, when, from where, action, target and reason.
	trail := live.json(t, "GET", base+"/audit?order=desc&limit=100", "", nil, 200)
	items := trail["items"].([]any)
	actions := map[string]map[string]any{}
	var previous string
	for _, item := range items {
		r := item.(map[string]any)
		if previous != "" && r["when"].(string) > previous {
			t.Fatalf("US-35 CA-1: order desc: %s after %s", r["when"], previous)
		}
		previous = r["when"].(string)
		if _, seen := actions[r["action"].(string)]; !seen {
			actions[r["action"].(string)] = r
		}
	}
	reject := actions["gate.reject"]
	if reject == nil || reject["reason"] != "Detalhar o objetivo da visão" || reject["origin"] != "web" ||
		reject["actor"].(map[string]any)["user"].(map[string]any)["name"] != "Dev" || reject["target"].(map[string]any)["type"] != "gate_round" ||
		reject["project"] != projectID || actions["gate.approve"] == nil || actions["project.create"] == nil || actions["project.pause"] == nil {
		t.Fatalf("US-35 CA-1: %v", actions)
	}
	if target := actions["project.create"]["target"].(map[string]any); target["type"] != "project" || target["id"] != projectID {
		t.Fatalf("US-35 CA-1: target of project.create: %v", target)
	}
	// CA-2: by action type and period.
	only := live.json(t, "GET", base+"/audit?action=gate.reject&action=gate.approve&from="+started.UTC().Format(time.RFC3339), "", nil, 200)["items"].([]any)
	if len(only) != 2 {
		t.Fatalf("US-35 CA-2: %v", only)
	}
	if none := live.json(t, "GET", base+"/audit?from="+time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "", nil, 200)["items"].([]any); len(none) != 0 {
		t.Fatalf("US-35 CA-2: future period: %v", none)
	}
	page := live.json(t, "GET", base+"/audit?limit=1", "", nil, 200)
	next := live.json(t, "GET", base+"/audit?limit=1&cursor="+page["next_cursor"].(string), "", nil, 200)
	if page["items"].([]any)[0].(map[string]any)["id"] == next["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("US-35: the next page repeats the record")
	}
	live.json(t, "GET", base+"/audit?order=sideways", "", nil, 400)
	// CA-3: no route changes or deletes a record of the trail.
	for _, route := range httpapi.RouteTemplates(live.deps) {
		if method, path, _ := strings.Cut(route, " "); strings.HasSuffix(path, "/audit") && method != "GET" {
			t.Fatalf("US-35 CA-3: %s", route)
		}
	}
	// A member only gets "forbidden"; the organization trail is the administrator's.
	rita := live.as(t, oidctest.User{Subject: "rita", Name: "Rita Melo"})
	live.json(t, "PUT", base+"/members/rita", `{"roles":["member"]}`, nil, 200)
	rita.json(t, "GET", base+"/audit", "", nil, 403)
	rita.json(t, "GET", "/api/v1/audit", "", nil, 403)
	organization := live.json(t, "GET", "/api/v1/audit?action=price.record&action=model.record", "", nil, 200)["items"].([]any)
	if len(organization) != 4 || organization[0].(map[string]any)["project"] != nil {
		t.Fatalf("listOrganizationAudit: %v", organization)
	}

	// US-50 CA-1..CA-3: the dashboard shows only the projects of the person, by state.
	bruno := live.as(t, oidctest.User{Subject: "bruno", Name: "Bruno Lima"})
	createProject(t, bruno, "Projeto do Bruno")
	board := live.json(t, "GET", "/api/v1/projects", "", nil, 200)
	counts := board["count_by_state"].(map[string]any)
	if len(board["items"].([]any)) != 2 || counts["paused"] != float64(2) || counts["created"] != float64(0) {
		t.Fatalf("US-50 CA-1, CA-3: %v", board)
	}
	if filtered := live.json(t, "GET", "/api/v1/projects?state=in_progress", "", nil, 200)["items"].([]any); len(filtered) != 0 {
		t.Fatalf("US-50 CA-2: %v", filtered)
	}
	card := live.json(t, "GET", "/api/v1/projects?state=paused", "", nil, 200)["items"].([]any)
	var visible map[string]any
	for _, item := range card {
		if p := item.(map[string]any); p["id"] == projectID {
			visible = p
		}
	}
	summary := visible["summary"].(map[string]any)
	findings := summary["open_findings"].(map[string]any)
	if visible == nil || summary["approved_stages"] != float64(1) || money(t, summary["accumulated_cost"]) != "0.152" ||
		findings["escalated"] != float64(0) || summary["last_budget_alert_percentage"] != nil || visible["pause"] == nil {
		t.Fatalf("US-50 CA-1: %v", visible)
	}
	if strings.Contains(fmt.Sprint(board), "Projeto do Bruno") {
		t.Fatal("US-50 CA-3: a project without a role shows")
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.waves WHERE cost_estimate > 0 AND estimate_basis ? 'input_size_tokens'"); n != 2 {
		t.Fatalf("US-40: estimate recorded at dispatch (%d)", n)
	}
}
