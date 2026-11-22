// Package fake is a deterministic provider for tests and local development: same request,
// same response and usage, no network. It never runs against a real provider key.
//
// Behavior:
//   - the answer is Script[n] for the n-th call when Script is set, else "echo: <last user text>";
//   - usage counts one token per 4 bytes (rounded up) of system+messages (input) and of the answer
//     (output); with CachePrompt the system tokens count as cache read instead of input;
//   - a last user text "fake:error:<class>" fails with that ErrorClass (retry and lost-cost tests);
//   - streaming sends one chunk per word; canceling ctx aborts with the partial usage;
//   - the key "invalid" fails TestKey and Complete with ErrorAuthentication.
package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"easycode/internal/domain/provider"
)

// Name is the provider name of the fake.
const Name = "fake"

// InvalidKey is the key the fake treats as revoked.
const InvalidKey = "invalid"

const errorPrefix = "fake:error:"

// Provider implements provider.Provider deterministically.
type Provider struct {
	// Script, when set, answers the calls in order (the last one repeats).
	Script []provider.Message
	// Alias, when set, is the provider name it answers for (development stand-in, such as
	// "anthropic=fake" in EASYCODE_GATEWAY_PROVIDERS), so the product flow runs with no real key.
	Alias string

	mu    sync.Mutex
	calls int
}

var _ provider.Provider = (*Provider)(nil)

// Name implements provider.Provider.
func (p *Provider) Name() string {
	if p.Alias != "" {
		return p.Alias
	}
	return Name
}

// Capabilities implements provider.Provider.
func (*Provider) Capabilities(context.Context, string) (provider.Capabilities, error) {
	return provider.Capabilities{ToolUse: true, ContextTokens: 200_000, CachePrompt: true}, nil
}

// TestKey implements provider.Provider.
func (*Provider) TestKey(_ context.Context, key provider.Secret) error {
	if key.Empty() || string(key.Reveal()) == InvalidKey {
		return &provider.Error{Class: provider.ErrorAuthentication, Message: "key rejected"}
	}
	return nil
}

// Complete implements provider.Provider.
func (p *Provider) Complete(ctx context.Context, req provider.Request, onChunk func(provider.Chunk)) (provider.Response, error) {
	key, ok := provider.KeyFromContext(ctx)
	if !ok || string(key.Reveal()) == InvalidKey {
		return provider.Response{}, &provider.Error{Class: provider.ErrorAuthentication, Message: "key rejected"}
	}
	if req.Model == "" || len(req.Messages) == 0 {
		return provider.Response{}, &provider.Error{Class: provider.ErrorRequest, Message: "model and messages are required"}
	}
	number := p.nextCall()
	usage := inputUsage(req)
	if class, failing := requestedError(req); failing {
		return provider.Response{}, &provider.Error{Class: class, Message: "fake failure", RetryAfter: retryAfter(class), PartialUsage: usage}
	}
	answer := p.answer(req, number)

	if onChunk != nil {
		partial := usage
		for _, word := range strings.SplitAfter(answer.Text, " ") {
			if err := ctx.Err(); err != nil {
				return provider.Response{}, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted", PartialUsage: partial}
			}
			partial.Output += tokens(word)
			onChunk(provider.Chunk{PartialText: word, PartialUsage: partial})
		}
	}
	if err := ctx.Err(); err != nil {
		return provider.Response{}, &provider.Error{Class: provider.ErrorAborted, Message: "call aborted", PartialUsage: usage}
	}

	usage.Output = tokens(answer.Text)
	stop := provider.StopEnd
	if len(answer.Calls) > 0 {
		stop = provider.StopTool
	}
	if req.MaxTokens > 0 && usage.Output > req.MaxTokens {
		usage.Output = req.MaxTokens
		stop = provider.StopLimit
	}
	return provider.Response{Message: answer, Usage: usage, Stop: stop, ProviderID: fmt.Sprintf("fake-%d", number)}, nil
}

func (p *Provider) nextCall() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.calls
}

func (p *Provider) answer(req provider.Request, number int) provider.Message {
	if len(p.Script) > 0 {
		answer := p.Script[min(number, len(p.Script))-1]
		answer.Role = "assistant"
		return answer
	}
	return provider.Message{Role: "assistant", Text: "echo: " + lastUserText(req)}
}

func inputUsage(req provider.Request) provider.Usage {
	var usage provider.Usage
	if req.CachePrompt {
		usage.CacheRead = tokens(req.System)
	} else {
		usage.Input = tokens(req.System)
	}
	for _, message := range req.Messages {
		usage.Input += tokens(message.Text)
		for _, result := range message.Results {
			usage.Input += tokens(result.Content)
		}
		for _, call := range message.Calls {
			usage.Input += tokens(string(call.Args))
		}
	}
	return usage
}

func requestedError(req provider.Request) (provider.ErrorClass, bool) {
	text := lastUserText(req)
	if !strings.HasPrefix(text, errorPrefix) {
		return "", false
	}
	return provider.ErrorClass(strings.TrimPrefix(text, errorPrefix)), true
}

func retryAfter(class provider.ErrorClass) time.Duration {
	if class == provider.ErrorRateLimit {
		return time.Second
	}
	return 0
}

func lastUserText(req provider.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return req.Messages[i].Text
		}
	}
	return ""
}

func tokens(text string) int { return (len(text) + 3) / 4 }

// KeyOpener opens "sealed" keys in unit tests: the sealed bytes are the key. No binary wires it:
// easycode-gateway always opens with the platform private key (infrastructure/sealing, ADR-13).
type KeyOpener struct{}

// Open returns the sealed bytes as the key.
func (KeyOpener) Open(_ context.Context, sealed []byte) (provider.Secret, error) {
	return provider.NewSecret(append([]byte(nil), sealed...)), nil
}
