package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"easycode/internal/application/runtime"
	appsandbox "easycode/internal/application/sandbox"
	"easycode/internal/domain/sandbox"
)

// Client is the worker side of easycode-sandbox (execute_command of the harness).
type Client struct {
	http *http.Client
}

var _ runtime.CommandRunner = (*Client)(nil)

// NewClient builds the client of the runner socket (/run/easycode-sandbox/runner.sock).
func NewClient(socket string) *Client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}
	return &Client{http: &http.Client{Transport: &http.Transport{DialContext: dial, DisableCompression: true}}}
}

// RunnerError is a refusal of the runner (problem type).
type RunnerError struct {
	Status int
	Type   string
}

func (e *RunnerError) Error() string { return fmt.Sprintf("sandbox runner: %d %s", e.Status, e.Type) }

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://sandbox"+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		var problem struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &problem)
		return nil, &RunnerError{Status: resp.StatusCode, Type: strings.TrimPrefix(problem.Type, "urn:easycode:problem:")}
	}
	return raw, nil
}

// Health asks the runner whether it can run (GET /v1/health).
func (c *Client) Health(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/v1/health", "", nil)
	return err
}

// ClaudeCodeStatus says whether agent sessions can run and with which Claude Code version (ADR-23).
type ClaudeCodeStatus = appsandbox.ClaudeCodeStatus

// ClaudeCode reports whether agent sessions can run (GET /v1/claude-code).
func (c *Client) ClaudeCode(ctx context.Context) (ClaudeCodeStatus, error) {
	raw, err := c.do(ctx, http.MethodGet, "/v1/claude-code", "", nil)
	if err != nil {
		return ClaudeCodeStatus{}, err
	}
	var status ClaudeCodeStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return ClaudeCodeStatus{}, err
	}
	return status, nil
}

// Run implements runtime.CommandRunner: prepare the workspace from the files of the wave, execute
// (the dependency action goes to its own route, ADR-07), fetch the full output and collect the files.
// Canceling ctx aborts the execution in the runner.
func (c *Client) Run(ctx context.Context, request sandbox.ExecutionRequest, files map[string]string) (runtime.RunOutcome, error) {
	contents := make(map[string][]byte, len(files))
	for p, content := range files {
		contents[p] = []byte(content)
	}
	archive, err := appsandbox.Archive(contents)
	if err != nil {
		return runtime.RunOutcome{}, err
	}
	project := "/v1/workspaces/" + url.PathEscape(request.ProjectID)
	if _, err := c.do(ctx, http.MethodPut, project, "application/x-tar", bytes.NewReader(archive)); err != nil {
		return runtime.RunOutcome{}, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return runtime.RunOutcome{}, err
	}
	route := "/v1/executions"
	if request.Category == sandbox.CategoryDependencies {
		route = "/v1/dependencies"
	}
	raw, err := c.do(ctx, http.MethodPost, route, "application/json", bytes.NewReader(body))
	if err != nil {
		if ctx.Err() != nil {
			_, _ = c.do(context.WithoutCancel(ctx), http.MethodDelete, "/v1/executions/"+url.PathEscape(request.ExecutionID), "", nil)
		}
		return runtime.RunOutcome{}, err
	}
	var outcome runtime.RunOutcome
	if err := json.Unmarshal(raw, &outcome.Result); err != nil {
		return runtime.RunOutcome{}, err
	}
	if outcome.Output, err = c.do(ctx, http.MethodGet, "/v1/executions/"+url.PathEscape(request.ExecutionID)+"/output", "", nil); err != nil {
		return runtime.RunOutcome{}, err
	}
	if outcome.Result.EndReason != sandbox.EndCompleted {
		return outcome, nil
	}
	raw, err = c.do(ctx, http.MethodPost, project+"/collection", "application/json", nil)
	if err != nil {
		return runtime.RunOutcome{}, err
	}
	var collection Collection
	if err := json.Unmarshal(raw, &collection); err != nil {
		return runtime.RunOutcome{}, err
	}
	collected := make(map[string][]byte, len(collection.Files))
	for _, f := range collection.Files {
		content, err := base64.StdEncoding.DecodeString(f.Content)
		if err == nil {
			collected[f.Path] = content
		}
	}
	outcome.Files = runtime.AcceptCollected(collected)
	return outcome, nil
}
