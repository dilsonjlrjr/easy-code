// Package claudecode is the adapter of the "claude-code" provider (ADR-23), run only inside
// easycode-gateway. Claude Code, inside the sandbox, builds its own Messages API requests; the
// gateway forwards them unchanged to the Anthropic API, adding the credential. The credential is an
// API key (x-api-key) or a subscription token from "claude setup-token" (prefix sk-ant-oat: Bearer
// plus the oauth beta flag). Complete and Capabilities reuse the Anthropic adapter with the same
// credential rule. The key comes in the call and is never kept or logged.
package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/provider"
	"easycode/internal/infrastructure/provider/anthropic"
)

// Name is the provider id of the contract.
const Name = provider.ClaudeCode

// SubscriptionPrefix marks a subscription token (claude setup-token); any other key is an API key.
const SubscriptionPrefix = "sk-ant-oat"

// OAuthBeta is the beta flag the Anthropic API needs to accept a subscription token.
const OAuthBeta = "oauth-2025-04-20"

// Provider implements provider.Provider and appgateway.Forwarder.
type Provider struct {
	*anthropic.Provider
	baseURL   string
	client    *http.Client
	testModel string
}

var (
	_ provider.Provider    = (*Provider)(nil)
	_ appgateway.Forwarder = (*Provider)(nil)
)

// New builds the adapter. baseURL is the API root (egress proxy in production, a test server in
// tests); client may be nil for a default one that honors HTTPS_PROXY and the call timeout.
// testModel is the model of the minimal key test call; empty falls back to listing one model.
func New(baseURL string, client *http.Client, callTimeout time.Duration, testModel string) *Provider {
	if client == nil {
		// No Client.Timeout: it would cut a long streamed answer; the forward is bounded by the request context and
		// the header timeout. Redirects are never followed, so the credential header never goes to another host.
		client = &http.Client{
			Transport:     &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: callTimeout},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return &Provider{
		Provider: anthropic.New(baseURL, client, callTimeout).WithAuthorizer(Authorize),
		baseURL:  strings.TrimRight(baseURL, "/"), client: client, testModel: testModel,
	}
}

// Name implements provider.Provider.
func (*Provider) Name() string { return Name }

// IsSubscription reports whether the key is a subscription token.
func IsSubscription(key provider.Secret) bool {
	return bytes.HasPrefix(key.Reveal(), []byte(SubscriptionPrefix))
}

// Authorize sets the credential: Bearer plus the oauth beta flag for a subscription token, x-api-key
// otherwise. Any credential header already present is replaced.
func Authorize(header http.Header, key provider.Secret) {
	header.Del("Authorization")
	header.Del("x-api-key")
	if !IsSubscription(key) {
		header.Set("x-api-key", string(key.Reveal()))
		return
	}
	header.Set("Authorization", "Bearer "+string(key.Reveal()))
	header.Set("anthropic-beta", withBeta(header.Get("anthropic-beta"), OAuthBeta))
}

func withBeta(current, flag string) string {
	for _, existing := range strings.Split(current, ",") {
		if strings.TrimSpace(existing) == flag {
			return current
		}
	}
	if strings.TrimSpace(current) == "" {
		return flag
	}
	return current + "," + flag
}

// Forward implements appgateway.Forwarder: one raw request to the allowed path, answer unchanged.
func (p *Provider) Forward(ctx context.Context, key provider.Secret, req appgateway.ForwardRequest) (appgateway.ForwardResponse, error) {
	if key.Empty() {
		return appgateway.ForwardResponse{}, &provider.Error{Class: provider.ErrorAuthentication, Message: "no key in scope"}
	}
	if !appgateway.ForwardPaths[req.Path] {
		return appgateway.ForwardResponse{}, &provider.Error{Class: provider.ErrorRequest, Message: "path not allowed"}
	}
	target := p.baseURL + "/" + req.Path
	if req.RawQuery != "" {
		target += "?" + req.RawQuery
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(req.Body))
	if err != nil {
		return appgateway.ForwardResponse{}, &provider.Error{Class: provider.ErrorRequest, Message: "build request"}
	}
	version := req.Version
	if version == "" {
		version = anthropic.APIVersion
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", version)
	if req.Beta != "" {
		httpReq.Header.Set("anthropic-beta", req.Beta)
	}
	Authorize(httpReq.Header, key)
	resp, err := p.client.Do(httpReq) //nolint:bodyclose // the body goes to the caller in ForwardResponse, which closes it
	if err != nil {
		// Never wrap err: keep the message short and fixed.
		if ctx.Err() != nil {
			return appgateway.ForwardResponse{}, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted"}
		}
		return appgateway.ForwardResponse{}, &provider.Error{Class: provider.ErrorTransient, Message: "provider unreachable"}
	}
	return appgateway.ForwardResponse{
		Status: resp.StatusCode, ContentType: resp.Header.Get("content-type"), RequestID: resp.Header.Get("request-id"),
		RetryAfter: resp.Header.Get("retry-after"), Body: resp.Body,
	}, nil
}

// TestKey implements provider.Provider with a minimal Messages call (a subscription token may not
// list models). Without a configured test model it lists one model, as the Anthropic adapter does.
func (p *Provider) TestKey(ctx context.Context, key provider.Secret) error {
	if key.Empty() {
		return &provider.Error{Class: provider.ErrorAuthentication, Message: "empty key"}
	}
	if p.testModel == "" {
		return p.Provider.TestKey(ctx, key)
	}
	body, err := json.Marshal(map[string]any{
		"model": p.testModel, "max_tokens": 1,
		"messages": []map[string]string{{"role": "user", "content": "ping"}},
	})
	if err != nil {
		return &provider.Error{Class: provider.ErrorRequest, Message: "build request"}
	}
	resp, err := p.Forward(ctx, key, appgateway.ForwardRequest{Path: "v1/messages", Body: body})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Status/100 == 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil
	}
	header := http.Header{}
	header.Set("retry-after", resp.RetryAfter)
	return anthropic.ClassifyStatus(&http.Response{StatusCode: resp.Status, Header: header, Body: resp.Body})
}
