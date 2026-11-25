package http

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/providerkey"
	"easycode/internal/domain/identity"
)

// ProviderUseCases are the provider key use cases (US-15).
type ProviderUseCases interface {
	List(ctx context.Context, principal identity.Principal) ([]providerkey.KeyStatus, error)
	Save(ctx context.Context, principal identity.Principal, providerName, key string) (providerkey.KeyStatus, error)
	Remove(ctx context.Context, principal identity.Principal, providerName string) error
}

var providerNames = map[string]string{"anthropic": "Anthropic", "openai": "OpenAI", providerkey.ProviderClaudeCode: "Claude Code"}

type userRefJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type lastTestJSON struct {
	Result   string    `json:"result"`
	Message  string    `json:"message,omitempty"`
	TestedAt time.Time `json:"tested_at"`
}

type providerKeyJSON struct {
	Status   string        `json:"status"`
	LastTest *lastTestJSON `json:"last_test"`
	Last4    *string       `json:"last_4"`
	SavedAt  *time.Time    `json:"saved_at"`
	SavedBy  *userRefJSON  `json:"saved_by"`
	TestedAt *time.Time    `json:"tested_at"`
	// CredentialKind is only on claude-code with an active or testing key: api_key or
	// subscription_token (US-58 CA-1, CA-4); never the value.
	CredentialKind *string `json:"credential_kind,omitempty"`
}

// detectionJSON is the Claude Code detection by easycode-sandbox (US-58 CA-1, CA-2).
type detectionJSON struct {
	Detected  bool       `json:"detected"`
	Version   *string    `json:"version"`
	CheckedAt *time.Time `json:"checked_at"`
}

type providerJSON struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Key       providerKeyJSON `json:"key"`
	Detection *detectionJSON  `json:"detection,omitempty"`
}

type itemsJSON[T any] struct {
	Items []T `json:"items"`
}

// providerOf never carries the key value: only status, last 4 characters and dates (US-15 CA-2).
func providerOf(status providerkey.KeyStatus) providerJSON {
	key := providerKeyJSON{Status: status.Status, SavedAt: status.SavedAt, TestedAt: status.TestedAt}
	if status.Last4 != "" {
		last4 := status.Last4
		key.Last4 = &last4
	}
	if status.SavedBy != nil {
		key.SavedBy = &userRefJSON{ID: status.SavedBy.Subject, Name: status.SavedBy.Name}
	}
	if status.LastTest != nil {
		key.LastTest = &lastTestJSON{Result: status.LastTest.Result, Message: status.LastTest.Message, TestedAt: status.LastTest.TestedAt}
	}
	if status.CredentialKind != "" {
		kind := status.CredentialKind
		key.CredentialKind = &kind
	}
	provider := providerJSON{ID: status.Provider, Name: providerNames[status.Provider], Key: key}
	if status.Runtime != nil {
		detection := &detectionJSON{Detected: status.Runtime.Detected, CheckedAt: status.Runtime.CheckedAt}
		if status.Runtime.Version != "" {
			version := status.Runtime.Version
			detection.Version = &version
		}
		provider.Detection = detection
	}
	return provider
}

func (a *api) listProviders(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	statuses, err := a.deps.Providers.List(RequestContext(rc), principal)
	if err != nil {
		return err
	}
	items := make([]providerJSON, 0, len(statuses))
	for _, status := range statuses {
		items = append(items, providerOf(status))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, itemsJSON[providerJSON]{Items: items})
	return nil
}

// saveProviderKey answers only after the key test (up to 15 s). The body is never logged.
func (a *api) saveProviderKey(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	status, err := a.deps.Providers.Save(RequestContext(rc), principal, PathParam(rc, "provider"), body.Key)
	body.Key = ""
	rc.Request.ResetBody()
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, providerOf(status))
	return nil
}

func (a *api) removeProviderKey(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	if err := a.deps.Providers.Remove(RequestContext(rc), principal, PathParam(rc, "provider")); err != nil {
		return err
	}
	a.responder.NoContent(rc)
	return nil
}
