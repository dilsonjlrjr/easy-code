package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/provider/openai"
)

const testKey = "sk-proj-test-0123456789abcdef"

// fakeAPI records the last request and answers with handler. It never reaches the real API.
type fakeAPI struct {
	server  *httptest.Server
	request *http.Request
	body    map[string]any
}

func newFakeAPI(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *fakeAPI {
	t.Helper()
	api := &fakeAPI{}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.request = r
		raw, _ := io.ReadAll(r.Body)
		api.body = nil
		_ = json.Unmarshal(raw, &api.body)
		if r.Header.Get("authorization") != "Bearer "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided: sk-proj-****wxyz.","type":"invalid_request_error","code":"invalid_api_key"}}`)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(api.server.Close)
	return api
}

func (f *fakeAPI) adapter() *openai.Provider {
	return openai.New(f.server.URL, f.server.Client(), 5*time.Second)
}

func keyed(key string) context.Context {
	return provider.WithKey(context.Background(), provider.NewSecret([]byte(key)))
}

func request() provider.Request {
	return provider.Request{
		Model: "configured-model", System: "You write requirements.", MaxTokens: 1024, CachePrompt: true,
		Tools: []provider.ToolDefinition{{Name: "write_file", Description: "Writes a file", Schema: json.RawMessage(`{"type":"object"}`)}},
		Messages: []provider.Message{
			{Role: "user", Text: "Need: book rooms."},
			{Role: "assistant", Calls: []provider.ToolCall{{ID: "call_1", Name: "write_file", Args: json.RawMessage(`{"path":"a"}`)}}},
			{Role: "tool", Results: []provider.ToolResult{{ID: "call_1", Content: "ok"}, {ID: "call_2", Content: "missing", Error: true}}},
		},
	}
}

// US-24 CA-1: the same neutral request becomes a Chat Completions request, nothing agent specific changes.
func TestCompleteSendsChatCompletionsRequestAndReadsAnswer(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-request-id", "req_123")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Writing.",
			"tool_calls":[{"id":"call_9","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"docs/VISAO.md\"}"}}]},
			"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":140,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40}}}`)
	})
	resp, err := api.adapter().Complete(keyed(testKey), request(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if api.request.URL.Path != "/v1/chat/completions" || api.request.Method != http.MethodPost {
		t.Fatalf("unexpected request: %s %s", api.request.Method, api.request.URL.Path)
	}
	if api.body["model"] != "configured-model" || api.body["max_completion_tokens"] != float64(1024) {
		t.Fatalf("model and max tokens come from the request: %v", api.body)
	}
	messages := api.body["messages"].([]any)
	if first := messages[0].(map[string]any); first["role"] != "system" || first["content"] != "You write requirements." {
		t.Fatalf("system prompt first: %v", first)
	}
	assistant := messages[2].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	function := calls[0].(map[string]any)["function"].(map[string]any)
	if assistant["role"] != "assistant" || function["arguments"] != `{"path":"a"}` {
		t.Fatalf("assistant tool calls with string arguments: %v", assistant)
	}
	results := messages[3:]
	if len(results) != 2 || results[0].(map[string]any)["tool_call_id"] != "call_1" || !strings.HasPrefix(results[1].(map[string]any)["content"].(string), "[erro] ") {
		t.Fatalf("one tool message per result, errors marked: %v", results)
	}
	tools := api.body["tools"].([]any)
	if tools[0].(map[string]any)["type"] != "function" {
		t.Fatalf("tools as functions: %v", tools)
	}
	if resp.Stop != provider.StopTool || resp.ProviderID != "req_123" || resp.Message.Text != "Writing." || len(resp.Message.Calls) != 1 ||
		string(resp.Message.Calls[0].Args) != `{"path":"docs/VISAO.md"}` {
		t.Fatalf("unexpected response: %+v", resp)
	}
	// RNF-16: cached prompt tokens are cache reads, the rest is input; no cache write in OpenAI.
	if resp.Usage != (provider.Usage{Input: 100, Output: 20, CacheRead: 40}) {
		t.Fatalf("usage per token type: %+v", resp.Usage)
	}
}

func TestCompleteStreamsChunksAndRebuildsToolCalls(t *testing.T) {
	chunks := []string{
		`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"content":"Hello "}}]}`,
		`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"content":"world"}}]}`,
		`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_3","type":"function","function":{"name":"write_file","arguments":""}}]}}]}`,
		`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x.md\"}"}}]}}]}`,
		`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"chatcmpl-9","choices":[],"usage":{"prompt_tokens":50,"completion_tokens":17,"prompt_tokens_details":{"cached_tokens":0}}}`,
	}
	api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	var texts []string
	resp, err := api.adapter().Complete(keyed(testKey), request(), func(c provider.Chunk) { texts = append(texts, c.PartialText) })
	if err != nil {
		t.Fatal(err)
	}
	options, _ := api.body["stream_options"].(map[string]any)
	if api.body["stream"] != true || options["include_usage"] != true || strings.Join(texts, "") != "Hello world" {
		t.Fatalf("stream flag, usage option and chunks: %v %v", api.body, texts)
	}
	if resp.Message.Text != "Hello world" || len(resp.Message.Calls) != 1 || string(resp.Message.Calls[0].Args) != `{"path":"x.md"}` {
		t.Fatalf("rebuilt message: %+v", resp.Message)
	}
	if resp.Usage.Input != 50 || resp.Usage.Output != 17 || resp.Stop != provider.StopTool {
		t.Fatalf("usage and stop: %+v %s", resp.Usage, resp.Stop)
	}
}

func TestErrorsAreClassifiedWithoutTheKey(t *testing.T) {
	cases := map[string]struct {
		status  int
		body    string
		headers map[string]string
		key     string
		want    provider.ErrorClass
	}{
		"invalid key":    {key: "sk-proj-wrong-key-000000000000", want: provider.ErrorAuthentication},
		"rate limit":     {status: 429, body: `{"error":{"message":"slow down","type":"requests","code":"rate_limit_exceeded"}}`, headers: map[string]string{"retry-after": "7"}, want: provider.ErrorRateLimit},
		"no quota":       {status: 429, body: `{"error":{"message":"quota","type":"insufficient_quota","code":"insufficient_quota"}}`, want: provider.ErrorRequest},
		"overloaded":     {status: 503, body: `{"error":{"message":"overloaded","type":"server_error"}}`, want: provider.ErrorTransient},
		"bad request":    {status: 400, body: `{"error":{"message":"bad field","type":"invalid_request_error","code":null}}`, want: provider.ErrorRequest},
		"context":        {status: 400, body: `{"error":{"message":"maximum context length","type":"invalid_request_error","code":"context_length_exceeded"}}`, want: provider.ErrorContextExceeded},
		"refusal (200)":  {status: 200, body: `{"id":"c","choices":[{"message":{"role":"assistant","content":null,"refusal":"no"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3}}`, want: provider.ErrorContentRefused},
		"server failure": {status: 500, body: `oops`, want: provider.ErrorTransient},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			key := tc.key
			if key == "" {
				key = testKey
			}
			_, err := api.adapter().Complete(keyed(key), request(), nil)
			var providerErr *provider.Error
			if !errors.As(err, &providerErr) || providerErr.Class != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
			if strings.Contains(err.Error(), key) {
				t.Fatal("error must never contain the key (RNF-01)")
			}
			if tc.want == provider.ErrorRateLimit && providerErr.RetryAfter != 7*time.Second {
				t.Fatalf("retry-after hint: %v", providerErr.RetryAfter)
			}
		})
	}
}

// US-15 CA-1, CA-3 with OpenAI: the key test lists the models, which costs no token.
func TestTestKeyListsTheModelsWithoutTokens(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"some-model","object":"model"}]}`)
	})
	adapter := api.adapter()
	if err := adapter.TestKey(context.Background(), provider.NewSecret([]byte(testKey))); err != nil {
		t.Fatalf("valid key: %v", err)
	}
	err := adapter.TestKey(context.Background(), provider.NewSecret([]byte("sk-proj-revoked-0000000000000")))
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Class != provider.ErrorAuthentication || !strings.HasPrefix(providerErr.Message, "Incorrect API key") {
		t.Fatalf("invalid key gives the summarized provider error: %v", err)
	}
}

func TestCapabilitiesConfirmTheModel(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/configured-model" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"message":"The model does not exist","type":"invalid_request_error","code":"model_not_found"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"configured-model","object":"model"}`)
	})
	caps, err := api.adapter().Capabilities(keyed(testKey), "configured-model")
	if err != nil || !caps.ToolUse || !caps.CachePrompt {
		t.Fatalf("capabilities: %+v %v", caps, err)
	}
	if _, err := api.adapter().Capabilities(keyed(testKey), "unknown"); err == nil {
		t.Fatal("unknown model must fail")
	}
}

// RNF-10: canceling the context aborts the call in flight.
func TestCanceledCallIsAborted(t *testing.T) {
	api := newFakeAPI(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(keyed(testKey))
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := api.adapter().Complete(ctx, request(), nil)
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Class != provider.ErrorAborted {
		t.Fatalf("want aborted, got %v", err)
	}
}
