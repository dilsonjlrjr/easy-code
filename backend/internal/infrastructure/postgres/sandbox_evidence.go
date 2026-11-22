package postgres

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"
	"time"

	"easycode/internal/application/runtime"
	"easycode/internal/domain/sandbox"
)

// recordExecution writes a sandbox execution with its evidence (US-41 CA-1, CA-2, CA-3): the command,
// exit code, duration, end reason, test counts, the first 64 KiB for display and the full output
// compressed for the download. The public id is the execution id of the runner (idempotency).
func (l Ledger) recordExecution(ctx context.Context, lease runtime.Lease, attempt, step int, tool *runtime.ToolRecord, durationMs int64) error {
	execution := tool.Execution
	result := execution.Result
	status := "error"
	switch {
	case sandbox.LimitExceeded(result.EndReason):
		status = "limit_exceeded"
	case result.Succeeded():
		status = "success"
	}
	endReason := result.EndReason
	if endReason == "" {
		endReason = sandbox.EndExecutorError
	}
	compressed, err := compress(execution.Output)
	if err != nil {
		return err
	}
	var total, passed, failed, skipped any
	if t := result.Tests; t != nil {
		total, passed, failed, skipped = t.Total, t.Passed, t.Failed, t.Skipped
	}
	var digest any
	if strings.HasPrefix(result.ImageDigest, "sha256:") && len(result.ImageDigest) == 71 {
		digest = result.ImageDigest
	}
	var sum any
	if len(result.OutputSHA256) == 64 {
		sum = result.OutputSHA256
	}
	var outputBytes any
	if sum != nil {
		outputBytes = result.OutputBytes
	}
	started := result.Start
	if started.IsZero() {
		started = time.Now()
	}
	duration := result.Duration.Milliseconds()
	if duration == 0 {
		duration = durationMs
	}
	_, err = conn(ctx, l.pool).Exec(WithOperation(ctx, "tool_executions.insert_sandbox"), `
INSERT INTO easycode.tool_executions (public_id, wave_id, attempt, step, tool, permission, status, category, profile, image_digest, end_reason,
  command, summary, exit_code, duration_ms, output, output_start, output_bytes, output_sha256, tests_total, tests_passed, tests_failed,
  tests_skipped, started_at, finished_at)
VALUES ($1::uuid, $2, $3, $4, $5, 'allowed', $6, $7, nullif($8, ''), $9, $10, $11, nullif(left($12, 200), ''), $13, $14, $15, $16, $17, $18,
  $19, $20, $21, $22, $23, now())`,
		execution.ID, lease.WaveID, attempt, step, tool.Tool, status, execution.Category, result.Profile, digest, endReason,
		execution.Command, tool.Summary, result.ExitCode, duration, compressed, cleanText(result.OutputStart, sandbox.OutputStartBytes),
		outputBytes, sum, total, passed, failed, skipped, started)
	if err != nil {
		return fmt.Errorf("record sandbox execution: %w", classify(err))
	}
	return nil
}

func compress(output []byte) ([]byte, error) {
	if output == nil {
		return nil, nil
	}
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(output); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// cleanText makes untrusted output storable as text: valid UTF-8, without NUL, at most limit bytes.
func cleanText(text string, limit int) string {
	text = strings.ToValidUTF8(strings.ReplaceAll(text, "\x00", ""), "?")
	for len(text) > limit {
		text = strings.ToValidUTF8(text[:limit], "")
	}
	return text
}

// RecordCommit implements runtime.Ledger: the commit of the wave and of the artifact versions it wrote
// (git_commit goes from null to the hash once, US-42 CA-2).
func (l Ledger) RecordCommit(ctx context.Context, lease runtime.Lease, sha string, versions []runtime.CommittedVersion) error {
	q := conn(ctx, l.pool)
	if _, err := q.Exec(WithOperation(ctx, "waves.record_commit"), `UPDATE easycode.waves SET git_commit = $2 WHERE id = $1`, lease.WaveID, sha); err != nil {
		return fmt.Errorf("record wave commit: %w", classify(err))
	}
	for _, v := range versions {
		_, err := q.Exec(WithOperation(ctx, "artifact_versions.record_commit"), `
UPDATE easycode.artifact_versions av SET git_commit = $4
  FROM easycode.artifacts a, easycode.waves w
 WHERE av.artifact_id = a.id AND a.project_id = w.project_id AND w.id = $1 AND a.code = $2 AND av.version = $3 AND av.git_commit IS NULL`,
			lease.WaveID, v.Code, v.Version, sha)
		if err != nil {
			return fmt.Errorf("record artifact commit: %w", classify(err))
		}
	}
	return nil
}
