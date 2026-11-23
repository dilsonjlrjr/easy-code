package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type fakeTokens struct {
	current string
	renews  int
	next    string
}

func (f *fakeTokens) AccessToken(context.Context) (string, error) { return f.current, nil }
func (f *fakeTokens) Renew(_ context.Context, stale string) (string, error) {
	f.renews++
	f.current = f.next
	return f.current, nil
}

func problem(w http.ResponseWriter, status int, kind string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": kind, "title": "t", "status": status})
}

func TestGetSendsBearerUserAgentAndLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/projects" || r.URL.Query().Get("limit") != "50" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer a1" || r.Header.Get("User-Agent") != "easycode-desktop/1.0" ||
			r.Header.Get("Accept-Language") != "en" {
			t.Errorf("headers %v", r.Header)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"p1","name":"Alpha"}],"next_cursor":null}`))
	}))
	defer server.Close()
	client := New(server.URL, &fakeTokens{current: "a1"}, "easycode-desktop/1.0", func() string { return "en" })
	var page ProjectsPage
	if err := client.GetInto(context.Background(), "/projects", url.Values{"limit": {"50"}}, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "Alpha" {
		t.Fatalf("page %+v", page)
	}
}

func TestGetRenewsOnceAfter401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a2" {
			problem(w, 401, ProblemSessionExpired)
			return
		}
		_, _ = w.Write([]byte(`{"id":"u1"}`))
	}))
	defer server.Close()
	tokens := &fakeTokens{current: "a1", next: "a2"}
	client := New(server.URL, tokens, "", nil)
	body, err := client.Get(context.Background(), "/me", nil)
	if err != nil || string(body) != `{"id":"u1"}` || tokens.renews != 1 {
		t.Fatalf("%s %v renews=%d", body, err, tokens.renews)
	}
}

func TestGetEndsSessionWhenStill401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem(w, 401, ProblemRevokedSession)
	}))
	defer server.Close()
	tokens := &fakeTokens{current: "a1", next: "a2"}
	client := New(server.URL, tokens, "", nil)
	if _, err := client.Get(context.Background(), "/me", nil); !errors.Is(err, ErrSessionEnded) {
		t.Fatalf("want ErrSessionEnded, got %v", err)
	}
	if tokens.renews != 0 {
		t.Fatal("revoked-session must not renew the token")
	}
}

func TestProblemDecidedByType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/session/logout" {
			if r.Method != http.MethodPost {
				t.Errorf("endSession method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/api/v1/plain" {
			w.WriteHeader(502)
			return
		}
		problem(w, 404, ProblemNotFound)
	}))
	defer server.Close()
	client := New(server.URL, &fakeTokens{current: "a1"}, "", nil)
	var p *Problem
	if _, err := client.Get(context.Background(), "/projects/x", nil); !errors.As(err, &p) || p.Type != ProblemNotFound || p.Status != 404 {
		t.Fatalf("want not-found problem, got %v", err)
	}
	if _, err := client.Get(context.Background(), "/plain", nil); !errors.As(err, &p) || p.Status != 502 {
		t.Fatalf("want 502 problem, got %v", err)
	}
	if err := client.EndSession(context.Background()); err != nil {
		t.Fatal(err)
	}
}
