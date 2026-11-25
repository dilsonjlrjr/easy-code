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
	appfinding "easycode/internal/application/finding"
	appgate "easycode/internal/application/gate"
	"easycode/internal/application/runtime"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	domainartifact "easycode/internal/domain/artifact"
	"easycode/internal/domain/sandbox"
	domainworkflow "easycode/internal/domain/workflow"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/files"
	"easycode/internal/infrastructure/git"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/specs"
	gatewayapi "easycode/internal/interfaces/gateway"
	sandboxapi "easycode/internal/interfaces/sandbox"
)

// harnessAgents are the AI agents with a harness (the seven agents); with the orchestrator, which writes the
// RELEASE (MARCO-6), they run every stage of the workflow in force (specs.StandardWorkflow(specs.ReleaseAgents)).
var harnessAgents = specs.StandardAgents

// runtimeParts is the agent runtime of the worker: executor, gate progress, finding machine and the
// answer handler.
type runtimeParts struct {
	executor *runtime.Executor
	progress *appgate.Progress
	findings *appfinding.Service
	answers  *appwave.AnswerHandler
	// orchestrator applies the project budget and resumes the waves held by a resumed project (reconcile).
	orchestrator *appworkflow.Orchestrator
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
	gateway := gatewayapi.NewClient(cfg.Gateway.Socket, propagator)
	keys := postgres.NewProviderKeys(pool)
	models := runtime.NewGatewayModels(gateway, keys, ledger, ledger)
	runner := sandboxapi.NewClient(cfg.Sandbox.Socket)
	// ADR-23: a wave whose pair is of the claude-code provider runs through the Claude Code of the server,
	// its model calls through the proxy socket of the wave in the volume shared with the sandbox runner.
	claude := runtime.NewClaudeCodeAgent(runtime.NewModelProxy(cfg.Sandbox.AgentProxyDir, gateway, keys, ledger, ledger), runner)
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
		agents[name] = runtime.Agent{Steps: runtime.NewDispatch(steps, claude), Policy: &policy, Modes: stageModes(wf, name)}
	}
	governance, err := specs.Governance()
	if err != nil {
		return runtimeParts{}, err
	}
	progress, err := appgate.NewProgress(tx, postgres.NewGates(pool), store, validator, stageFlow{workflows, orchestrator}, outbox, wf.Definition, governance)
	if err != nil {
		return runtimeParts{}, err
	}
	repository := git.New(cfg.ReposDir)
	progress.WithRepository(repository, specs.HeaderParser{}).WithRelease(postgres.NewGates(pool), repository, specs.ArtifactRenderer{}, store)
	// US-08: the orchestrator consolidates the RELEASE in the release wave, with no model call.
	agents[runtime.OrchestratorAgent] = runtime.Agent{Steps: runtime.NewReleaseSteps(progress.ReleaseFile), WritablePaths: []string{"docs/release/"}}
	findings := appfinding.NewService(tx, postgres.NewFindings(pool), store, orchestrator, progress, postgres.NewWaves(pool, wf.Definition), outbox, wf.Definition)
	progress.WithHook(findings)
	worker := cfg.Worker.ID
	if worker == "" {
		worker, _ = os.Hostname()
	}
	executor := runtime.NewExecutor(tx, ledger, agents, instruction, outbox, postgres.NewTelemetryNotifier(pool), store, progress,
		stageOutputs(wf), runtime.Settings{Worker: worker, Lease: cfg.Worker.WaveLease, MaxCorrections: cfg.Worker.MaxArtifactCorrections,
			RetryBackoff: 2 * time.Second, Poll: cfg.Worker.CommandPollInterval, Image: cfg.Sandbox.Image,
			ProjectLimits: sandbox.Limits{CPU: cfg.Sandbox.CPU, MemoryMiB: cfg.Sandbox.MemoryMiB, DiskMiB: cfg.Sandbox.DiskMiB, Time: cfg.Sandbox.CommandTime}}, logger).
		WithRepository(repository, stageAttachments(wf)).WithAttachmentSpecs(stageAttachmentSpecs(wf)).WithSources(files.New(cfg.FilesDir)).WithFindings(findings).WithBudget(orchestrator)
	return runtimeParts{executor: executor, progress: progress, findings: findings, orchestrator: orchestrator,
		answers: appwave.NewAnswerHandler(postgres.NewWaves(pool, wf.Definition), outbox)}, nil
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
	if agent == runtime.OrchestratorAgent {
		return "", "", nil // the orchestrator has no instruction (permissoes.yaml)
	}
	defaults, err := specs.DefaultAgent(agent)
	return string(defaults.Instruction), defaults.InstructionHash, err
}

// stageModes is the agent mode in each stage or parallel activity the agent runs in (agent_mode of the
// workflow); a participant of a stage led by another agent gives its review opinion (PARECER).
func stageModes(wf specs.Workflow, agent string) map[string]string {
	return stageModesOf(wf.Definition, agent)
}

// stageModesOf is stageModes over a definition.
func stageModesOf(definition domainworkflow.Definition, agent string) map[string]string {
	modes := map[string]string{}
	for _, stage := range definition.Stages {
		switch {
		case stage.IsReviewer(agent):
			modes[stage.ID] = domainworkflow.ModeReview
		case stage.Agent == agent:
			modes[stage.ID] = stage.AgentMode
		}
	}
	for _, activity := range definition.Activities {
		if activity.Agent == agent {
			modes[activity.ID] = activity.AgentMode
		}
	}
	return modes
}

// stageAttachments lists the output paths of a stage that are files of an artifact, not artifacts
// (attachment: true in the workflow, such as openapi.yaml and estrutura.md); a folder ends with "/".
func stageAttachments(wf specs.Workflow) func(string) []string {
	return func(stage string) []string {
		outputs := stageOutputList(wf, stage)
		if outputs == nil {
			return nil
		}
		var paths []string
		for _, output := range outputs {
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

// stageAttachmentSpecs lists the attachment outputs of a stage with the artifact type they belong to
// (Artifact.attachments, PL-24); the api/ folder of openapi.yaml comes along, as in stageAttachments.
func stageAttachmentSpecs(wf specs.Workflow) func(string) []domainartifact.AttachmentSpec {
	return func(stage string) []domainartifact.AttachmentSpec {
		var out []domainartifact.AttachmentSpec
		for _, output := range stageOutputList(wf, stage) {
			if !output.Attachment || output.Type == "" {
				continue
			}
			out = append(out, domainartifact.AttachmentSpec{Type: output.Type, Path: output.Path})
			if dir := path.Dir(output.Path); strings.HasSuffix(dir, "/api") {
				out = append(out, domainartifact.AttachmentSpec{Type: output.Type, Path: dir + "/"})
			}
		}
		return out
	}
}

// stageOutputList is the output list of a stage or parallel activity.
func stageOutputList(wf specs.Workflow, stage string) []domainworkflow.Output {
	if definition, ok := wf.Definition.Stage(stage); ok {
		return definition.Outputs
	}
	if activity, ok := wf.Definition.Activity(stage); ok {
		return activity.Outputs
	}
	return nil
}

func stageOutputs(wf specs.Workflow) func(string) []string {
	return func(stage string) []string {
		var types []string
		seen := map[string]bool{}
		for _, output := range stageOutputList(wf, stage) {
			if output.Required && output.Type != "" && !output.Attachment && !seen[output.Type] {
				seen[output.Type] = true
				types = append(types, output.Type)
			}
		}
		return types
	}
}

// runReconcile applies the effect of gate and finding decisions when events commit and at every poll, the
// project budget (a lowered limit, PL-67) and the resume of the waves held by a resumed project.
func runReconcile(ctx context.Context, jobs jobRunner, progress *appgate.Progress, findings *appfinding.Service, orchestrator *appworkflow.Orchestrator,
	tx postgres.TxRunner, wake <-chan struct{}, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		jobs.run(ctx, "apply_gate_decisions", true, func(ctx context.Context) ([]slog.Attr, error) {
			applied, err := progress.Reconcile(ctx)
			return []slog.Attr{slog.Int("applied", applied)}, err
		})
		jobs.run(ctx, "apply_finding_decisions", true, func(ctx context.Context) ([]slog.Attr, error) {
			applied, err := findings.Reconcile(ctx)
			return []slog.Attr{slog.Int("applied", applied)}, err
		})
		jobs.run(ctx, "apply_project_budget", true, func(ctx context.Context) ([]slog.Attr, error) {
			checked, err := orchestrator.CheckBudgets(ctx, tx.InTx)
			return []slog.Attr{slog.Int("checked", checked)}, err
		})
		jobs.run(ctx, "resume_held_waves", true, func(ctx context.Context) ([]slog.Attr, error) {
			var resumed int
			err := tx.InTx(ctx, func(ctx context.Context) error {
				var err error
				resumed, err = orchestrator.ResumeHeld(ctx)
				return err
			})
			return []slog.Attr{slog.Int("resumed", resumed)}, err
		})
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}
