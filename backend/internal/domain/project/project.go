// Package project holds the project rules of creation and configuration (US-03, US-16, US-39,
// US-54): business need and provider acknowledgment, budget in exact decimals, technical
// constraints with stable RESTR-NN ids, and the project state machine names.
package project

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Project states (machine "project" of estados.yaml).
const (
	StateCreated    = "created"
	StateInProgress = "in_progress"
	StatePaused     = "paused"
	StateCompleted  = "completed"
	StateCanceled   = "canceled"
)

// States lists every project state (ProjectState).
var States = []string{StateCreated, StateInProgress, StatePaused, StateCompleted, StateCanceled}

// Failure codes (openapi Problem).
const (
	CodeDuplicateName      = "duplicate-name"
	CodeBudgetAboveCeiling = "budget-above-ceiling"
	CodeIncompleteConfig   = "incomplete-configuration"
	CodeApprovalNotAllowed = "approval-mode-not-allowed"
	// CodeProjectNotEnded refuses deleting a project that is not completed or canceled (US-04 CA-2).
	CodeProjectNotEnded = "project-not-ended"
	// CodeConfirmationRequired refuses a deletion whose typed name differs from the project name (US-04 CA-1).
	CodeConfirmationRequired = "confirmation-required"
)

// Actions are the project triggers of estados.yaml a person may use in a state (Project.available_actions):
// start for any role in created; pause, resume and cancel for the lead or the organization administrator
// (US-51 RN-1); record_publication (completeProject) for the lead or an approver (US-52 RN-2). An ended
// project offers nothing (US-51 CA-5, US-52 CA-4).
func Actions(state string, roles []identity.Role, administrator bool) []string {
	lead := identity.Has(roles, identity.RoleLead) || administrator
	decides := identity.Has(roles, identity.RoleLead, identity.RoleApprover)
	actions := []string{}
	switch state {
	case StateCreated:
		if len(roles) > 0 {
			actions = append(actions, "start")
		}
		if lead {
			actions = append(actions, "cancel")
		}
	case StateInProgress:
		if lead {
			actions = append(actions, "pause", "cancel")
		}
		if decides {
			actions = append(actions, "record_publication")
		}
	case StatePaused:
		if lead {
			actions = append(actions, "resume", "cancel")
		}
	}
	return actions
}

// DefaultAlertPercentages are the budget alert limits when none is given (US-39 RN-2).
var DefaultAlertPercentages = []int{50, 80}

// ConstraintCategories of the contract (ConstraintCategory).
var ConstraintCategories = []string{"language", "front_end", "database", "integration", "infrastructure", "standard", "other"}

var (
	moneyPattern      = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,4})?$`)
	currencyPattern   = regexp.MustCompile(`^[A-Z]{3}$`)
	constraintPattern = regexp.MustCompile(`^RESTR-([0-9]{2,})$`)
	languagePattern   = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)
)

// Money is an exact decimal amount in a currency (numeric(14,4) for budgets).
type Money struct {
	Value    string
	Currency string
}

// Budget is policy.project_budget of the project.
type Budget struct {
	StopLimit        Money
	AlertPercentages []int
}

// Constraint is one technical constraint (US-54).
type Constraint struct {
	ID          string
	Category    string
	Description string
}

// NewProject is the creation input (NewProject).
type NewProject struct {
	Name            string
	BusinessNeed    string
	Acknowledgement bool
	Budget          *Budget
	Constraints     []Constraint
}

// Validate checks the creation input (US-03 CA-2, CA-3) and normalizes it.
func (n *NewProject) Validate(currency string, ceiling *string) error {
	n.Name = strings.TrimSpace(n.Name)
	var fields []failure.FieldError
	if length := len([]rune(n.Name)); length < 3 || length > 120 {
		fields = append(fields, failure.FieldError{Field: "name", Message: "length"})
	}
	if strings.TrimSpace(n.BusinessNeed) == "" {
		fields = append(fields, failure.FieldError{Field: "business_need", Message: "required"})
	} else if len([]rune(n.BusinessNeed)) > 50000 {
		fields = append(fields, failure.FieldError{Field: "business_need", Message: "length"})
	}
	if !n.Acknowledgement {
		fields = append(fields, failure.FieldError{Field: "provider_submission_acknowledgment", Message: "must_be_true"})
	}
	if n.Budget != nil {
		fields = append(fields, n.Budget.fieldErrors("budget", currency)...)
	}
	constraints, constraintErrors := NumberConstraints(n.Constraints, nil)
	fields = append(fields, constraintErrors...)
	if len(fields) > 0 {
		return failure.Invalid(fields...)
	}
	n.Constraints = constraints
	if n.Budget != nil {
		n.Budget.normalize()
		return n.Budget.CheckCeiling(ceiling)
	}
	return nil
}

// ValidateBudget checks a budget change (modifyProjectConfiguration).
func ValidateBudget(b *Budget, currency string, ceiling *string) error {
	if fields := b.fieldErrors("budget", currency); len(fields) > 0 {
		return failure.Invalid(fields...)
	}
	b.normalize()
	return b.CheckCeiling(ceiling)
}

func (b *Budget) fieldErrors(prefix, currency string) []failure.FieldError {
	var fields []failure.FieldError
	if !moneyPattern.MatchString(b.StopLimit.Value) || compare(b.StopLimit.Value, "0.01") < 0 {
		fields = append(fields, failure.FieldError{Field: prefix + ".stop_limit.value", Message: "decimal"})
	}
	if !currencyPattern.MatchString(b.StopLimit.Currency) || b.StopLimit.Currency != currency {
		fields = append(fields, failure.FieldError{Field: prefix + ".stop_limit.currency", Message: "currency"})
	}
	seen := map[int]bool{}
	for _, percentage := range b.AlertPercentages {
		if percentage < 1 || percentage > 99 {
			fields = append(fields, failure.FieldError{Field: prefix + ".percentage_alert_limits", Message: "range"})
			break
		}
		if seen[percentage] {
			fields = append(fields, failure.FieldError{Field: prefix + ".percentage_alert_limits", Message: "unique"})
			break
		}
		seen[percentage] = true
	}
	return fields
}

func (b *Budget) normalize() {
	if b.AlertPercentages == nil {
		b.AlertPercentages = slices.Clone(DefaultAlertPercentages)
	}
	slices.Sort(b.AlertPercentages)
}

// CheckCeiling refuses a stop limit above the organization ceiling (US-39 RN-1).
func (b Budget) CheckCeiling(ceiling *string) error {
	if ceiling != nil && compare(b.StopLimit.Value, *ceiling) > 0 {
		return failure.New(failure.KindConflict, CodeBudgetAboveCeiling, nil)
	}
	return nil
}

// AlertValues are the money values of the alert limits (payload project.created).
func (b Budget) AlertValues() []Money {
	limit, _ := new(big.Rat).SetString(b.StopLimit.Value)
	values := make([]Money, 0, len(b.AlertPercentages))
	for _, percentage := range b.AlertPercentages {
		value := new(big.Rat).Mul(limit, big.NewRat(int64(percentage), 100))
		values = append(values, Money{Value: TrimDecimal(value.FloatString(6)), Currency: b.StopLimit.Currency})
	}
	return values
}

// ValidLanguage reports whether a language tag is accepted for artifacts (RNF-21).
func ValidLanguage(tag string) bool { return languagePattern.MatchString(tag) }

// NumberConstraints validates a full constraint list and gives RESTR-NN ids to new items; ids
// already used (previous) are never reused (US-54, PL-13).
func NumberConstraints(items []Constraint, previous []Constraint) ([]Constraint, []failure.FieldError) {
	var fields []failure.FieldError
	if len(items) > 100 {
		return nil, []failure.FieldError{{Field: "constraints.items", Message: "too_many"}}
	}
	highest := 0
	for _, item := range append(slices.Clone(previous), items...) {
		if match := constraintPattern.FindStringSubmatch(item.ID); match != nil {
			var n int
			_, _ = fmt.Sscanf(match[1], "%d", &n)
			highest = max(highest, n)
		}
	}
	out := make([]Constraint, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		item.Description = strings.TrimSpace(item.Description)
		field := fmt.Sprintf("constraints.items[%d]", i)
		if !slices.Contains(ConstraintCategories, item.Category) {
			fields = append(fields, failure.FieldError{Field: field + ".category", Message: "enum"})
		}
		if length := len([]rune(item.Description)); length < 3 || length > 2000 {
			fields = append(fields, failure.FieldError{Field: field + ".description", Message: "length"})
		}
		if item.ID == "" {
			highest++
			item.ID = fmt.Sprintf("RESTR-%02d", highest)
		} else if !constraintPattern.MatchString(item.ID) || seen[item.ID] {
			fields = append(fields, failure.FieldError{Field: field + ".id", Message: "pattern"})
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out, fields
}

// TrimDecimal drops trailing zeros ("10.000000" → "10").
func TrimDecimal(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	return strings.TrimSuffix(strings.TrimRight(value, "0"), ".")
}

func compare(a, b string) int {
	left, ok := new(big.Rat).SetString(a)
	right, ok2 := new(big.Rat).SetString(b)
	if !ok || !ok2 {
		return -1
	}
	return left.Cmp(right)
}

// ConstraintChanges lists the ids of the items added, changed or removed between two versions of
// the constraints, sorted (audit of constraints.modify, US-54 CA-3).
func ConstraintChanges(previous, next []Constraint) []string {
	before := map[string]Constraint{}
	for _, c := range previous {
		before[c.ID] = c
	}
	changed := []string{}
	for _, c := range next {
		if old, ok := before[c.ID]; !ok || old != c {
			changed = append(changed, c.ID)
		}
		delete(before, c.ID)
	}
	for id := range before {
		changed = append(changed, id)
	}
	slices.Sort(changed)
	return changed
}
