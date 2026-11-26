package http

import (
	"context"
	"encoding/json"
	"path"
	"strconv"
	"strings"
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
	// Attachment is getArtifactAttachment (PL-24).
	Attachment(ctx context.Context, principal identity.Principal, projectID, code string, version int, path string) (appartifact.AttachmentContent, error)
	// File is getArtifactVersion as text/markdown: the exact file of the version (US-26 CA-2, US-42 CA-2).
	File(ctx context.Context, principal identity.Principal, projectID, code string, version int) (appartifact.View, []byte, error)
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
	Header      json.RawMessage  `json:"header"`
	Body        string           `json:"body"`
	Commit      *string          `json:"commit"`
	Attachments []attachmentJSON `json:"attachments"`
}

// attachmentJSON is one item of Artifact.attachments (PL-24); the repository path stays internal.
type attachmentJSON struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	SizeBytes int    `json:"size_bytes"`
	SHA256    string `json:"sha256"`
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
		Commit: optional(view.Commit), Attachments: []attachmentJSON{}}
	for _, a := range view.Attachments {
		out.Attachments = append(out.Attachments, attachmentJSON{Path: a.Path, MediaType: a.MediaType, SizeBytes: a.SizeBytes, SHA256: a.SHA256})
	}
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

// getArtifactVersion is the exact content of a version (US-26 CA-2): structured as application/json, or the
// file of the project repository as text/markdown (US-42 CA-2), a download the browser never renders (RNF-05).
func (a *api) getArtifactVersion(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := a.artifactPath(rc)
	if err != nil {
		return err
	}
	version, err := strconv.Atoi(PathParam(rc, "version"))
	if err != nil || version < 1 {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if wantsMarkdown(rc) {
		view, content, err := a.deps.Artifacts.File(RequestContext(rc), principal, projectID, code, version)
		if err != nil {
			return err
		}
		name := path.Base(view.Path)
		if view.Path == "" {
			name = code + ".md"
		}
		setETag(rc, strconv.Itoa(view.Version))
		rc.Response.Header.Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
		rc.Response.Header.Set("X-Content-Type-Options", "nosniff")
		rc.Response.Header.Set("Content-Security-Policy", "sandbox")
		rc.SetContentType("text/markdown; charset=utf-8")
		rc.SetStatusCode(fasthttp.StatusOK)
		rc.SetBody(content)
		return nil
	}
	view, err := a.deps.Artifacts.Get(RequestContext(rc), principal, projectID, code, version)
	if err != nil {
		return err
	}
	a.writeArtifact(rc, view)
	return nil
}

// wantsMarkdown reports whether the Accept header asks for text/markdown before application/json.
func wantsMarkdown(rc *fasthttp.RequestCtx) bool {
	accept := strings.ToLower(string(rc.Request.Header.Peek(fasthttp.HeaderAccept)))
	markdown := strings.Index(accept, "text/markdown")
	if markdown < 0 {
		return false
	}
	jsonAt := strings.Index(accept, "application/json")
	return jsonAt < 0 || markdown < jsonAt
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

// getArtifactAttachment is a text attachment of a version (PL-24, CA-21-16), served as a download that the
// browser never renders: Content-Disposition attachment, nosniff and CSP sandbox (RNF-05, ADR-19).
func (a *api) getArtifactAttachment(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, code, err := a.artifactPath(rc)
	if err != nil {
		return err
	}
	version, err := strconv.Atoi(PathParam(rc, "version"))
	if err != nil || version < 1 {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	found, err := a.deps.Artifacts.Attachment(RequestContext(rc), principal, projectID, code, version, string(rc.QueryArgs().Peek("path")))
	if err != nil {
		return err
	}
	contentType := "text/plain; charset=utf-8"
	if found.Attachment.MediaType == "text/markdown" {
		contentType = "text/markdown; charset=utf-8"
	}
	name := path.Base(found.Attachment.Path)
	rc.Response.Header.Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	rc.Response.Header.Set("X-Content-Type-Options", "nosniff")
	rc.Response.Header.Set("Content-Security-Policy", "sandbox")
	rc.SetContentType(contentType)
	rc.SetStatusCode(fasthttp.StatusOK)
	rc.SetBody(found.Content)
	return nil
}
