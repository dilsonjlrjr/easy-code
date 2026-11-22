package project

import (
	"context"
	"fmt"
	"slices"
	"time"

	"easycode/internal/application/audit"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/pricing"
	"easycode/internal/domain/project"
	"easycode/internal/domain/workflow"
)

// CodeFailedPrecondition fails a change made over an outdated ETag (412).
const CodeFailedPrecondition = "failed-precondition"

// ModelRef names a model of a provider.
type ModelRef struct {
	Provider string
	Model    string
}

// Pair is the reasoning × execution model pair (PROJETO 9.3, US-16).
type Pair struct {
	Reasoning ModelRef
	Execution ModelRef
}

// GateMode is the approval mode of a gate in the project.
type GateMode struct {
	Mode        string
	Allowed     []string
	AlwaysHuman bool
}

// ConstraintsView is the current version of the technical constraints.
type ConstraintsView struct {
	Version   int
	Items     []project.Constraint
	DefinedBy UserRef
	DefinedAt time.Time
}

// Configuration is getProjectConfiguration.
type Configuration struct {
	ModelPair        *Pair
	PairByStage      map[string]Pair
	GateApproval     map[string]GateMode
	Budget           *project.Budget
	StopLimitCeiling *project.Money
	ArtifactLanguage string
	Constraints      *ConstraintsView
	Version          int // projects.row_version, the ETag
}

// Change is a JSON Merge Patch of the configuration; nil keeps the value. A nil pair in
// PairByStage removes the stage adjustment.
type Change struct {
	ModelPair        *Pair
	PairByStage      map[string]*Pair
	GateApproval     map[string]string
	Budget           *project.Budget
	ArtifactLanguage *string
	Constraints      *[]project.Constraint
}

// StoredConfiguration is what the repository reads.
type StoredConfiguration struct {
	Configuration
	GateModes map[string]string
	Currency  string
}

// ConfigurationRepository reads and writes the configuration of a project.
type ConfigurationRepository interface {
	Configuration(ctx context.Context, projectID string) (StoredConfiguration, error)
	// Save applies the change if the row version is still version (else failed-precondition)
	// and bumps it; constraints become a new version.
	Save(ctx context.Context, principal identity.Principal, projectID string, version int, change Change) error
}

// ModelLookup finds a registered model of the capability matrix.
type ModelLookup interface {
	Model(ctx context.Context, providerName, name string) (pricing.Model, error)
}

// ConfigurationService implements the project configuration use cases (US-16, US-39, US-54).
type ConfigurationService struct {
	tx         transaction.Runner
	projects   Repository
	repo       ConfigurationRepository
	models     ModelLookup
	trail      audit.Trail
	definition workflow.Definition
}

// NewConfigurationService builds the service for the workflow in force.
func NewConfigurationService(tx transaction.Runner, projects Repository, repo ConfigurationRepository, models ModelLookup, trail audit.Trail, definition workflow.Definition) *ConfigurationService {
	return &ConfigurationService{tx: tx, projects: projects, repo: repo, models: models, trail: trail, definition: definition}
}

// Get is getProjectConfiguration (member, approver or lead).
func (s *ConfigurationService) Get(ctx context.Context, principal identity.Principal, projectID string) (Configuration, error) {
	if _, err := Authorize(ctx, s.projects, principal, projectID); err != nil {
		return Configuration{}, err
	}
	stored, err := s.repo.Configuration(ctx, projectID)
	if err != nil {
		return Configuration{}, err
	}
	config := stored.Configuration
	config.GateApproval = map[string]GateMode{}
	for _, stage := range s.definition.Stages {
		mode := stored.GateModes[stage.Gate]
		if mode == "" {
			mode = "human"
		}
		// Phase 1: every gate of the basic workflow is human; automatic mode is US-07 (Phase 2).
		config.GateApproval[stage.Gate] = GateMode{Mode: mode, Allowed: []string{"human"}, AlwaysHuman: stage.Gate == "gate.release"}
	}
	return config, nil
}

// Modify is modifyProjectConfiguration (lead only, If-Match).
func (s *ConfigurationService) Modify(ctx context.Context, principal identity.Principal, projectID string, version int, change Change) (Configuration, error) {
	access, err := Authorize(ctx, s.projects, principal, projectID, identity.RoleLead)
	if err != nil {
		return Configuration{}, err
	}
	if access.State == project.StateCompleted || access.State == project.StateCanceled {
		return Configuration{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", access.State).With("available_actions", []string{})
	}
	stored, err := s.repo.Configuration(ctx, projectID)
	if err != nil {
		return Configuration{}, err
	}
	if stored.Version != version {
		return Configuration{}, failure.New(failure.KindConflict, CodeFailedPrecondition, nil)
	}
	if err := s.validate(ctx, stored, &change); err != nil {
		return Configuration{}, err
	}
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Save(ctx, principal, projectID, version, change); err != nil {
			return err
		}
		if err := s.record(ctx, principal, projectID, "configuration.modify", "configuration"); err != nil {
			return err
		}
		if change.Constraints != nil {
			// US-54 CA-3: who, when and the difference (ids added, changed or removed) of the new version.
			next := 1
			var previous []project.Constraint
			if stored.Constraints != nil {
				next, previous = stored.Constraints.Version+1, stored.Constraints.Items
			}
			_, err := s.trail.Append(ctx, domainaudit.Record{
				ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
				Origin: domainaudit.OriginWeb, Action: "constraints.modify", Target: "constraints",
				Decision: fmt.Sprintf("version %d", next), References: project.ConstraintChanges(previous, *change.Constraints),
			})
			return err
		}
		return nil
	})
	if err != nil {
		return Configuration{}, err
	}
	return s.Get(ctx, principal, projectID)
}

func (s *ConfigurationService) validate(ctx context.Context, stored StoredConfiguration, change *Change) error {
	if change.ModelPair != nil {
		if err := s.checkPair(ctx, "model_pair", *change.ModelPair); err != nil {
			return err
		}
	}
	for stage, pair := range change.PairByStage {
		if _, ok := s.definition.Stage(stage); !ok {
			return failure.Invalid(failure.FieldError{Field: "pair_by_stage." + stage, Message: "enum"})
		}
		if pair != nil {
			if err := s.checkPair(ctx, "pair_by_stage."+stage, *pair); err != nil {
				return err
			}
		}
	}
	for gate, mode := range change.GateApproval {
		if !slices.ContainsFunc(s.definition.Stages, func(st workflow.Stage) bool { return st.Gate == gate }) || (mode != "human" && mode != "automatic") {
			return failure.Invalid(failure.FieldError{Field: "gate_approval." + gate, Message: "enum"})
		}
		if mode == "automatic" {
			return failure.New(failure.KindConflict, project.CodeApprovalNotAllowed, nil)
		}
	}
	if change.ArtifactLanguage != nil && !project.ValidLanguage(*change.ArtifactLanguage) {
		return failure.Invalid(failure.FieldError{Field: "artifact_language", Message: "pattern"})
	}
	if change.Budget != nil {
		var ceiling *string
		if stored.StopLimitCeiling != nil {
			ceiling = &stored.StopLimitCeiling.Value
		}
		if err := project.ValidateBudget(change.Budget, stored.Currency, ceiling); err != nil {
			return err
		}
	}
	if change.Constraints != nil {
		var previous []project.Constraint
		if stored.Constraints != nil {
			previous = stored.Constraints.Items
		}
		numbered, fields := project.NumberConstraints(*change.Constraints, previous)
		if len(fields) > 0 {
			return failure.Invalid(fields...)
		}
		change.Constraints = &numbered
	}
	return nil
}

// checkPair refuses a model that is unknown, inactive or lacks what the stages need (US-16 CA-2).
func (s *ConfigurationService) checkPair(ctx context.Context, field string, pair Pair) error {
	for role, ref := range map[string]ModelRef{"reasoning": pair.Reasoning, "execution": pair.Execution} {
		model, err := s.models.Model(ctx, ref.Provider, ref.Model)
		if err != nil || !model.Active {
			return failure.Invalid(failure.FieldError{Field: field + "." + role, Message: "invalid"})
		}
		if missing := model.Missing(workflow.ModelRequirement()); missing != "" {
			return failure.New(failure.KindConflict, pricing.CodeModelWithoutCapability, nil).With("missing_capacity", string(missing))
		}
	}
	return nil
}

func (s *ConfigurationService) record(ctx context.Context, principal identity.Principal, projectID, action, target string) error {
	_, err := s.trail.Append(ctx, domainaudit.Record{
		ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
		Origin: domainaudit.OriginWeb, Action: action, Target: target,
	})
	return err
}
