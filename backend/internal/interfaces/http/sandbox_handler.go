package http

import (
	"bytes"
	"compress/gzip"
	"io"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

type sandboxTestsJSON struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Ignored int `json:"ignored"`
}

type sandboxExecutionJSON struct {
	ID           string            `json:"id"`
	Command      string            `json:"command"`
	Category     string            `json:"category"`
	Profile      string            `json:"profile,omitempty"`
	Start        time.Time         `json:"start"`
	DurationMs   int64             `json:"duration_ms"`
	ExitCode     *int              `json:"exit_code"`
	EndReason    string            `json:"end_reason"`
	Tests        *sandboxTestsJSON `json:"tests"`
	OutputStart  string            `json:"output_start"`
	OutputBytes  int64             `json:"output_bytes"`
	OutputSHA256 string            `json:"output_sha256,omitempty"`
	ImageDigest  string            `json:"image_digest,omitempty"`
}

func value(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// listSandboxExecutions is listSandboxExecutions (US-41 CA-1, CA-2, CA-3): each command with exit code,
// duration, end reason, test counts and the start of the output (untrusted content).
func (a *api) listSandboxExecutions(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := wavePath(rc)
	if err != nil {
		return err
	}
	limit, err := queryLimit(rc)
	if err != nil {
		return err
	}
	after := string(rc.QueryArgs().Peek("cursor"))
	executions, err := a.deps.Waves.SandboxExecutions(RequestContext(rc), principal, projectID, code, limit+1, after)
	if err != nil {
		return err
	}
	var next *string
	if len(executions) > limit {
		executions = executions[:limit]
		next = optional(executions[limit-1].ID)
	}
	items := make([]sandboxExecutionJSON, 0, len(executions))
	for _, e := range executions {
		item := sandboxExecutionJSON{ID: e.ID, Command: e.Command, Category: e.Category, Profile: e.Profile, Start: e.Start, DurationMs: e.DurationMs,
			ExitCode: e.ExitCode, EndReason: e.EndReason, OutputStart: e.OutputStart, OutputBytes: e.OutputBytes, OutputSHA256: e.OutputSHA256, ImageDigest: e.ImageDigest}
		if e.TestsTotal != nil {
			item.Tests = &sandboxTestsJSON{Total: *e.TestsTotal, Passed: value(e.TestsPassed), Failed: value(e.TestsFailed), Ignored: value(e.TestsSkipped)}
		}
		items = append(items, item)
	}
	a.responder.JSON(rc, fasthttp.StatusOK, map[string]any{"items": items, "next_cursor": next})
	return nil
}

// downloadExecutionOutput is downloadExecutionOutput (US-41 CA-1): the full output as an attachment,
// never rendered by the browser (untrusted content).
func (a *api) downloadExecutionOutput(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := wavePath(rc)
	if err != nil {
		return err
	}
	executionID, err := pathUUID(rc, "executionId")
	if err != nil {
		return err
	}
	output, err := a.deps.Waves.ExecutionOutput(RequestContext(rc), principal, projectID, code, executionID)
	if err != nil {
		return err
	}
	var body []byte
	if len(output.Compressed) > 0 {
		reader, err := gzip.NewReader(bytes.NewReader(output.Compressed))
		if err != nil {
			return failure.New(failure.KindInternal, failure.CodeInternal, err)
		}
		if body, err = io.ReadAll(reader); err != nil {
			return failure.New(failure.KindInternal, failure.CodeInternal, err)
		}
	}
	rc.Response.Header.Set("Content-Disposition", `attachment; filename="`+output.FileName+`"`)
	rc.Response.Header.Set("X-Content-Type-Options", "nosniff")
	rc.SetContentType("text/plain; charset=utf-8")
	rc.SetStatusCode(fasthttp.StatusOK)
	rc.SetBody(body)
	return nil
}
