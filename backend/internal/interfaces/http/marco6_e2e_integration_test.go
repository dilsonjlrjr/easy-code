package http_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/application/command"
	appgateway "easycode/internal/application/gateway"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/runtime"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
	"easycode/internal/infrastructure/specs"
	httpapi "easycode/internal/interfaces/http"
)

// controlDeps is teamDeps with the controls of MARCO-6: pause, resume and restart of a wave (US-12, US-13)
// and pause, resume, cancel and conclusion of a project (US-51, US-52).
func controlDeps(t *testing.T) func(*httpapi.Dependencies, *postgrestest.Database) {
	return func(d *httpapi.Dependencies, db *postgrestest.Database) {
		teamDeps(t)(d, db)
		pool := db.PoolAs("easycode_api")
		wf, _ := specs.StandardWorkflow(specs.TeamAgents)
		tx := postgres.NewTxRunner(pool)
		trail := postgres.NewAuditTrail(pool)
		projects := postgres.NewProjects(pool, wf.Definition.ID, len(wf.Definition.Stages))
		workflows := postgres.NewWorkflow(pool, wf.Definition)
		waveStore := postgres.NewWaves(pool, wf.Definition)
		waves := appwave.NewService(tx, projects, waveStore, waveStore, trail).WithControls(waveStore)
		d.Waves, d.Access = waves, waves
		members := appproject.NewMemberService(tx, projects, postgres.NewMembers(pool), nil, postgres.NewOutbox(pool), trail)
		d.Workflow = appworkflow.NewService(tx, projects, workflows, workflows, workflows, trail).WithLifecycle(workflows, members)
	}
}

// controlWorker is the team worker with the control commands and the budget of cmd/easycode-worker.
type controlWorker struct {
	teamWorker
	orchestrator *appworkflow.Orchestrator
	tx           postgres.TxRunner
}

func newControlWorker(t *testing.T, db *postgrestest.Database, gateway runtime.Gateway) controlWorker {
	t.Helper()
	work := newTeamWorker(t, db, gateway, nil)
	pool := db.PoolAs("easycode_worker")
	outbox := postgres.NewOutbox(pool)
	wf, _ := specs.StandardWorkflow(specs.TeamAgents)
	workflows := postgres.NewWorkflow(pool, wf.Definition)
	orchestrator := appworkflow.NewOrchestrator(workflows, workflows, outbox, nil, wf.Definition, wf.JSON, wf.Hash).WithLifecycle(workflows)
	controls := appwave.NewControlHandler(postgres.NewWaves(pool, wf.Definition), outbox)
	for name, handler := range map[string]command.Handler{
		"wave.pause": controls.Pause, "wave.pause_now": controls.PauseNow, "wave.resume": controls.Resume, "wave.restart": controls.Restart,
		"project.pause": orchestrator.PauseProject, "project.resume": orchestrator.ResumeProject, "project.cancel": orchestrator.CancelProject,
		"project.record_publication": orchestrator.CompleteProject,
	} {
		work.dispatcher.Register(name, handler)
	}
	work.executor.WithBudget(orchestrator)
	return controlWorker{teamWorker: work, orchestrator: orchestrator, tx: postgres.NewTxRunner(pool)}
}

// reconcile runs the jobs of the worker reconcile (budget and the waves held by a resumed project).
func (w controlWorker) budgets(t *testing.T) {
	t.Helper()
	if _, err := w.orchestrator.CheckBudgets(context.Background(), w.tx.InTx); err != nil {
		t.Fatal(err)
	}
	if err := w.tx.InTx(context.Background(), func(ctx context.Context) error { _, err := w.orchestrator.ResumeHeld(ctx); return err }); err != nil {
		t.Fatal(err)
	}
}

// step is one scripted model call: held calls wait for release (or end aborted when the wave stops).
type step struct {
	hold    bool
	message provider.Message
	fail    *provider.Error
}

// stepProvider answers the model calls in order (one script for every project of the test).
type stepProvider struct {
	mu      sync.Mutex
	steps   []step
	calls   int
	started chan struct{}
	release chan struct{}
}

func newStepProvider() *stepProvider {
	return &stepProvider{started: make(chan struct{}, 16), release: make(chan struct{}, 16)}
}

func (p *stepProvider) push(steps ...step) {
	p.mu.Lock()
	p.steps = append(p.steps, steps...)
	p.mu.Unlock()
}

func (p *stepProvider) Name() string { return "anthropic" }

func (p *stepProvider) Capabilities(context.Context, string) (provider.Capabilities, error) {
	return provider.Capabilities{ToolUse: true, ContextTokens: 200_000}, nil
}

func (p *stepProvider) TestKey(context.Context, provider.Secret) error { return nil }

func (p *stepProvider) Complete(ctx context.Context, _ provider.Request, _ func(provider.Chunk)) (provider.Response, error) {
	p.mu.Lock()
	if len(p.steps) == 0 {
		p.mu.Unlock()
		return provider.Response{}, &provider.Error{Class: provider.ErrorRequest, Message: "no scripted step"}
	}
	s := p.steps[0]
	p.steps = p.steps[1:]
	p.calls++
	p.mu.Unlock()
	if s.fail != nil {
		return provider.Response{}, s.fail
	}
	if s.hold {
		p.started <- struct{}{}
		select {
		case <-p.release:
		case <-ctx.Done():
			return provider.Response{}, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted", PartialUsage: provider.Usage{Input: 50}}
		}
	}
	return provider.Response{Message: s.message, Usage: provider.Usage{Input: 900, Output: 200}, Stop: provider.StopTool, ProviderID: "req"}, nil
}

func (p *stepProvider) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(30 * time.Second):
		t.Fatal("the model call never started")
	}
}

// requirementsSteps deliver the reference requirements in two calls: the files, then finish.
func requirementsSteps(t *testing.T, holdFirst bool) []step {
	return []step{{hold: holdFirst, message: provider.Message{Calls: writeAll(referenceFiles(t), "w")}},
		{message: provider.Message{Calls: []provider.ToolCall{call("f", runtime.ToolFinish, map[string]any{})}}}}
}

func eventCount(t *testing.T, db *postgrestest.Database, eventType, condition string) int {
	t.Helper()
	sql := "SELECT count(*) FROM easycode.events WHERE type = '" + eventType + "'"
	if condition != "" {
		sql += " AND " + condition
	}
	return count(t, db, sql)
}

func runAsync(work controlWorker) chan error {
	done := make(chan error, 1)
	go func() { _, err := work.executor.RunOnce(context.Background()); done <- err }()
	return done
}

func waitRun(t *testing.T, done chan error, want error) {
	t.Helper()
	select {
	case err := <-done:
		if (want == nil && err != nil) || (want != nil && !errors.Is(err, want)) {
			t.Fatalf("executor: %v (want %v)", err, want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the wave did not stop")
	}
}

// TestMarco6WaveControl covers US-12 and US-13 end to end with scripted fake providers: a cooperative pause
// stops the wave at the next checkpoint without lost cost (CA-1) and warns past the pausing limit without
// aborting (CA-4); pausing now asks for the confirmation, aborts the call in flight within RNF-10 and charges
// it (CA-2); resuming continues from the last checkpoint (CA-3); a failed wave restarts with the previous
// attempt readable (US-13 CA-1) and only a failed wave offers restart (CA-2). Every action is audited (RN-1).
func TestMarco6WaveControl(t *testing.T) {
	live := newLiveAPI(t, controlDeps(t))
	db := live.db
	opener := sealKeys(t, db, map[string]string{"anthropic": anthropicKey})
	script := newStepProvider()
	work := newControlWorker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, script)})
	registerModels(t, live, "anthropic")

	// US-12 CA-1, CA-4: cooperative pause.
	script.push(requirementsSteps(t, true)...)
	base := newProject(t, live, work.teamWorker, "Reserva pausada", "anthropic", "anthropic")
	wave := base + "/waves/REQ-01"
	done := runAsync(work)
	script.waitStarted(t)
	if w := live.json(t, "GET", wave, "", nil, 200); fmt.Sprint(w["available_actions"]) != "[pause pause_now cancel]" {
		t.Fatalf("actions of a running wave: %v", w["available_actions"])
	}
	live.json(t, "POST", wave+"/pause", `{"mode":"sideways"}`, key(), 400)
	accepted := live.json(t, "POST", wave+"/pause", `{"mode":"cooperative"}`, key(), 202)
	if accepted["type"] != "pause" {
		t.Fatalf("AcceptedCommand: %v", accepted)
	}
	work.drain(t)
	appwave.PausingLimit = time.Nanosecond
	pausing := live.json(t, "GET", wave, "", nil, 200)
	appwave.PausingLimit = 10 * time.Minute
	pause, _ := pausing["pause"].(map[string]any)
	if pausing["state"] != "pausing" || pause == nil || pause["cause"] != "user_request" || pause["pausing_limit_exceeded"] != true ||
		fmt.Sprint(pausing["available_actions"]) != "[pause_now cancel]" {
		t.Fatalf("US-12 CA-1, CA-4: %v", pausing)
	}
	script.release <- struct{}{}
	waitRun(t, done, nil)
	paused := live.json(t, "GET", wave, "", nil, 200)
	if paused["state"] != "paused" || paused["pause"].(map[string]any)["mode"] != "cooperative" ||
		paused["cost"].(map[string]any)["lost"].(map[string]any)["value"] != "0" || fmt.Sprint(paused["available_actions"]) != "[resume cancel]" {
		t.Fatalf("US-12 CA-1: paused at the checkpoint without lost cost: %v", paused)
	}
	checkpoint := paused["last_checkpoint"]
	if eventCount(t, db, "br.easycode.wave.paused.v1", "envelope->'data'->>'mode' = 'cooperative' AND envelope->'data'->>'cause' = 'user_request'") != 1 {
		t.Fatal("wave.paused (cooperative) not emitted")
	}
	// US-12 CA-3: resume from the last checkpoint (no model call redone).
	live.json(t, "POST", wave+"/resume", "", key(), 202)
	work.drain(t)
	if w := live.json(t, "GET", wave, "", nil, 200); w["state"] != "running" {
		t.Fatalf("US-12 CA-3: %v", w["state"])
	}
	work.runAll(t)
	resumed := live.json(t, "GET", wave, "", nil, 200)
	calls := live.json(t, "GET", wave+"/calls?type=model", "", nil, 200)["items"].([]any)
	if resumed["state"] != "completed" || len(calls) != 2 || resumed["last_checkpoint"].(float64) <= checkpoint.(float64) {
		t.Fatalf("US-12 CA-3: resumed from checkpoint %v: %v, %d calls", checkpoint, resumed["state"], len(calls))
	}
	live.json(t, "POST", wave+"/pause", `{"mode":"cooperative"}`, key(), 409)

	// US-12 CA-2: pause now, with the confirmation of the lost cost.
	script.push(requirementsSteps(t, true)[0])
	now := newProject(t, live, work.teamWorker, "Reserva pausada agora", "anthropic", "anthropic")
	done = runAsync(work)
	script.waitStarted(t)
	refused := live.send("POST", now+"/waves/REQ-01/pause", `{"mode":"immediate"}`, key())
	if refused.Response.StatusCode() != 400 || decode(t, refused)["type"] != "urn:easycode:problem:confirmation-required" {
		t.Fatalf("US-12 CA-2: confirmation: %d %s", refused.Response.StatusCode(), refused.Response.Body())
	}
	asked := time.Now()
	live.json(t, "POST", now+"/waves/REQ-01/pause", `{"mode":"immediate","confirm_lost_cost":true}`, key(), 202)
	work.drain(t)
	waitRun(t, done, runtime.ErrLeaseLost)
	immediate := live.json(t, "GET", now+"/waves/REQ-01", "", nil, 200)
	if elapsed := time.Since(asked); elapsed > 5*time.Second || immediate["state"] != "paused" || immediate["pause"].(map[string]any)["mode"] != "immediate" ||
		immediate["cost"].(map[string]any)["lost"].(map[string]any)["value"] == "0" {
		t.Fatalf("US-12 CA-2, RNF-10: paused now in %s: %v", elapsed, immediate)
	}
	if eventCount(t, db, "br.easycode.wave.paused.v1", "envelope->'data'->>'mode' = 'immediate' AND envelope->'data'->'lost_cost'->>'value' <> '0'") != 1 {
		t.Fatal("US-12 CA-2: wave.paused with the lost cost")
	}
	// The paused wave resumes and redoes the aborted call from its checkpoint.
	script.push(requirementsSteps(t, false)...)
	live.json(t, "POST", now+"/waves/REQ-01/resume", "", key(), 202)
	work.drain(t)
	work.runAll(t)
	if w := live.json(t, "GET", now+"/waves/REQ-01", "", nil, 200); w["state"] != "completed" {
		t.Fatalf("resumed after pause now: %v", w["state"])
	}
	for _, action := range []string{"wave.pause", "wave.pause_now", "wave.resume"} {
		if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = '"+action+"' AND actor_id = 'dev' AND origin = 'web'"); n < 1 {
			t.Fatalf("US-12 RN-1: %s audited (%d)", action, n)
		}
	}

	// US-13: restart a failed wave; the previous attempt stays readable.
	script.push(step{fail: &provider.Error{Class: provider.ErrorRequest, Message: "bad request"}})
	failed := newProject(t, live, work.teamWorker, "Reserva com falha", "anthropic", "anthropic")
	work.runAll(t)
	w := live.json(t, "GET", failed+"/waves/REQ-01", "", nil, 200)
	if w["state"] != "failed" || fmt.Sprint(w["available_actions"]) != "[restart cancel]" {
		t.Fatalf("failed wave: %v", w)
	}
	live.json(t, "POST", failed+"/waves/REQ-01/restart", `{"confirm":false}`, key(), 400)
	restarted := live.json(t, "POST", failed+"/waves/REQ-01/restart", `{"confirm":true}`, key(), 202)
	if restarted["type"] != "restart" {
		t.Fatalf("AcceptedCommand: %v", restarted)
	}
	work.drain(t)
	queued := live.json(t, "GET", failed+"/waves/REQ-01", "", nil, 200)
	attempts := queued["attempts"].([]any)
	if queued["state"] != "queued" || queued["attempt"] != float64(2) || len(attempts) != 1 ||
		attempts[0].(map[string]any)["final_state"] != "failed" || attempts[0].(map[string]any)["failure_cause"] != "unrecoverable_error" {
		t.Fatalf("US-13 CA-1: %v", queued)
	}
	if eventCount(t, db, "br.easycode.wave.restarted.v1", "(envelope->'data'->>'attempt')::int = 2") != 1 {
		t.Fatal("wave.restarted not emitted")
	}
	script.push(requirementsSteps(t, false)...)
	work.runAll(t)
	done2 := live.json(t, "GET", failed+"/waves/REQ-01", "", nil, 200)
	if done2["state"] != "completed" || len(done2["attempts"].([]any)) != 1 {
		t.Fatalf("US-13 CA-1: the second attempt completes: %v", done2)
	}
	// US-13 CA-2: a completed wave offers no restart and refuses it.
	if strings.Contains(fmt.Sprint(done2["available_actions"]), "restart") {
		t.Fatalf("US-13 CA-2: %v", done2["available_actions"])
	}
	live.json(t, "POST", failed+"/waves/REQ-01/restart", `{"confirm":true}`, key(), 409)
}

// TestMarco6ProjectLifecycle covers US-51: only the lead or the administrator pauses, resumes and cancels
// (CA-6); pausing pauses the running waves and retains the queue (CA-1); resuming resumes them (CA-2);
// canceling with the project name typed cancels the active waves with the lost cost and keeps the history
// (CA-4); an ended project refuses everything with state-conflict (CA-5).
func TestMarco6ProjectLifecycle(t *testing.T) {
	live := newLiveAPI(t, controlDeps(t))
	db := live.db
	opener := sealKeys(t, db, map[string]string{"anthropic": anthropicKey})
	script := newStepProvider()
	work := newControlWorker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, script)})
	registerModels(t, live, "anthropic")

	script.push(requirementsSteps(t, true)...)
	base := newProject(t, live, work.teamWorker, "Reserva controlada", "anthropic", "anthropic")
	if p := live.json(t, "GET", base, "", nil, 200); fmt.Sprint(p["available_actions"]) != "[pause cancel record_publication]" {
		t.Fatalf("Project.available_actions in progress: %v", p["available_actions"])
	}
	rita := live.as(t, oidctest.User{Subject: "rita", Name: "Rita Melo"})
	live.json(t, "PUT", base+"/members/rita", `{"roles":["member","approver"]}`, nil, 200)
	rita.json(t, "POST", base+"/pause", "", key(), 403) // CA-6
	if p := rita.json(t, "GET", base, "", nil, 200); fmt.Sprint(p["available_actions"]) != "[record_publication]" {
		t.Fatalf("CA-6: actions of an approver: %v", p["available_actions"])
	}
	done := runAsync(work)
	script.waitStarted(t)
	live.json(t, "POST", base+"/pause", "", key(), 202)
	work.drain(t)
	project := live.json(t, "GET", base, "", nil, 200)
	if project["state"] != "paused" || project["pause_reason"] != "manual" || fmt.Sprint(project["available_actions"]) != "[resume cancel]" {
		t.Fatalf("US-51 CA-1: %v", project)
	}
	if w := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200); w["state"] != "pausing" || w["pause"].(map[string]any)["cause"] != "project_paused" {
		t.Fatalf("US-51 CA-1: the running wave is paused by policy: %v", w)
	}
	script.release <- struct{}{}
	waitRun(t, done, nil)
	if w := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200); w["state"] != "paused" {
		t.Fatalf("US-51 CA-1: paused at the checkpoint: %v", w["state"])
	}
	if ran, _ := work.executor.RunOnce(context.Background()); ran {
		t.Fatal("US-51 CA-1: no wave runs while the project is paused")
	}
	live.json(t, "POST", base+"/pause", "", key(), 409)
	if eventCount(t, db, "br.easycode.project.paused.v1", "envelope->'data'->>'pause_reason' = 'manual' AND envelope->'data'->'actor'->>'type' = 'human'") != 1 {
		t.Fatal("project.paused not emitted")
	}
	// CA-2: resume.
	live.json(t, "POST", base+"/resume", "", key(), 202)
	work.drain(t)
	if p := live.json(t, "GET", base, "", nil, 200); p["state"] != "in_progress" || p["pause_reason"] != nil {
		t.Fatalf("US-51 CA-2: %v", p)
	}
	work.runAll(t)
	if w := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200); w["state"] != "completed" {
		t.Fatalf("US-51 CA-2: the paused wave is resumed: %v", w["state"])
	}
	if eventCount(t, db, "br.easycode.wave.resumed.v1", "envelope->'data'->>'cause' = 'project_resumed'") != 1 {
		t.Fatal("wave.resumed by the project not emitted")
	}

	// CA-4: cancel, with the name typed, while a wave runs.
	script.push(requirementsSteps(t, true)[0])
	other := newProject(t, live, work.teamWorker, "Reserva cancelada", "anthropic", "anthropic")
	done = runAsync(work)
	script.waitStarted(t)
	wrong := live.send("POST", other+"/cancellation", `{"name_confirmation":"Outro nome"}`, key())
	if wrong.Response.StatusCode() != 400 || decode(t, wrong)["type"] != "urn:easycode:problem:confirmation-required" {
		t.Fatalf("US-51 RN-1: %d %s", wrong.Response.StatusCode(), wrong.Response.Body())
	}
	live.json(t, "POST", other+"/cancellation", `{"name_confirmation":"Reserva cancelada"}`, key(), 202)
	work.drain(t)
	waitRun(t, done, runtime.ErrLeaseLost)
	canceled := live.json(t, "GET", other, "", nil, 200)
	wave := live.json(t, "GET", other+"/waves/REQ-01", "", nil, 200)
	if canceled["state"] != "canceled" || fmt.Sprint(canceled["available_actions"]) != "[]" || wave["state"] != "canceled" ||
		wave["cost"].(map[string]any)["lost"].(map[string]any)["value"] == "0" {
		t.Fatalf("US-51 CA-4: %v %v", canceled["state"], wave)
	}
	if eventCount(t, db, "br.easycode.project.canceled.v1", "envelope->'data'->'canceled_waves' ? 'REQ-01'") != 1 ||
		eventCount(t, db, "br.easycode.wave.canceled.v1", "envelope->'data'->>'cause' = 'project_canceled'") != 1 {
		t.Fatal("US-51 CA-4: project.canceled and wave.canceled by policy")
	}
	if artifacts := live.json(t, "GET", other+"/artifacts", "", nil, 200)["items"].([]any); artifacts == nil {
		t.Fatal("US-51 CA-4: the history stays readable")
	}
	// CA-5: an ended project refuses pause, resume and cancel.
	for _, path := range []string{"/pause", "/resume"} {
		if rc := live.send("POST", other+path, "", key()); rc.Response.StatusCode() != 409 {
			t.Fatalf("US-51 CA-5 %s: %d", path, rc.Response.StatusCode())
		}
	}
	live.json(t, "POST", other+"/cancellation", `{"name_confirmation":"Reserva cancelada"}`, key(), 409)
	live.json(t, "PATCH", other+"/configuration", `{"artifact_language":"en"}`, map[string]string{"If-Match": `"2"`}, 409)
	for _, action := range []string{"project.pause", "project.resume", "project.cancel"} {
		if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = '"+action+"'"); n != 1 {
			t.Fatalf("RNF-13: %s audited (%d)", action, n)
		}
	}
}

// TestMarco6Budget covers US-39 and RNF-16 (complete, with the budget): crossing an alert limit notifies in
// the pending items without changing the state (CA-1); reaching the stop limit pauses the project by the
// budget and the running wave at its next checkpoint, with no model call started afterwards and the excess
// bounded by the call in flight (CA-2, RNF-16); resuming needs the stop limit raised above the spend, and
// raising it does not resume alone (CA-3); a limit lowered below the spend pauses the project (CA-5).
func TestMarco6Budget(t *testing.T) {
	live := newLiveAPI(t, controlDeps(t))
	db := live.db
	opener := sealKeys(t, db, map[string]string{"anthropic": anthropicKey})
	script := newStepProvider()
	work := newControlWorker(t, db, localGateway{service: appgateway.NewService(opener, time.Minute, script)})
	registerModels(t, live, "anthropic")

	// Each call costs 900 × 20/M + 200 × 100/M = 0.038 BRL; the stop limit is 0.05 (alerts at 50% and 80%).
	files := writeAll(referenceFiles(t), "w")
	script.push(step{message: provider.Message{Calls: files[:1]}}, step{message: provider.Message{Calls: files[1:]}},
		step{message: provider.Message{Calls: []provider.ToolCall{call("f", runtime.ToolFinish, map[string]any{})}}})
	base := newProject(t, live, work.teamWorker, "Reserva no limite", "anthropic", "anthropic")
	live.json(t, "PATCH", base+"/configuration", `{"budget":{"stop_limit":{"value":"0.05","currency":"BRL"}}}`, map[string]string{"If-Match": `"2"`}, 200)
	work.runAll(t)
	work.drain(t)
	project := live.json(t, "GET", base, "", nil, 200)
	wave := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200)
	if project["state"] != "paused" || project["pause_reason"] != "budget" || wave["state"] != "paused" || wave["pause"].(map[string]any)["cause"] != "budget_exhausted" {
		t.Fatalf("US-39 CA-2: %v %v", project, wave)
	}
	// RNF-16: two calls, none started after the limit; the excess is at most the call in flight.
	if calls := live.json(t, "GET", base+"/waves/REQ-01/calls?type=model", "", nil, 200)["items"].([]any); len(calls) != 2 || script.calls != 2 {
		t.Fatalf("RNF-16: model calls after the budget: %d (provider %d)", len(calls), script.calls)
	}
	if spend := project["summary"].(map[string]any)["accumulated_cost"].(map[string]any)["value"]; spend != "0.076" {
		t.Fatalf("RNF-16: spend %v", spend)
	}
	if eventCount(t, db, "br.easycode.project.budget_alerted.v1", "envelope->'data'->'alert_limit'->>'value' = '0.025'") != 1 ||
		eventCount(t, db, "br.easycode.project.budget_exhausted.v1", "envelope->'data'->>'cause' = 'spend_reached_limit'") != 1 ||
		eventCount(t, db, "br.easycode.project.paused.v1", "envelope->'data'->>'pause_reason' = 'budget' AND envelope->'data'->'actor'->>'type' = 'orchestrator'") != 1 {
		t.Fatal("US-39 CA-1, CA-2: budget events")
	}
	inbox := live.json(t, "GET", "/api/v1/pending-items?type=budget", "", nil, 200)["items"].([]any)
	if len(inbox) != 1 || inbox[0].(map[string]any)["summary"].(map[string]any)["budget_percentage"] != float64(152) {
		t.Fatalf("US-39 CA-1, RN-5: budget in the pending items: %v", inbox)
	}
	// CA-3: resume refused without a larger limit; a larger limit does not resume alone.
	if rc := live.send("POST", base+"/resume", "", key()); rc.Response.StatusCode() != 409 || decode(t, rc)["type"] != "urn:easycode:problem:budget-exhausted" {
		t.Fatalf("US-39 CA-3: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	if rc := live.send("POST", base+"/waves/REQ-01/resume", "", key()); rc.Response.StatusCode() != 409 {
		t.Fatalf("resumeWave without budget: %d", rc.Response.StatusCode())
	}
	live.json(t, "PATCH", base+"/configuration", `{"budget":{"stop_limit":{"value":"1.00","currency":"BRL"}}}`, map[string]string{"If-Match": `"3"`}, 200)
	work.budgets(t)
	if p := live.json(t, "GET", base, "", nil, 200); p["state"] != "paused" {
		t.Fatalf("US-39 CA-3: only a human resumes: %v", p["state"])
	}
	if n := count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'configuration.modify' AND target = 'budget' AND decision LIKE '%1.00 BRL%'"); n != 1 {
		t.Fatalf("US-39 CA-3: the new limit is recorded with who and when (%d)", n)
	}
	live.json(t, "POST", base+"/resume", "", key(), 202)
	work.drain(t)
	work.runAll(t)
	if w := live.json(t, "GET", base+"/waves/REQ-01", "", nil, 200); w["state"] != "completed" {
		t.Fatalf("US-39 CA-3: the paused wave resumes: %v", w["state"])
	}
	// CA-5: a limit below the spend is accepted and the orchestrator pauses the project.
	live.json(t, "PATCH", base+"/configuration", `{"budget":{"stop_limit":{"value":"0.10","currency":"BRL"}}}`, map[string]string{"If-Match": `"4"`}, 200)
	work.budgets(t)
	if p := live.json(t, "GET", base, "", nil, 200); p["state"] != "paused" || p["pause_reason"] != "budget" {
		t.Fatalf("US-39 CA-5: %v", p)
	}
}

// marco6Release is the Release part of the MARCO-6 done criterion (US-08, US-33 CA-4, US-52). It runs at the
// end of TestMarco5QualityAndFindings, when the eight stages of the reference project are approved, so the
// long flow over the real sandbox runs once: the orchestrator consolidated the RELEASE (REL-01, no model
// call), gate.release waits for the human; who accepted a risk was refused before this point (CA-5).
func marco6Release(t *testing.T, live liveAPI, work qualityWorker, repos git.Repositories, base string) {
	t.Helper()
	db, ctx := live.db, context.Background()
	projectID := strings.TrimPrefix(base, "/api/v1/projects/")
	round := roundOf(t, live, base, "gate.release", 1)
	if round["state"] != "awaiting_human" {
		t.Fatalf("gate.release round 1: %v %v", round["state"], failedChecks(round))
	}
	wave := live.json(t, "GET", base+"/waves/REL-01", "", nil, 200)
	if wave["agent"] != "orchestrator" || wave["state"] != "completed" ||
		len(live.json(t, "GET", base+"/waves/REL-01/calls?type=model", "", nil, 200)["items"].([]any)) != 0 {
		t.Fatalf("US-08: the orchestrator writes the RELEASE without a model call: %v", wave)
	}
	release := live.json(t, "GET", base+"/artifacts/RELEASE-01", "", nil, 200)
	header := release["header"].(map[string]any)
	trace := header["rastreabilidade"].(map[string]any)
	if release["state"] != "in_review" || header["autor"] != "orquestrador" || fmt.Sprint(header["riscos_aceitos"]) != "[ACH-04]" ||
		fmt.Sprint(header["achados_abertos"]) != "[]" || trace["us_must_total"] != trace["us_must_completas"] || len(header["gates"].([]any)) != 8 ||
		header["entrega"].(map[string]any)["tag"] != "release-01" || !strings.Contains(release["body"].(string), "Risco aceito ACH-04") {
		t.Fatalf("US-08 CA-1, US-33 CA-4: RELEASE: %v", header)
	}
	// US-08 CA-1, US-33 CA-4: the consolidated summary with the accepted risks and who accepted them.
	summary := live.json(t, "GET", base+"/release", "", nil, 200)
	accepted := summary["accepted_risks"].([]any)
	if len(summary["blocks"].([]any)) != 0 || len(accepted) != 1 || accepted[0].(map[string]any)["finding"].(map[string]any)["id"] != "ACH-04" ||
		accepted[0].(map[string]any)["acceptance"].(map[string]any)["justification"] == "" || summary["tests"].(map[string]any)["ct_total"] != float64(4) ||
		summary["cost_total"].(map[string]any)["total"].(map[string]any)["value"] == "0" || len(summary["gates"].([]any)) != 9 {
		t.Fatalf("US-08 CA-1: %v", summary)
	}
	// US-52 CA-2: the project does not conclude before the Release is approved.
	early := live.send("POST", base+"/completion", "", key())
	if early.Response.StatusCode() != 409 || decode(t, early)["reason"] != "release_not_approved" {
		t.Fatalf("US-52 CA-2: %d %s", early.Response.StatusCode(), early.Response.Body())
	}
	live.json(t, "PUT", base+"/members/carla", `{"roles":["member","approver"]}`, nil, 200)
	carla := live.as(t, oidctest.User{Subject: "carla", Name: "Carla Dias"})
	csrf := carla.json(t, "GET", "/api/v1/me", "", nil, 200)["csrf_token"].(string)
	headers := func() map[string]string { return mergeHeaders(key(), map[string]string{"X-CSRF-Token": csrf}) }
	// US-08 CA-6: an acceptance past its validity blocks the Release, with the finding and the date.
	if _, err := db.Admin.Exec(ctx, `UPDATE easycode.findings f SET acceptance_valid_until = current_date - 1 FROM easycode.artifacts a
 WHERE a.id = f.artifact_id AND a.code = 'ACH-04'`); err != nil {
		t.Fatal(err)
	}
	blocks := live.json(t, "GET", base+"/release", "", nil, 200)["blocks"].([]any)
	if len(blocks) != 1 || blocks[0].(map[string]any)["type"] != "acceptance_expired" || blocks[0].(map[string]any)["reference"] != "ACH-04" ||
		blocks[0].(map[string]any)["valid_until"] == nil {
		t.Fatalf("US-08 CA-6: %v", blocks)
	}
	refused := carla.send("POST", base+"/gates/gate.release/rounds/1/approval", confirmedChecks(t, round), headers())
	if refused.Response.StatusCode() != 409 || decode(t, refused)["type"] != "urn:easycode:problem:blocking-finding" ||
		fmt.Sprint(decode(t, refused)["findings"]) != "[ACH-04]" {
		t.Fatalf("US-08 CA-2, CA-6: %d %s", refused.Response.StatusCode(), refused.Response.Body())
	}
	if _, err := db.Admin.Exec(ctx, `UPDATE easycode.findings f SET acceptance_valid_until = current_date + 90 FROM easycode.artifacts a
 WHERE a.id = f.artifact_id AND a.code = 'ACH-04'`); err != nil {
		t.Fatal(err)
	}
	// US-08: another approver approves, confirming the delivery (release.delivery_checked).
	approved := carla.json(t, "POST", base+"/gates/gate.release/rounds/1/approval", confirmedChecks(t, round), headers(), 200)
	if approved["state"] != "approved" {
		t.Fatalf("US-08: %v", approved)
	}
	work.settle(t)
	// US-08 CA-3: the repository has the version mark and the approved RELEASE in docs/.
	repository := live.json(t, "GET", base+"/repository", "", nil, 200)
	marks := repository["marks"].([]any)
	if len(marks) != 1 || marks[0].(map[string]any)["name"] != "release-01" || marks[0].(map[string]any)["commit"] != repository["last_commit"].(map[string]any)["sha"] {
		t.Fatalf("US-08 CA-3: marks %v, last commit %v", marks, repository["last_commit"])
	}
	files, err := repos.Files(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if file := files["docs/release/RELEASE-01.md"]; !strings.Contains(file, "situacao: aprovado") || !strings.Contains(file, "decisao: aprovado") ||
		!strings.Contains(file, "por: Carla Dias") {
		t.Fatalf("US-08 CA-3: approved RELEASE in docs/:\n%s", file)
	}
	if r := live.json(t, "GET", base+"/artifacts/RELEASE-01", "", nil, 200); r["state"] != "approved" || r["header"].(map[string]any)["situacao"] != "aprovado" {
		t.Fatalf("US-08: RELEASE approved: %v %v", r["state"], r["header"].(map[string]any)["situacao"])
	}
	// US-52: only the lead or an approver concludes (RN-2); the project becomes read-only (CA-1, CA-4).
	rita := live.as(t, oidctest.User{Subject: "rita", Name: "Rita Melo"})
	rita.json(t, "POST", base+"/completion", "", key(), 403)
	live.json(t, "POST", base+"/completion", "", key(), 202)
	work.drain(t)
	project := live.json(t, "GET", base, "", nil, 200)
	if project["state"] != "completed" || fmt.Sprint(project["available_actions"]) != "[]" {
		t.Fatalf("US-52 CA-1: %v", project)
	}
	if eventCount(t, db, "br.easycode.release.published.v1", "envelope->'data'->>'publication_reference' = 'release-01'") != 1 ||
		eventCount(t, db, "br.easycode.project.completed.v1", "envelope->'data'->>'release' = 'RELEASE-01'") != 1 ||
		count(t, db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'project.record_publication'") != 1 {
		t.Fatal("US-52: release.published, project.completed and the audit")
	}
	read := live.send("POST", base+"/pause", "", key())
	if read.Response.StatusCode() != 409 || decode(t, read)["current_state"] != "completed" {
		t.Fatalf("US-52 CA-4: pause of a concluded project: %d %s", read.Response.StatusCode(), read.Response.Body())
	}
	accept := carla.send("POST", base+"/findings/ACH-04/risk-acceptance", `{"justification":"Renovação depois de concluído"}`, headers())
	if accept.Response.StatusCode() != 409 {
		t.Fatalf("US-52 CA-4: finding decision of a concluded project: %d %s", accept.Response.StatusCode(), accept.Response.Body())
	}
	if artifacts := live.json(t, "GET", base+"/artifacts", "", nil, 200)["items"].([]any); len(artifacts) == 0 {
		t.Fatal("US-52 CA-1: artifacts preserved")
	}
}
