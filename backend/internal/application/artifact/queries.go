package artifact

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	appproject "easycode/internal/application/project"
	domainartifact "easycode/internal/domain/artifact"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Summary is an artifact in a list (ArtifactSummary).
type Summary struct {
	Code      string
	Type      string
	Title     string
	State     string
	Version   int
	Stage     string
	Author    string // agent id, "orchestrator" or "human"
	WaveCode  string
	Path      string
	UpdatedAt time.Time
}

// View is one version with header and body (Artifact).
type View struct {
	Summary
	Header json.RawMessage
	Body   string
	Commit string // "" until the wave commit (git arrives with US-42)
}

// VersionSummary is a history entry (ArtifactVersionSummary).
type VersionSummary struct {
	Version     int
	CreatedAt   time.Time
	WaveCode    string
	Author      string
	Description string
	Approved    bool
	Commit      string
}

// Filter narrows listArtifacts.
type Filter struct {
	Types  []string
	Stage  string
	States []string
	Search string
	Limit  int
	Cursor string
}

// Page is one page of artifacts.
type Page struct {
	Items      []Summary
	NextCursor string
}

// Reader reads artifacts of a project (api role).
type Reader interface {
	List(ctx context.Context, projectID string, filter Filter) (Page, error)
	// Version returns a version (0 = current), or not-found.
	Version(ctx context.Context, projectID, code string, version int) (View, error)
	Versions(ctx context.Context, projectID, code string) ([]VersionSummary, error)
}

// Queries are the artifact reads of the api (US-26), for members of the project.
type Queries struct {
	projects appproject.Repository
	reader   Reader
}

// NewQueries builds the queries.
func NewQueries(projects appproject.Repository, reader Reader) *Queries {
	return &Queries{projects: projects, reader: reader}
}

// List is listArtifacts (order by type and id).
func (q *Queries) List(ctx context.Context, principal identity.Principal, projectID string, filter Filter) (Page, error) {
	if _, err := appproject.Authorize(ctx, q.projects, principal, projectID); err != nil {
		return Page{}, err
	}
	for _, artifactType := range filter.Types {
		if !slices.Contains(domainartifact.Types, artifactType) {
			return Page{}, failure.Invalid(failure.FieldError{Field: "type", Message: "enum"})
		}
	}
	for _, state := range filter.States {
		if !slices.Contains([]string{domainartifact.StateDraft, domainartifact.StateInReview, domainartifact.StateApproved, domainartifact.StateReplaced}, state) {
			return Page{}, failure.Invalid(failure.FieldError{Field: "state", Message: "enum"})
		}
	}
	if filter.Search != "" && (len([]rune(filter.Search)) < 2 || len([]rune(filter.Search)) > 200) {
		return Page{}, failure.Invalid(failure.FieldError{Field: "search", Message: "length"})
	}
	return q.reader.List(ctx, projectID, filter)
}

// Get is getArtifact (current version, US-26 CA-1, CA-3) or getArtifactVersion (CA-2).
func (q *Queries) Get(ctx context.Context, principal identity.Principal, projectID, code string, version int) (View, error) {
	if _, err := appproject.Authorize(ctx, q.projects, principal, projectID); err != nil {
		return View{}, err
	}
	if !domainartifact.ValidCode(code) || version < 0 {
		return View{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return q.reader.Version(ctx, projectID, code, version)
}

// Versions is listArtifactVersions (descending version, US-26 CA-1).
func (q *Queries) Versions(ctx context.Context, principal identity.Principal, projectID, code string) ([]VersionSummary, error) {
	if _, err := appproject.Authorize(ctx, q.projects, principal, projectID); err != nil {
		return nil, err
	}
	if !domainartifact.ValidCode(code) {
		return nil, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return q.reader.Versions(ctx, projectID, code)
}
