// Package content issues and honors the signed URLs of the content origin (ADR-19, PD-13, PL-06): the
// prototype HTML of a TELAS version is never served by the platform origin, only by
// content.<domain>, without cookie, through a URL whose token covers the prototype folder of the version.
// The URL opens for 5 minutes; once opened, a short content session (30 minutes) keeps the relative links
// between the prototype pages working (ER-11-10). Nothing is written to the database (getArtifactContentUrl).
package content

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Durations of ADR-19: the URL opens for OpenFor; the content session lasts SessionFor after the first use.
const (
	OpenFor    = 5 * time.Minute
	SessionFor = 30 * time.Minute
	// MaxFileBytes bounds one file of the prototype.
	MaxFileBytes = 10 << 20
	// PrototypeType is the artifact whose header points at the prototype folder.
	PrototypeType = "TELAS"
)

// ErrNoFile means the file does not exist at the commit (or is not a regular file within the limit).
var ErrNoFile = errors.New("content: no such file")

// Files reads the project repository at a commit (git adapter).
type Files interface {
	ContentFile(ctx context.Context, projectID, commit, path string) ([]byte, error)
}

// Artifacts reads an artifact version for a person with a role in the project (404 otherwise).
type Artifacts interface {
	Get(ctx context.Context, principal identity.Principal, projectID, code string, version int) (appartifact.View, error)
}

// URL is openapi ContentUrl.
type URL struct {
	URL       string
	Prefix    string
	ExpiresAt time.Time
}

// Page is one file served by the content origin.
type Page struct {
	Body      []byte
	MediaType string
}

// grant is what the token signs: the repository folder the URL path is relative to (Root), the
// signed folder inside it (Prefix) and until when the URL opens.
type grant struct {
	ProjectID string `json:"p"`
	Commit    string `json:"c"`
	Root      string `json:"r"`
	Prefix    string `json:"x"`
	OpenBy    int64  `json:"o"`
}

// Service issues the URLs (api routes) and opens them (content origin).
type Service struct {
	artifacts Artifacts
	files     Files
	origin    string
	key       []byte
	now       func() time.Time

	mu       sync.Mutex
	sessions map[string]time.Time // token signature → first use
}

// DeriveKey derives the signing key of the content URLs from the session secret of the api.
func DeriveKey(secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("easycode/content-url/v1"))
	return mac.Sum(nil)
}

// NewService builds the service; origin is the public origin of the content host (https://content.<domain>).
func NewService(artifacts Artifacts, files Files, origin string, key []byte) *Service {
	return &Service{artifacts: artifacts, files: files, origin: strings.TrimRight(origin, "/"), key: key, now: time.Now,
		sessions: map[string]time.Time{}}
}

// Issue is getArtifactContentUrl: the signed URL of a page of the prototype of the version. page is
// relative to the folder that holds the prototype folder ("prototipo/index.html" by default).
func (s *Service) Issue(ctx context.Context, principal identity.Principal, projectID, code string, version int, page string) (URL, error) {
	notFound := failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	if version < 1 {
		return URL{}, notFound
	}
	view, err := s.artifacts.Get(ctx, principal, projectID, code, version)
	if err != nil {
		return URL{}, err
	}
	root, prefix, ok := prototypeFolder(view)
	if !ok || view.Commit == "" {
		return URL{}, notFound
	}
	if page == "" {
		page = prefix + "index.html"
	}
	if !safeRelative(page) || !strings.HasPrefix(page, prefix) {
		return URL{}, failure.Invalid(failure.FieldError{Field: "path", Message: "invalid"})
	}
	if _, err := s.files.ContentFile(ctx, projectID, view.Commit, root+page); err != nil {
		if errors.Is(err, ErrNoFile) {
			return URL{}, notFound
		}
		return URL{}, err
	}
	openBy := s.now().Add(OpenFor)
	token, err := s.sign(grant{ProjectID: projectID, Commit: view.Commit, Root: root, Prefix: prefix, OpenBy: openBy.Unix()})
	if err != nil {
		return URL{}, err
	}
	return URL{URL: s.origin + "/c/" + token + "/" + escapePath(page), Prefix: prefix, ExpiresAt: openBy.UTC().Truncate(time.Second)}, nil
}

// Open serves a file under the signed prefix of token (content origin). Every refusal is ErrNoFile: the
// content origin says nothing about why.
func (s *Service) Open(ctx context.Context, token, rel string) (Page, error) {
	g, signature, ok := s.verify(token)
	if !ok || !s.session(signature, time.Unix(g.OpenBy, 0)) {
		return Page{}, ErrNoFile
	}
	if strings.HasSuffix(rel, "/") {
		rel += "index.html"
	}
	if !safeRelative(rel) || !strings.HasPrefix(rel, g.Prefix) {
		return Page{}, ErrNoFile
	}
	mediaType, known := MediaType(rel)
	if !known {
		return Page{}, ErrNoFile
	}
	body, err := s.files.ContentFile(ctx, g.ProjectID, g.Commit, g.Root+rel)
	if err != nil {
		return Page{}, err
	}
	return Page{Body: body, MediaType: mediaType}, nil
}

// session admits the first use of a URL until openBy and then its content session for SessionFor.
func (s *Service) session(signature string, openBy time.Time) bool {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	first, used := s.sessions[signature]
	if !used {
		if now.After(openBy) {
			return false
		}
		for key, at := range s.sessions {
			if now.Sub(at) > SessionFor {
				delete(s.sessions, key)
			}
		}
		s.sessions[signature] = now
		return true
	}
	return now.Sub(first) <= SessionFor
}

func (s *Service) sign(g grant) (string, error) {
	payload, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(s.mac(encoded)), nil
}

func (s *Service) verify(token string) (grant, string, bool) {
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return grant{}, "", false
	}
	given, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || subtle.ConstantTimeCompare(given, s.mac(encoded)) != 1 {
		return grant{}, "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	var g grant
	if err != nil || json.Unmarshal(payload, &g) != nil || g.ProjectID == "" || g.Commit == "" || g.Prefix == "" {
		return grant{}, "", false
	}
	return g, signature, true
}

func (s *Service) mac(encoded string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(encoded))
	return mac.Sum(nil)
}

// prototypeFolder splits the prototype folder of a TELAS header ("docs/design/prototipo/") into the
// folder the URL path is relative to ("docs/design/") and the signed prefix ("prototipo/").
func prototypeFolder(view appartifact.View) (string, string, bool) {
	if view.Type != PrototypeType {
		return "", "", false
	}
	var header struct {
		Prototype string `json:"prototipo"`
	}
	if json.Unmarshal(view.Header, &header) != nil {
		return "", "", false
	}
	folder := strings.TrimSuffix(header.Prototype, "/")
	if folder == "" || !safeRelative(folder) {
		return "", "", false
	}
	root := path.Dir(folder)
	if root == "." {
		return "", folder + "/", true
	}
	return root + "/", folder[len(root)+1:] + "/", true
}

// safeRelative is the openapi pattern of the path: relative, no "..", no backslash, nothing odd.
func safeRelative(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00\r\n") || len(p) > 512 {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." || segment == "." {
			return false
		}
	}
	return path.Clean(p) == strings.TrimSuffix(p, "/")
}

func escapePath(p string) string {
	segments := strings.Split(p, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

var mediaTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".json": "application/json",
	".txt": "text/plain; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg",
	".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon", ".woff": "font/woff",
	".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
}

// MediaType is the type the content origin serves a file with; unknown extensions are not served.
func MediaType(p string) (string, bool) {
	t, ok := mediaTypes[strings.ToLower(path.Ext(p))]
	return t, ok
}
