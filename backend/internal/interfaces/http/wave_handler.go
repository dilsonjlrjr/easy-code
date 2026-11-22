package http

import (
	"context"
	"encoding/json"
	"time"

	"github.com/valyala/fasthttp"

	appwave "easycode/internal/application/wave"
	appworkflow "easycode/internal/application/workflow"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/provider"
)

// WaveUseCases are the wave reads and the answer to a question (US-09, US-10, US-11, US-36).
type WaveUseCases interface {
	Authorize(ctx context.Context, principal identity.Principal, projectID string) error
	List(ctx context.Context, principal identity.Principal, projectID, stage, agent string, states []string, limit int) ([]appwave.Summary, error)
	Get(ctx context.Context, principal identity.Principal, projectID, code string) (appwave.View, error)
	Questions(ctx context.Context, principal identity.Principal, projectID, code string) ([]appwave.QuestionView, error)
	Checkpoints(ctx context.Context, principal identity.Principal, projectID, code string) ([]appwave.CheckpointView, error)
	Calls(ctx context.Context, principal identity.Principal, projectID, code, kind string) ([]appwave.CallView, error)
	Answer(ctx context.Context, principal identity.Principal, projectID, waveCode, questionCode string, answers map[int]string, key, bodyHash string) (appworkflow.CommandView, error)
	SandboxExecutions(ctx context.Context, principal identity.Principal, projectID, code string, limit int, after string) ([]appwave.SandboxExecution, error)
	ExecutionOutput(ctx context.Context, principal identity.Principal, projectID, code, executionID string) (appwave.ExecutionOutput, error)
}

type tokensJSON struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	ReadCache  int64 `json:"read_cache"`
	WriteCache int64 `json:"write_cache"`
}

func tokensOf(u provider.Usage) tokensJSON {
	return tokensJSON{Input: int64(u.Input), Output: int64(u.Output), ReadCache: int64(u.CacheRead), WriteCache: int64(u.CacheWrite)}
}

type detailedCostJSON struct {
	Useful        moneyJSON `json:"useful"`
	Lost          moneyJSON `json:"lost"`
	Total         moneyJSON `json:"total"`
	EstimatedLost moneyJSON `json:"estimated_lost"`
}

func costOf(c appwave.Cost) detailedCostJSON {
	estimated := "0"
	if c.EstimatedLost {
		estimated = c.Lost
	}
	return detailedCostJSON{Useful: moneyJSON{c.Useful, c.Currency}, Lost: moneyJSON{c.Lost, c.Currency},
		Total: moneyJSON{addDecimal(c.Useful, c.Lost), c.Currency}, EstimatedLost: moneyJSON{estimated, c.Currency}}
}

type waveSummaryJSON struct {
	ID            string     `json:"id"`
	Stage         string     `json:"stage"`
	Agent         string     `json:"agent"`
	State         string     `json:"state"`
	Purpose       string     `json:"purpose"`
	Finding       *string    `json:"finding"`
	Attempt       int        `json:"attempt"`
	Task          *string    `json:"task"`
	ConfigVersion int        `json:"agent_configuration_version,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at"`
	CostTotal     moneyJSON  `json:"cost_total"`
}

func waveSummaryOf(s appwave.Summary) waveSummaryJSON {
	return waveSummaryJSON{ID: s.Code, Stage: s.Stage, Agent: s.Agent, State: s.State, Purpose: s.Purpose, Attempt: s.Attempt, Task: optional(s.Task),
		ConfigVersion: s.ConfigVersion, CreatedAt: s.CreatedAt, StartedAt: s.StartedAt, EndedAt: s.EndedAt,
		CostTotal: moneyJSON{addDecimal(s.Cost.Useful, s.Cost.Lost), s.Cost.Currency}}
}

type expectedOutputJSON struct {
	Type     string  `json:"type"`
	Artifact *string `json:"artifact"`
	Version  *int    `json:"version"`
	Status   string  `json:"status"`
}

type waveJSON struct {
	waveSummaryJSON
	Models           pairJSON             `json:"models"`
	Versions         map[string]any       `json:"versions"`
	Tokens           tokensJSON           `json:"tokens"`
	Cost             detailedCostJSON     `json:"cost"`
	Time             map[string]int64     `json:"time"`
	Result           *string              `json:"result"`
	FailureCause     *string              `json:"failure_cause"`
	ArtifactErrors   json.RawMessage      `json:"artifact_errors"`
	Pause            *struct{}            `json:"pause"`
	LastCheckpoint   *int                 `json:"last_checkpoint"`
	QuestionOpen     *string              `json:"question_open"`
	ExpectedOutputs  []expectedOutputJSON `json:"expected_outputs"`
	GitCommit        *string              `json:"git_commit"`
	Attempts         []any                `json:"attempts"`
	AvailableActions []string             `json:"available_actions"`
}

func waveOf(v appwave.View) waveJSON {
	out := waveJSON{
		waveSummaryJSON: waveSummaryOf(v.Summary),
		Models: pairJSON{Reasoning: &modelRefJSON{Provider: v.Reasoning.Provider, Model: v.Reasoning.Model},
			Execution: &modelRefJSON{Provider: v.Execution.Provider, Model: v.Execution.Model}},
		Versions: map[string]any{"instructions": v.InstructionVersion, "agent_configuration": v.ConfigVersion, "skills": []any{}},
		Tokens:   tokensOf(v.Tokens), Cost: costOf(v.Cost),
		Time:         map[string]int64{"execution_ms": v.ExecutionMs, "cycle_ms": v.CycleMs},
		FailureCause: optional(v.FailureCause), ArtifactErrors: v.ArtifactErrors, LastCheckpoint: v.LastCheckpoint,
		QuestionOpen: optional(v.QuestionOpen), ExpectedOutputs: []expectedOutputJSON{}, Attempts: []any{}, AvailableActions: []string{},
		GitCommit: optional(v.GitCommit),
	}
	if v.PriceTable != "" {
		out.Versions["price_table"] = v.PriceTable
	}
	if v.InputsDigest != "" {
		out.Versions["inputs_summary_sha256"] = v.InputsDigest
	}
	if out.ArtifactErrors == nil {
		out.ArtifactErrors = json.RawMessage("[]")
	}
	switch v.State {
	case "completed":
		out.Result = optional("success")
	case "failed", "canceled":
		out.Result = optional(v.State)
	case "awaiting_user":
		out.AvailableActions = []string{"answer"}
	}
	for _, o := range v.Outputs {
		item := expectedOutputJSON{Type: o.Type, Artifact: optional(o.Artifact), Version: optional(o.Version), Status: o.Status}
		out.ExpectedOutputs = append(out.ExpectedOutputs, item)
	}
	return out
}

func wavePath(rc *fasthttp.RequestCtx) (string, string, error) {
	projectID, err := pathUUID(rc, "projectId")
	return projectID, PathParam(rc, "waveId"), err
}

func (a *api) listWaves(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	limit, err := queryLimit(rc)
	if err != nil {
		return err
	}
	list, err := a.deps.Waves.List(RequestContext(rc), principal, projectID, string(rc.QueryArgs().Peek("stage")),
		string(rc.QueryArgs().Peek("agent")), queryList(rc, "state"), limit)
	if err != nil {
		return err
	}
	items := make([]waveSummaryJSON, 0, len(list))
	for _, s := range list {
		items = append(items, waveSummaryOf(s))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	return nil
}

func (a *api) getWave(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := wavePath(rc)
	if err != nil {
		return err
	}
	view, err := a.deps.Waves.Get(RequestContext(rc), principal, projectID, code)
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, waveOf(view))
	return nil
}

type questionItemJSON struct {
	Number     int               `json:"number"`
	Text       string            `json:"text"`
	Suggestion string            `json:"suggestion,omitempty"`
	Origin     map[string]string `json:"origin"`
}

func (a *api) listWaveQuestions(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := wavePath(rc)
	if err != nil {
		return err
	}
	questions, err := a.deps.Waves.Questions(RequestContext(rc), principal, projectID, code)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(questions))
	for _, q := range questions {
		list := make([]questionItemJSON, 0, len(q.Items))
		answers := []map[string]any{}
		for _, item := range q.Items {
			entry := questionItemJSON{Number: item.Number, Text: item.Text, Suggestion: item.Suggestion}
			if item.Source != "" {
				entry.Origin = map[string]string{"source": item.Source, "excerpt": item.Excerpt}
			}
			list = append(list, entry)
			if item.Answer != "" {
				answers = append(answers, map[string]any{"number": item.Number, "text": item.Answer})
			}
		}
		var answer any
		if q.AnsweredAt != nil && q.AnsweredBy != nil {
			answer = map[string]any{"record": decisionJSON{Per: actorJSON{Type: "human", User: &userRefJSON{ID: q.AnsweredBy.Subject, Name: q.AnsweredBy.Name}},
				When: *q.AnsweredAt, Origin: optional("web")}, "answers": answers}
		}
		items = append(items, map[string]any{"id": q.Code, "items": list, "opened_at": q.OpenedAt, "answer": answer})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items})
	return nil
}

// answerQuestion is answerQuestion: 202 with the answer command (US-10 CA-1).
func (a *api) answerQuestion(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := wavePath(rc)
	if err != nil {
		return err
	}
	var body struct {
		Answers []struct {
			Number int    `json:"number"`
			Text   string `json:"text"`
		} `json:"answers"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	answers := map[int]string{}
	for _, item := range body.Answers {
		if item.Number < 1 {
			return failure.Invalid(failure.FieldError{Field: "answers.number", Message: "range"})
		}
		answers[item.Number] = item.Text
	}
	req, err := idempotencyRequest(rc, principal, "answerQuestion")
	if err != nil {
		return err
	}
	question := PathParam(rc, "questionId")
	return a.idempotent(rc, principal, "answerQuestion", func(ctx context.Context) (created, error) {
		queued, err := a.deps.Waves.Answer(ctx, principal, projectID, code, question, answers, req.Key, req.BodyHash)
		if err != nil {
			return created{}, err
		}
		return created{status: fasthttp.StatusAccepted, body: a.commandOf(rc, queued), location: commandLocation(projectID, queued.ID)}, nil
	})
}

func (a *api) listCheckpoints(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := wavePath(rc)
	if err != nil {
		return err
	}
	list, err := a.deps.Waves.Checkpoints(RequestContext(rc), principal, projectID, code)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(list))
	for _, c := range list {
		items = append(items, map[string]any{"number": c.Number, "attempt": c.Attempt, "after": c.After, "written_at": c.WrittenAt,
			"accumulated_tokens": tokensOf(c.Tokens), "accumulated_cost": costOf(c.Cost)})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	return nil
}

func (a *api) listWaveCalls(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := wavePath(rc)
	if err != nil {
		return err
	}
	calls, err := a.deps.Waves.Calls(RequestContext(rc), principal, projectID, code, string(rc.QueryArgs().Peek("type")))
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		item := map[string]any{"number": c.Number, "type": c.Type, "start": c.Start, "duration_ms": c.DurationMs, "result": c.Result,
			"lost": c.Lost, "attempt": c.Attempt, "summary": optional(c.Summary)}
		if c.Type == "model" {
			item["provider"], item["model"], item["role"], item["tokens"] = c.Provider, c.Model, c.Role, tokensOf(c.Tokens)
			item["cost"], item["lost_cost"], item["estimated_cost"], item["price_table_version"] = moneyJSON{c.Cost, c.Currency}, c.Lost, c.Estimated, c.PriceVersion
			if c.Error != "" {
				item["error"] = c.Error
			}
		} else {
			item["tool"] = c.Tool
			if c.Refusal != "" {
				item["refusal_reason"] = c.Refusal
			}
		}
		items = append(items, item)
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	return nil
}

// addDecimal adds two non-negative decimals in text exactly.
func addDecimal(a, b string) string {
	return decimalSum(a, b)
}
