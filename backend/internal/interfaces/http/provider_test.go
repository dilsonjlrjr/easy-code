package http_test

import (
	"context"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/providerkey"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	httpapi "easycode/internal/interfaces/http"
)

type fakeProviders struct{ saved string }

func (f *fakeProviders) List(context.Context, identity.Principal) ([]providerkey.KeyStatus, error) {
	return []providerkey.KeyStatus{{Provider: "anthropic", Status: providerkey.StatusActive, Last4: "abcd"}, {Provider: "openai", Status: providerkey.StatusMissing}}, nil
}

func (f *fakeProviders) Save(_ context.Context, _ identity.Principal, name, key string) (providerkey.KeyStatus, error) {
	f.saved = key
	if strings.HasPrefix(key, "bad") {
		return providerkey.KeyStatus{}, failure.New(failure.KindConflict, providerkey.CodeInvalidKey, nil).With("provider_message", "invalid x-api-key")
	}
	return providerkey.KeyStatus{Provider: name, Status: providerkey.StatusActive, Last4: key[len(key)-4:]}, nil
}

func (f *fakeProviders) Remove(context.Context, identity.Principal, string) error { return nil }

func withProviders(p httpapi.ProviderUseCases) func(*httpapi.Dependencies) {
	return func(d *httpapi.Dependencies) { d.Providers = p }
}

func write(h harness, method, path, cookie, body string) *fasthttp.RequestCtx {
	rc := &fasthttp.RequestCtx{}
	rc.Request.Header.SetMethod(method)
	rc.Request.SetRequestURI(path)
	rc.Request.Header.Set("Cookie", cookie)
	rc.Request.Header.Set("Origin", "https://easycode.test")
	rc.Request.Header.SetContentType("application/json")
	rc.Request.SetBodyString(body)
	h.handler(rc)
	return rc
}

// US-15 CA-1, CA-2, CA-3: the answer shows status and last 4 only; the key is never echoed or logged.
func TestSaveProviderKeyNeverEchoesOrLogsTheKey(t *testing.T) {
	providers := &fakeProviders{}
	h := productHarness(t, withProviders(providers))
	cookie := sessionCookie(t, h)
	key := "sk-ant-secret-0123456789wxyz"
	rc := write(h, fasthttp.MethodPut, "/api/v1/providers/anthropic/key", cookie, `{"key":"`+key+`"}`)
	if rc.Response.StatusCode() != fasthttp.StatusOK || providers.saved != key {
		t.Fatalf("save: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	body := decode(t, rc)
	if strings.Contains(string(rc.Response.Body()), key) || body["key"].(map[string]any)["last_4"] != "wxyz" {
		t.Fatalf("CA-2: the answer must carry only the last 4: %s", rc.Response.Body())
	}
	rc = write(h, fasthttp.MethodPut, "/api/v1/providers/anthropic/key", cookie, `{"key":"bad-key-0000000000"}`)
	problem := decode(t, rc)
	if rc.Response.StatusCode() != fasthttp.StatusConflict || problem["type"] != "urn:easycode:problem:invalid-key" || !strings.Contains(problem["detail"].(string), "invalid x-api-key") {
		t.Fatalf("CA-3: %d %v", rc.Response.StatusCode(), problem)
	}
	if strings.Contains(h.logText.String(), key) || strings.Contains(h.logText.String(), "bad-key-0000000000") {
		t.Fatal("RNF-01: key in logs")
	}
	rc = write(h, fasthttp.MethodPut, "/api/v1/providers/anthropic/key", cookie, `{"key":"x","extra":1}`)
	if rc.Response.StatusCode() != fasthttp.StatusBadRequest {
		t.Fatalf("unknown member is a validation error: %d", rc.Response.StatusCode())
	}
}

func TestListProvidersShowsStatusWithoutValue(t *testing.T) {
	h := productHarness(t, withProviders(&fakeProviders{}))
	rc := h.do(fasthttp.MethodGet, "/api/v1/providers", map[string]string{"Cookie": sessionCookie(t, h)})
	items := decode(t, rc)["items"].([]any)
	first := items[0].(map[string]any)
	if len(items) != 2 || first["id"] != "anthropic" || first["name"] != "Anthropic" || first["key"].(map[string]any)["status"] != "active" {
		t.Fatalf("unexpected providers: %v", items)
	}
}

type claudeCodeProviders struct{ fakeProviders }

func (claudeCodeProviders) List(context.Context, identity.Principal) ([]providerkey.KeyStatus, error) {
	return []providerkey.KeyStatus{
		{Provider: "anthropic", Status: providerkey.StatusMissing},
		{Provider: providerkey.ProviderClaudeCode, Status: providerkey.StatusActive, Last4: "wxyz",
			CredentialKind: providerkey.CredentialSubscriptionToken, Runtime: &providerkey.Runtime{Detected: true, Version: "2.1.0"}},
	}, nil
}

// US-58 CA-1, CA-4: Claude Code shows its detection and the credential kind, never the value.
func TestListProvidersShowsClaudeCodeDetectionAndCredentialKind(t *testing.T) {
	h := productHarness(t, withProviders(&claudeCodeProviders{}))
	rc := h.do(fasthttp.MethodGet, "/api/v1/providers", map[string]string{"Cookie": sessionCookie(t, h)})
	items := decode(t, rc)["items"].([]any)
	anthropic, claudeCode := items[0].(map[string]any), items[1].(map[string]any)
	if _, ok := anthropic["detection"]; ok {
		t.Fatalf("detection only on claude-code: %v", anthropic)
	}
	if _, ok := anthropic["key"].(map[string]any)["credential_kind"]; ok {
		t.Fatalf("credential kind only on claude-code: %v", anthropic)
	}
	detection := claudeCode["detection"].(map[string]any)
	key := claudeCode["key"].(map[string]any)
	if claudeCode["name"] != "Claude Code" || detection["detected"] != true || detection["version"] != "2.1.0" || key["credential_kind"] != "subscription_token" {
		t.Fatalf("unexpected claude-code: %v", claudeCode)
	}
}
