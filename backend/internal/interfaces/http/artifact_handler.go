package http

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// ArtifactUseCases are the artifact reads (US-26).
type ArtifactUseCases interface {
	List(ctx context.Context, principal identity.Principal, projectID string, filter appartifact.Filter) (appartifact.Page, error)
	Get(ctx context.Context, principal identity.Principal, projectID, code string, version int) (appartifact.View, error)
	Versions(ctx context.Context, principal identity.Principal, projectID, code string) ([]appartifact.VersionSummary, error)
	Compare(ctx context.Context, principal identity.Principal, projectID, code string, from, to int) (appartifact.Comparison, error)
}

type artifactAuthorJSON struct {
	Type  string `json:"type"`
	Agent string `json:"agent,omitempty"`
}

type artifactSummaryJSON struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	Title     string             `json:"title"`
	State     string             `json:"state"`
	Version   int                `json:"version"`
	Stage     *string            `json:"stage"`
	Author    artifactAuthorJSON `json:"author"`
	Wave      string             `json:"wave"`
	Path      string             `json:"path"`
	UpdatedAt time.Time          `json:"updated_at"`
	Situation *string            `json:"situation"`
}

type artifactJSON struct {
	artifactSummaryJSON
	Header      json.RawMessage `json:"header"`
	Body        string          `json:"body"`
	Commit      *string         `json:"commit"`
	Attachments []any           `json:"attachments"`
}

type artifactsPageJSON struct {
	Items      []artifactSummaryJSON `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}

type artifactVersionJSON struct {
	Version     int                `json:"version"`
	Date        string             `json:"data"`
	Wave        string             `json:"wave"`
	Author      artifactAuthorJSON `json:"author"`
	Description string             `json:"description"`
	Finding     *string            `json:"finding"`
	Approved    bool               `json:"approved"`
	Commit      *string            `json:"commit"`
}

func authorOf(author string) artifactAuthorJSON {
	switch author {
	case "orchestrator", "human":
		return artifactAuthorJSON{Type: author}
	default:
		return artifactAuthorJSON{Type: "agent", Agent: author}
	}
}

func artifactSummaryOf(s appartifact.Summary) artifactSummaryJSON {
	return artifactSummaryJSON{ID: s.Code, Type: s.Type, Title: s.Title, State: s.State, Version: s.Version, Stage: optional(s.Stage),
		Author: authorOf(s.Author), Wave: s.WaveCode, Path: s.Path, UpdatedAt: s.UpdatedAt, Situation: optional(s.Situation)}
}

func (a *api) listArtifacts(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	limit, err := queryLimit(rc)
	if err != nil {
		return err
	}
	page, err := a.deps.Artifacts.List(RequestContext(rc), principal, projectID, appartifact.Filter{
		Types: queryList(rc, "type"), Stage: string(rc.QueryArgs().Peek("stage")), States: queryList(rc, "state"),
		Search: string(rc.QueryArgs().Peek("search")), Limit: limit, Cursor: string(rc.QueryArgs().Peek("cursor")),
	})
	if err != nil {
		return err
	}
	out := artifactsPageJSON{Items: []artifactSummaryJSON{}}
	for _, item := range page.Items {
		out.Items = append(out.Items, artifactSummaryOf(item))
	}
	if page.NextCursor != "" {
		out.NextCursor = &page.NextCursor
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}

func (a *api) artifactPath(rc *fasthttp.RequestCtx) (string, string, error) {
	projectID, err := pathUUID(rc, "projectId")
	return projectID, PathParam(rc, "artifactId"), err
}

func (a *api) writeArtifact(rc *fasthttp.RequestCtx, view appartifact.View) {
	out := artifactJSON{artifactSummaryJSON: artifactSummaryOf(view.Summary), Header: view.Header, Body: view.Body,
		Commit: optional(view.Commit), Attachments: []any{}}
	setETag(rc, strconv.Itoa(view.Version))
	a.responder.JSON(rc, fasthttp.StatusOK, out)
}

// getArtifact is the current version (US-26 CA-1, CA-3: "wave" leads to the origin wave).
func (a *api) getArtifact(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := a.artifactPath(rc)
	if err != nil {
		return err
	}
	view, err := a.deps.Artifacts.Get(RequestContext(rc), principal, projectID, code, 0)
	if err != nil {
		return err
	}
	a.writeArtifact(rc, view)
	return nil
}

// getArtifactVersion is the exact content of a version (US-26 CA-2). Only application/json in
// MARCO-1: the file of the git repository (text/markdown) arrives with US-42.
func (a *api) getArtifactVersion(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := a.artifactPath(rc)
	if err != nil {
		return err
	}
	version, err := strconv.Atoi(PathParam(rc, "version"))
	if err != nil || version < 1 {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	view, err := a.deps.Artifacts.Get(RequestContext(rc), principal, projectID, code, version)
	if err != nil {
		return err
	}
	a.writeArtifact(rc, view)
	return nil
}

func (a *api) listArtifactVersions(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := a.artifactPath(rc)
	if err != nil {
		return err
	}
	versions, err := a.deps.Artifacts.Versions(RequestContext(rc), principal, projectID, code)
	if err != nil {
		return err
	}
	items := make([]artifactVersionJSON, 0, len(versions))
	for _, v := range versions {
		items = append(items, artifactVersionJSON{Version: v.Version, Date: v.CreatedAt.UTC().Format(time.DateOnly), Wave: v.WaveCode,
			Author: authorOf(v.Author), Description: v.Description, Approved: v.Approved, Commit: optional(v.Commit), Finding: optional(v.Finding)})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, itemsJSON[artifactVersionJSON]{Items: items})
	return nil
}

type headerChangeJSON struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Before    any    `json:"before,omitempty"`
	After     any    `json:"after,omitempty"`
}

type lineChangeJSON struct {
	Operation string `json:"operation"`
	LineFrom  *int   `json:"line_from"`
	LineTo    *int   `json:"line_to"`
	Text      string `json:"text"`
}

type comparisonJSON struct {
	Artifact        string             `json:"artifact"`
	From            int                `json:"from"`
	To              int                `json:"to"`
	Header          []headerChangeJSON `json:"header"`
	Body            []lineChangeJSON   `json:"body"`
	DrivingFindings []string           `json:"driving_findings"`
}

func lineNumber(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// compareArtifactVersions is the comparison of two versions (US-27 CA-1) with the findings that drove the
// versions in between (CA-2).
func (a *api) compareArtifactVersions(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := a.artifactPath(rc)
	if err != nil {
		return err
	}
	versionArg := func(name string) int {
		n, err := strconv.Atoi(string(rc.QueryArgs().Peek(name)))
		if err != nil {
			return 0
		}
		return n
	}
	cmp, err := a.deps.Artifacts.Compare(RequestContext(rc), principal, projectID, code, versionArg("from"), versionArg("to"))
	if err != nil {
		return err
	}
	out := comparisonJSON{Artifact: cmp.Code, From: cmp.From, To: cmp.To, Header: []headerChangeJSON{}, Body: []lineChangeJSON{},
		DrivingFindings: cmp.DrivingFindings}
	for _, c := range cmp.Header {
		out.Header = append(out.Header, headerChangeJSON(c))
	}
	for _, c := range cmp.Body {
		out.Body = append(out.Body, lineChangeJSON{Operation: c.Operation, LineFrom: lineNumber(c.LineFrom), LineTo: lineNumber(c.LineTo), Text: c.Text})
	}
	a.responder.JSON(rc, fasthttp.StatusOK, out)
	return nil
}
