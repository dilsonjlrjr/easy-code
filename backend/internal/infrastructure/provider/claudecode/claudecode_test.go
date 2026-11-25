package claudecode_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/provider/claudecode"
)

const (
	apiKey   = "sk-ant-api03-0123456789abcdef"
	oauthKey = "sk-ant-oat01-0123456789abcdef"
)

type seen struct {
	path, query string
	header      http.Header
	body        string
}

func upstream(t *testing.T, status int, answer string) (*httptest.Server, *seen) {
	t.Helper()
	last := &seen{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		*last = seen{path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: string(raw)}
		w.Header().Set("content-type", "text/event-stream")
		w.Header().Set("request-id", "req_123")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(server.Close)
	return server, last
}

func TestAuthorizeBySubscriptionPrefix(t *testing.T) {
	header := http.Header{}
	header.Set("anthropic-beta", "claude-code-20250219")
	header.Set("x-api-key", "session-token")
	claudecode.Authorize(header, provider.NewSecret([]byte(oauthKey)))
	if header.Get("Authorization") != "Bearer "+oauthKey || header.Get("x-api-key") != "" {
		t.Fatalf("subscription headers %v", header)
	}
	if header.Get("anthropic-beta") != "claude-code-20250219,"+claudecode.OAuthBeta {
		t.Fatalf("beta %q", header.Get("anthropic-beta"))
	}
	claudecode.Authorize(header, provider.NewSecret([]byte(oauthKey)))
	if strings.Count(header.Get("anthropic-beta"), claudecode.OAuthBeta) != 1 {
		t.Fatalf("beta flag repeated: %q", header.Get("anthropic-beta"))
	}

	header = http.Header{}
	header.Set("Authorization", "Bearer session-token")
	claudecode.Authorize(header, provider.NewSecret([]byte(apiKey)))
	if header.Get("x-api-key") != apiKey || header.Get("Authorization") != "" || header.Get("anthropic-beta") != "" {
		t.Fatalf("api key headers %v", header)
	}
}

func TestForwardSendsTheRawRequestAndReturnsTheAnswer(t *testing.T) {
	server, last := upstream(t, http.StatusOK, "event: message_start\ndata: {}\n\n")
	adapter := claudecode.New(server.URL, server.Client(), 5*time.Second, "")
	body := `{"model":"claude-test","stream":true}`
	response, err := adapter.Forward(context.Background(), provider.NewSecret([]byte(oauthKey)), appgateway.ForwardRequest{
		Path: "v1/messages", RawQuery: "beta=true", Version: "2023-06-01", Beta: "fine-grained-tool-streaming-2025-05-14", Body: []byte(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.Status != http.StatusOK || response.ContentType != "text/event-stream" || response.RequestID != "req_123" || !strings.Contains(string(answer), "message_start") {
		t.Fatalf("response %+v %q", response, answer)
	}
	if last.path != "/v1/messages" || last.query != "beta=true" || last.body != body {
		t.Fatalf("upstream saw %+v", last)
	}
	if last.header.Get("anthropic-version") != "2023-06-01" || last.header.Get("Authorization") != "Bearer "+oauthKey ||
		last.header.Get("anthropic-beta") != "fine-grained-tool-streaming-2025-05-14,"+claudecode.OAuthBeta {
		t.Fatalf("upstream headers %v", last.header)
	}
}

func TestForwardReturnsUpstreamErrorsUnchanged(t *testing.T) {
	server, _ := upstream(t, http.StatusUnauthorized, `{"type":"error","error":{"type":"authentication_error","message":"invalid"}}`)
	adapter := claudecode.New(server.URL, server.Client(), 5*time.Second, "")
	response, err := adapter.Forward(context.Background(), provider.NewSecret([]byte(apiKey)), appgateway.ForwardRequest{Path: "v1/messages/count_tokens", Body: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.Status != http.StatusUnauthorized {
		t.Fatalf("status %d", response.Status)
	}
	if _, err := adapter.Forward(context.Background(), provider.NewSecret([]byte(apiKey)), appgateway.ForwardRequest{Path: "v1/models", Body: []byte(`{}`)}); err == nil {
		t.Fatal("path outside the allowlist must be refused")
	}
}

func TestTestKeyMakesAMinimalMessagesCall(t *testing.T) {
	server, last := upstream(t, http.StatusOK, `{"id":"msg"}`)
	adapter := claudecode.New(server.URL, server.Client(), 5*time.Second, "claude-test-model")
	if err := adapter.TestKey(context.Background(), provider.NewSecret([]byte(oauthKey))); err != nil {
		t.Fatal(err)
	}
	if last.path != "/v1/messages" || !strings.Contains(last.body, `"model":"claude-test-model"`) || !strings.Contains(last.body, `"max_tokens":1`) {
		t.Fatalf("test call %+v", last)
	}

	denied, _ := upstream(t, http.StatusUnauthorized, `{"type":"error","error":{"type":"authentication_error","message":"invalid bearer token"}}`)
	err := claudecode.New(denied.URL, denied.Client(), 5*time.Second, "claude-test-model").TestKey(context.Background(), provider.NewSecret([]byte(oauthKey)))
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Class != provider.ErrorAuthentication || strings.Contains(err.Error(), oauthKey) {
		t.Fatalf("expected authentication error without the key, got %v", err)
	}
}

func TestCompleteUsesTheSameCredentialRule(t *testing.T) {
	server, last := upstream(t, http.StatusOK, "")
	adapter := claudecode.New(server.URL, server.Client(), 5*time.Second, "")
	ctx := provider.WithKey(context.Background(), provider.NewSecret([]byte(oauthKey)))
	_, _ = adapter.Complete(ctx, provider.Request{Model: "m", MaxTokens: 1, Messages: []provider.Message{{Role: "user", Text: "hi"}}}, nil)
	if last.header.Get("Authorization") != "Bearer "+oauthKey || !strings.Contains(last.header.Get("anthropic-beta"), claudecode.OAuthBeta) {
		t.Fatalf("complete headers %v", last.header)
	}
	if adapter.Name() != provider.ClaudeCode {
		t.Fatal("name")
	}
}
