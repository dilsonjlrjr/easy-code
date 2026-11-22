package workflow

import (
	"context"
	"encoding/json"
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

func (o *Orchestrator) queueFirstWave(ctx context.Context, cmd command.Command, stage domainworkflow.Stage,
	emit func(string, string, string, map[string]any) error, orchestrator map[string]any) error {
	pair, err := o.store.ResolvePair(ctx, cmd.ProjectID, stage.ID)
	if err != nil {
		return err
	}
	configuration, err := o.agents(stage.Agent)
	if err != nil {
		return err
	}
	wave, err := o.store.QueueWave(ctx, NewWave{ProjectID: cmd.ProjectID, Stage: stage.ID, Agent: stage.Agent, Pair: pair, Configuration: configuration})
	if err != nil {
		return err
	}
	err = emit("br.easycode.wave.created.v1", "waves/"+wave.Code, wave.Code, map[string]any{
		"wave": wave.Code, "stage": stage.ID, "agent": stage.Agent, "purpose": "execution", "actor": orchestrator,
		"transition": map[string]any{"machine": "wave", "from_state": nil, "to_state": "queued"},
	})
	if err != nil {
		return err
	}
	// US-16 CA-1: each wave records the models it uses.
	for role, model := range map[string]ModelID{"reasoning": pair.Reasoning, "execution": pair.Execution} {
		err := emit("br.easycode.wave.model_selected.v1", "waves/"+wave.Code, wave.Code, map[string]any{
			"wave": wave.Code, "stage": stage.ID, "agent": stage.Agent, "purpose": "execution", "actor": orchestrator,
			"provider": model.Provider, "model": model.Name, "role": role, "mode": "manual", "reason": "project_configuration",
		})
		if err != nil {
			return err
		}
	}
	return nil
}
