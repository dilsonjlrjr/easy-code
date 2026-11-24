package installer_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"

	"easycode/internal/application/install"
	"easycode/internal/interfaces/installer"
)

type fakeUseCases struct {
	installed bool
	applied   *install.Plan
}

func (f *fakeUseCases) State() install.Status { return install.Status{Installed: f.installed} }
func (f *fakeUseCases) CheckDatabase(context.Context, install.Database) (install.DatabaseProbe, error) {
	return install.DatabaseProbe{Exists: true, HasPlatformTables: true}, install.Invalids{{Field: "database.name", Reason: "has_platform_tables"}}
}
func (f *fakeUseCases) CheckOrganization(install.Organization) error { return nil }
func (f *fakeUseCases) CheckSSO(context.Context, install.SSO, install.Organization) (install.SSOProbe, error) {
	return install.SSOProbe{}, nil
}
func (f *fakeUseCases) SearchUsers(context.Context, install.SSO, string) ([]install.DirectoryUser, error) {
	return nil, nil
}
func (f *fakeUseCases) CheckPerson(context.Context, install.SSO, install.Person, string) error {
	return nil
}
func (f *fakeUseCases) NativeSkills() ([]install.SkillSummary, error) { return nil, nil }
func (f *fakeUseCases) CheckSkill(install.SkillPackage) (install.SkillSummary, error) {
	return install.SkillSummary{}, nil
}
func (f *fakeUseCases) Apply(_ context.Context, plan install.Plan) error {
	if f.installed {
		return install.ErrInstalled
	}
	f.applied = &plan
	return nil
}
func (f *fakeUseCases) Retry(context.Context) error { return nil }

func serve(t *testing.T, uc *fakeUseCases, webDir string) *http.Client {
	t.Helper()
	ln := fasthttputil.NewInmemoryListener()
	server := &fasthttp.Server{Handler: installer.NewHandler(installer.Settings{
		UseCases: uc, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Token: "the-token", Version: "test", WebDir: webDir,
	}), MaxRequestBodySize: installer.MaxBodyBytes}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close() })
	return &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return ln.Dial() }}}
}

func call(t *testing.T, client *http.Client, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://installer"+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set(installer.TokenHeader, token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestEveryRouteButStateNeedsTheToken(t *testing.T) {
	client := serve(t, &fakeUseCases{}, "")
	if status, body := call(t, client, "GET", "/api/state", "", ""); status != 200 || body["installed"] != false {
		t.Fatalf("state = %d %v", status, body)
	}
	for _, path := range []string{"/api/token", "/api/check/database", "/api/install"} {
		if status, body := call(t, client, "POST", path, "wrong", "{}"); status != 401 || body["code"] != "invalid_token" {
			t.Fatalf("%s = %d %v", path, status, body)
		}
	}
	if status, _ := call(t, client, "POST", "/api/token", "the-token", ""); status != 204 {
		t.Fatalf("token = %d", status)
	}
}

func TestFailedCheckReturnsTheFieldsAndWhatItFound(t *testing.T) {
	client := serve(t, &fakeUseCases{}, "")
	status, body := call(t, client, "POST", "/api/check/database", "the-token", `{"database":{}}`)
	if status != 422 {
		t.Fatalf("status = %d", status)
	}
	errs := body["errors"].([]any)
	if errs[0].(map[string]any)["reason"] != "has_platform_tables" || body["found"].(map[string]any)["exists"] != true {
		t.Fatalf("body = %v", body)
	}
}

func TestInstalledRefusesWrites(t *testing.T) {
	uc := &fakeUseCases{installed: true}
	client := serve(t, uc, "")
	if status, body := call(t, client, "POST", "/api/install", "the-token", `{"plan":{}}`); status != 409 || body["code"] != "installed" {
		t.Fatalf("install = %d %v", status, body)
	}
	if status, _ := call(t, client, "POST", "/api/install", "the-token", `not json`); status != 400 {
		t.Fatalf("bad json = %d", status)
	}
}

func TestStaticPageWithFallbackAndHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "installer.html"), []byte("<html>wizard</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := serve(t, &fakeUseCases{}, dir)
	for _, path := range []string{"/", "/some/step"} {
		resp, err := client.Get("http://installer" + path)
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(content), "wizard") {
			t.Fatalf("%s = %d %q", path, resp.StatusCode, content)
		}
		if resp.Header.Get("X-Frame-Options") != "DENY" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
			t.Fatalf("%s headers = %v", path, resp.Header)
		}
	}
}
