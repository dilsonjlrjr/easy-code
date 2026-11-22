// Package openai is the OpenAI adapter of the provider interface (ADR-03, US-24), run only inside
// easycode-gateway. It speaks the Chat Completions API over raw HTTP (POST /v1/chat/completions,
// streaming by SSE with the usage in the last chunk) and the Models API (GET /v1/models) for the key
// test, which costs no token, and the capability check. Same contract as the Anthropic adapter: it
// never retries (retry, backoff and lost cost belong to the orchestrator), model ids come from the
// request (the project's model pair) and the key comes in the context, never kept or logged.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"easycode/internal/domain/provider"
)

// Name is the provider id of the contract (ProviderId).
const Name = "openai"

// maxErrorBody bounds what is read from an error answer.
const maxErrorBody = 64 << 10

// errorPrefix marks a tool result that is an error: Chat Completions has no is_error flag.
const errorPrefix = "[erro] "

// Provider implements provider.Provider for OpenAI.
type Provider struct {
	baseURL string
	client  *http.Client
}

var _ provider.Provider = (*Provider)(nil)

// New builds the adapter. baseURL is the API root (the egress proxy in production, a test server in
// tests); client may be nil for a default one that honors HTTPS_PROXY and the call timeout.
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

// TestKey implements provider.Provider with an authenticated call that uses no token: list the models.
func (p *Provider) TestKey(ctx context.Context, key provider.Secret) error {
	if key.Empty() {
		return &provider.Error{Class: provider.ErrorAuthentication, Message: "empty key"}
	}
	resp, err := p.send(ctx, key, http.MethodGet, "/v1/models", nil, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	return nil
}

// Capabilities implements provider.Provider from GET /v1/models/{id}. The Models API only confirms the
// model exists for the key; tool use and automatic prompt caching are features of the Chat Completions
// models, and context size and image input are not reported, so the registered matrix (models table)
// stays the source of truth (US-24 CA-4).
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
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	return provider.Capabilities{ToolUse: true, CachePrompt: true}, nil
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
	resp, err := p.send(ctx, key, http.MethodPost, "/v1/chat/completions", body, onChunk != nil)
	if err != nil {
		return provider.Response{}, err
	}
	defer resp.Body.Close()
	requestID := resp.Header.Get("x-request-id")
	if onChunk == nil {
		var answer completion
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
	httpReq.Header.Set("authorization", "Bearer "+string(key.Reveal()))
	if body != nil {
		httpReq.Header.Set("content-type", "application/json")
	}
	if stream {
		httpReq.Header.Set("accept", "text/event-stream")
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		// Never wrap err: keep the message short and fixed.
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
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// classifyStatus maps an error answer to an error class with a short message (never the key: the
// provider masks it, and the message is cut).
func classifyStatus(resp *http.Response) *provider.Error {
	var body errorBody
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxErrorBody)).Decode(&body)
	message := shorten(body.Error.Message)
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	code := fmt.Sprint(body.Error.Code)
	result := &provider.Error{Message: message}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || code == "invalid_api_key":
		result.Class = provider.ErrorAuthentication
	case code == "insufficient_quota":
		result.Class = provider.ErrorRequest // billing, not a transient limit: retrying does not help
	case resp.StatusCode == http.StatusTooManyRequests:
		result.Class = provider.ErrorRateLimit
		result.RetryAfter = retryAfter(resp.Header.Get("retry-after"))
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		result.Class = provider.ErrorTransient
		result.RetryAfter = retryAfter(resp.Header.Get("retry-after"))
	case code == "context_length_exceeded" || resp.StatusCode == http.StatusRequestEntityTooLarge:
		result.Class = provider.ErrorContextExceeded
	case code == "content_policy_violation" || code == "content_filter":
		result.Class = provider.ErrorContentRefused
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

type function struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   *string         `json:"arguments,omitempty"`
}

type tool struct {
	Type     string   `json:"type"`
	Function function `json:"function"`
}

type toolCall struct {
	Index    *int     `json:"index,omitempty"`
	ID       string   `json:"id,omitempty"`
	Type     string   `json:"type,omitempty"`
	Function function `json:"function"`
}

type part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []toolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type completionRequest struct {
	Model               string         `json:"model"`
	Messages            []message      `json:"messages"`
	Tools               []tool         `json:"tools,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens"`
	Stream              bool           `json:"stream,omitempty"`
	StreamOptions       *streamOptions `json:"stream_options,omitempty"`
}

func text(value string) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

// buildRequest converts the neutral request. Prompt caching is automatic in OpenAI for long prefixes,
// so CachePrompt needs no marker: the system prompt and tools already come first and stable.
func buildRequest(req provider.Request, stream bool) ([]byte, error) {
	if req.Model == "" || len(req.Messages) == 0 || req.MaxTokens <= 0 {
		return nil, errors.New("model, messages and max_tokens are required")
	}
	out := completionRequest{Model: req.Model, MaxCompletionTokens: req.MaxTokens, Stream: stream}
	if stream {
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	if req.System != "" {
		out.Messages = append(out.Messages, message{Role: "system", Content: text(req.System)})
	}
	for _, definition := range req.Tools {
		schema := definition.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out.Tools = append(out.Tools, tool{Type: "function", Function: function{Name: definition.Name, Description: definition.Description, Parameters: schema}})
	}
	for _, m := range req.Messages {
		converted, err := convertMessage(m)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, converted...)
	}
	return json.Marshal(out)
}

func convertMessage(m provider.Message) ([]message, error) {
	switch m.Role {
	case "assistant":
		out := message{Role: "assistant", Content: json.RawMessage(`null`)}
		if m.Text != "" {
			out.Content = text(m.Text)
		}
		for _, call := range m.Calls {
			args := string(call.Args)
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, toolCall{ID: call.ID, Type: "function", Function: function{Name: call.Name, Arguments: &args}})
		}
		if m.Text == "" && len(m.Calls) == 0 {
			return nil, errors.New("empty assistant message")
		}
		return []message{out}, nil
	case "user":
		if len(m.Images) == 0 {
			if m.Text == "" {
				return nil, errors.New("empty user message")
			}
			return []message{{Role: "user", Content: text(m.Text)}}, nil
		}
		var parts []part
		for _, image := range m.Images {
			parts = append(parts, part{Type: "image_url", ImageURL: &imageURL{URL: "data:" + image.MediaType + ";base64," + base64.StdEncoding.EncodeToString(image.Data)}})
		}
		if m.Text != "" {
			parts = append(parts, part{Type: "text", Text: m.Text})
		}
		encoded, err := json.Marshal(parts)
		return []message{{Role: "user", Content: encoded}}, err
	case "tool":
		if len(m.Results) == 0 {
			return nil, errors.New("empty tool message")
		}
		out := make([]message, 0, len(m.Results))
		for _, result := range m.Results {
			content := result.Content
			if result.Error && !strings.HasPrefix(content, "NEGADO:") {
				content = errorPrefix + content
			}
			out = append(out, message{Role: "tool", ToolCallID: result.ID, Content: text(content)})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown role %q", m.Role)
	}
}

// ---------------------------------------------------------------- response

type usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// neutral splits the prompt tokens into uncached input and cache reads; OpenAI does not bill cache
// writes, so CacheWrite is 0 (provider.Usage).
func (u usage) neutral() provider.Usage {
	cached := min(u.PromptTokensDetails.CachedTokens, u.PromptTokens)
	return provider.Usage{Input: u.PromptTokens - cached, Output: u.CompletionTokens, CacheRead: cached}
}

type choice struct {
	Message struct {
		Content   *string    `json:"content"`
		Refusal   *string    `json:"refusal"`
		ToolCalls []toolCall `json:"tool_calls"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type completion struct {
	ID      string   `json:"id"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

func (c completion) toResponse(requestID string) (provider.Response, error) {
	if requestID == "" {
		requestID = c.ID
	}
	used := c.Usage.neutral()
	if len(c.Choices) == 0 {
		return provider.Response{}, &provider.Error{Class: provider.ErrorTransient, Message: "answer without choice", PartialUsage: used}
	}
	first := c.Choices[0]
	if (first.Message.Refusal != nil && *first.Message.Refusal != "") || first.FinishReason == "content_filter" {
		return provider.Response{}, &provider.Error{Class: provider.ErrorContentRefused, Message: "provider refused the request", PartialUsage: used}
	}
	answer := provider.Message{Role: "assistant"}
	if first.Message.Content != nil {
		answer.Text = *first.Message.Content
	}
	for _, call := range first.Message.ToolCalls {
		args := json.RawMessage(`{}`)
		if call.Function.Arguments != nil && json.Valid([]byte(*call.Function.Arguments)) && *call.Function.Arguments != "" {
			args = json.RawMessage(*call.Function.Arguments)
		}
		answer.Calls = append(answer.Calls, provider.ToolCall{ID: call.ID, Name: call.Function.Name, Args: args})
	}
	stop := provider.StopEnd
	switch first.FinishReason {
	case "tool_calls", "function_call":
		stop = provider.StopTool
	case "length":
		stop = provider.StopLimit
	}
	if stop == provider.StopEnd && len(answer.Calls) > 0 {
		stop = provider.StopTool
	}
	return provider.Response{Message: answer, Usage: used, Stop: stop, ProviderID: requestID}, nil
}

// ---------------------------------------------------------------- streaming

type streamChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content   *string    `json:"content"`
			Refusal   *string    `json:"refusal"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type partialCall struct {
	id, name string
	args     strings.Builder
}

// streamState rebuilds the final completion from the chunks.
type streamState struct {
	id      string
	text    strings.Builder
	refusal strings.Builder
	calls   map[int]*partialCall
	finish  string
	usage   usage
	output  int // progress estimate until the final usage arrives
}

func readStream(ctx context.Context, body io.Reader, requestID string, onChunk func(provider.Chunk)) (provider.Response, error) {
	state := &streamState{calls: map[int]*partialCall{}}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return state.response(requestID)
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			result := &provider.Error{Class: provider.ErrorTransient, Message: shorten(chunk.Error.Message), PartialUsage: state.progress()}
			if chunk.Error.Type == "rate_limit_exceeded" || chunk.Error.Type == "rate_limit_error" {
				result.Class = provider.ErrorRateLimit
			}
			return provider.Response{}, result
		}
		state.apply(chunk, onChunk)
	}
	return provider.Response{}, abortedOr(ctx, state.progress(), &provider.Error{Class: provider.ErrorTransient, Message: "stream ended early"})
}

func (s *streamState) progress() provider.Usage {
	used := s.usage.neutral()
	if used.Output == 0 {
		used.Output = s.output
	}
	return used
}

func (s *streamState) apply(chunk streamChunk, onChunk func(provider.Chunk)) {
	if chunk.ID != "" {
		s.id = chunk.ID
	}
	if chunk.Usage != nil {
		s.usage = *chunk.Usage
	}
	for _, c := range chunk.Choices {
		if c.FinishReason != nil && *c.FinishReason != "" {
			s.finish = *c.FinishReason
		}
		if c.Delta.Refusal != nil {
			s.refusal.WriteString(*c.Delta.Refusal)
		}
		if c.Delta.Content != nil && *c.Delta.Content != "" {
			s.text.WriteString(*c.Delta.Content)
			s.output += (len(*c.Delta.Content) + 3) / 4
			onChunk(provider.Chunk{PartialText: *c.Delta.Content, PartialUsage: s.progress()})
		}
		for _, call := range c.Delta.ToolCalls {
			index := 0
			if call.Index != nil {
				index = *call.Index
			}
			partial, ok := s.calls[index]
			if !ok {
				partial = &partialCall{}
				s.calls[index] = partial
			}
			if call.ID != "" {
				partial.id = call.ID
			}
			if call.Function.Name != "" {
				partial.name = call.Function.Name
			}
			if call.Function.Arguments != nil {
				partial.args.WriteString(*call.Function.Arguments)
			}
		}
	}
}

func (s *streamState) response(requestID string) (provider.Response, error) {
	var answer completion
	answer.ID, answer.Usage = s.id, s.usage
	var c choice
	c.FinishReason = s.finish
	if s.text.Len() > 0 {
		content := s.text.String()
		c.Message.Content = &content
	}
	if s.refusal.Len() > 0 {
		refusal := s.refusal.String()
		c.Message.Refusal = &refusal
	}
	indexes := make([]int, 0, len(s.calls))
	for index := range s.calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		partial := s.calls[index]
		args := partial.args.String()
		c.Message.ToolCalls = append(c.Message.ToolCalls, toolCall{ID: partial.id, Type: "function", Function: function{Name: partial.name, Arguments: &args}})
	}
	answer.Choices = []choice{c}
	return answer.toResponse(requestID)
}
