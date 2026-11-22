package specs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"easycode/internal/domain/governance"
)

type gatesDocument struct {
	Common        governance.Checks          `json:"common_checks"`
	Gates         []governance.Gate          `json:"gates"`
	InternalCycle []governance.InternalCycle `json:"internal_cycle"`
}

type catalogDocument struct {
	Checks []governance.CatalogCheck `json:"checks"`
}

// Governance loads gates.yaml and checagens.yaml (ADR-10): the worker does not start when a gate cites a
// check out of the catalog.
func Governance() (governance.Composition, error) {
	var composition governance.Composition
	gatesRaw, err := files.ReadFile("data/governanca/gates.yaml")
	if err != nil {
		return composition, err
	}
	catalogRaw, err := files.ReadFile("data/governanca/checagens.yaml")
	if err != nil {
		return composition, err
	}
	var gates gatesDocument
	if err := decodeYAML(gatesRaw, &gates); err != nil {
		return composition, fmt.Errorf("gates.yaml: %w", err)
	}
	var catalog catalogDocument
	if err := decodeYAML(catalogRaw, &catalog); err != nil {
		return composition, fmt.Errorf("checagens.yaml: %w", err)
	}
	composition.Catalog = map[string]governance.CatalogCheck{}
	for _, check := range catalog.Checks {
		composition.Catalog[check.ID] = check
	}
	composition.Common, composition.InternalCycle = gates.Common, gates.InternalCycle
	composition.Gates = map[string]governance.Gate{}
	for _, gate := range gates.Gates {
		composition.Gates[gate.ID] = gate
	}
	sum := sha256.New()
	sum.Write(gatesRaw)
	sum.Write(catalogRaw)
	composition.Version = hex.EncodeToString(sum.Sum(nil))[:16]
	return composition, composition.Validate()
}

func decodeYAML(raw []byte, target any) error {
	encoded, err := YAMLToJSON(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}
