package http

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	appproject "easycode/internal/application/project"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// ConfigurationUseCases are the project configuration use cases (US-16, US-39, US-54).
type ConfigurationUseCases interface {
	Get(ctx context.Context, principal identity.Principal, projectID string) (appproject.Configuration, error)
	Modify(ctx context.Context, principal identity.Principal, projectID string, version int, change appproject.Change) (appproject.Configuration, error)
}

type modelRefJSON struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type pairJSON struct {
	Reasoning *modelRefJSON `json:"reasoning"`
	Execution *modelRefJSON `json:"execution"`
}

type gateModeJSON struct {
	Mode        string   `json:"mode"`
	Allowed     []string `json:"allowed"`
	AlwaysHuman bool     `json:"always_human"`
}

type constraintsJSON struct {
	Version   int                  `json:"version"`
	Items     []constraintItemJSON `json:"items"`
	DefinedBy userRefJSON          `json:"defined_by"`
	DefinedAt time.Time            `json:"defined_at"`
}

type configurationJSON struct {
	ModelPair        *pairJSON               `json:"model_pair"`
	PairByStage      map[string]pairJSON     `json:"pair_by_stage"`
	GateApproval     map[string]gateModeJSON `json:"gate_approval"`
	Budget           *budgetJSON             `json:"budget"`
	StopLimitCeiling *moneyJSON              `json:"stop_limit_ceiling"`
	ArtifactLanguage string                  `json:"artifact_language"`
	Constraints      *constraintsJSON        `json:"constraints"`
}

func pairOf(p appproject.Pair) pairJSON {
	return pairJSON{Reasoning: &modelRefJSON{Provider: p.Reasoning.Provider, Model: p.Reasoning.Model}, Execution: &modelRefJSON{Provider: p.Execution.Provider, Model: p.Execution.Model}}
}

func configurationOf(c appproject.Configuration) configurationJSON {
	out := configurationJSON{PairByStage: map[string]pairJSON{}, GateApproval: map[string]gateModeJSON{}, ArtifactLanguage: c.ArtifactLanguage}
	if c.ModelPair != nil {
		pair := pairOf(*c.ModelPair)
		out.ModelPair = &pair
	}
	for stage, pair := range c.PairByStage {
		out.PairByStage[stage] = pairOf(pair)
	}
	for gate, mode := range c.GateApproval {
		out.GateApproval[gate] = gateModeJSON(mode)
	}
	if c.Budget != nil {
		limit := moneyJSON(c.Budget.StopLimit)
		out.Budget = &budgetJSON{StopLimit: &limit, AlertPercentages: c.Budget.AlertPercentages}
	}
	if c.StopLimitCeiling != nil {
		ceiling := moneyJSON(*c.StopLimitCeiling)
		out.StopLimitCeiling = &ceiling
	}
	if c.Constraints != nil {
		view := constraintsJSON{Version: c.Constraints.Version, Items: []constraintItemJSON{}, DefinedAt: c.Constraints.DefinedAt,
			DefinedBy: userRefJSON{ID: c.Constraints.DefinedBy.Subject, Name: c.Constraints.DefinedBy.Name}}
		for _, item := range c.Constraints.Items {
			view.Items = append(view.Items, constraintItemJSON(item))
		}
		out.Constraints = &view
	}
	return out
}

func (a *api) getProjectConfiguration(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	config, err := a.deps.Configuration.Get(RequestContext(rc), principal, projectID)
	if err != nil {
		return err
	}
	setETag(rc, strconv.Itoa(config.Version))
	a.responder.JSON(rc, fasthttp.StatusOK, configurationOf(config))
	return nil
}

type configurationPatchJSON struct {
	ModelPair        *pairJSON                  `json:"model_pair"`
	PairByStage      map[string]json.RawMessage `json:"pair_by_stage"`
	GateApproval     map[string]string          `json:"gate_approval"`
	Budget           *budgetJSON                `json:"budget"`
	ArtifactLanguage *string                    `json:"artifact_language"`
	Constraints      *newConstraintsJSON        `json:"constraints"`
}

func pairFrom(field string, body *pairJSON) (*appproject.Pair, error) {
	if body == nil || body.Reasoning == nil || body.Execution == nil {
		return nil, failure.Invalid(failure.FieldError{Field: field, Message: "required"})
	}
	return &appproject.Pair{
		Reasoning: appproject.ModelRef{Provider: body.Reasoning.Provider, Model: body.Reasoning.Model},
		Execution: appproject.ModelRef{Provider: body.Execution.Provider, Model: body.Execution.Model},
	}, nil
}

// modifyProjectConfiguration applies a JSON Merge Patch under If-Match (lead only).
func (a *api) modifyProjectConfiguration(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	raw, err := ifMatch(rc)
	if err != nil {
		return err
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return failure.New(failure.KindConflict, appproject.CodeFailedPrecondition, nil)
	}
	var body configurationPatchJSON
	if err := decodeJSON(rc, &body); err != nil {
		return err
	}
	change := appproject.Change{GateApproval: body.GateApproval, ArtifactLanguage: body.ArtifactLanguage}
	if body.ModelPair != nil {
		if change.ModelPair, err = pairFrom("model_pair", body.ModelPair); err != nil {
			return err
		}
	}
	if len(body.PairByStage) > 0 {
		change.PairByStage = map[string]*appproject.Pair{}
		for stage, rawPair := range body.PairByStage {
			if bytes.Equal(bytes.TrimSpace(rawPair), []byte("null")) {
				change.PairByStage[stage] = nil
				continue
			}
			var pair pairJSON
			if err := json.Unmarshal(rawPair, &pair); err != nil {
				return failure.Invalid(failure.FieldError{Field: "pair_by_stage." + stage, Message: "invalid"})
			}
			if change.PairByStage[stage], err = pairFrom("pair_by_stage."+stage, &pair); err != nil {
				return err
			}
		}
	}
	if body.Budget != nil {
		if body.Budget.StopLimit == nil {
			return failure.Invalid(failure.FieldError{Field: "budget.stop_limit", Message: "required"})
		}
		change.Budget = budgetOf(body.Budget)
	}
	if body.Constraints != nil {
		items := constraintsOf(body.Constraints)
		change.Constraints = &items
	}
	config, err := a.deps.Configuration.Modify(RequestContext(rc), principal, projectID, version, change)
	if err != nil {
		return err
	}
	setETag(rc, strconv.Itoa(config.Version))
	a.responder.JSON(rc, fasthttp.StatusOK, configurationOf(config))
	return nil
}
