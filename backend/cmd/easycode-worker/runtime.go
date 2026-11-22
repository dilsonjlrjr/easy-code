package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/propagation"

	appartifact "easycode/internal/application/artifact"
	appgate "easycode/internal/application/gate"
	"easycode/internal/application/runtime"
	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/specs"
	gatewayapi "easycode/internal/interfaces/gateway"
)

// harnessAgents are the agents the runtime runs in MARCO-1 (Requisitos, D-E4-03); waves of other
// agents stay queued until their harness exists.
var harnessAgents = []string{"requirements-agent"}

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
	agents := map[string]runtime.Agent{}
	for _, name := range harnessAgents {
		defaults, err := specs.DefaultAgent(name)
		if err != nil {
			return runtimeParts{}, err
		}
		agents[name] = runtime.Agent{Steps: runtime.NewFileAgent(models, cfg.Worker.MaxOutputTokens), WritablePaths: writablePaths(defaults.Tools)}
	}
	progress := appgate.NewProgress(tx, postgres.NewGates(pool), store, validator, stageFlow{workflows, orchestrator}, outbox, wf.Definition)
	worker := cfg.Worker.ID
	if worker == "" {
		worker, _ = os.Hostname()
	}
	executor := runtime.NewExecutor(tx, ledger, agents, instruction, outbox, postgres.NewTelemetryNotifier(pool), store, progress,
		stageOutputs(wf), runtime.Settings{Worker: worker, Lease: cfg.Worker.WaveLease, MaxCorrections: cfg.Worker.MaxArtifactCorrections,
			RetryBackoff: 2 * time.Second, Poll: cfg.Worker.CommandPollInterval}, logger)
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

// writablePaths turns the write_file globs of permissoes.yaml into path prefixes.
func writablePaths(tools json.RawMessage) []string {
	var parsed struct {
		WriteFile []string `json:"write_file"`
	}
	_ = json.Unmarshal(tools, &parsed)
	var prefixes []string
	for _, glob := range parsed.WriteFile {
		prefixes = append(prefixes, strings.TrimSuffix(strings.TrimSuffix(glob, "**"), "/"))
	}
	return prefixes
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
