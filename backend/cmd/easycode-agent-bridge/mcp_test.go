package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fixedDecider struct {
	answer decision
	got    []permissionInput
}

func (f *fixedDecider) decide(_ context.Context, input permissionInput) decision {
	f.got = append(f.got, input)
	return f.answer
}

func mcpSession(t *testing.T, worker decider, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	serveMCP(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, worker)
	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var reply map[string]any
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("reply %q: %v", line, err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func toolText(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	content := reply["result"].(map[string]any)["content"].([]any)
	var decided map[string]any
	if err := json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &decided); err != nil {
		t.Fatal(err)
	}
	return decided
}

// ADR-24: the MCP server answers the handshake and the permission tool with the decision of the worker.
func TestMCPPermissionTool(t *testing.T) {
	worker := &fixedDecider{answer: decision{Behavior: "allow"}}
	replies := mcpSession(t, worker,
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"permission","arguments":{"tool_name":"Write","input":{"file_path":"/workspace/a"},"tool_use_id":"tu1"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"server/discover"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"other","arguments":{}}}`,
		`not json`)
	if len(replies) != 6 {
		t.Fatalf("one reply per request, none per notification: %v", replies)
	}
	if v := replies[0]["result"].(map[string]any)["protocolVersion"]; v != "2025-06-18" {
		t.Fatalf("initialize: %v", replies[0])
	}
	if tools := replies[1]["result"].(map[string]any)["tools"].([]any); len(tools) != 1 {
		t.Fatalf("tools: %v", tools)
	}
	allowed := toolText(t, replies[2])
	if allowed["behavior"] != "allow" || allowed["updatedInput"].(map[string]any)["file_path"] != "/workspace/a" {
		t.Fatalf("allow keeps the input: %v", allowed)
	}
	if len(worker.got) != 1 || worker.got[0].ToolUseID != "tu1" || worker.got[0].ToolName != "Write" {
		t.Fatalf("worker asked with %v", worker.got)
	}
	if replies[3]["error"] == nil || replies[4]["error"] == nil || replies[5]["error"] == nil {
		t.Fatalf("unknown method, unknown tool and bad JSON are errors: %v", replies[3:])
	}
	denied := toolText(t, mcpSession(t, &fixedDecider{answer: decision{Behavior: "deny"}},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"permission","arguments":{"tool_name":"Bash","input":{"command":"ls"}}}}`)[0])
	if denied["behavior"] != "deny" || denied["message"] != refusedUnchecked || denied["updatedInput"] != nil {
		t.Fatalf("deny: %v", denied)
	}
}

// The client sends the run token and refuses whenever the worker does not answer allow or deny.
func TestPermissionClient(t *testing.T) {
	answer := `{"behavior":"allow"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != permissionPath || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()
	input := permissionInput{ToolName: "Write", Input: map[string]any{"file_path": "/workspace/a"}}
	if got := newPermissionClient(server.URL+"/", "tok").decide(context.Background(), input); got.Behavior != "allow" {
		t.Fatalf("allow: %v", got)
	}
	answer = `{"behavior":"deny","message":"no"}`
	if got := newPermissionClient(server.URL, "tok").decide(context.Background(), input); got.Behavior != "deny" || got.Message != "no" {
		t.Fatalf("deny: %v", got)
	}
	for name, client := range map[string]permissionClient{
		"wrong token": newPermissionClient(server.URL, "bad"),
		"no token":    newPermissionClient(server.URL, ""),
		"no worker":   newPermissionClient("http://127.0.0.1:1", "tok"),
		"not http":    newPermissionClient("file:///x", "tok"),
	} {
		if got := client.decide(context.Background(), input); got.Behavior != "deny" {
			t.Fatalf("%s: %v", name, got)
		}
	}
	answer = `{"behavior":"maybe"}`
	if got := newPermissionClient(server.URL, "tok").decide(context.Background(), input); got.Behavior != "deny" {
		t.Fatalf("unknown behavior: %v", got)
	}
}
