package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"easycode/internal/application/command"
	"easycode/internal/application/event"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/project"
	domainworkflow "easycode/internal/domain/workflow"
)

// Orchestrator applies the start command in the worker: it rechecks the full guard, pins the
// workflow, instantiates the stages (all blocked, those without dependency then ready) and
// queues the first wave of each ready stage with the pair in force, all with the events in one
// transaction (US-05 CA-1, CA-3; US-16 CA-1, CA-4; RNF-15).
type Orchestrator struct {
	store     Orchestration
	readiness Readiness
	outbox    event.Outbox
	agents    AgentDefaults
	workflow  domainworkflow.Definition
	json      []byte
	hash      string
	now       func() time.Time
}

// NewOrchestrator builds the orchestrator for the workflow in force.
func NewOrchestrator(store Orchestration, readiness Readiness, outbox event.Outbox, agents AgentDefaults, definition domainworkflow.Definition, definitionJSON []byte, hash string) *Orchestrator {
	return &Orchestrator{store: store, readiness: readiness, outbox: outbox, agents: agents, workflow: definition, json: definitionJSON, hash: hash, now: time.Now}
}

// Start is the handler of the "start" project command.
func (o *Orchestrator) Start(ctx context.Context, cmd command.Command) error {
	state, err := o.store.LockProject(ctx, cmd.ProjectID)
	if err != nil {
		return err
	}
	if state != project.StateCreated {
		return failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
	}
	missing, err := o.readiness.Missing(ctx, cmd.ProjectID, o.now())
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return failure.New(failure.KindConflict, project.CodeIncompleteConfig, nil)
	}
	// Every guard is checked: from here on, write.
	if err := o.store.StartProject(ctx, cmd.ProjectID, o.workflow, o.json, o.hash); err != nil {
		return err
	}
	initial := o.workflow.Initial()
	ready := []string{}
	for _, stage := range o.workflow.Stages {
		if initial[stage.ID] == domainworkflow.StageReady {
			ready = append(ready, stage.ID)
		}
	}
	emit := func(eventType, subject, wave string, data map[string]any) error {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = o.outbox.Append(ctx, domainevent.Draft{
			Type: eventType, Subject: subject, ProjectID: cmd.ProjectID, WaveID: wave, CorrelationID: cmd.ID, Data: encoded,
		})
		return err
	}
	orchestrator := map[string]any{"type": "orchestrator", "id": "orchestrator"}
	err = emit("br.easycode.project.started.v1", "project", "", map[string]any{
		"project":      cmd.ProjectID,
		"transition":   map[string]any{"machine": "project", "from_state": project.StateCreated, "to_state": project.StateInProgress},
		"actor":        map[string]any{"type": "human", "id": cmd.RequestedBy, "origin": "web"},
		"ready_stages": ready,
	})
	if err != nil {
		return err
	}
	for _, stageID := range ready {
		stage, _ := o.workflow.Stage(stageID)
		if err := o.store.SetStageState(ctx, cmd.ProjectID, stageID, domainworkflow.StageReady); err != nil {
			return err
		}
		err := emit("br.easycode.stage.unblocked.v1", "stages/"+stageID, "", map[string]any{
			"stage": stageID, "actor": orchestrator, "version_inputs": []any{},
			"transition": map[string]any{"machine": "stage", "from_state": domainworkflow.StageBlocked, "to_state": domainworkflow.StageReady},
		})
		if err != nil {
			return err
		}
		if err := o.queueFirstWave(ctx, cmd, stage, emit, orchestrator); err != nil {
			return err
		}
	}
	return nil
}

// emitter builds the event writer of a correlation.
func (o *Orchestrator) emitter(ctx context.Context, projectID, correlation string) func(string, string, string, map[string]any) error {
	return func(eventType, subject, wave string, data map[string]any) error {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = o.outbox.Append(ctx, domainevent.Draft{
			Type: eventType, Subject: subject, ProjectID: projectID, WaveID: wave, CorrelationID: correlation, Data: encoded,
		})
		return err
	}
}

// UnblockReady moves to ready every blocked stage or activity whose dependencies are all approved and
// queues its first waves, without human action (US-05 CA-4); a stage blocked by a reopened dependency
// resumes its paused waves instead (resume_by_policy, US-34 CA-2). An approved stage marked for
// reevaluation whose dependencies are approved again gets a reevaluation wave. Call it in the
// transaction of the approval effect.
func (o *Orchestrator) UnblockReady(ctx context.Context, projectID, correlation string) error {
	states, err := o.store.StageStates(ctx, projectID)
	if err != nil {
		return err
	}
	emit := o.emitter(ctx, projectID, correlation)
	orchestrator := map[string]any{"type": "orchestrator", "id": "orchestrator"}
	approved := func(dependencies []string) bool {
		for _, dependency := range dependencies {
			if states[dependency] != domainworkflow.StageApproved {
				return false
			}
		}
		return true
	}
	unblock := func(id string, machine string) (bool, error) {
		if err := o.store.SetStageState(ctx, projectID, id, domainworkflow.StageReady); err != nil {
			return false, err
		}
		if err := emit("br.easycode.stage.unblocked.v1", "stages/"+id, "", map[string]any{
			"stage": id, "actor": orchestrator, "version_inputs": []any{},
			"transition": map[string]any{"machine": machine, "from_state": domainworkflow.StageBlocked, "to_state": domainworkflow.StageReady},
		}); err != nil {
			return false, err
		}
		resumed, err := o.store.ResumePaused(ctx, projectID, id)
		if err != nil {
			return false, err
		}
		for _, code := range resumed {
			if err := emit("br.easycode.wave.resumed.v1", "waves/"+code, code, map[string]any{"wave": code, "stage": id, "actor": orchestrator,
				"cause": "dependency_reapproved", "transition": map[string]any{"machine": "wave", "from_state": "paused", "to_state": "running"}}); err != nil {
				return false, err
			}
		}
		return o.store.HasWaves(ctx, projectID, id)
	}
	for _, stage := range o.workflow.Stages {
		if states[stage.ID] != domainworkflow.StageBlocked || !approved(stage.DependsOn) {
			continue
		}
		pending, err := unblock(stage.ID, "stage")
		if err != nil {
			return err
		}
		if !pending {
			if err := o.queueStageWaves(ctx, projectID, stage, emit, orchestrator); err != nil {
				return err
			}
		}
	}
	for _, activity := range o.workflow.Activities {
		if states[activity.ID] != domainworkflow.StageBlocked || !approved(activity.DependsOn) {
			continue
		}
		pending, err := unblock(activity.ID, "activity")
		if err != nil {
			return err
		}
		if !pending {
			if _, err := o.QueueWave(ctx, projectID, activity.ID, activity.Agent, "execution", "", correlation); err != nil {
				return err
			}
		}
	}
	pendingReevaluation, err := o.store.ReevaluationPending(ctx, projectID)
	if err != nil {
		return err
	}
	for _, stage := range o.workflow.Stages {
		if !pendingReevaluation[stage.ID] || !approved(stage.DependsOn) {
			continue
		}
		if busy, err := o.store.HasWaves(ctx, projectID, stage.ID); err != nil || busy {
			if err != nil {
				return err
			}
			continue
		}
		code, err := o.QueueWave(ctx, projectID, stage.ID, stage.Agent, "reevaluation", "", correlation)
		if err != nil {
			return err
		}
		if err := emit("br.easycode.stage.reevaluation_requested.v1", "stages/"+stage.ID, "", map[string]any{
			"stage": stage.ID, "wave": code, "actor": orchestrator,
			"transition": map[string]any{"machine": "stage", "from_state": domainworkflow.StageApproved, "to_state": domainworkflow.StageApproved},
		}); err != nil {
			return err
		}
	}
	return nil
}

// QueueWave queues one wave of a stage or activity with a purpose (rework, revalidation and reevaluation
// waves of the findings and of the reopening, interno.md section 4) and returns its code.
func (o *Orchestrator) QueueWave(ctx context.Context, projectID, stageID, agent, purpose, finding, correlation string) (string, error) {
	return o.queueWave(ctx, projectID, stageID, agent, "", purpose, finding, o.emitter(ctx, projectID, correlation),
		map[string]any{"type": "orchestrator", "id": "orchestrator"})
}

// TaskStage is the stage whose waves run one approved T each (PL-20).
const TaskStage = "implementation"

// queueStageWaves queues the first waves of a stage: one per approved T in the implementation, run in
// order by the executor; in a stage with participants (validation), one wave per participant for its
// opinion and then the lead's, which consolidates them (template VALIDACAO); one wave otherwise.
func (o *Orchestrator) queueStageWaves(ctx context.Context, projectID string, stage domainworkflow.Stage,
	emit func(string, string, string, map[string]any) error, orchestrator map[string]any) error {
	if stage.ID != TaskStage {
		for _, agent := range stage.Agents() {
			if _, err := o.queueWave(ctx, projectID, stage.ID, agent, "", "execution", "", emit, orchestrator); err != nil {
				return err
			}
		}
		return nil
	}
	tasks, err := o.store.ApprovedTasks(ctx, projectID)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		tasks = []string{""}
	}
	for _, task := range tasks {
		if _, err := o.queueWave(ctx, projectID, stage.ID, stage.Agent, task, "execution", "", emit, orchestrator); err != nil {
			return err
		}
	}
	return nil
}

// QueueRework queues the new waves of a stage whose gate was rejected: the activities approved in its
// gate (their outputs went back to draft), the participants and the lead in the validation, which is
// redone as a whole, or the stage agent; it returns the wave codes.
func (o *Orchestrator) QueueRework(ctx context.Context, projectID, stageID, correlation string) ([]string, error) {
	stage, ok := o.workflow.Stage(stageID)
	if !ok {
		return nil, fmt.Errorf("unknown stage %s", stageID)
	}
	var codes []string
	actor := map[string]any{"type": "orchestrator", "id": "orchestrator"}
	for _, activity := range o.workflow.ActivitiesFor(stageID) {
		code, err := o.queueWave(ctx, projectID, activity.ID, activity.Agent, "", "execution", "", o.emitter(ctx, projectID, correlation), actor)
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	for _, agent := range stage.Agents() {
		code, err := o.queueWave(ctx, projectID, stage.ID, agent, "", "execution", "", o.emitter(ctx, projectID, correlation), actor)
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func (o *Orchestrator) queueFirstWave(ctx context.Context, cmd command.Command, stage domainworkflow.Stage,
	emit func(string, string, string, map[string]any) error, orchestrator map[string]any) error {
	return o.queueStageWaves(ctx, cmd.ProjectID, stage, emit, orchestrator)
}

func (o *Orchestrator) queueWave(ctx context.Context, projectID string, stageID string, agent, task, purpose, finding string,
	emit func(string, string, string, map[string]any) error, orchestrator map[string]any) (string, error) {
	pair, err := o.store.ResolvePair(ctx, projectID, stageID)
	if err != nil {
		return "", err
	}
	configuration, err := o.agents(agent)
	if err != nil {
		return "", err
	}
	if purpose == "" {
		purpose = "execution"
	}
	wave, err := o.store.QueueWave(ctx, NewWave{ProjectID: projectID, Stage: stageID, Agent: agent, Pair: pair, Configuration: configuration, Task: task,
		Purpose: purpose, Finding: finding})
	if err != nil {
		return "", err
	}
	created := map[string]any{
		"wave": wave.Code, "stage": stageID, "agent": agent, "purpose": purpose, "actor": orchestrator,
		"transition": map[string]any{"machine": "wave", "from_state": nil, "to_state": "queued"},
	}
	if task != "" {
		created["task"] = task
	}
	if finding != "" {
		created["finding"] = finding
	}
	err = emit("br.easycode.wave.created.v1", "waves/"+wave.Code, wave.Code, created)
	if err != nil {
		return "", err
	}
	// US-16 CA-1: each wave records the models it uses.
	for role, model := range map[string]ModelID{"reasoning": pair.Reasoning, "execution": pair.Execution} {
		err := emit("br.easycode.wave.model_selected.v1", "waves/"+wave.Code, wave.Code, map[string]any{
			"wave": wave.Code, "stage": stageID, "agent": agent, "purpose": purpose, "actor": orchestrator,
			"provider": model.Provider, "model": model.Name, "role": role, "mode": "manual", "reason": "project_configuration",
		})
		if err != nil {
			return "", err
		}
	}
	return wave.Code, nil
}
