// Package anthropic is the Anthropic adapter of the provider interface (ADR-03), run only inside
// easycode-gateway. It speaks the Messages API over raw HTTP (POST /v1/messages, streaming by SSE)
// and the Models API (GET /v1/models) for the key test and the capability check. It never retries:
// retry, backoff and lost cost belong to the orchestrator. Model ids come from the request (the
// project's model pair); none is fixed here. The key comes in the context and is never kept or logged.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easycode/internal/domain/provider"
)

// Name is the provider id of the contract (ProviderId).
const Name = "anthropic"

// APIVersion is the anthropic-version header sent on every request.
const APIVersion = "2026-10-21"

// maxErrorBody bounds what is read from an error answer.
const maxErrorBody = 64 << 10

// Provider implements provider.Provider for Anthropic.
type Provider struct {
	baseURL string
	client  *http.Client
}

var _ provider.Provider = (*Provider)(nil)

// New builds the adapter. baseURL is the API root (the egress proxy in production, a test server
// in tests); client may be nil for a default one that honors HTTPS_PROXY and the call timeout.
func New(baseURL string, client *http.Client, callTimeout time.Duration) *Provider {
	if client == nil {
		client = &http.Client{
			Timeout:   callTimeout,
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: callTimeout},
		}
	}
	return &Provider{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

// Name implements provider.Provider.
func (*Provider) Name() string { return Name }

// TestKey implements provider.Provider with the cheapest authenticated call: list one model.
func (p *Provider) TestKey(ctx context.Context, key provider.Secret) error {
	if key.Empty() {
		return &provider.Error{Class: provider.ErrorAuthentication, Message: "empty key"}
	}
	resp, err := p.send(ctx, key, http.MethodGet, "/v1/models?limit=1", nil, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	return nil
}

type modelInfo struct {
	ID             string `json:"id"`
	MaxInputTokens int    `json:"max_input_tokens"`
	Capabilities   struct {
		ImageInput struct {
			Supported bool `json:"supported"`
		} `json:"image_input"`
	} `json:"capabilities"`
}

// Capabilities implements provider.Provider from GET /v1/models/{id}. Tool use and prompt caching
// are available on every Claude model of the Messages API, so they are reported as supported; the
// registered matrix (models table) stays the source of truth (US-24 CA-4).
func (p *Provider) Capabilities(ctx context.Context, model string) (provider.Capabilities, error) {
	key, ok := provider.KeyFromContext(ctx)
	if !ok {
		return provider.Capabilities{}, &provider.Error{Class: provider.ErrorAuthentication, Message: "no key in scope"}
	}
	resp, err := p.send(ctx, key, http.MethodGet, "/v1/models/"+url.PathEscape(model), nil, false)
	if err != nil {
		return provider.Capabilities{}, err
	}
	defer resp.Body.Close()
	var info modelInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return provider.Capabilities{}, &provider.Error{Class: provider.ErrorTransient, Message: "unreadable model answer"}
	}
	return provider.Capabilities{
		ToolUse: true, CachePrompt: true, ContextTokens: info.MaxInputTokens, ImageInput: info.Capabilities.ImageInput.Supported,
	}, nil
}

// Complete implements provider.Provider. With onChunk it streams (SSE) and reports progress.
func (p *Provider) Complete(ctx context.Context, req provider.Request, onChunk func(provider.Chunk)) (provider.Response, error) {
	key, ok := provider.KeyFromContext(ctx)
	if !ok {
		return provider.Response{}, &provider.Error{Class: provider.ErrorAuthentication, Message: "no key in scope"}
	}
	body, err := buildRequest(req, onChunk != nil)
	if err != nil {
		return provider.Response{}, &provider.Error{Class: provider.ErrorRequest, Message: err.Error()}
	}
	resp, err := p.send(ctx, key, http.MethodPost, "/v1/messages", body, onChunk != nil)
	if err != nil {
		return provider.Response{}, err
	}
	defer resp.Body.Close()
	requestID := resp.Header.Get("request-id")
	if onChunk == nil {
		var answer messageResponse
		if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
			return provider.Response{}, abortedOr(ctx, provider.Usage{}, &provider.Error{Class: provider.ErrorTransient, Message: "unreadable answer"})
		}
		return answer.toResponse(requestID)
	}
	return readStream(ctx, resp.Body, requestID, onChunk)
}

func (p *Provider) send(ctx context.Context, key provider.Secret, method, path string, body []byte, stream bool) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reader)
	if err != nil {
		return nil, &provider.Error{Class: provider.ErrorRequest, Message: "build request"}
	}
	httpReq.Header.Set("x-api-key", string(key.Reveal()))
	httpReq.Header.Set("anthropic-version", APIVersion)
	if body != nil {
		httpReq.Header.Set("content-type", "application/json")
	}
	if stream {
		httpReq.Header.Set("accept", "text/event-stream")
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		// Never wrap err: the URL error text is safe, but keep the message short and fixed.
		return nil, abortedOr(ctx, provider.Usage{}, &provider.Error{Class: provider.ErrorTransient, Message: "provider unreachable"})
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	defer resp.Body.Close()
	return nil, classifyStatus(resp)
}

// abortedOr reports a canceled context as an aborted call with its partial usage (lost cost).
func abortedOr(ctx context.Context, usage provider.Usage, err *provider.Error) *provider.Error {
	if ctx.Err() != nil {
		return &provider.Error{Class: provider.ErrorAborted, Message: "call aborted", PartialUsage: usage}
	}
	err.PartialUsage = usage
	return err
}

type errorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// classifyStatus maps an error answer to an error class with a short message (never the key:
// the provider does not echo it, and the message is cut).
func classifyStatus(resp *http.Response) *provider.Error {
	var body errorBody
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxErrorBody)).Decode(&body)
	message := shorten(body.Error.Message)
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	result := &provider.Error{Message: message}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || body.Error.Type == "authentication_error" || body.Error.Type == "permission_error":
		result.Class = provider.ErrorAuthentication
	case resp.StatusCode == http.StatusTooManyRequests:
		result.Class = provider.ErrorRateLimit
		result.RetryAfter = retryAfter(resp.Header.Get("retry-after"))
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		result.Class = provider.ErrorTransient
		result.RetryAfter = retryAfter(resp.Header.Get("retry-after"))
	case resp.StatusCode == http.StatusRequestEntityTooLarge || strings.Contains(strings.ToLower(message), "prompt is too long"):
		result.Class = provider.ErrorContextExceeded
	default:
		result.Class = provider.ErrorRequest
	}
	return result
}

func retryAfter(header string) time.Duration {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(header), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func shorten(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}

// ---------------------------------------------------------------- request

type cacheControl struct {
	Type string `json:"type"`
}

type block struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	Source       *imageSource    `json:"source,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      *string         `json:"content,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      []byte `json:"data"` // base64 by encoding/json
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    []block   `json:"system,omitempty"`
	Messages  []message `json:"messages"`
	Tools     []tool    `json:"tools,omitempty"`
	Stream    bool      `json:"stream,omitempty"`
}

func buildRequest(req provider.Request, stream bool) ([]byte, error) {
	if req.Model == "" || len(req.Messages) == 0 || req.MaxTokens <= 0 {
		return nil, errors.New("model, messages and max_tokens are required")
	}
	out := messagesRequest{Model: req.Model, MaxTokens: req.MaxTokens, Stream: stream}
	var cache *cacheControl
	if req.CachePrompt {
		cache = &cacheControl{Type: "ephemeral"}
	}
	if req.System != "" {
		out.System = []block{{Type: "text", Text: req.System, CacheControl: cache}}
	}
	for i, definition := range req.Tools {
		schema := definition.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		t := tool{Name: definition.Name, Description: definition.Description, InputSchema: schema}
		if i == len(req.Tools)-1 {
			t.CacheControl = cache
		}
		out.Tools = append(out.Tools, t)
	}
	for _, m := range req.Messages {
		converted, err := convertMessage(m)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, converted)
	}
	return json.Marshal(out)
}

func convertMessage(m provider.Message) (message, error) {
	switch m.Role {
	case "assistant":
		if len(m.Opaque) > 0 {
			return message{Role: "assistant", Content: m.Opaque}, nil
		}
		var blocks []block
		if m.Text != "" {
			blocks = append(blocks, block{Type: "text", Text: m.Text})
		}
		for _, call := range m.Calls {
			args := call.Args
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			blocks = append(blocks, block{Type: "tool_use", ID: call.ID, Name: call.Name, Input: args})
		}
		return encodeBlocks("assistant", blocks)
	case "user":
		var blocks []block
		for _, image := range m.Images {
			blocks = append(blocks, block{Type: "image", Source: &imageSource{Type: "base64", MediaType: image.MediaType, Data: image.Data}})
		}
		if m.Text != "" {
			blocks = append(blocks, block{Type: "text", Text: m.Text})
		}
		return encodeBlocks("user", blocks)
	case "tool":
		var blocks []block
		for _, result := range m.Results {
			content := result.Content
			blocks = append(blocks, block{Type: "tool_result", ToolUseID: result.ID, Content: &content, IsError: result.Error})
		}
		return encodeBlocks("user", blocks)
	default:
		return message{}, fmt.Errorf("unknown role %q", m.Role)
	}
}

func encodeBlocks(role string, blocks []block) (message, error) {
	if len(blocks) == 0 {
		return message{}, fmt.Errorf("empty %s message", role)
	}
	encoded, err := json.Marshal(blocks)
	return message{Role: role, Content: encoded}, err
}

// ---------------------------------------------------------------- response

type usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u usage) neutral() provider.Usage {
	return provider.Usage{Input: u.InputTokens, Output: u.OutputTokens, CacheRead: u.CacheReadInputTokens, CacheWrite: u.CacheCreationInputTokens}
}

type responseBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type messageResponse struct {
	ID         string            `json:"id"`
	Content    []json.RawMessage `json:"content"`
	StopReason string            `json:"stop_reason"`
	Usage      usage             `json:"usage"`
}

func (r messageResponse) toResponse(requestID string) (provider.Response, error) {
	if requestID == "" {
		requestID = r.ID
	}
	answer := provider.Message{Role: "assistant"}
	var texts []string
	for _, raw := range r.Content {
		var b responseBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			continue
		}
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_use":
			answer.Calls = append(answer.Calls, provider.ToolCall{ID: b.ID, Name: b.Name, Args: b.Input})
		}
	}
	answer.Text = strings.Join(texts, "")
	if len(r.Content) > 0 {
		if encoded, err := json.Marshal(r.Content); err == nil {
			answer.Opaque = encoded
		}
	}
	used := r.Usage.neutral()
	stop := provider.StopEnd
	switch r.StopReason {
	case "tool_use":
		stop = provider.StopTool
	case "max_tokens", "model_context_window_exceeded":
		stop = provider.StopLimit
	case "refusal":
		return provider.Response{}, &provider.Error{Class: provider.ErrorContentRefused, Message: "provider refused the request", PartialUsage: used}
	}
	return provider.Response{Message: answer, Usage: used, Stop: stop, ProviderID: requestID}, nil
}

// ---------------------------------------------------------------- streaming

type streamEvent struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string `json:"id"`
		Usage usage  `json:"usage"`
	} `json:"message"`
	Index        int             `json:"index"`
	ContentBlock json.RawMessage `json:"content_block"`
	Delta        *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *usage `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// streamState rebuilds the final message from the events.
type streamState struct {
	id         string
	usage      usage
	stopReason string
	blocks     []map[string]any
	partial    map[int]*strings.Builder
}

func readStream(ctx context.Context, body io.Reader, requestID string, onChunk func(provider.Chunk)) (provider.Response, error) {
	state := &streamState{partial: map[int]*strings.Builder{}}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			continue
		}
		done, err := state.apply(event, onChunk)
		if err != nil {
			return provider.Response{}, err
		}
		if done {
			return state.response(requestID)
		}
	}
	return provider.Response{}, abortedOr(ctx, state.usage.neutral(), &provider.Error{Class: provider.ErrorTransient, Message: "stream ended early"})
}

func (s *streamState) apply(event streamEvent, onChunk func(provider.Chunk)) (bool, error) {
	switch event.Type {
	case "message_start":
		if event.Message != nil {
			s.id = event.Message.ID
			s.usage = event.Message.Usage
		}
	case "content_block_start":
		var started map[string]any
		_ = json.Unmarshal(event.ContentBlock, &started)
		for len(s.blocks) <= event.Index {
			s.blocks = append(s.blocks, map[string]any{})
		}
		s.blocks[event.Index] = started
	case "content_block_delta":
		s.delta(event, onChunk)
	case "content_block_stop":
		if builder, ok := s.partial[event.Index]; ok && event.Index < len(s.blocks) {
			var input any = map[string]any{}
			if text := builder.String(); text != "" {
				_ = json.Unmarshal([]byte(text), &input)
			}
			s.blocks[event.Index]["input"] = input
		}
	case "message_delta":
		if event.Delta != nil && event.Delta.StopReason != "" {
			s.stopReason = event.Delta.StopReason
		}
		if event.Usage != nil {
			s.usage.OutputTokens = event.Usage.OutputTokens
		}
	case "message_stop":
		return true, nil
	case "error":
		result := &provider.Error{Class: provider.ErrorTransient, Message: "stream error", PartialUsage: s.usage.neutral()}
		if event.Error != nil {
			result.Message = shorten(event.Error.Message)
			if event.Error.Type == "rate_limit_error" {
				result.Class = provider.ErrorRateLimit
			}
		}
		return false, result
	}
	return false, nil
}

func (s *streamState) delta(event streamEvent, onChunk func(provider.Chunk)) {
	if event.Delta == nil || event.Index >= len(s.blocks) {
		return
	}
	target := s.blocks[event.Index]
	switch event.Delta.Type {
	case "text_delta":
		target["text"] = stringOf(target["text"]) + event.Delta.Text
		s.usage.OutputTokens += (len(event.Delta.Text) + 3) / 4 // progress estimate; message_delta has the real count
		onChunk(provider.Chunk{PartialText: event.Delta.Text, PartialUsage: s.usage.neutral()})
	case "input_json_delta":
		builder, ok := s.partial[event.Index]
		if !ok {
			builder = &strings.Builder{}
			s.partial[event.Index] = builder
		}
		builder.WriteString(event.Delta.PartialJSON)
	case "thinking_delta":
		target["thinking"] = stringOf(target["thinking"]) + event.Delta.Thinking
	case "signature_delta":
		target["signature"] = stringOf(target["signature"]) + event.Delta.Signature
	}
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

func (s *streamState) response(requestID string) (provider.Response, error) {
	content := make([]json.RawMessage, 0, len(s.blocks))
	for _, b := range s.blocks {
		encoded, err := json.Marshal(b)
		if err != nil {
			continue
		}
		content = append(content, encoded)
	}
	return messageResponse{ID: s.id, Content: content, StopReason: s.stopReason, Usage: s.usage}.toResponse(requestID)
}
