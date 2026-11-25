package runtime_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/application/runtime"
	"easycode/internal/domain/provider"
)

func openProxy(t *testing.T) (*runtime.ProxySession, *fakeForwarder, *fakeLedger, *claudeClient) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ccp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	gateway, ledger := &fakeForwarder{}, &fakeLedger{}
	session, err := runtime.NewModelProxy(dir, gateway, sealedKeys{}, ledger, ledger).Open(ccWave(t), 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	socket := filepath.Join(dir, session.SocketDir, "model.sock")
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0o666 {
		t.Fatalf("the sandbox user reaches the socket: %v %v", info, err)
	}
	client := &claudeClient{ctx: context.Background(), token: session.Token, http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}}
	return session, gateway, ledger, client
}

// ADR-23: the socket only serves the Messages API of a model of the pair, with the run token.
func TestModelProxyRefusesOutsideItsScope(t *testing.T) {
	session, gateway, ledger, client := openProxy(t)
	wrong := *client
	wrong.token = "other"
	if status, body := wrong.messages("claude-sonnet", user("x")); status != http.StatusUnauthorized || !strings.Contains(body, "authentication_error") {
		t.Fatalf("wrong token: %d %s", status, body)
	}
	if status, _ := client.post("/v1/complete", map[string]any{"model": "claude-sonnet"}); status != http.StatusNotFound {
		t.Fatalf("path outside the allowlist: %d", status)
	}
	if status, body := client.messages("claude-haiku", user("x")); status != http.StatusBadRequest || !strings.Contains(body, "invalid_request_error") {
		t.Fatalf("model outside the pair: %d %s", status, body)
	}
	if len(gateway.paths) != 0 || len(ledger.steps) != 0 {
		t.Fatal("nothing refused reaches the gateway or the call record")
	}
	select {
	case event := <-session.Events():
		t.Fatalf("no event for a refused request: %+v", event)
	default:
	}
}

// A non streamed answer is read for its usage; counting tokens opens no call row.
func TestModelProxyReadsJSONAnswersAndCountsForFree(t *testing.T) {
	session, gateway, ledger, client := openProxy(t)
	gateway.queue(canned{http.StatusOK, "application/json", `{"input_tokens":12}`}, canned{http.StatusOK, "application/json", `{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}],` +
		`"usage":{"input_tokens":30,"output_tokens":4,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}`})
	if status, body := client.post("/v1/messages/count_tokens?beta=true", map[string]any{"model": "claude-opus"}); status != http.StatusOK || !strings.Contains(body, "12") {
		t.Fatalf("count tokens: %d %s", status, body)
	}
	if len(ledger.steps) != 0 {
		t.Fatal("count_tokens is not a model call")
	}
	if status, _ := client.post("/v1/messages", map[string]any{"model": "claude-opus", "messages": []any{user("x")}}); status != http.StatusOK {
		t.Fatalf("call: %d", status)
	}
	event := <-session.Events()
	call := event.Call
	if call == nil || call.Role != "reasoning" || call.Usage != (provider.Usage{Input: 30, Output: 4, CacheRead: 2, CacheWrite: 1}) || call.Summary != "pediu execute_command" {
		t.Fatalf("call: %+v", call)
	}
	if ledger.steps[0] != 5 || gateway.paths[0] != "v1/messages/count_tokens?beta=true" || gateway.paths[1] != "v1/messages?" {
		t.Fatalf("numbers and paths: %v %v", ledger.steps, gateway.paths)
	}
	client.post("/v1/messages", map[string]any{"model": "claude-opus", "messages": []any{user("x"),
		toolResult("t1", "Claude requested permissions to use Bash, but you haven't granted it yet.", true)}})
	tool := (<-session.Events()).Tool
	if tool == nil || tool.Tool != runtime.ToolExecuteCommand || tool.Allowed || tool.RefusalReason != "command_not_allowed" {
		t.Fatalf("a command outside the list is refused: %+v", tool)
	}
	if gateway.sessions[0].Provider != provider.ClaudeCode || gateway.sessions[0].Model != "claude-opus" {
		t.Fatalf("one gateway session per model of the pair: %+v", gateway.sessions)
	}
}

// A stream cut before message_stop is an aborted call with the usage seen so far (lost cost).
func TestModelProxyCutStreamIsAborted(t *testing.T) {
	session, gateway, _, client := openProxy(t)
	full := sseAnswer("", "", "")
	cut, _, _ := strings.Cut(full, "event: message_delta")
	gateway.queue(stream(cut))
	client.messages("claude-sonnet", user("x"))
	call := (<-session.Events()).Call
	if call.Result != "aborted" || call.Usage.Input != 10 {
		t.Fatalf("aborted call: %+v", call)
	}
}
