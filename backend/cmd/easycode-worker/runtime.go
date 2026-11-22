package main

import (
	"context"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/propagation"

	appartifact "easycode/internal/application/artifact"
	appgate "easycode/internal/application/gate"
	"easycode/internal/application/runtime"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/sandbox"
	domainworkflow "easycode/internal/domain/workflow"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/specs"
	gatewayapi "easycode/internal/interfaces/gateway"
	sandboxapi "easycode/internal/interfaces/sandbox"
)

// harnessAgents are the agents with a harness (MARCO-4: Requisitos, Arquiteto, DBA, Designer and
// Desenvolvedor); the workflow in force has only their stages (specs.StandardWorkflow).
var harnessAgents = specs.StandardAgents

// runtimeParts is the agent runtime of the worker: executor, gate progress and the answer handler.
type runtimeParts struct {
	executor *runtime.Executor
	progress *appgate.Progress
	answers  *appwave.AnswerHandler
}

func newRuntime(cfg config.Config, pool *pgxpool.Pool, propagator propagation.TextMapPropagator, orchestrator *appworkflow.Orchestrator,
	workflows postgres.Workflow, wf specs.Workflow, logger *slog.Logger) (runtimeParts, error) {
	tx := postgres.NewTxRunner(pool)
	outbox := postgres.NewOutbox(pool)
	validator, err := specs.NewArtifactValidator()
	if err != nil {
		return runtimeParts{}, err
	}
	store := appartifact.NewStore(tx, specs.HeaderParser{}, validator, postgres.NewArtifacts(pool), outbox)
	ledger := postgres.NewLedger(pool)
	models := runtime.NewGatewayModels(gatewayapi.NewClient(cfg.Gateway.Socket, propagator), postgres.NewProviderKeys(pool), ledger, ledger)
	runner := sandboxapi.NewClient(cfg.Sandbox.Socket)
	agents := map[string]runtime.Agent{}
	for _, name := range harnessAgents {
		policy, err := specs.AgentPolicy(name)
		if err != nil {
			return runtimeParts{}, err
		}
		steps := runtime.NewFileAgent(models, cfg.Worker.MaxOutputTokens)
		if policy.Execute {
			steps = runtime.NewToolAgent(models, cfg.Worker.MaxOutputTokens, runner)
		}
		agents[name] = runtime.Agent{Steps: steps, Policy: &policy, Modes: stageModes(wf, name)}
	}
	progress := appgate.NewProgress(tx, postgres.NewGates(pool), store, validator, stageFlow{workflows, orchestrator}, outbox, wf.Definition)
	worker := cfg.Worker.ID
	if worker == "" {
		worker, _ = os.Hostname()
	}
	executor := runtime.NewExecutor(tx, ledger, agents, instruction, outbox, postgres.NewTelemetryNotifier(pool), store, progress,
		stageOutputs(wf), runtime.Settings{Worker: worker, Lease: cfg.Worker.WaveLease, MaxCorrections: cfg.Worker.MaxArtifactCorrections,
			RetryBackoff: 2 * time.Second, Poll: cfg.Worker.CommandPollInterval, Image: cfg.Sandbox.Image,
			ProjectLimits: sandbox.Limits{CPU: cfg.Sandbox.CPU, MemoryMiB: cfg.Sandbox.MemoryMiB, DiskMiB: cfg.Sandbox.DiskMiB, Time: cfg.Sandbox.CommandTime}}, logger).
		WithRepository(git.New(cfg.ReposDir), stageAttachments(wf))
	return runtimeParts{executor: executor, progress: progress, answers: appwave.NewAnswerHandler(postgres.NewWaves(pool, wf.Definition), outbox)}, nil
}

// stageFlow joins the stage writes of the workflow adapter and the orchestrator for the gate.
type stageFlow struct {
	postgres.Workflow
	*appworkflow.Orchestrator
}

func (f stageFlow) SetStageState(ctx context.Context, projectID, stage, state string) error {
	return f.Workflow.SetStageState(ctx, projectID, stage, state)
}

func instruction(agent string) (string, string, error) {
	defaults, err := specs.DefaultAgent(agent)
	return string(defaults.Instruction), defaults.InstructionHash, err
}

// stageModes is the agent mode in each stage the agent runs in (agent_mode of the workflow); a
// participant of a stage led by another agent gives its review opinion (PARECER).
func stageModes(wf specs.Workflow, agent string) map[string]string {
	modes := map[string]string{}
	for _, stage := range wf.Definition.Stages {
		switch {
		case stage.IsReviewer(agent):
			modes[stage.ID] = domainworkflow.ModeReview
		case stage.Agent == agent:
			modes[stage.ID] = stage.AgentMode
		}
	}
	return modes
}

// stageAttachments lists the output paths of a stage that are files of an artifact, not artifacts
// (attachment: true in the workflow, such as openapi.yaml and estrutura.md); a folder ends with "/".
func stageAttachments(wf specs.Workflow) func(string) []string {
	return func(stage string) []string {
		definition, ok := wf.Definition.Stage(stage)
		if !ok {
			return nil
		}
		var paths []string
		for _, output := range definition.Outputs {
			if output.Attachment {
				paths = append(paths, output.Path)
				if dir := path.Dir(output.Path); strings.HasSuffix(dir, "/api") {
					paths = append(paths, dir+"/")
				}
			}
		}
		return paths
	}
}

func stageOutputs(wf specs.Workflow) func(string) []string {
	return func(stage string) []string {
		definition, ok := wf.Definition.Stage(stage)
		if !ok {
			return nil
		}
		var types []string
		seen := map[string]bool{}
		for _, output := range definition.Outputs {
			if output.Required && output.Type != "" && !seen[output.Type] {
				seen[output.Type] = true
				types = append(types, output.Type)
			}
		}
		return types
	}
}

// runReconcile applies the effect of gate decisions when events commit and at every poll.
func runReconcile(ctx context.Context, jobs jobRunner, progress *appgate.Progress, wake <-chan struct{}, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		jobs.run(ctx, "apply_gate_decisions", true, func(ctx context.Context) ([]slog.Attr, error) {
			applied, err := progress.Reconcile(ctx)
			return []slog.Attr{slog.Int("applied", applied)}, err
		})
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}
