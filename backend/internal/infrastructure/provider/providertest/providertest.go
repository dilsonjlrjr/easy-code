// Package providertest serves scripted stand-ins of the Anthropic Messages API and the OpenAI Chat
// Completions API on httptest servers, so tests drive the real adapters end to end without the real
// providers (US-24 CA-2: the same wave with both). Only non-streaming calls and the models list are
// served; every answer has the same usage, so costs compare across providers.
package providertest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"

	"easycode/internal/domain/provider"
)

// Usage is the usage of every scripted answer, as the adapter reports it.
var Usage = provider.Usage{Input: 1000, Output: 200, CacheRead: 100}

// Script returns the answer of the n-th call (from 0) of the server.
type Script func(call int) provider.Message

// Server is a scripted provider API.
type Server struct {
	*httptest.Server
	mu    sync.Mutex
	calls int
	// Bodies are the decoded request bodies of the model calls, in order.
	Bodies []map[string]any
}

// Calls counts the model calls served.
func (s *Server) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *Server) next(r *http.Request) (int, bool) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.calls
	s.calls++
	s.Bodies = append(s.Bodies, body)
	return n, true
}

// NewAnthropic serves POST /v1/messages and GET /v1/models for the key.
func NewAnthropic(key string, script Script) *Server {
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"data":[{"id":"scripted"}],"has_more":false}`)
			return
		}
		n, ok := s.next(r)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer := script(n)
		content := []map[string]any{}
		if answer.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": answer.Text})
		}
		for _, call := range answer.Calls {
			content = append(content, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": call.Args})
		}
		stop := "end_turn"
		if len(answer.Calls) > 0 {
			stop = "tool_use"
		}
		w.Header().Set("request-id", "req_scripted")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg_scripted", "type": "message", "role": "assistant", "content": content,
			"stop_reason": stop, "usage": map[string]int{"input_tokens": Usage.Input, "output_tokens": Usage.Output,
				"cache_read_input_tokens": Usage.CacheRead, "cache_creation_input_tokens": Usage.CacheWrite}})
	}))
	return s
}

// NewOpenAI serves POST /v1/chat/completions and GET /v1/models for the key.
func NewOpenAI(key string, script Script) *Server {
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided.","type":"invalid_request_error","code":"invalid_api_key"}}`)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"scripted","object":"model"}]}`)
			return
		}
		n, ok := s.next(r)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer := script(n)
		message := map[string]any{"role": "assistant", "content": nil}
		if answer.Text != "" {
			message["content"] = answer.Text
		}
		var calls []map[string]any
		for _, call := range answer.Calls {
			calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Args)}})
		}
		finish := "stop"
		if len(calls) > 0 {
			message["tool_calls"] = calls
			finish = "tool_calls"
		}
		w.Header().Set("x-request-id", "req_scripted")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-scripted", "object": "chat.completion",
			"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finish}},
			"usage": map[string]any{"prompt_tokens": Usage.Input + Usage.CacheRead, "completion_tokens": Usage.Output,
				"prompt_tokens_details": map[string]int{"cached_tokens": Usage.CacheRead}}})
	}))
	return s
}
