package anthropic_test

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
	"easycode/internal/infrastructure/provider/anthropic"
)

const testKey = "sk-ant-test-0123456789abcdef"

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
		if r.Header.Get("x-api-key") != testKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(api.server.Close)
	return api
}

func (f *fakeAPI) adapter() *anthropic.Provider {
	return anthropic.New(f.server.URL, f.server.Client(), 5*time.Second)
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
			{Role: "assistant", Calls: []provider.ToolCall{{ID: "toolu_1", Name: "write_file", Args: json.RawMessage(`{"path":"a"}`)}}},
			{Role: "tool", Results: []provider.ToolResult{{ID: "toolu_1", Content: "ok"}}},
		},
	}
}

func TestCompleteSendsMessagesRequestAndReadsAnswer(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("request-id", "req_123")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","content":[
			{"type":"thinking","thinking":"","signature":"sig"},
			{"type":"text","text":"Writing."},
			{"type":"tool_use","id":"toolu_2","name":"write_file","input":{"path":"docs/VISAO.md"}}],
			"stop_reason":"tool_use","usage":{"input_tokens":100,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40}}`)
	})
	resp, err := api.adapter().Complete(keyed(testKey), request(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if api.request.URL.Path != "/v1/messages" || api.request.Header.Get("anthropic-version") != anthropic.APIVersion {
		t.Fatalf("unexpected request: %s %v", api.request.URL.Path, api.request.Header)
	}
	if api.body["model"] != "configured-model" || api.body["max_tokens"] != float64(1024) {
		t.Fatalf("model and max_tokens come from the request: %v", api.body)
	}
	system := api.body["system"].([]any)[0].(map[string]any)
	if system["cache_control"] == nil {
		t.Fatal("cache_prompt must mark the system prompt cacheable")
	}
	messages := api.body["messages"].([]any)
	toolTurn := messages[2].(map[string]any)
	result := toolTurn["content"].([]any)[0].(map[string]any)
	if toolTurn["role"] != "user" || result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool results go back as a user turn of tool_result blocks: %v", toolTurn)
	}
	if resp.Stop != provider.StopTool || resp.ProviderID != "req_123" || resp.Message.Text != "Writing." || len(resp.Message.Calls) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Usage != (provider.Usage{Input: 100, Output: 20, CacheRead: 40, CacheWrite: 30}) {
		t.Fatalf("usage per token type: %+v", resp.Usage)
	}
	// The assistant answer is replayed unchanged on the next call (thinking blocks included).
	next := request()
	next.Messages = append(next.Messages, resp.Message)
	if _, err := api.adapter().Complete(keyed(testKey), next, nil); err != nil {
		t.Fatal(err)
	}
	replayed := api.body["messages"].([]any)[3].(map[string]any)["content"].([]any)[0].(map[string]any)
	if replayed["type"] != "thinking" || replayed["signature"] != "sig" {
		t.Fatalf("assistant content must be replayed as received: %v", replayed)
	}
}

func TestCompleteStreamsChunksAndRebuildsToolCalls(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_9","usage":{"input_tokens":50,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_3","name":"write_file","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"x.md\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":17}}`,
		`{"type":"message_stop"}`,
	}
	api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		for _, event := range events {
			var typed struct{ Type string }
			_ = json.Unmarshal([]byte(event), &typed)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, event)
		}
	})
	var chunks []string
	resp, err := api.adapter().Complete(keyed(testKey), request(), func(c provider.Chunk) { chunks = append(chunks, c.PartialText) })
	if err != nil {
		t.Fatal(err)
	}
	if api.body["stream"] != true || strings.Join(chunks, "") != "Hello world" {
		t.Fatalf("stream flag and chunks: %v %v", api.body["stream"], chunks)
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
		"invalid key":    {key: "sk-ant-wrong-key-000000000000", want: provider.ErrorAuthentication},
		"rate limit":     {status: 429, body: `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, headers: map[string]string{"retry-after": "7"}, want: provider.ErrorRateLimit},
		"overloaded":     {status: 529, body: `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, want: provider.ErrorTransient},
		"bad request":    {status: 400, body: `{"type":"error","error":{"type":"invalid_request_error","message":"bad field"}}`, want: provider.ErrorRequest},
		"context":        {status: 400, body: `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 2000000 tokens"}}`, want: provider.ErrorContextExceeded},
		"refusal (200)":  {status: 200, body: `{"id":"m","content":[],"stop_reason":"refusal","usage":{"input_tokens":3,"output_tokens":0}}`, want: provider.ErrorContentRefused},
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

func TestTestKeyListsOneModel(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"some-model"}],"has_more":true}`)
	})
	adapter := api.adapter()
	if err := adapter.TestKey(context.Background(), provider.NewSecret([]byte(testKey))); err != nil {
		t.Fatalf("valid key (US-15 CA-1): %v", err)
	}
	err := adapter.TestKey(context.Background(), provider.NewSecret([]byte("sk-ant-revoked-0000000000000")))
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Class != provider.ErrorAuthentication || providerErr.Message != "invalid x-api-key" {
		t.Fatalf("invalid key gives the summarized provider error (US-15 CA-3): %v", err)
	}
}

func TestCapabilitiesComeFromTheModelsAPI(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/configured-model" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"id":"configured-model","max_input_tokens":1000000,"capabilities":{"image_input":{"supported":true}}}`)
	})
	caps, err := api.adapter().Capabilities(keyed(testKey), "configured-model")
	if err != nil || caps.ContextTokens != 1_000_000 || !caps.ImageInput || !caps.ToolUse {
		t.Fatalf("capabilities: %+v %v", caps, err)
	}
}

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
