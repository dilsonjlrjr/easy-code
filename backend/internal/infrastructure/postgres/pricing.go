package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	apppricing "easycode/internal/application/pricing"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
)

// Catalog stores the capability matrix (models) and the versioned price table (model_prices).
type Catalog struct {
	pool *pgxpool.Pool
}

var _ apppricing.Catalog = Catalog{}

// NewCatalog builds the adapter.
func NewCatalog(pool *pgxpool.Pool) Catalog { return Catalog{pool: pool} }

const priceColumns = `mp.id, mp.effective_since, mp.currency, mp.input_price::text, mp.output_price::text,
  mp.read_cache_price::text, mp.write_cache_price::text, mp.source, mp.recorded_at, mp.fixed_at, u.sub, u.display_name,
  EXISTS (SELECT 1 FROM easycode.model_calls c WHERE c.price_id = mp.id)`

func scanPrice(row pgx.Row) (pricing.Price, error) {
	var p pricing.Price
	err := row.Scan(&p.ID, &p.EffectiveSince, &p.Currency, &p.PerMillion.Input, &p.PerMillion.Output,
		&p.PerMillion.ReadCache, &p.PerMillion.WriteCache, &p.Source, &p.RecordedAt, &p.FixedAt, &p.RecordedBy, &p.RecordedByName, &p.Used)
	p.PerMillion = pricing.PerMillion{
		Input: pricing.NormalizeDecimal(p.PerMillion.Input), Output: pricing.NormalizeDecimal(p.PerMillion.Output),
		ReadCache: pricing.NormalizeDecimal(p.PerMillion.ReadCache), WriteCache: pricing.NormalizeDecimal(p.PerMillion.WriteCache),
	}
	return p, err
}

// Models implements apppricing.Catalog with the price in force at the moment for the organization; a
// claude-code model without an own price shows the one it inherits from anthropic (0020).
func (c Catalog) Models(ctx context.Context, organizationID string, filter apppricing.ModelFilter, at time.Time) ([]apppricing.ModelView, error) {
	rows, err := conn(ctx, c.pool).Query(WithOperation(ctx, "models.list"), `
SELECT m.provider, m.name, m.display_name, m.tool_use, m.context_tokens, m.prompt_cache, m.image_input, m.active, cp.id,
       coalesce(cp.model_id <> m.id, false)
  FROM easycode.models m
  LEFT JOIN easycode.model_prices cp ON cp.id = easycode.price_in_force(
    (SELECT o.id FROM easycode.organizations o WHERE o.public_id = $1::uuid), m.id, $4)
 WHERE ($2 = '' OR m.provider = $2) AND ($3::boolean IS NULL OR m.active = $3)
 ORDER BY m.provider, m.name`, organizationID, filter.Provider, filter.Active, at)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", classify(err))
	}
	type row struct {
		view    apppricing.ModelView
		priceID *int64
	}
	collected, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var out row
		m := &out.view.Model
		err := r.Scan(&m.Provider, &m.Name, &m.DisplayName, &m.Capabilities.ToolUse, &m.Capabilities.ContextTokens,
			&m.Capabilities.PromptCache, &m.Capabilities.ImageInput, &m.Active, &out.priceID, &out.view.PriceInherited)
		if m.DisplayName == "" {
			m.DisplayName = m.Name
		}
		return out, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan models: %w", err)
	}
	views := make([]apppricing.ModelView, 0, len(collected))
	for _, item := range collected {
		if item.priceID != nil {
			price, err := c.priceByID(ctx, *item.priceID)
			if err != nil {
				return nil, err
			}
			item.view.CurrentPrice = &price
		}
		views = append(views, item.view)
	}
	return views, nil
}

func (c Catalog) priceByID(ctx context.Context, id int64) (pricing.Price, error) {
	price, err := scanPrice(conn(ctx, c.pool).QueryRow(WithOperation(ctx, "model_prices.by_id"), `
SELECT `+priceColumns+` FROM easycode.model_prices mp JOIN easycode.users u ON u.id = mp.recorded_by WHERE mp.id = $1`, id))
	if err != nil {
		return pricing.Price{}, fmt.Errorf("read price: %w", classify(err))
	}
	return price, nil
}

// Model implements apppricing.Catalog.
func (c Catalog) Model(ctx context.Context, providerName, name string) (pricing.Model, error) {
	m := pricing.Model{Provider: providerName, Name: name}
	err := conn(ctx, c.pool).QueryRow(WithOperation(ctx, "models.get"), `
SELECT display_name, tool_use, context_tokens, prompt_cache, image_input, active FROM easycode.models WHERE provider = $1 AND name = $2`,
		providerName, name).Scan(&m.DisplayName, &m.Capabilities.ToolUse, &m.Capabilities.ContextTokens, &m.Capabilities.PromptCache, &m.Capabilities.ImageInput, &m.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return m, fmt.Errorf("read model: %w", classify(err))
	}
	if m.DisplayName == "" {
		m.DisplayName = name
	}
	return m, nil
}

// SaveModel implements apppricing.Catalog (insert or update the capabilities).
func (c Catalog) SaveModel(ctx context.Context, m pricing.Model) error {
	_, err := conn(ctx, c.pool).Exec(WithOperation(ctx, "models.save"), `
INSERT INTO easycode.models (provider, name, display_name, tool_use, context_tokens, prompt_cache, image_input, active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (provider, name) DO UPDATE SET display_name = excluded.display_name, tool_use = excluded.tool_use,
  context_tokens = excluded.context_tokens, prompt_cache = excluded.prompt_cache, image_input = excluded.image_input, active = excluded.active`,
		m.Provider, m.Name, m.DisplayName, m.Capabilities.ToolUse, m.Capabilities.ContextTokens, m.Capabilities.PromptCache, m.Capabilities.ImageInput, m.Active)
	if err != nil {
		return fmt.Errorf("save model: %w", classify(err))
	}
	return nil
}

// Prices implements apppricing.Catalog.
func (c Catalog) Prices(ctx context.Context, organizationID, providerName, name string) ([]pricing.Price, error) {
	rows, err := conn(ctx, c.pool).Query(WithOperation(ctx, "model_prices.list"), `
SELECT `+priceColumns+`
  FROM easycode.model_prices mp
  JOIN easycode.models m ON m.id = mp.model_id
  JOIN easycode.organizations o ON o.id = mp.organization_id
  JOIN easycode.users u ON u.id = mp.recorded_by
 WHERE o.public_id = $1::uuid AND m.provider = $2 AND m.name = $3
 ORDER BY mp.effective_since DESC`, organizationID, providerName, name)
	if err != nil {
		return nil, fmt.Errorf("list prices: %w", classify(err))
	}
	prices, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pricing.Price, error) { return scanPrice(r) })
	if err != nil {
		return nil, fmt.Errorf("scan prices: %w", err)
	}
	return prices, nil
}

// Price implements apppricing.Catalog.
func (c Catalog) Price(ctx context.Context, organizationID, providerName, name string, id int64) (pricing.Price, error) {
	price, err := scanPrice(conn(ctx, c.pool).QueryRow(WithOperation(ctx, "model_prices.get"), `
SELECT `+priceColumns+`
  FROM easycode.model_prices mp
  JOIN easycode.models m ON m.id = mp.model_id
  JOIN easycode.organizations o ON o.id = mp.organization_id
  JOIN easycode.users u ON u.id = mp.recorded_by
 WHERE o.public_id = $1::uuid AND m.provider = $2 AND m.name = $3 AND mp.id = $4`, organizationID, providerName, name, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return price, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return price, fmt.Errorf("read price: %w", classify(err))
	}
	return price, nil
}

// RecordPrice implements apppricing.Catalog.
func (c Catalog) RecordPrice(ctx context.Context, p identity.Principal, providerName, name string, price pricing.Price) (pricing.Price, error) {
	var id int64
	err := conn(ctx, c.pool).QueryRow(WithOperation(ctx, "model_prices.insert"), `
INSERT INTO easycode.model_prices (organization_id, model_id, effective_since, currency, input_price, output_price,
  read_cache_price, write_cache_price, source, recorded_by)
SELECT u.organization_id, m.id, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10::numeric, $11, u.id
  FROM easycode.users u JOIN easycode.organizations o ON o.id = u.organization_id, easycode.models m
 WHERE o.public_id = $1::uuid AND u.sub = $2 AND m.provider = $3 AND m.name = $4
RETURNING id`, p.OrganizationID, p.Subject, providerName, name, price.EffectiveSince, price.Currency,
		price.PerMillion.Input, price.PerMillion.Output, price.PerMillion.ReadCache, price.PerMillion.WriteCache, price.Source).Scan(&id)
	if err != nil {
		return pricing.Price{}, priceFailure("record price", err)
	}
	return c.priceByID(ctx, id)
}

// CorrectPrice implements apppricing.Catalog.
func (c Catalog) CorrectPrice(ctx context.Context, organizationID, providerName, name string, price pricing.Price) (pricing.Price, error) {
	tag, err := conn(ctx, c.pool).Exec(WithOperation(ctx, "model_prices.correct"), `
UPDATE easycode.model_prices mp SET effective_since = $5, input_price = $6::numeric, output_price = $7::numeric,
  read_cache_price = $8::numeric, write_cache_price = $9::numeric, source = $10
  FROM easycode.models m, easycode.organizations o
 WHERE m.id = mp.model_id AND o.id = mp.organization_id AND o.public_id = $1::uuid AND m.provider = $2 AND m.name = $3 AND mp.id = $4`,
		organizationID, providerName, name, price.ID, price.EffectiveSince,
		price.PerMillion.Input, price.PerMillion.Output, price.PerMillion.ReadCache, price.PerMillion.WriteCache, price.Source)
	if err != nil {
		return pricing.Price{}, priceFailure("correct price", err)
	}
	if tag.RowsAffected() == 0 {
		return pricing.Price{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return c.priceByID(ctx, price.ID)
}

// priceFailure translates the price table rules of the database (model_prices_immutable,
// unique effective period) into contract problems.
func priceFailure(what string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "model_prices_immutable":
			return failure.New(failure.KindConflict, pricing.CodeEffectivePeriodInUse, err)
		case "model_prices_effective_period_uk":
			return failure.Invalid(failure.FieldError{Field: "effective_since", Message: "unique"})
		}
	}
	return fmt.Errorf("%s: %w", what, classify(err))
}

// OrganizationCurrency implements apppricing.Catalog.
func (c Catalog) OrganizationCurrency(ctx context.Context, organizationID string) (string, error) {
	var currency string
	err := conn(ctx, c.pool).QueryRow(WithOperation(ctx, "organizations.currency"),
		"SELECT currency FROM easycode.organizations WHERE public_id = $1::uuid", organizationID).Scan(&currency)
	if err != nil {
		return "", fmt.Errorf("organization currency: %w", classify(err))
	}
	return currency, nil
}

// CurrentPrice implements apppricing.Catalog.
func (c Catalog) CurrentPrice(ctx context.Context, projectID, providerName, name string, at time.Time) (pricing.Price, error) {
	price, err := scanPrice(conn(ctx, c.pool).QueryRow(WithOperation(ctx, "model_prices.current"), `
SELECT `+priceColumns+`
  FROM easycode.model_prices mp
  JOIN easycode.models m ON m.id = mp.model_id
  JOIN easycode.projects p ON p.organization_id = mp.organization_id
  JOIN easycode.users u ON u.id = mp.recorded_by
 WHERE p.public_id = $1::uuid AND m.provider = $2 AND m.name = $3 AND mp.effective_since <= $4
 ORDER BY mp.effective_since DESC LIMIT 1`, projectID, providerName, name, at))
	if errors.Is(err, pgx.ErrNoRows) {
		return price, failure.New(failure.KindConflict, pricing.CodeModelWithoutCurrentPrice, nil)
	}
	if err != nil {
		return price, fmt.Errorf("current price: %w", classify(err))
	}
	return price, nil
}
