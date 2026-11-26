package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	permissionToolName = "permission"
	permissionPath     = "/easycode/v1/permission"
	permissionTimeout  = 30 * time.Second
	maxMCPLine         = 8 << 20
	// refusedUnchecked is the answer when the worker cannot be asked: the call is refused, never allowed.
	refusedUnchecked = "easycode: the tool call could not be checked"
)

// rpcMessage is one JSON-RPC 2.0 message of MCP.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// permissionInput is the input Claude Code gives the permission prompt tool.
type permissionInput struct {
	ToolName  string         `json:"tool_name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
}

// decision is the answer of the worker: behavior allow or deny, with the message of a refusal.
type decision struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message,omitempty"`
}

// decider asks the worker about one tool call.
type decider interface {
	decide(ctx context.Context, input permissionInput) decision
}

// serveMCP is the MCP stdio server: one JSON-RPC message per line, the permission tool only.
func serveMCP(stdin io.Reader, stdout io.Writer, worker decider) int {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 64<<10), maxMCPLine)
	encoder := json.NewEncoder(stdout)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			_ = encoder.Encode(rpcReply{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		if len(message.ID) == 0 {
			continue // a notification: no reply
		}
		_ = encoder.Encode(handle(message, worker))
	}
	return 0
}

func handle(message rpcMessage, worker decider) rpcReply {
	reply := rpcReply{JSONRPC: "2.0", ID: message.ID}
	switch message.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(message.Params, &params)
		if params.ProtocolVersion == "" {
			params.ProtocolVersion = "2024-11-05"
		}
		reply.Result = map[string]any{"protocolVersion": params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]string{"name": "easycode", "version": version}}
	case "ping":
		reply.Result = map[string]any{}
	case "tools/list":
		reply.Result = map[string]any{"tools": []any{map[string]any{
			"name":        permissionToolName,
			"description": "Permission prompt of the Easy Code platform: decides whether a tool call is allowed.",
			"inputSchema": map[string]any{"type": "object", "required": []string{"tool_name", "input"}, "properties": map[string]any{
				"tool_name": map[string]string{"type": "string"}, "input": map[string]string{"type": "object"},
				"tool_use_id": map[string]string{"type": "string"}}},
		}}}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments permissionInput `json:"arguments"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil || params.Name != permissionToolName {
			reply.Error = &rpcError{Code: -32602, Message: "unknown tool"}
			return reply
		}
		answer := worker.decide(context.Background(), params.Arguments)
		result := map[string]any{"behavior": answer.Behavior}
		if answer.Behavior == "allow" {
			result["updatedInput"] = params.Arguments.Input
		} else {
			result = map[string]any{"behavior": "deny", "message": orDefault(answer.Message, refusedUnchecked)}
		}
		text, _ := json.Marshal(result)
		reply.Result = map[string]any{"content": []any{map[string]string{"type": "text", "text": string(text)}}}
	default:
		reply.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return reply
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// permissionClient asks the worker through the loopback listener of exec, which forwards to the socket of
// the wave model proxy; the run token authenticates the request as it does the model calls.
type permissionClient struct {
	url    string
	token  string
	client *http.Client
}

func newPermissionClient(baseURL, token string) permissionClient {
	return permissionClient{url: strings.TrimSuffix(baseURL, "/") + permissionPath, token: token,
		client: &http.Client{Timeout: permissionTimeout}}
}

// decide refuses when the worker cannot answer: a call is never allowed without the platform.
func (c permissionClient) decide(ctx context.Context, input permissionInput) decision {
	refused := decision{Behavior: "deny", Message: refusedUnchecked}
	if c.token == "" || !strings.HasPrefix(c.url, "http://") {
		return refused
	}
	body, err := json.Marshal(input)
	if err != nil {
		return refused
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return refused
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.client.Do(request)
	if err != nil {
		return refused
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return refused
	}
	var answer decision
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&answer); err != nil {
		return refused
	}
	switch answer.Behavior {
	case "allow":
		return decision{Behavior: "allow"}
	case "deny":
		return decision{Behavior: "deny", Message: orDefault(answer.Message, refusedUnchecked)}
	}
	return refused
}
