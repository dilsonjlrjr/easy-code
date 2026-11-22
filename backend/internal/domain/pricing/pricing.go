// Package pricing holds the model capability matrix and the versioned price table (PROJETO 9.2
// and 10, US-24, US-37). Prices are exact decimals in text, never floating point, and come only
// from the table: nothing here fixes a price or a model name (US-37 RN-1).
package pricing

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"easycode/internal/domain/failure"
)

// Failure codes (openapi Problem).
const (
	CodeModelWithoutCurrentPrice = "model-without-current-price"
	CodeEffectivePeriodInUse     = "effective-period-in-use"
	CodeModelWithoutCapability   = "model-without-capability"
)

// Capacity names a column of the capability matrix (openapi Capacity).
type Capacity string

// Capacities.
const (
	CapacityToolUse       Capacity = "tool_use"
	CapacityContextTokens Capacity = "context_tokens"
	CapacityPromptCache   Capacity = "prompt_cache"
	CapacityImageInput    Capacity = "image_input"
)

var (
	modelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	// decimalPattern accepts what numeric(18,8) stores exactly: up to 10 integer and 8 decimal digits.
	decimalPattern  = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,8})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// Capabilities is a row of the matrix.
type Capabilities struct {
	ToolUse       bool
	ContextTokens int
	PromptCache   bool
	ImageInput    bool
}

// Model is a registered model of a provider.
type Model struct {
	Provider     string
	Name         string
	DisplayName  string
	Capabilities Capabilities
	Active       bool
}

// Requirement is what a stage needs from a model (policy.eligible_model).
type Requirement struct {
	Capacities       []Capacity
	MinContextTokens int
}

// Missing returns the first capacity the model lacks for the requirement, or "".
func (m Model) Missing(req Requirement) Capacity {
	for _, capacity := range req.Capacities {
		switch capacity {
		case CapacityToolUse:
			if !m.Capabilities.ToolUse {
				return capacity
			}
		case CapacityPromptCache:
			if !m.Capabilities.PromptCache {
				return capacity
			}
		case CapacityImageInput:
			if !m.Capabilities.ImageInput {
				return capacity
			}
		}
	}
	if m.Capabilities.ContextTokens < req.MinContextTokens {
		return CapacityContextTokens
	}
	return ""
}

// PerMillion are the prices per million tokens of each type, as exact decimals.
type PerMillion struct {
	Input      string
	Output     string
	ReadCache  string
	WriteCache string
}

// Price is one effective period of the price table (model_prices row).
type Price struct {
	ID             int64
	EffectiveSince time.Time
	Currency       string
	PerMillion     PerMillion
	Source         string
	Used           bool // cited by a model call: immutable
	RecordedAt     time.Time
	RecordedBy     string // subject
	RecordedByName string
	FixedAt        *time.Time
}

// Version is the opaque version for If-Match: it changes with each correction.
func (p Price) Version() string {
	at := p.RecordedAt
	if p.FixedAt != nil {
		at = *p.FixedAt
	}
	return at.UTC().Format("20060102150405.000000")
}

// TableVersion is the price_table_version cited by cost events: the id in text.
func (p Price) TableVersion() string { return strconv.FormatInt(p.ID, 10) }

// ValidModelName reports whether name fits the contract (ModelName).
func ValidModelName(name string) bool { return modelNamePattern.MatchString(name) }

// ValidateModel checks a model registration.
func ValidateModel(m Model) error {
	var fields []failure.FieldError
	if len([]rune(m.DisplayName)) > 120 {
		fields = append(fields, failure.FieldError{Field: "display_name", Message: "length"})
	}
	if m.Capabilities.ContextTokens < 1 {
		fields = append(fields, failure.FieldError{Field: "capabilities.context_tokens", Message: "range"})
	}
	if len(fields) > 0 {
		return failure.Invalid(fields...)
	}
	return nil
}

// ValidatePrice checks a new or corrected effective period. orgCurrency is the budget currency.
func ValidatePrice(p Price, orgCurrency string, now time.Time) error {
	var fields []failure.FieldError
	for field, value := range map[string]string{
		"per_million_tokens.input": p.PerMillion.Input, "per_million_tokens.output": p.PerMillion.Output,
		"per_million_tokens.read_cache": p.PerMillion.ReadCache, "per_million_tokens.write_cache": p.PerMillion.WriteCache,
	} {
		if !decimalPattern.MatchString(value) {
			fields = append(fields, failure.FieldError{Field: field, Message: "decimal"})
		}
	}
	if !currencyPattern.MatchString(p.Currency) || p.Currency != orgCurrency {
		fields = append(fields, failure.FieldError{Field: "currency", Message: "currency"})
	}
	if source := strings.TrimSpace(p.Source); source == "" || len([]rune(source)) > 500 {
		fields = append(fields, failure.FieldError{Field: "source", Message: "length"})
	}
	// A period never starts in the past: it would change costs already computed (RNF-16).
	if p.EffectiveSince.IsZero() || p.EffectiveSince.Before(now.Add(-time.Minute)) {
		fields = append(fields, failure.FieldError{Field: "effective_since", Message: "past"})
	}
	if len(fields) > 0 {
		slices.SortFunc(fields, func(a, b failure.FieldError) int { return strings.Compare(a.Field, b.Field) })
		return failure.Invalid(fields...)
	}
	return nil
}

// NormalizeDecimal drops trailing zeros of a stored numeric ("3.50000000" → "3.5").
func NormalizeDecimal(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	return strings.TrimSuffix(value, ".")
}
