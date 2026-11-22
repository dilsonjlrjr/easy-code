package project_test

import (
	"testing"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/project"
)

func valid() project.NewProject {
	return project.NewProject{
		Name: "Reserva de salas", BusinessNeed: "Reservar salas sem conflito.", Acknowledgement: true,
		Budget: &project.Budget{StopLimit: project.Money{Value: "200.00", Currency: "BRL"}},
	}
}

// US-03 CA-2 and CA-3: need required and acknowledgment confirmed, each pointed by field.
func TestValidateRequiresNeedAndAcknowledgment(t *testing.T) {
	input := valid()
	input.BusinessNeed = "   "
	input.Acknowledgement = false
	err := failure.From(input.Validate("BRL", nil))
	fields := map[string]string{}
	for _, f := range err.Fields {
		fields[f.Field] = f.Message
	}
	if fields["business_need"] != "required" || fields["provider_submission_acknowledgment"] != "must_be_true" {
		t.Fatalf("unexpected fields %v", fields)
	}
}

func TestValidateDefaultsAlertsAndChecksCeiling(t *testing.T) {
	input := valid()
	if err := input.Validate("BRL", nil); err != nil {
		t.Fatal(err)
	}
	if len(input.Budget.AlertPercentages) != 2 || input.Budget.AlertPercentages[0] != 50 {
		t.Fatalf("alerts default to 50 and 80: %v", input.Budget.AlertPercentages)
	}
	alerts := input.Budget.AlertValues()
	if alerts[0].Value != "100" || alerts[1].Value != "160" {
		t.Fatalf("alert values: %+v", alerts)
	}
	ceiling := "100"
	again := valid()
	if err := again.Validate("BRL", &ceiling); failure.From(err).Code != project.CodeBudgetAboveCeiling {
		t.Fatalf("above the ceiling: %v", err)
	}
	other := valid()
	other.Budget.StopLimit.Currency = "USD"
	if err := other.Validate("BRL", nil); failure.From(err).Code != failure.CodeValidation {
		t.Fatalf("other currency: %v", err)
	}
}

func TestNumberConstraintsNeverReusesIDs(t *testing.T) {
	previous := []project.Constraint{{ID: "RESTR-01"}, {ID: "RESTR-02"}}
	items := []project.Constraint{{ID: "RESTR-01", Category: "database", Description: "PostgreSQL 16"}, {Category: "other", Description: "SSO"}}
	numbered, fields := project.NumberConstraints(items, previous)
	if len(fields) != 0 || numbered[1].ID != "RESTR-03" {
		t.Fatalf("got %+v %v", numbered, fields)
	}
}
