package runtime_test

import (
	"context"
	"encoding/json"
	"testing"

	"easycode/internal/application/runtime"
	"easycode/internal/domain/permission"
	"easycode/internal/domain/sandbox"
)

func permissionPolicy() permission.Policy {
	return permission.Policy{
		Agent: "development-agent", ReadPaths: []string{"**"}, WritePaths: []string{"app/**", "docs/dev/*.md"},
		TrackingPaths: []string{"docs/requisitos/RTM.md"}, Execute: true, Categories: []string{"go", "build"},
		Commands: map[string]sandbox.Category{
			"go":    {Argv0: []string{"go"}, Defaults: []string{"test *", "vet *"}},
			"build": {Argv0: []string{"make"}},
			"shell": {Shell: true}, // not in the categories of the agent
		},
	}
}

// ADR-24: every write and every command of a Claude Code run is decided by the agent policy in the worker.
func TestDecidePermission(t *testing.T) {
	w := runtime.WaveContext{Agent: "development-agent", Approved: map[string]string{
		"app/api.md": "architecture-agent", "app/own.md": "development-agent", "docs/requisitos/RTM.md": "requirements-agent"}}
	cases := []struct {
		name   string
		tool   string
		input  map[string]any
		reason string // "" = allowed
	}{
		{"read", "Read", map[string]any{"file_path": "/workspace/x"}, ""},
		{"write inside the globs", "Write", map[string]any{"file_path": "/workspace/app/main.go"}, ""},
		{"relative write", "Edit", map[string]any{"file_path": "docs/dev/notes.md"}, ""},
		{"write outside the globs", "Write", map[string]any{"file_path": "/workspace/docs/other.md"}, permission.RefusedPath},
		{"write outside the workspace", "Write", map[string]any{"file_path": "/etc/passwd"}, permission.RefusedPath},
		{"prefix of the workspace", "Write", map[string]any{"file_path": "/workspace2/app/x.go"}, permission.RefusedPath},
		{"dot dot", "Write", map[string]any{"file_path": "/workspace/app/../docs/x.md"}, permission.RefusedPath},
		{"platform folder", "Write", map[string]any{"file_path": "/workspace/.easycode/task.md"}, permission.RefusedPath},
		{"no path", "Write", map[string]any{}, permission.RefusedPath},
		{"approved artifact of another agent", "Edit", map[string]any{"file_path": "/workspace/app/api.md"}, permission.RefusedForeignArtifact},
		{"own approved artifact", "Edit", map[string]any{"file_path": "/workspace/app/own.md"}, ""},
		{"tracking fields", "Edit", map[string]any{"file_path": "/workspace/docs/requisitos/RTM.md"}, ""},
		{"command of a category", "Bash", map[string]any{"command": "go test ./..."}, ""},
		{"argv0 without defaults", "Bash", map[string]any{"command": "make build"}, ""},
		{"argument out of the defaults", "Bash", map[string]any{"command": "go run main.go"}, permission.RefusedCommand},
		{"command out of the categories", "Bash", map[string]any{"command": "curl http://x"}, permission.RefusedCommand},
		{"chained command", "Bash", map[string]any{"command": "go test ./... && rm -rf /"}, permission.RefusedCommand},
		{"substitution", "Bash", map[string]any{"command": "go test $(cat x)"}, permission.RefusedCommand},
		{"sh -c without a shell category", "Bash", map[string]any{"command": "sh -c ls"}, permission.RefusedCommand},
		{"empty command", "Bash", map[string]any{"command": " "}, permission.RefusedCommand},
		{"unknown tool", "WebFetch", map[string]any{"url": "http://x"}, permission.RefusedTool},
		{"subagent", "Task", map[string]any{}, permission.RefusedTool},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runtime.DecidePermission(w, permissionPolicy(), c.tool, c.input)
			if got.Allow != (c.reason == "") || got.Reason != c.reason {
				t.Fatalf("%s %v: %+v, want reason %q", c.tool, c.input, got, c.reason)
			}
			if !got.Allow && got.Message == "" {
				t.Fatal("a refusal carries the message the model reads")
			}
		})
	}
}

func TestDecidePermissionShellCategoryAndToolList(t *testing.T) {
	p := permissionPolicy()
	p.Categories = append(p.Categories, "shell")
	if got := runtime.DecidePermission(runtime.WaveContext{}, p, "Bash", map[string]any{"command": "go build ./... && go test ./..."}); !got.Allow {
		t.Fatalf("a shell category takes sh -c (D-38): %+v", got)
	}
	reader := permission.Policy{ReadPaths: []string{"**"}}
	for _, tool := range []string{"Write", "Bash"} {
		if got := runtime.DecidePermission(runtime.WaveContext{}, reader, tool, map[string]any{"file_path": "/workspace/a", "command": "ls"}); got.Allow || got.Reason != permission.RefusedTool {
			t.Fatalf("%s without the tool in the policy: %+v", tool, got)
		}
	}
}

// ADR-24: the permission prompt goes through the socket of the run with the run token; a refusal is the
// reason of the refused tool step, whatever the text of the tool_result.
func TestModelProxyAnswersThePermissionPrompt(t *testing.T) {
	f := newClaudeFixture(t, func(c *claudeClient) (string, map[string]string) {
		status, body := c.post("/easycode/v1/permission", map[string]any{"tool_name": "Write", "tool_use_id": "tu1",
			"input": map[string]any{"file_path": "/workspace/docs/outra-pasta/x.md", "content": "x"}})
		var answer map[string]string
		_ = json.Unmarshal([]byte(body), &answer)
		if status != 200 || answer["behavior"] != "deny" || answer["message"] == "" {
			return "unexpected deny " + body, nil
		}
		_, body = c.post("/easycode/v1/permission", map[string]any{"tool_name": "Write", "tool_use_id": "tu0",
			"input": map[string]any{"file_path": "/workspace/" + visao}})
		if err := json.Unmarshal([]byte(body), &answer); err != nil || answer["behavior"] != "allow" {
			return "unexpected allow " + body, nil
		}
		bad := *c
		bad.token = "other"
		if status, _ := bad.post("/easycode/v1/permission", map[string]any{"tool_name": "Write", "input": map[string]any{}}); status != 401 {
			return "token not checked", nil
		}
		c.messages("claude-sonnet", user("task"))
		c.messages("claude-sonnet", user("task"), toolResult("tu1", "any text", true))
		return resultLine(map[string]any{"status": "complete", "summary": "nada"}), nil
	})
	f.gateway.queue(stream(sseAnswer("tu1", "Write", `{"file_path":"/workspace/docs/outra-pasta/x.md","content":"x"}`)), stream(sseAnswer("", "", "")))
	w := ccWave(t)
	cp, _, _ := f.agent.Start(context.Background(), w)
	results, _, err := f.run(t, w, cp)
	if err != nil {
		t.Fatal(err)
	}
	refused := results[1].Tool
	if refused == nil || refused.Allowed || refused.RefusalReason != permission.RefusedPath || refused.Tool != runtime.ToolWriteFile {
		t.Fatalf("refusal: %+v (results %+v)", refused, results)
	}
	argv := f.runner.requests[0].Argv
	if !hasArg(argv, "--permission-prompt-tool", "mcp__easycode__permission") || !hasArg(argv, "--allowedTools", "Read,Glob,Grep") {
		t.Fatalf("argv: %v", argv)
	}
}

func hasArg(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}
