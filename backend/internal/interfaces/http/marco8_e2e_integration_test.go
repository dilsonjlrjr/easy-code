package http_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appartifact "easycode/internal/application/artifact"
	appevent "easycode/internal/application/event"
	appgateway "easycode/internal/application/gateway"
	"easycode/internal/application/runtime"
	appskill "easycode/internal/application/skill"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/sealing"
	"easycode/internal/infrastructure/specs"
	httpapi "easycode/internal/interfaces/http"
)

// marco8Deps adds to the api of MARCO-1 the agents of the project (US-25) and the human edits (US-53).
func marco8Deps(d *httpapi.Dependencies, db *postgrestest.Database) {
	pool := db.PoolAs("easycode_api")
	wf, _ := specs.BasicWorkflow()
	tx, trail := postgres.NewTxRunner(pool), postgres.NewAuditTrail(pool)
	projects := postgres.NewProjects(pool, wf.Definition.ID, 3)
	d.Agents = appskill.NewAgents(tx, projects, specs.AgentProfiles{}, postgres.NewSkills(pool), postgres.NewSkills(pool), trail)
	d.ArtifactEdits = appartifact.NewEditor(tx, projects, postgres.NewArtifactEdits(pool), trail)
}

// editedUS is the US-01 of the reference project as the review wave writes it: the person's change, validated
// and formatted, with the human author and the agent as reviewer (US-53 CA-1, cabecalho-comum).
func editedUS(t *testing.T) string {
	t.Helper()
	original := referenceFiles(t)["docs/requisitos/US-01-reservar-sala.md"]
	edited := strings.Replace(original, "autor: requirements-agent\nonda: REQ-01\nversao: 1\n",
		"autor: humano\neditado_por: dev\nrevisado_por: requirements-agent\nonda: REQ-02\nversao: 2\n", 1)
	edited = strings.Replace(edited, "  descricao: criação\n", "  descricao: criação\n- versao: 2\n  data: '2026-11-23'\n  onda: REQ-02\n"+
		"  descricao: edição humana validada (sala acessível)\n", 1)
	if edited == original {
		t.Fatal("reference US-01 changed: update the edit of the e2e")
	}
	return edited
}

// TestMarco8EndToEnd: the reference project with the lead's instruction adjustment (US-25), the desktop app
// following it read-only (US-45, US-46), a human edit validated by the requirements agent and a refused one
// (US-53), the approval mode refused on a human gate (US-07) and the deletion of the project (US-04).
func TestMarco8EndToEnd(t *testing.T) {
	broker := appevent.NewBroker()
	shutdown := make(chan struct{})
	live := newLiveAPI(t, liveRuntimeDeps(broker, shutdown), marco8Deps)
	db := live.db
	ctx := context.Background()

	dir := t.TempDir()
	if _, err := sealing.GenerateKeyPair(filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")); err != nil {
		t.Fatal(err)
	}
	sealer, _ := sealing.LoadSealer(filepath.Join(dir, "public.pem"))
	opener, _ := sealing.LoadOpener(filepath.Join(dir, "private.pem"))
	sealed, ref, _ := sealer.Seal([]byte("sk-ant-e2e-0000000000"))
	if _, err := db.Admin.Exec(ctx, `INSERT INTO easycode.provider_keys (organization_id, provider, encrypted_value, encryption_key_ref, last4, created_by)
SELECT organization_id, 'anthropic', $1, $2, '0000', id FROM easycode.users WHERE sub = 'dev'`, sealed, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Admin.Exec(ctx, "UPDATE easycode.provider_keys SET state = 'active', tested_at = now(), test_result = 'valid'"); err != nil {
		t.Fatal(err)
	}
	files := referenceFiles(t)
	deliver := provider.Message{}
	i := 0
	for path, content := range files {
		deliver.Calls = append(deliver.Calls, call("w"+itoa(i), runtime.ToolWriteFile, map[string]string{"path": path, "content": content}))
		i++
	}
	deliver.Calls = append(deliver.Calls, call("f1", runtime.ToolFinish, map[string]any{}))
	review := provider.Message{Calls: []provider.ToolCall{
		call("e1", runtime.ToolWriteFile, map[string]string{"path": "docs/requisitos/US-01-reservar-sala.md", "content": editedUS(t)}),
		call("e2", runtime.ToolFinish, map[string]any{}),
	}}
	refuse := provider.Message{Calls: []provider.ToolCall{call("r1", runtime.ToolFinish, map[string]any{})}}
	scripted := &scriptedProvider{script: []provider.Message{deliver, review, refuse}, blockAt: -1, blocked: make(chan struct{})}
	work := newWorker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, scripted)})
	pool := db.PoolAs("easycode_worker")
	wf, _ := specs.BasicWorkflow()
	workflows := postgres.NewWorkflow(pool, wf.Definition)
	agents := func(agent string) (appworkflow.AgentConfiguration, error) {
		d, err := specs.DefaultAgent(agent)
		return appworkflow.AgentConfiguration{Agent: agent, InstructionVersion: d.InstructionVersion, InstructionHash: d.InstructionHash, Tools: d.Tools, Limits: d.Limits}, err
	}
	orchestrator := appworkflow.NewOrchestrator(workflows, workflows, postgres.NewOutbox(pool), agents, wf.Definition, wf.JSON, wf.Hash)
	work.dispatcher.Register("edit_artifact", appartifact.NewEditApplier(postgres.NewArtifactEdits(pool), orchestrator,
		appartifact.OutboxEmitter(postgres.NewOutbox(pool)), func(string) string { return "requirements-agent" }).Handle)
	run := func(want string) {
		t.Helper()
		for range 5 {
			if ran, err := work.executor("worker-8").RunOnce(ctx); err != nil {
				t.Fatal(err)
			} else if !ran {
				break
			}
		}
		if n := count(t, db, "SELECT count(*) FROM easycode.waves WHERE code = '"+want+"' AND state = 'completed'"); n != 1 {
			var state, detail string
			if err := db.Admin.QueryRow(ctx, `SELECT w.state, coalesce((SELECT string_agg(e.type || ' ' || (e.envelope->'data')::text, ' | ' ORDER BY e.id)
  FROM easycode.events e WHERE e.waveid = w.code), '') FROM easycode.waves w WHERE w.code = $1`, want).Scan(&state, &detail); err != nil {
				t.Log(err)
			}
			t.Fatalf("wave %s did not complete: %s %s", want, state, detail)
		}
	}

	for _, model := range []string{"reasoning-model", "execution-model"} {
		live.json(t, "PUT", "/api/v1/providers/anthropic/models/"+model,
			`{"display_name":"`+model+`","capabilities":{"tool_use":true,"context_tokens":200000,"prompt_cache":true,"image_input":false},"active":true}`, nil, 200)
		live.json(t, "POST", "/api/v1/providers/anthropic/models/"+model+"/prices", `{"effective_since":"`+time.Now().UTC().Format(time.RFC3339)+
			`","currency":"BRL","per_million_tokens":{"input":"20","output":"100","read_cache":"2","write_cache":"25"},"source":"e2e"}`, key(), 201)
	}
	need, _ := specs.ReferenceNeed()
	body, _ := json.Marshal(map[string]any{"name": "Reserva MARCO-8", "business_need": need, "provider_submission_acknowledgment": true,
		"budget": map[string]any{"stop_limit": map[string]string{"value": "200.00", "currency": "BRL"}}})
	projectID := live.json(t, "POST", "/api/v1/projects", string(body), key(), 201)["id"].(string)
	base := "/api/v1/projects/" + projectID
	live.json(t, "PATCH", base+"/configuration", `{"model_pair":{"reasoning":{"provider":"anthropic","model":"reasoning-model"},"execution":{"provider":"anthropic","model":"execution-model"}}}`,
		map[string]string{"If-Match": `"1"`}, 200)

	// US-07 CA-3: a human gate refuses the automatic mode.
	if rc := live.send("PATCH", base+"/configuration", `{"gate_approval":{"gate.requirements":"automatic"}}`, map[string]string{"If-Match": `"2"`}); rc.Response.StatusCode() != 409 {
		t.Fatalf("US-07 CA-3: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}

	// US-25: the lead adjusts the instructions; the configuration shows the exit criteria; a tool beyond the role is refused.
	agent := live.json(t, "GET", base+"/agents/requirements-agent", "", nil, 200)
	configuration := agent["configuration"].(map[string]any)
	if criteria := configuration["output_criteria"].([]any); len(criteria) == 0 || configuration["instructions_version"] == "" {
		t.Fatalf("US-25 CA-1: %v", configuration)
	}
	tools, _ := json.Marshal(agent["allowed_by_role"])
	wider := strings.Replace(string(tools), `"write_file":[`, `"write_file":["app/**",`, 1)
	if refused := live.json(t, "PUT", base+"/agents/requirements-agent/configuration", `{"tools":`+wider+`,"skills":[]}`, map[string]string{"If-Match": `"1"`}, 409); refused["tool"] != "write_file" {
		t.Fatalf("US-25 CA-3: %v", refused)
	}
	adjusted := live.json(t, "PUT", base+"/agents/requirements-agent/configuration",
		`{"adjustment_instructions":"Use o vocabulário de salas da empresa: sala, auditório, cabine.","tools":`+string(tools)+`,"skills":[]}`,
		map[string]string{"If-Match": `"1"`}, 200)
	if adjusted["version"] != float64(2) || !strings.Contains(adjusted["adjustment_instructions"].(string), "auditório") {
		t.Fatalf("US-25 CA-2: %v", adjusted)
	}

	live.json(t, "POST", base+"/start", "", key(), 202)
	work.drain(t)
	run("REQ-01")
	if system := scripted.requests[0].System; !strings.Contains(system, "Project adjustment") || !strings.Contains(system, "auditório") {
		t.Fatal("US-25 CA-2: the wave uses the adjusted instructions")
	}
	waves := live.json(t, "GET", base+"/waves", "", nil, 200)["items"].([]any)
	if waves[0].(map[string]any)["agent_configuration_version"] != float64(2) {
		t.Fatalf("US-25 CA-4: %v", waves[0])
	}
	if state := live.json(t, "GET", base+"/gates/gate.requirements/rounds/1", "", nil, 200)["state"]; state != "awaiting_human" {
		t.Fatalf("round 1: %v", state)
	}

	// US-45, US-46: the desktop app reads the project and is refused any decision.
	token := live.idp.DesktopToken(devUser, "sid-dev-desktop", 5*time.Minute, nil)
	if items := decode(t, live.bearer("GET", "/api/v1/projects", token, ""))["items"].([]any); len(items) != 1 {
		t.Fatalf("US-46 CA-1: %v", items)
	}
	if rc := live.bearer("POST", base+"/gates/gate.requirements/rounds/1/approval", token, "{}"); rc.Response.StatusCode() != 403 ||
		decode(t, rc)["type"] != "urn:easycode:problem:requires-web-session" {
		t.Fatalf("US-45 CA-6: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if strings.Contains(string(live.bearer("GET", base+"/waves/REQ-01/calls", token, "").Response.Body()), "sk-ant-") {
		t.Fatal("US-45 CA-3: no AI key reaches the app")
	}

	// US-53 CA-1, CA-3: the person edits US-01 in review; the round is invalidated, the requirements agent
	// reviews the edit and the version it writes has the human author and the agent as reviewer.
	edit := `{"operation":"modify","artifact":"US-01","base_version":1,"content":` + jsonString(editedUS(t)) + `,"reason":"Sala acessível é exigência da área de facilities."}`
	accepted := live.json(t, "POST", base+"/artifact-edits", edit, key(), 202)
	editID := accepted["edit"].(string)
	if rc := live.send("POST", base+"/artifact-edits", strings.Replace(edit, `"base_version":1`, `"base_version":3`, 1), key()); rc.Response.StatusCode() != 409 ||
		decode(t, rc)["type"] != "urn:easycode:problem:outdated-version" {
		t.Fatalf("outdated base version: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	work.drain(t)
	if state := live.json(t, "GET", base+"/gates/gate.requirements/rounds/1", "", nil, 200)["state"]; state != "invalidated" {
		t.Fatalf("the edit invalidates the open round: %v", state)
	}
	if got := live.json(t, "GET", base+"/artifact-edits/"+editID, "", nil, 200); got["state"] != "reviewing" || got["review_wave"] != "REQ-02" {
		t.Fatalf("edit reviewing: %v", got)
	}
	run("REQ-02")
	if got := live.json(t, "GET", base+"/artifact-edits/"+editID, "", nil, 200); got["state"] != "applied" || got["version"] != float64(2) {
		t.Fatalf("US-53 CA-1 applied: %v", got)
	}
	if n := count(t, db, `SELECT count(*) FROM easycode.artifact_versions v JOIN easycode.artifacts a ON a.id = v.artifact_id
	   JOIN easycode.users u ON u.id = v.author_user_id WHERE a.code = 'US-01' AND v.version = 2 AND v.author = 'human' AND u.sub = 'dev'
	   AND v.reviewed_by = 'requirements-agent'`); n != 1 {
		t.Fatal("US-53 CA-1: human author and agent reviewer")
	}
	if n := count(t, db, `SELECT count(*) FROM easycode.audit_records WHERE action = 'artifact.edit' AND target = 'artifacts/US-01' AND actor_id = 'dev'`); n != 1 {
		t.Fatalf("US-53 CA-3, RN-4: every edit audited (%d)", n)
	}
	round := live.json(t, "GET", base+"/gates/gate.requirements/rounds/2", "", nil, 200)
	if round["state"] != "awaiting_human" {
		t.Fatalf("US-53 CA-3: the edited version goes to the gate: %v", round)
	}

	// US-53 CA-2: an edit the agent does not validate never becomes a version; the previous one holds.
	second := live.json(t, "POST", base+"/artifact-edits", strings.Replace(edit, `"base_version":1`, `"base_version":2`, 1), key(), 202)["edit"].(string)
	work.drain(t)
	run("REQ-03")
	if got := live.json(t, "GET", base+"/artifact-edits/"+second, "", nil, 200); got["state"] != "refused" {
		t.Fatalf("US-53 CA-2: %v", got)
	}
	if us := live.json(t, "GET", base+"/artifacts/US-01", "", nil, 200); us["version"] != float64(2) {
		t.Fatalf("US-53 CA-2: the previous version holds: %v", us["version"])
	}

	// US-53 CA-6: an approved artifact is not edited.
	live.approve(t, base, "gate.requirements", 3)
	if rc := live.send("POST", base+"/artifact-edits", strings.Replace(edit, `"base_version":1`, `"base_version":2`, 1), key()); rc.Response.StatusCode() != 409 {
		t.Fatalf("US-53 CA-6: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}

	// US-04: in progress is refused; canceled is deleted for everybody, also for the desktop app.
	if rc := live.send("POST", base+"/deletion", `{"name_confirmation":"Reserva MARCO-8"}`, key()); rc.Response.StatusCode() != 409 {
		t.Fatalf("US-04 CA-2: %d", rc.Response.StatusCode())
	}
	if _, err := db.Admin.Exec(ctx, `UPDATE easycode.projects SET state = 'canceled', finished_at = now() WHERE public_id = '`+projectID+`'`); err != nil {
		t.Fatal(err)
	}
	live.json(t, "POST", base+"/deletion", `{"name_confirmation":"Reserva MARCO-8"}`, key(), 202)
	if rc := live.bearer("GET", base, token, ""); rc.Response.StatusCode() != 404 {
		t.Fatalf("US-04 CA-1 for the app: %d", rc.Response.StatusCode())
	}
	other := live.as(t, oidctest.User{Subject: "ana", Name: "Ana"})
	if n := len(other.json(t, "GET", "/api/v1/projects", "", nil, 200)["items"].([]any)); n != 0 {
		t.Fatalf("US-04 CA-1: %d", n)
	}
}

func jsonString(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

func itoa(n int) string {
	encoded, _ := json.Marshal(n)
	return string(encoded)
}
