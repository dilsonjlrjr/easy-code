// Package search is searchPlatform (US-56): the header search over project names and the title and id
// of artifacts and findings, only in the projects where the person has a role (RNF-04, US-56 RN-1; the
// organization administrator too). The body of artifacts and the text of findings stay out (RN-2,
// PENDENCIAS PD-24 option a). The term is never written to the trail or to a log (RN-4, RNF-20).
package search

import (
	"context"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Bounds of the term (openapi `search`: minLength 2, maxLength 100; US-56 CA-3).
const (
	MinTermLength = 2
	MaxTermLength = 100
	// PerGroup is the most results of each type (US-56 RN-3).
	PerGroup = 10
)

// Hit is one result. Project hits leave the item fields empty.
type Hit struct {
	ProjectID    string
	ProjectName  string
	ProjectState string
	ID           string // artifact or finding id (US-12, ACH-07)
	Type         string // artifact type
	Title        string
	State        string // artifact state or finding status
	Severity     string // findings
}

// Group is the first results of one type, with how many match in all and in how many projects.
type Group struct {
	Items    []Hit
	Total    int
	Projects int
}

// Results are the three groups of US-56 CA-2.
type Results struct {
	Projects  Group
	Artifacts Group
	Findings  Group
}

// Store finds the hits in the projects of the person.
type Store interface {
	// Find matches term (already trimmed) case-insensitively as a substring, at most limit per group.
	Find(ctx context.Context, principal identity.Principal, term string, limit int) (Results, error)
}

// Service is searchPlatform.
type Service struct {
	store Store
}

// NewService builds the service.
func NewService(store Store) *Service { return &Service{store: store} }

// Search validates the term and returns the groups (US-56 CA-1, CA-2, CA-3, RN-3).
func (s *Service) Search(ctx context.Context, principal identity.Principal, term string) (Results, error) {
	term = strings.TrimSpace(term)
	if n := utf8.RuneCountInString(term); n < MinTermLength || n > MaxTermLength {
		return Results{}, failure.Invalid(failure.FieldError{Field: "search", Message: "length"})
	}
	return s.store.Find(ctx, principal, term, PerGroup)
}
