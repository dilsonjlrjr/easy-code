// Package pending is the single inbox of a person (listPendingItems, US-48, US-10 CA-2): what waits
// for a human in the projects where the person has a role. Phase 1 has gates (for whoever decides:
// approver or lead, never a member only, US-48 CA-3) and agent questions (every role, US-48 RN-2);
// Phase 2 adds the escalated findings waiting for the approver decision (US-32, ESP-19). Budget alerts
// (US-39) join in MARCO-6.
package pending

import (
	"context"
	"slices"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Item types of the contract (PendingItemType).
const (
	TypeGate     = "gate"
	TypeQuestion = "question"
	TypeFinding  = "finding_escalated"
)

// Phase 1 types; the other ones of the contract are accepted and give nothing yet.
var knownTypes = []string{TypeGate, TypeQuestion, TypeFinding, "budget"}

// Item is one waiting item.
type Item struct {
	Type         string
	ProjectID    string
	ProjectName  string
	Stage        string
	Agent        string
	TargetType   string // gate_round or question
	TargetID     string // gate.requirements/2 or REQ-01/PERG-1
	OpenedAt     time.Time
	Gate         string // gate items
	Round        int
	FailedChecks int
	Blocks       bool
	Wave         string // question items
	Questions    int
	Finding      string // finding items: the ACH, its severity, cycles and whether it blocks the Release
	Severity     string
	Cycles       int
	YouDecide    bool
}

// Filter narrows the inbox.
type Filter struct {
	Types  []string
	Limit  int
	Offset int
}

// Store reads the inbox.
type Store interface {
	// Items returns the items of the person, oldest first, and the total with the same filter.
	Items(ctx context.Context, principal identity.Principal, types []string, limit, offset int) ([]Item, int, error)
}

// Service is listPendingItems.
type Service struct {
	store Store
}

// NewService builds the service.
func NewService(store Store) *Service { return &Service{store: store} }

// List returns one page of the inbox (US-48 CA-1: type, project, stage and age, oldest first).
func (s *Service) List(ctx context.Context, principal identity.Principal, filter Filter) ([]Item, int, error) {
	for _, t := range filter.Types {
		if !slices.Contains(knownTypes, t) {
			return nil, 0, failure.Invalid(failure.FieldError{Field: "type", Message: "enum"})
		}
	}
	types := filter.Types
	if len(types) == 0 {
		types = []string{TypeGate, TypeQuestion, TypeFinding}
	}
	return s.store.Items(ctx, principal, types, filter.Limit, filter.Offset)
}
