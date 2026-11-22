// Package project holds the project use cases: create (US-03), read and list (US-50 basics),
// and the access check every project route runs (RNF-04: no role = not found).
package project

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	"easycode/internal/application/audit"
	"easycode/internal/application/event"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	"easycode/internal/domain/project"
)

// UserRef names a person by subject and display name.
type UserRef struct {
	Subject string
	Name    string
}

// StageState is a stage and its state.
type StageState struct {
	Stage string
	State string
}

// WaveRef is an active wave.
type WaveRef struct {
	Code  string
	Agent string
	State string
}

// Summary holds the dashboard indicators (ProjectSummary).
type Summary struct {
	CurrentStages   []StageState
	ApprovedStages  int
	StagesTotal     int
	ActiveWaves     []WaveRef
	GatesAwaiting   int
	OpenQuestions   int
	AccumulatedCost project.Money
	StopLimit       *project.Money
}

// View is a project as the api shows it.
type View struct {
	ID               string
	Name             string
	State            string
	PauseReason      string
	BusinessNeed     string
	Workflow         string
	AcknowledgedBy   UserRef
	AcknowledgedAt   time.Time
	CreatedAt        time.Time
	CreatedBy        UserRef
	UpdatedAt        time.Time
	MyRoles          []identity.Role
	Lead             UserRef
	AvailableActions []string
	Summary          Summary
	LastSequence     int64
	// MetadataOnly marks the administrator view of a project without a role: id, name, state,
	// lead and dates, no content nor summary (US-02 CA-6).
	MetadataOnly bool
}

// Filter narrows listProjects.
type Filter struct {
	// Organization is scope=organization (administrator only): every project, metadata only.
	Organization bool
	States       []string
	Limit        int
	Cursor       string
}

// Page is one page of projects.
type Page struct {
	Items        []View
	NextCursor   string
	CountByState map[string]int
}

// Access is what authorizes an action on a project.
type Access struct {
	ProjectID string
	State     string
	Roles     []identity.Role
}

// Repository stores projects.
type Repository interface {
	// Create inserts the project in created, its creator as lead and approver, and constraints
	// version 1; a name already used in the organization fails with duplicate-name.
	Create(ctx context.Context, principal identity.Principal, input project.NewProject) (string, error)
	// Get returns the project for a person with a role in it, else not-found.
	Get(ctx context.Context, principal identity.Principal, projectID string) (View, error)
	List(ctx context.Context, principal identity.Principal, filter Filter) (Page, error)
	Access(ctx context.Context, principal identity.Principal, projectID string) (Access, error)
	// Overview returns the metadata of a project (administrator without a role, US-02 CA-6).
	Overview(ctx context.Context, projectID string) (View, error)
	// BudgetRules returns the organization currency and stop limit ceiling (nil = none).
	BudgetRules(ctx context.Context, organizationID string) (string, *string, error)
}

// Service implements the project use cases.
type Service struct {
	tx         transaction.Runner
	repo       Repository
	outbox     event.Outbox
	trail      audit.Trail
	workflowID string
	version    int
}

// NewService builds the service. workflowID and version name the workflow in force (Phase 1:
// workflow-basico v1), recorded in project.created.
func NewService(tx transaction.Runner, repo Repository, outbox event.Outbox, trail audit.Trail, workflowID string, version int) *Service {
	return &Service{tx: tx, repo: repo, outbox: outbox, trail: trail, workflowID: workflowID, version: version}
}

// Create is createProject (US-03 CA-1..CA-3, RN-1, RN-2). Call it inside the idempotency transaction.
func (s *Service) Create(ctx context.Context, principal identity.Principal, input project.NewProject) (View, error) {
	currency, ceiling, err := s.repo.BudgetRules(ctx, principal.OrganizationID)
	if err != nil {
		return View{}, err
	}
	if err := input.Validate(currency, ceiling); err != nil {
		return View{}, err
	}
	var projectID string
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if projectID, err = s.repo.Create(ctx, principal, input); err != nil {
			return err
		}
		correlation, err := uuid.NewV7()
		if err != nil {
			return err
		}
		envelope, err := s.outbox.Append(ctx, domainevent.Draft{
			Type: "br.easycode.project.created.v1", Subject: "project", ProjectID: projectID,
			CorrelationID: correlation.String(), Data: s.createdPayload(projectID, principal, input),
		})
		if err != nil {
			return err
		}
		_, err = s.trail.Append(ctx, domainaudit.Record{
			ProjectID: projectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
			Origin: domainaudit.OriginWeb, Action: "project.create", Target: "project",
			EventID: envelope.ID, CorrelationID: correlation.String(),
		})
		return err
	})
	if err != nil {
		return View{}, err
	}
	return s.repo.Get(ctx, principal, projectID)
}

type moneyJSON struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

func (s *Service) createdPayload(projectID string, principal identity.Principal, input project.NewProject) json.RawMessage {
	payload := map[string]any{
		"project":    projectID,
		"transition": map[string]any{"machine": "project", "from_state": nil, "to_state": project.StateCreated},
		"actor":      map[string]any{"type": "human", "id": principal.Subject, "origin": "web"},
		"workflow":   map[string]any{"id": s.workflowID, "version": s.version},
	}
	if input.Budget != nil {
		alerts := []moneyJSON{}
		for _, value := range input.Budget.AlertValues() {
			alerts = append(alerts, moneyJSON{Value: value.Value, Currency: value.Currency})
		}
		payload["budget"] = map[string]any{
			"stop_limit":   moneyJSON{Value: project.TrimDecimal(input.Budget.StopLimit.Value), Currency: input.Budget.StopLimit.Currency},
			"alert_limits": alerts,
		}
	}
	encoded, _ := json.Marshal(payload)
	return encoded
}

// Get is getProject.
func (s *Service) Get(ctx context.Context, principal identity.Principal, projectID string) (View, error) {
	return s.repo.Get(ctx, principal, projectID)
}

// GetAsAdministrator is getProject for the administrator without a role in the project: only
// the metadata, after the access is authorized and recorded by the member service (US-02 CA-6).
func (s *Service) GetAsAdministrator(ctx context.Context, projectID string) (View, error) {
	return s.repo.Overview(ctx, projectID)
}

// List is listProjects; scope=organization is only for the administrator (US-02 CA-6, US-51).
func (s *Service) List(ctx context.Context, principal identity.Principal, filter Filter) (Page, error) {
	if filter.Organization && !principal.Administrator {
		return Page{}, failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	for _, state := range filter.States {
		if !slices.Contains(project.States, state) {
			return Page{}, failure.Invalid(failure.FieldError{Field: "state", Message: "enum"})
		}
	}
	return s.repo.List(ctx, principal, filter)
}

// Authorize returns the access of a principal with one of the roles, else 404 (no role) or 403.
func Authorize(ctx context.Context, repo Repository, principal identity.Principal, projectID string, roles ...identity.Role) (Access, error) {
	access, err := repo.Access(ctx, principal, projectID)
	if err != nil {
		return Access{}, err
	}
	if len(roles) > 0 && !identity.Has(access.Roles, roles...) {
		return Access{}, failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	return access, nil
}
