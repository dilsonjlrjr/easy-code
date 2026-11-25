package content

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

const (
	project = "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b10"
	commit  = "0123456789abcdef0123456789abcdef01234567"
)

type fakeArtifacts map[int]appartifact.View

func (f fakeArtifacts) Get(_ context.Context, _ identity.Principal, projectID, code string, version int) (appartifact.View, error) {
	view, ok := f[version]
	if !ok || projectID != project || code != view.Code {
		return appartifact.View{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return view, nil
}

type fakeFiles map[string]string // "<commit>:<path>" → content

func (f fakeFiles) ContentFile(_ context.Context, projectID, at, p string) ([]byte, error) {
	content, ok := f[at+":"+p]
	if !ok || projectID != project {
		return nil, ErrNoFile
	}
	return []byte(content), nil
}

func telas(prototype, at string) appartifact.View {
	header, _ := json.Marshal(map[string]any{"id": "TELAS", "prototipo": prototype})
	return appartifact.View{Summary: appartifact.Summary{Code: "TELAS", Type: "TELAS"}, Header: header, Commit: at}
}

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func newService(t *testing.T) (*Service, *clock) {
	t.Helper()
	artifacts := fakeArtifacts{
		1: telas("docs/design/prototipo/", commit),
		2: telas("docs/design/prototipo", ""),
		3: {Summary: appartifact.Summary{Code: "TELAS", Type: "TELAS"}, Header: json.RawMessage(`{"prototipo":"../etc/"}`), Commit: commit},
	}
	files := fakeFiles{
		commit + ":docs/design/prototipo/index.html":    "<h1>início</h1>",
		commit + ":docs/design/prototipo/guias.html":    "<h1>guias</h1>",
		commit + ":docs/design/prototipo/app.js":        "console.log(1)",
		commit + ":docs/design/prototipo/dados.bin":     "x",
		commit + ":docs/design/TELAS.md":                "---",
		commit + ":docs/design/prototipo/sub/tela.html": "<p>sub</p>",
	}
	c := &clock{at: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)}
	s := NewService(artifacts, files, "https://content.easycode.test/", DeriveKey("test-secret"))
	s.now = c.now
	return s, c
}

func tokenOf(t *testing.T, raw string) (string, string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	rest, ok := strings.CutPrefix(u.Path, "/c/")
	if !ok {
		t.Fatalf("not a content URL: %s", raw)
	}
	token, page, _ := strings.Cut(rest, "/")
	return token, page
}

func kindOf(err error) failure.Kind {
	var f *failure.Error
	if errors.As(err, &f) {
		return f.Kind
	}
	return failure.KindInternal
}

// ER-11-10: the URL is on the content origin, covers the prototype folder of the version and opens for 5 min.
func TestIssueSignsThePrototypeFolderOfTheVersion(t *testing.T) {
	s, c := newService(t)
	got, err := s.Issue(context.Background(), identity.Principal{}, project, "TELAS", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.URL, "https://content.easycode.test/c/") || !strings.HasSuffix(got.URL, "/prototipo/index.html") {
		t.Fatalf("url %s", got.URL)
	}
	if got.Prefix != "prototipo/" || !got.ExpiresAt.Equal(c.at.Add(OpenFor)) {
		t.Fatalf("prefix %q, expires %v", got.Prefix, got.ExpiresAt)
	}
	page, err := s.Issue(context.Background(), identity.Principal{}, project, "TELAS", 1, "prototipo/guias.html")
	if err != nil || !strings.HasSuffix(page.URL, "/prototipo/guias.html") {
		t.Fatalf("page url %v %v", page, err)
	}
}

func TestIssueRefusesWhatIsNotAPrototypePage(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	cases := []struct {
		name    string
		code    string
		version int
		page    string
		want    failure.Kind
	}{
		{"outside the prefix", "TELAS", 1, "TELAS.md", failure.KindInvalidInput},
		{"parent path", "TELAS", 1, "prototipo/../TELAS.md", failure.KindInvalidInput},
		{"absolute path", "TELAS", 1, "/etc/passwd", failure.KindInvalidInput},
		{"missing file (CA-11-5)", "TELAS", 1, "prototipo/detalhe.html", failure.KindNotFound},
		{"version without commit", "TELAS", 2, "", failure.KindNotFound},
		{"unsafe prototype folder", "TELAS", 3, "", failure.KindNotFound},
		{"no such version", "TELAS", 9, "", failure.KindNotFound},
		{"version zero", "TELAS", 0, "", failure.KindNotFound},
	}
	for _, tc := range cases {
		if _, err := s.Issue(ctx, identity.Principal{}, project, tc.code, tc.version, tc.page); kindOf(err) != tc.want {
			t.Errorf("%s: got %v", tc.name, err)
		}
	}
}

// CA-11-12: the relative links of the prototype load other pages of the prefix with the same token.
func TestOpenServesEveryPageUnderTheSignedPrefix(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	issued, _ := s.Issue(ctx, identity.Principal{}, project, "TELAS", 1, "")
	token, _ := tokenOf(t, issued.URL)
	for page, want := range map[string]string{
		"prototipo/index.html": "<h1>início</h1>", "prototipo/guias.html": "<h1>guias</h1>", "prototipo/": "<h1>início</h1>",
		"prototipo/sub/tela.html": "<p>sub</p>",
	} {
		got, err := s.Open(ctx, token, page)
		if err != nil || string(got.Body) != want || !strings.HasPrefix(got.MediaType, "text/html") {
			t.Errorf("%s: %q %v", page, got.Body, err)
		}
	}
	js, err := s.Open(ctx, token, "prototipo/app.js")
	if err != nil || js.MediaType != "text/javascript; charset=utf-8" {
		t.Fatalf("script: %v %v", js, err)
	}
	for _, page := range []string{"TELAS.md", "prototipo/../TELAS.md", "prototipo/dados.bin", "prototipo/ausente.html", ""} {
		if _, err := s.Open(ctx, token, page); !errors.Is(err, ErrNoFile) {
			t.Errorf("%s must not be served: %v", page, err)
		}
	}
}

func TestOpenRefusesATamperedToken(t *testing.T) {
	s, _ := newService(t)
	issued, _ := s.Issue(context.Background(), identity.Principal{}, project, "TELAS", 1, "")
	token, _ := tokenOf(t, issued.URL)
	payload, signature, _ := strings.Cut(token, ".")
	forged, _ := json.Marshal(grant{ProjectID: project, Commit: commit, Root: "", Prefix: "docs/", OpenBy: 1 << 40})
	for _, bad := range []string{"", "x", payload, payload + ".AAAA", strings.TrimRight(string(forged), "}") + "." + signature} {
		if _, err := s.Open(context.Background(), bad, "prototipo/index.html"); !errors.Is(err, ErrNoFile) {
			t.Errorf("token %q accepted", bad)
		}
	}
	other := NewService(fakeArtifacts{}, fakeFiles{}, "https://content.easycode.test", DeriveKey("other-secret"))
	if _, err := other.Open(context.Background(), token, "prototipo/index.html"); !errors.Is(err, ErrNoFile) {
		t.Fatal("a token signed with another key opened")
	}
}

// ADR-19 / ER-11-9: 5 min to open; once opened, a 30 min content session; an expired URL is never reused.
func TestOpenKeepsTheContentSessionAndExpiresTheURL(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	issued, _ := s.Issue(ctx, identity.Principal{}, project, "TELAS", 1, "")
	token, _ := tokenOf(t, issued.URL)
	if _, err := s.Open(ctx, token, "prototipo/index.html"); err != nil {
		t.Fatal(err)
	}
	c.at = c.at.Add(20 * time.Minute)
	if _, err := s.Open(ctx, token, "prototipo/guias.html"); err != nil {
		t.Fatalf("within the content session: %v", err)
	}
	c.at = c.at.Add(11 * time.Minute)
	if _, err := s.Open(ctx, token, "prototipo/guias.html"); !errors.Is(err, ErrNoFile) {
		t.Fatal("the content session must end after 30 min")
	}

	late, _ := s.Issue(ctx, identity.Principal{}, project, "TELAS", 1, "")
	lateToken, _ := tokenOf(t, late.URL)
	c.at = c.at.Add(OpenFor + time.Second)
	if _, err := s.Open(ctx, lateToken, "prototipo/index.html"); !errors.Is(err, ErrNoFile) {
		t.Fatal("a URL never opened in 5 min must not open")
	}
}

func TestDeriveKeyDependsOnTheSecret(t *testing.T) {
	if string(DeriveKey("a")) == string(DeriveKey("b")) || len(DeriveKey("a")) != 32 {
		t.Fatal("derived key")
	}
}
