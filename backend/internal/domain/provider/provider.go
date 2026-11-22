// Package provider is the boundary of AI provider independence (PROJETO 9.1, ADR-03,
// docs/arquitetura/api/interno.md section 3). Adapters live only in easycode-gateway.
// JSON tags define the wire format between the worker and easycode-gateway.
package provider

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// Message is provider neutral. Role is "user", "assistant" or "tool".
type Message struct {
	Role    string       `json:"role"`
	Text    string       `json:"text,omitempty"`
	Images  []Image      `json:"images,omitempty"`  // only when the model has image_input
	Calls   []ToolCall   `json:"calls,omitempty"`   // asked by the model (Role = "assistant")
	Results []ToolResult `json:"results,omitempty"` // returned to the model (Role = "tool")
	// Opaque is the provider's own encoding of an assistant answer (for example reasoning
	// blocks that must be replayed unchanged). Only the adapter that produced it reads it;
	// it is content, so it never goes to logs or events.
	Opaque json.RawMessage `json:"opaque,omitempty"`
}

// Image is an inline image input.
type Image struct {
	MediaType string `json:"media_type"`
	Data      []byte `json:"data"`
}

// ToolCall is a tool use the model asked for.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolResult answers a ToolCall.
type ToolResult struct {
	ID      string `json:"id"`
	Content string `json:"content"` // includes "NEGADO: ..." when the tool was blocked (RNF-07)
	Error   bool   `json:"error,omitempty"`
}

// ToolDefinition is a tool offered to the model.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"` // JSON Schema of the arguments
}

// Request is one model call.
type Request struct {
	Model       string           `json:"model"`
	System      string           `json:"system,omitempty"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"` // only the ones allowed to the agent
	MaxTokens   int              `json:"max_tokens"`
	CachePrompt bool             `json:"cache_prompt,omitempty"` // mark system+tools cacheable when supported
}

// Usage counts tokens per type: the base of cost (price per type, PROJETO 10). A provider that
// does not bill cache writes returns CacheWrite = 0.
type Usage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
}

// Add sums two usages (partial usage across chunks or attempts).
func (u Usage) Add(other Usage) Usage {
	return Usage{
		Input: u.Input + other.Input, Output: u.Output + other.Output,
		CacheRead: u.CacheRead + other.CacheRead, CacheWrite: u.CacheWrite + other.CacheWrite,
	}
}

// Stop reasons.
const (
	StopEnd   = "end"
	StopTool  = "tool"
	StopLimit = "limit"
)

// Response is the result of a model call.
type Response struct {
	Message    Message `json:"message"`
	Usage      Usage   `json:"usage"`
	Stop       string  `json:"stop"`        // StopEnd | StopTool | StopLimit
	ProviderID string  `json:"provider_id"` // provider request id, for support (never the content)
}

// Chunk is a streaming fragment for live progress; its content never goes to events.
type Chunk struct {
	PartialText  string `json:"partial_text,omitempty"`
	PartialUsage Usage  `json:"partial_usage"`
}

// Capabilities of a model (PROJETO 9.2); price is not here (versioned price table).
type Capabilities struct {
	ToolUse       bool `json:"tool_use"`
	ContextTokens int  `json:"context_tokens"`
	CachePrompt   bool `json:"cache_prompt"`
	ImageInput    bool `json:"image_input"`
}

// ErrorClass classifies a provider failure.
type ErrorClass string

// Error classes. The gateway never retries: retry and backoff belong to the orchestrator, which
// records the lost cost of each attempt.
const (
	ErrorTransient       ErrorClass = "transient"        // network, 5xx, call timeout: retry
	ErrorRateLimit       ErrorClass = "rate_limit"       // 429: retry after RetryAfter
	ErrorAuthentication  ErrorClass = "authentication"   // invalid or revoked key: wave fails provider_without_key
	ErrorRequest         ErrorClass = "invalid_request"  // our mistake: not recoverable
	ErrorContextExceeded ErrorClass = "context_exceeded" // runtime compacts or fails
	ErrorContentRefused  ErrorClass = "content_refused"  // provider policy
	ErrorAborted         ErrorClass = "aborted"          // ctx canceled (pause now, cancel)
)

// Error is a classified provider failure.
type Error struct {
	Class        ErrorClass    `json:"class"`
	RetryAfter   time.Duration `json:"retry_after_ns,omitempty"` // provider hint (retry-after header), if any
	Message      string        `json:"message"`                  // short, without secret or content
	PartialUsage Usage         `json:"partial_usage"`            // tokens consumed by an aborted call (lost cost)
}

func (e *Error) Error() string { return string(e.Class) + ": " + e.Message }

// Provider is implemented by each provider adapter inside easycode-gateway.
type Provider interface {
	Name() string // "anthropic" | "openai"

	// Complete makes one call, streaming through onChunk when it is not nil and the provider
	// supports it. Canceling ctx aborts the call with *Error{Class: ErrorAborted, PartialUsage}.
	// The key comes in ctx (KeyFromContext) and is never kept by the adapter.
	Complete(ctx context.Context, req Request, onChunk func(Chunk)) (Response, error)

	// Capabilities reports the matrix for a model, to check the registered data (US-24 CA-4).
	Capabilities(ctx context.Context, model string) (Capabilities, error)

	// TestKey makes the minimal validation call (US-15 CA-1, CA-3).
	TestKey(ctx context.Context, key Secret) error
}

// Secret never prints: String, GoString, MarshalJSON and LogValue return "***" (RNF-01).
type Secret struct{ value []byte }

// NewSecret wraps key material. The caller should not keep its own copy.
func NewSecret(value []byte) Secret { return Secret{value: value} }

func (Secret) String() string { return "***" }

// GoString implements fmt.GoStringer.
func (Secret) GoString() string { return "***" }

// MarshalJSON implements json.Marshaler.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"***"`), nil }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue("***") }

// Reveal returns the key material: only inside the adapter, when building the request.
func (s Secret) Reveal() []byte { return s.value }

// Empty reports whether there is no key.
func (s Secret) Empty() bool { return len(s.value) == 0 }

// Zero overwrites the key material in memory (session closed).
func (s Secret) Zero() {
	for i := range s.value {
		s.value[i] = 0
	}
}

type keyContext struct{}

// WithKey carries the opened key of the wave scope to the adapter.
func WithKey(ctx context.Context, key Secret) context.Context {
	return context.WithValue(ctx, keyContext{}, key)
}

// KeyFromContext returns the key set by WithKey.
func KeyFromContext(ctx context.Context) (Secret, bool) {
	key, ok := ctx.Value(keyContext{}).(Secret)
	return key, ok && !key.Empty()
}
