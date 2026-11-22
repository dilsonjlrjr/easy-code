package http

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	apppricing "easycode/internal/application/pricing"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
)

// PricingUseCases are the model matrix and price table use cases (US-37, US-24, US-16).
type PricingUseCases interface {
	ListModels(ctx context.Context, principal identity.Principal, filter apppricing.ModelFilter) ([]apppricing.ModelView, error)
	RecordModel(ctx context.Context, principal identity.Principal, model pricing.Model) (apppricing.ModelView, error)
	ListPrices(ctx context.Context, principal identity.Principal, providerName, name string) ([]pricing.Price, error)
	RecordPrice(ctx context.Context, principal identity.Principal, providerName, name string, price pricing.Price) (pricing.Price, error)
	CorrectPrice(ctx context.Context, principal identity.Principal, providerName, name string, id int64, version string, change apppricing.PriceChange) (pricing.Price, error)
}

type capabilitiesJSON struct {
	ToolUse       bool `json:"tool_use"`
	ContextTokens int  `json:"context_tokens"`
	PromptCache   bool `json:"prompt_cache"`
	ImageInput    bool `json:"image_input"`
}

type perMillionJSON struct {
	Input      string `json:"input"`
	Output     string `json:"output"`
	ReadCache  string `json:"read_cache"`
	WriteCache string `json:"write_cache"`
}

type modelPriceJSON struct {
	ID             int64          `json:"id"`
	EffectiveSince time.Time      `json:"effective_since"`
	Currency       string         `json:"currency"`
	PerMillion     perMillionJSON `json:"per_million_tokens"`
	Source         string         `json:"source"`
	TableVersion   string         `json:"table_version"`
	Used           bool           `json:"used"`
	RecordedAt     time.Time      `json:"recorded_at"`
	RecordedBy     userRefJSON    `json:"recorded_by"`
}

type modelJSON struct {
	Provider     string           `json:"provider"`
	Model        string           `json:"model"`
	DisplayName  string           `json:"display_name"`
	Capabilities capabilitiesJSON `json:"capabilities"`
	Active       bool             `json:"active"`
	CurrentPrice *modelPriceJSON  `json:"current_price"`
}

func priceOf(p pricing.Price) modelPriceJSON {
	return modelPriceJSON{
		ID: p.ID, EffectiveSince: p.EffectiveSince, Currency: p.Currency, Source: p.Source, TableVersion: p.TableVersion(),
		Used: p.Used, RecordedAt: p.RecordedAt, RecordedBy: userRefJSON{ID: p.RecordedBy, Name: p.RecordedByName},
		PerMillion: perMillionJSON{Input: p.PerMillion.Input, Output: p.PerMillion.Output, ReadCache: p.PerMillion.ReadCache, WriteCache: p.PerMillion.WriteCache},
	}
}

func modelOf(view apppricing.ModelView) modelJSON {
	m := view.Model
	out := modelJSON{
		Provider: m.Provider, Model: m.Name, DisplayName: m.DisplayName, Active: m.Active,
		Capabilities: capabilitiesJSON{ToolUse: m.Capabilities.ToolUse, ContextTokens: m.Capabilities.ContextTokens, PromptCache: m.Capabilities.PromptCache, ImageInput: m.Capabilities.ImageInput},
	}
	if view.CurrentPrice != nil {
		price := priceOf(*view.CurrentPrice)
		out.CurrentPrice = &price
	}
	return out
}

func modelPath(rc *fasthttp.RequestCtx) (string, string) {
	name, err := url.PathUnescape(PathParam(rc, "model"))
	if err != nil {
		name = ""
	}
	return PathParam(rc, "provider"), name
}

func (a *api) listModels(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	filter := apppricing.ModelFilter{Provider: string(rc.QueryArgs().Peek("provider"))}
	if raw := string(rc.QueryArgs().Peek("active")); raw != "" {
		active, err := strconv.ParseBool(raw)
		if err != nil {
			return failure.Invalid(failure.FieldError{Field: "active", Message: "invalid"})
		}
		filter.Active = &active
	}
	views, err := a.deps.Pricing.ListModels(RequestContext(rc), principal, filter)
	if err != nil {
		return err
	}
	items := make([]modelJSON, 0, len(views))
	for _, view := range views {
		items = append(items, modelOf(view))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, itemsJSON[modelJSON]{Items: items})
	return nil
}

func (a *api) recordModel(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body struct {
		DisplayName  *string           `json:"display_name"`
		Capabilities *capabilitiesJSON `json:"capabilities"`
		Active       *bool             `json:"active"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	var missing []failure.FieldError
	if body.DisplayName == nil {
		missing = append(missing, failure.FieldError{Field: "display_name", Message: "required"})
	}
	if body.Capabilities == nil {
		missing = append(missing, failure.FieldError{Field: "capabilities", Message: "required"})
	}
	if body.Active == nil {
		missing = append(missing, failure.FieldError{Field: "active", Message: "required"})
	}
	if len(missing) > 0 {
		return failure.Invalid(missing...)
	}
	providerName, name := modelPath(rc)
	caps := body.Capabilities
	view, err := a.deps.Pricing.RecordModel(RequestContext(rc), principal, pricing.Model{
		Provider: providerName, Name: name, DisplayName: *body.DisplayName, Active: *body.Active,
		Capabilities: pricing.Capabilities{ToolUse: caps.ToolUse, ContextTokens: caps.ContextTokens, PromptCache: caps.PromptCache, ImageInput: caps.ImageInput},
	})
	if err != nil {
		return err
	}
	a.responder.JSON(rc, fasthttp.StatusOK, modelOf(view))
	return nil
}

func (a *api) listModelPrices(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	providerName, name := modelPath(rc)
	prices, err := a.deps.Pricing.ListPrices(RequestContext(rc), principal, providerName, name)
	if err != nil {
		return err
	}
	items := make([]modelPriceJSON, 0, len(prices))
	for _, price := range prices {
		items = append(items, priceOf(price))
	}
	a.responder.JSON(rc, fasthttp.StatusOK, itemsJSON[modelPriceJSON]{Items: items})
	return nil
}

type newPriceJSON struct {
	EffectiveSince *time.Time      `json:"effective_since"`
	Currency       string          `json:"currency"`
	PerMillion     *perMillionJSON `json:"per_million_tokens"`
	Source         string          `json:"source"`
}

func (a *api) recordModelPrice(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	var body newPriceJSON
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	if body.EffectiveSince == nil || body.PerMillion == nil {
		return failure.Invalid(failure.FieldError{Field: "effective_since", Message: "required"}, failure.FieldError{Field: "per_million_tokens", Message: "required"})
	}
	providerName, name := modelPath(rc)
	price := pricing.Price{
		EffectiveSince: *body.EffectiveSince, Currency: body.Currency, Source: body.Source,
		PerMillion: pricing.PerMillion{Input: body.PerMillion.Input, Output: body.PerMillion.Output, ReadCache: body.PerMillion.ReadCache, WriteCache: body.PerMillion.WriteCache},
	}
	return a.idempotent(rc, principal, "recordModelPrice", func(ctx context.Context) (created, error) {
		recorded, err := a.deps.Pricing.RecordPrice(ctx, principal, providerName, name, price)
		if err != nil {
			return created{}, err
		}
		location := APIPrefix + "/providers/" + providerName + "/models/" + url.PathEscape(name) + "/prices/" + recorded.TableVersion()
		return created{status: fasthttp.StatusCreated, body: priceOf(recorded), location: location}, nil
	})
}

func (a *api) correctModelPrice(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	version, err := ifMatch(rc)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(PathParam(rc, "effectivePeriodId"), 10, 64)
	if err != nil || id < 1 {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	var body struct {
		EffectiveSince *time.Time      `json:"effective_since"`
		PerMillion     *perMillionJSON `json:"per_million_tokens"`
		Source         *string         `json:"source"`
	}
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	change := apppricing.PriceChange{EffectiveSince: body.EffectiveSince, Source: body.Source}
	if body.PerMillion != nil {
		change.PerMillion = &pricing.PerMillion{Input: body.PerMillion.Input, Output: body.PerMillion.Output, ReadCache: body.PerMillion.ReadCache, WriteCache: body.PerMillion.WriteCache}
	}
	providerName, name := modelPath(rc)
	corrected, err := a.deps.Pricing.CorrectPrice(RequestContext(rc), principal, providerName, name, id, version, change)
	if err != nil {
		return err
	}
	setETag(rc, corrected.Version())
	a.responder.JSON(rc, fasthttp.StatusOK, priceOf(corrected))
	return nil
}
