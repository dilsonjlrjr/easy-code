package logging

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// Redacted replaces every value that must never reach a log line (RNF-01, RNF-20).
const Redacted = "[REDACTED]"

// sensitiveKeyParts mark a key as secret when its normalized form contains any of them.
var sensitiveKeyParts = []string{
	"password", "passwd", "secret", "token", "authorization", "cookie", "api_key", "apikey",
	"private_key", "credential", "session", "prompt", "completion", "dsn", "database_url",
	"nonce", "verifier", "csrf",
}

// sensitiveKeys mark content fields: prompts, model answers, artifact bodies and raw payloads.
var sensitiveKeys = map[string]bool{
	"key": true, "content": true, "body": true, "request_body": true, "response_body": true,
	"artifact_content": true, "response": true, "message_content": true, "payload": true,
}

// metadataSuffixes keep counters about sensitive things loggable, such as prompt_tokens.
var metadataSuffixes = []string{"_count", "_tokens", "_bytes"}

var secretPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`), "$1 " + Redacted},
	{regexp.MustCompile(`://([^:/@\s]+):([^@\s]+)@`), "://$1:" + Redacted + "@"},
	{regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key)\s*([=:])\s*("[^"]*"|'[^']*'|[^\s,;&]+)`), "$1$2" + Redacted},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), Redacted},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`), Redacted},
	// OIDC values in a URL or form (login flow of ADR-12): authorization code, state, nonce, PKCE.
	{regexp.MustCompile(`(?i)([?&\s]|^)(code|state|session_state|nonce|code_verifier|code_challenge)=[^&\s]+`), "$1$2=" + Redacted},
}

// IsSensitiveKey reports whether a log attribute key names a secret or content field.
func IsSensitiveKey(key string) bool {
	normalized := strings.ReplaceAll(strings.ToLower(key), "-", "_")
	for _, suffix := range metadataSuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return false
		}
	}
	if sensitiveKeys[normalized] {
		return true
	}
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

// ScrubText masks secrets embedded in free text, such as error messages and URLs.
func ScrubText(text string) string {
	for _, secret := range secretPatterns {
		text = secret.pattern.ReplaceAllString(text, secret.replacement)
	}
	return text
}

// Redact is the central redaction rule. It has the slog ReplaceAttr signature and runs on
// every attribute the logger writes.
func Redact(_ []string, attr slog.Attr) slog.Attr {
	if IsSensitiveKey(attr.Key) {
		return slog.String(attr.Key, Redacted)
	}
	attr.Value = redactValue(attr.Value)
	return attr
}

func redactValue(value slog.Value) slog.Value {
	switch value.Kind() {
	case slog.KindString:
		return slog.StringValue(ScrubText(value.String()))
	case slog.KindAny:
		return slog.AnyValue(redactAny(value.Any()))
	default:
		return value
	}
}

func redactAny(value any) any {
	switch typed := value.(type) {
	case error:
		return ScrubText(typed.Error())
	case fmt.Stringer:
		return ScrubText(typed.String())
	case map[string]any:
		return redactMap(typed)
	case map[string]string:
		return redactStringMap(typed)
	case map[string][]string:
		return redactHeaderMap(typed)
	case []any:
		return redactSlice(typed)
	case string:
		return ScrubText(typed)
	default:
		return value
	}
}

func redactMap(values map[string]any) map[string]any {
	clean := make(map[string]any, len(values))
	for key, value := range values {
		if IsSensitiveKey(key) {
			clean[key] = Redacted
			continue
		}
		clean[key] = redactAny(value)
	}
	return clean
}

func redactStringMap(values map[string]string) map[string]string {
	clean := make(map[string]string, len(values))
	for key, value := range values {
		if IsSensitiveKey(key) {
			clean[key] = Redacted
			continue
		}
		clean[key] = ScrubText(value)
	}
	return clean
}

func redactHeaderMap(values map[string][]string) map[string][]string {
	clean := make(map[string][]string, len(values))
	for key, list := range values {
		if IsSensitiveKey(key) {
			clean[key] = []string{Redacted}
			continue
		}
		scrubbed := make([]string, len(list))
		for i, item := range list {
			scrubbed[i] = ScrubText(item)
		}
		clean[key] = scrubbed
	}
	return clean
}

func redactSlice(values []any) []any {
	clean := make([]any, len(values))
	for i, value := range values {
		clean[i] = redactAny(value)
	}
	return clean
}
