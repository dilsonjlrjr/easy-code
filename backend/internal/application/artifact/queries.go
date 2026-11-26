package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	appproject "easycode/internal/application/project"
	domainartifact "easycode/internal/domain/artifact"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"

	"gopkg.in/yaml.v3"
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
	Situation string // T only: situacao of the current header (tracking field)
}

// View is one version with header and body (Artifact).
type View struct {
	Summary
	Header json.RawMessage
	Body   string
	Commit string // "" until the wave commit (git arrives with US-42)
	// Attachments are the other files of the version (PL-24).
	Attachments []domainartifact.Attachment
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
	Finding     string // ACH that drove the version (rework), "" otherwise (US-27 CA-2)
}

// Comparison is compareArtifactVersions (US-27): the header fields and body lines added, removed and
// changed from one version to another, and the findings that drove the versions in between.
type Comparison struct {
	Code            string
	From, To        int
	Header          []domainartifact.FieldChange
	Body            []domainartifact.LineChange
	DrivingFindings []string
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

// Files reads a file of the project repository at a commit (the content of an attachment, PL-24).
type Files interface {
	// FileAt returns the file content, or not-found when the commit has no such file.
	FileAt(ctx context.Context, projectID, commit, path string) ([]byte, error)
}

// Queries are the artifact reads of the api (US-26), for members of the project.
type Queries struct {
	projects appproject.Repository
	reader   Reader
	files    Files
}

// WithFiles gives the queries the project repository, for getArtifactAttachment (PL-24).
func (q *Queries) WithFiles(files Files) *Queries {
	q.files = files
	return q
}

// AttachmentContent is a text attachment of a version (getArtifactAttachment).
type AttachmentContent struct {
	Attachment domainartifact.Attachment
	Content    []byte
}

// Attachment is getArtifactAttachment (PL-24, RNF-05): a text attachment listed in the version, in the
// exact content of its commit. A path out of the attachments is not-found; HTML, script, style, image
// or font (the prototype) is a state-conflict, they only leave through the content origin (ADR-19).
func (q *Queries) Attachment(ctx context.Context, principal identity.Principal, projectID, code string, version int, path string) (AttachmentContent, error) {
	if _, err := appproject.Authorize(ctx, q.projects, principal, projectID); err != nil {
		return AttachmentContent{}, err
	}
	if !domainartifact.ValidCode(code) || version < 1 {
		return AttachmentContent{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if path == "" || len(path) > 512 || strings.HasPrefix(path, "/") || strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return AttachmentContent{}, failure.Invalid(failure.FieldError{Field: "path", Message: "pattern"})
	}
	view, err := q.reader.Version(ctx, projectID, code, version)
	if err != nil {
		return AttachmentContent{}, err
	}
	attachment, ok := domainartifact.FindAttachment(view.Attachments, path)
	if !ok {
		return AttachmentContent{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if !domainartifact.TextAttachment(attachment.MediaType) {
		return AttachmentContent{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).
			With("reason", "the attachment is not text: it opens only through getArtifactContentUrl")
	}
	if view.Commit == "" || q.files == nil {
		return AttachmentContent{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).
			With("reason", "the version has no commit in the project repository yet")
	}
	content, err := q.files.FileAt(ctx, projectID, view.Commit, attachment.RepositoryPath)
	if err != nil {
		return AttachmentContent{}, err
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != attachment.SHA256 {
		return AttachmentContent{}, failure.New(failure.KindInternal, failure.CodeInternal, nil)
	}
	return AttachmentContent{Attachment: attachment, Content: content}, nil
}

// File is getArtifactVersion as text/markdown (US-26 CA-2, US-42 CA-2): the exact file of the version in the
// project repository. A version without a commit yet (or queries without the repository) is rebuilt from its
// header and body, the same content the version stored.
func (q *Queries) File(ctx context.Context, principal identity.Principal, projectID, code string, version int) (View, []byte, error) {
	view, err := q.Get(ctx, principal, projectID, code, version)
	if err != nil {
		return View{}, nil, err
	}
	if view.Commit != "" && q.files != nil && view.Path != "" {
		content, err := q.files.FileAt(ctx, projectID, view.Commit, view.Path)
		if err == nil {
			return view, content, nil
		}
		if failure.From(err).Kind != failure.KindNotFound {
			return View{}, nil, err
		}
	}
	content, err := rebuildFile(view.Header, view.Body)
	if err != nil {
		return View{}, nil, err
	}
	return view, content, nil
}

// rebuildFile writes the Markdown file of a version: YAML front matter of the header and the body.
func rebuildFile(header json.RawMessage, body string) ([]byte, error) {
	var fields yaml.Node
	if err := yaml.Unmarshal(header, &fields); err != nil { // JSON is YAML; the node keeps the key order
		return nil, fmt.Errorf("artifact header: %w", err)
	}
	if fields.Kind == yaml.DocumentNode && len(fields.Content) == 1 {
		setBlockStyle(fields.Content[0])
	}
	var front strings.Builder
	encoder := yaml.NewEncoder(&front)
	encoder.SetIndent(2)
	if err := encoder.Encode(&fields); err != nil {
		return nil, fmt.Errorf("artifact header: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("artifact header: %w", err)
	}
	if !strings.HasPrefix(body, "\n") {
		body = "\n" + body
	}
	return []byte("---\n" + front.String() + "---" + body), nil
}

// setBlockStyle turns the flow style of the JSON source into YAML block style with plain keys; the values
// keep their quotes, so any text stays text.
func setBlockStyle(node *yaml.Node) {
	if node.Kind == yaml.MappingNode || node.Kind == yaml.SequenceNode {
		node.Style = 0
	}
	for i, child := range node.Content {
		if node.Kind == yaml.MappingNode && i%2 == 0 {
			child.Style = 0 // the encoder quotes a key only when it needs to
		}
		setBlockStyle(child)
	}
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

// Compare is compareArtifactVersions: any two versions, not only neighbors (ER-21-6); the findings that
// drove the versions after the older one up to the newer one come with the comparison (US-27 CA-2).
func (q *Queries) Compare(ctx context.Context, principal identity.Principal, projectID, code string, from, to int) (Comparison, error) {
	if _, err := appproject.Authorize(ctx, q.projects, principal, projectID); err != nil {
		return Comparison{}, err
	}
	if !domainartifact.ValidCode(code) {
		return Comparison{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	var fields []failure.FieldError
	if from < 1 {
		fields = append(fields, failure.FieldError{Field: "from", Message: "range"})
	}
	if to < 1 {
		fields = append(fields, failure.FieldError{Field: "to", Message: "range"})
	}
	if len(fields) > 0 {
		return Comparison{}, failure.Invalid(fields...)
	}
	versions, err := q.reader.Versions(ctx, projectID, code)
	if err != nil {
		return Comparison{}, err
	}
	low, high := min(from, to), max(from, to)
	out := Comparison{Code: code, From: from, To: to, DrivingFindings: []string{}}
	found := 0
	seen := map[string]bool{}
	for i := len(versions) - 1; i >= 0; i-- { // ascending version
		v := versions[i]
		if v.Version == from || v.Version == to {
			found++
		}
		if v.Version > low && v.Version <= high && v.Finding != "" && !seen[v.Finding] {
			seen[v.Finding] = true
			out.DrivingFindings = append(out.DrivingFindings, v.Finding)
		}
	}
	if found < 2 && (from != to || found != 1) {
		return Comparison{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	before, err := q.reader.Version(ctx, projectID, code, from)
	if err != nil {
		return Comparison{}, err
	}
	after, err := q.reader.Version(ctx, projectID, code, to)
	if err != nil {
		return Comparison{}, err
	}
	if out.Header, err = domainartifact.CompareHeaders(before.Header, after.Header); err != nil {
		return Comparison{}, err
	}
	out.Body = domainartifact.CompareBodies(before.Body, after.Body)
	return out, nil
}
