package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"easycode/internal/infrastructure/logging"
)

const secret = "s3cr3t-VALUE-never-logged"

func captureLog(t *testing.T, write func(*slog.Logger)) string {
	t.Helper()
	var buffer bytes.Buffer
	write(logging.New(&buffer, slog.LevelDebug, "test", "dev"))
	return buffer.String()
}

func TestRedactionMasksSensitiveKeys(t *testing.T) {
	keys := []string{
		"password", "db_password", "api_key", "apiKey", "Authorization", "cookie", "Set-Cookie",
		"access_token", "refresh_token", "client_secret", "private_key", "session", "prompt",
		"system_prompt", "completion", "content", "artifact_content", "body", "response_body",
		"credentials", "database_url", "key", "id_token", "code_verifier", "nonce", "csrf_token",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			output := captureLog(t, func(logger *slog.Logger) {
				logger.Info("event", slog.String(key, secret))
			})
			if strings.Contains(output, secret) {
				t.Fatalf("key %q leaked: %s", key, output)
			}
		})
	}
}

func TestRedactionMasksKeysInsideGroupsAndMaps(t *testing.T) {
	output := captureLog(t, func(logger *slog.Logger) {
		logger.Info("event",
			slog.Group("request", slog.String("authorization", secret)),
			slog.Any("headers", map[string][]string{"Cookie": {secret}, "Accept": {"text/html"}}),
			slog.Any("fields", map[string]any{"nested": map[string]any{"token": secret}}),
		)
	})
	if strings.Contains(output, secret) {
		t.Fatalf("secret leaked: %s", output)
	}
	if !strings.Contains(output, "text/html") {
		t.Fatalf("non-sensitive value dropped: %s", output)
	}
}

func TestRedactionScrubsSecretsInsideFreeText(t *testing.T) {
	texts := []string{
		"dial postgres://easycode:" + secret + "@db:5432/easycode failed",
		"header Authorization: Bearer " + secret,
		"connect password=" + secret + " host=db",
		"provider key sk-" + secret + "abcdefghij",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0." + secret,
		// OIDC login flow (ADR-12, MARCO-2): code, state, nonce and PKCE never reach a log line.
		"GET /api/v1/session/oidc/callback?code=" + secret + "&state=x",
		"callback?state=" + secret,
		"form grant_type=authorization_code&code_verifier=" + secret,
		"auth?nonce=" + secret + "&code_challenge=" + secret,
	}
	for _, text := range texts {
		output := captureLog(t, func(logger *slog.Logger) {
			logger.Error("failed", slog.String("detail", text), logging.ErrorAttr(errors.New(text)))
		})
		if strings.Contains(output, secret) {
			t.Fatalf("secret leaked from %q: %s", text, output)
		}
	}
}

func TestRedactionKeepsMetadata(t *testing.T) {
	output := captureLog(t, func(logger *slog.Logger) {
		logger.Info("model call", slog.Int("prompt_tokens", 120), slog.String("project_id", "42"))
	})
	var line map[string]any
	if err := json.Unmarshal([]byte(output), &line); err != nil {
		t.Fatal(err)
	}
	if line["prompt_tokens"] != float64(120) || line["project_id"] != "42" {
		t.Fatalf("metadata lost: %s", output)
	}
}

func TestErrorAttrRecordsCauseChain(t *testing.T) {
	cause := errors.New("connection refused")
	output := captureLog(t, func(logger *slog.Logger) {
		logger.Error("probe failed", logging.ErrorAttr(fmt.Errorf("ping: %w", cause)))
	})
	if !strings.Contains(output, "*fmt.wrapError") || !strings.Contains(output, "*errors.errorString") {
		t.Fatalf("cause chain missing: %s", output)
	}
}

func TestLoggerAddsRequestID(t *testing.T) {
	output := captureLog(t, func(logger *slog.Logger) {
		ctx := logging.WithRequestID(context.Background(), "req-1")
		logger.InfoContext(ctx, "handled")
	})
	if !strings.Contains(output, `"request_id":"req-1"`) {
		t.Fatalf("request id missing: %s", output)
	}
}
