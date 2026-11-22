// Package pricing holds the use cases of the model matrix and the versioned price table
// (US-37, US-24 CA-4, US-16). Only the administrator changes them; everyone authenticated reads.
package pricing

import (
	"context"
	"time"

	"easycode/internal/application/audit"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
)

// Providers of the contract (ProviderId).
var Providers = []string{"anthropic", "openai"}

// CodeFailedPrecondition fails a correction whose version changed (If-Match, 412).
const CodeFailedPrecondition = "failed-precondition"

// ModelView is a model with the organization's current price.
type ModelView struct {
	Model        pricing.Model
	CurrentPrice *pricing.Price
}

// ModelFilter narrows listModels.
type ModelFilter struct {
	Provider string
	Active   *bool
}

// Catalog stores models and prices (models, model_prices).
type Catalog interface {
	Models(ctx context.Context, organizationID string, filter ModelFilter, at time.Time) ([]ModelView, error)
	// Model returns a registered model or not-found.
	Model(ctx context.Context, providerName, name string) (pricing.Model, error)
	SaveModel(ctx context.Context, model pricing.Model) error
	Prices(ctx context.Context, organizationID, providerName, name string) ([]pricing.Price, error)
	Price(ctx context.Context, organizationID, providerName, name string, id int64) (pricing.Price, error)
	RecordPrice(ctx context.Context, principal identity.Principal, providerName, name string, price pricing.Price) (pricing.Price, error)
	// CorrectPrice rewrites a future period never used; one in force or used fails with
	// effective-period-in-use (the database trigger also refuses it).
	CorrectPrice(ctx context.Context, organizationID, providerName, name string, price pricing.Price) (pricing.Price, error)
	OrganizationCurrency(ctx context.Context, organizationID string) (string, error)
	// CurrentPrice is the price in force at a moment for the project's organization, or
	// model-without-current-price (US-37 CA-2). The runtime cites its id in model_calls.
	CurrentPrice(ctx context.Context, projectID, providerName, name string, at time.Time) (pricing.Price, error)
}

// PriceChange is a JSON Merge Patch of a period (ModelPriceModification): nil keeps the value.
type PriceChange struct {
	EffectiveSince *time.Time
	PerMillion     *pricing.PerMillion
	Source         *string
}

// Service implements the price table use cases.
type Service struct {
	tx      transaction.Runner
	catalog Catalog
	trail   audit.Trail
	now     func() time.Time
}

// NewService builds the service.
func NewService(tx transaction.Runner, catalog Catalog, trail audit.Trail) *Service {
	return &Service{tx: tx, catalog: catalog, trail: trail, now: time.Now}
}

// ListModels is listModels.
func (s *Service) ListModels(ctx context.Context, principal identity.Principal, filter ModelFilter) ([]ModelView, error) {
	if filter.Provider != "" && !known(filter.Provider) {
		return nil, failure.Invalid(failure.FieldError{Field: "provider", Message: "enum"})
	}
	return s.catalog.Models(ctx, principal.OrganizationID, filter, s.now())
}

// RecordModel is recordModel: create or update the capability row (idempotent PUT).
func (s *Service) RecordModel(ctx context.Context, principal identity.Principal, model pricing.Model) (ModelView, error) {
	if err := s.admin(principal, model.Provider, model.Name); err != nil {
		return ModelView{}, err
	}
	if err := pricing.ValidateModel(model); err != nil {
		return ModelView{}, err
	}
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.catalog.SaveModel(ctx, model); err != nil {
			return err
		}
		return s.record(ctx, principal, "model.record", modelTarget(model.Provider, model.Name), "")
	})
	if err != nil {
		return ModelView{}, err
	}
	return s.view(ctx, principal, model.Provider, model.Name)
}

func (s *Service) view(ctx context.Context, principal identity.Principal, providerName, name string) (ModelView, error) {
	views, err := s.catalog.Models(ctx, principal.OrganizationID, ModelFilter{Provider: providerName}, s.now())
	if err != nil {
		return ModelView{}, err
	}
	for _, view := range views {
		if view.Model.Name == name {
			return view, nil
		}
	}
	return ModelView{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
}

// ListPrices is listModelPrices (effective_since descending).
func (s *Service) ListPrices(ctx context.Context, principal identity.Principal, providerName, name string) ([]pricing.Price, error) {
	if err := s.model(ctx, providerName, name); err != nil {
		return nil, err
	}
	return s.catalog.Prices(ctx, principal.OrganizationID, providerName, name)
}

// RecordPrice is recordModelPrice (US-37 CA-1). Call it inside the idempotency transaction.
func (s *Service) RecordPrice(ctx context.Context, principal identity.Principal, providerName, name string, price pricing.Price) (pricing.Price, error) {
	if err := s.admin(principal, providerName, name); err != nil {
		return pricing.Price{}, err
	}
	if err := s.model(ctx, providerName, name); err != nil {
		return pricing.Price{}, err
	}
	currency, err := s.catalog.OrganizationCurrency(ctx, principal.OrganizationID)
	if err != nil {
		return pricing.Price{}, err
	}
	if err := pricing.ValidatePrice(price, currency, s.now()); err != nil {
		return pricing.Price{}, err
	}
	var recorded pricing.Price
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if recorded, err = s.catalog.RecordPrice(ctx, principal, providerName, name, price); err != nil {
			return err
		}
		return s.record(ctx, principal, "price.record", priceTarget(providerName, name, recorded), "")
	})
	return recorded, err
}

// CorrectPrice is correctModelPrice: only a future period never used changes (US-37 CA-3).
func (s *Service) CorrectPrice(ctx context.Context, principal identity.Principal, providerName, name string, id int64, version string, change PriceChange) (pricing.Price, error) {
	if err := s.admin(principal, providerName, name); err != nil {
		return pricing.Price{}, err
	}
	current, err := s.catalog.Price(ctx, principal.OrganizationID, providerName, name, id)
	if err != nil {
		return pricing.Price{}, err
	}
	if current.Version() != version {
		return pricing.Price{}, failure.New(failure.KindConflict, CodeFailedPrecondition, nil)
	}
	if current.Used || !current.EffectiveSince.After(s.now()) {
		return pricing.Price{}, failure.New(failure.KindConflict, pricing.CodeEffectivePeriodInUse, nil)
	}
	corrected := current
	if change.EffectiveSince != nil {
		corrected.EffectiveSince = *change.EffectiveSince
	}
	if change.PerMillion != nil {
		corrected.PerMillion = *change.PerMillion
	}
	if change.Source != nil {
		corrected.Source = *change.Source
	}
	if err := pricing.ValidatePrice(corrected, current.Currency, s.now()); err != nil {
		return pricing.Price{}, err
	}
	var saved pricing.Price
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if saved, err = s.catalog.CorrectPrice(ctx, principal.OrganizationID, providerName, name, corrected); err != nil {
			return err
		}
		return s.record(ctx, principal, "price.correct", priceTarget(providerName, name, saved), "")
	})
	return saved, err
}

func (s *Service) model(ctx context.Context, providerName, name string) error {
	if !known(providerName) || !pricing.ValidModelName(name) {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	_, err := s.catalog.Model(ctx, providerName, name)
	return err
}

func (s *Service) admin(principal identity.Principal, providerName, name string) error {
	if !known(providerName) || !pricing.ValidModelName(name) {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if !principal.Administrator {
		return failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	return nil
}

func (s *Service) record(ctx context.Context, principal identity.Principal, action, target, decision string) error {
	_, err := s.trail.Append(ctx, domainaudit.Record{
		OrganizationID: principal.OrganizationID,
		Actor:          domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
		Origin:         domainaudit.OriginWeb,
		Action:         action, Target: target, Decision: decision,
	})
	return err
}

func modelTarget(providerName, name string) string {
	return "providers/" + providerName + "/models/" + name
}

func priceTarget(providerName, name string, price pricing.Price) string {
	return modelTarget(providerName, name) + "/prices/" + price.TableVersion()
}

func known(providerName string) bool {
	for _, name := range Providers {
		if name == providerName {
			return true
		}
	}
	return false
}
