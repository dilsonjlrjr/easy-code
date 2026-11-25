package gate

import (
	"context"
	"testing"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/governance"
	"easycode/internal/domain/identity"
)

type accessProjects struct {
	appproject.Repository
	allowed string
}

func (p accessProjects) Access(_ context.Context, _ identity.Principal, projectID string) (appproject.Access, error) {
	if projectID != p.allowed {
		return appproject.Access{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return appproject.Access{}, nil
}

type fixedSettings Settings

func (s fixedSettings) Settings(context.Context, string, string) (Settings, error) {
	return Settings(s), nil
}

func exitComposition() governance.Composition {
	return governance.Composition{
		Catalog: map[string]governance.CatalogCheck{
			"artifact.schema_valid": {ID: "artifact.schema_valid", Name: "Esquema válido", Type: governance.TypeAutomatic},
			"us.vague":              {ID: "us.vague", Name: "Sem termo vago", Type: governance.TypeAutomatic},
			"human.scope":           {ID: "human.scope", Type: governance.TypeHuman},
		},
		Common: governance.Checks{Required: []string{"artifact.schema_valid"}},
		Gates: map[string]governance.Gate{"gate.requirements": {ID: "gate.requirements", Stage: "requirements",
			Required: []string{"human.scope"}, Alerts: []string{"us.vague"}}},
	}
}

// PC-20, PL-46: before any round the exit criteria list the gate checks with the catalog name, the type
// and the severity in the project (a promoted alert blocks); a gate out of the composition has none, and a
// project without access is not found (RNF-04).
func TestExitCriteriaListsTheGateChecksOfTheProject(t *testing.T) {
	criteria := NewExitCriteria(accessProjects{allowed: "p1"}, exitComposition(), fixedSettings{Promoted: []string{"us.vague"}})
	checks, err := criteria.Checks(context.Background(), identity.Principal{}, "p1", "gate.requirements")
	if err != nil {
		t.Fatal(err)
	}
	want := []StageCheck{
		{Check: "artifact.schema_valid", Name: "Esquema válido", Type: governance.TypeAutomatic, Severity: governance.SeverityBlocks},
		{Check: "human.scope", Name: "human.scope", Type: governance.TypeHuman, Severity: governance.SeverityBlocks},
		{Check: "us.vague", Name: "Sem termo vago", Type: governance.TypeAutomatic, Severity: governance.SeverityBlocks},
	}
	if len(checks) != len(want) {
		t.Fatalf("checks %v", checks)
	}
	for i := range want {
		if checks[i] != want[i] {
			t.Fatalf("check %d: %+v, want %+v", i, checks[i], want[i])
		}
	}
	if none, err := criteria.Checks(context.Background(), identity.Principal{}, "p1", "gate.unknown"); err != nil || none == nil || len(none) != 0 {
		t.Fatalf("unknown gate: %v %v", none, err)
	}
	if _, err := criteria.Checks(context.Background(), identity.Principal{}, "p2", "gate.requirements"); failure.From(err).Kind != failure.KindNotFound {
		t.Fatalf("no access: %v", err)
	}
	if criteria.NameOf("us.vague") != "Sem termo vago" || criteria.NameOf("other") != "other" {
		t.Fatal("NameOf: catalog name, else the id")
	}
}
