package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The test binary is also the child of "exec": with BRIDGE_TEST_CHILD set it calls the bridge address,
// prints the answer and exits with code 3.
func TestMain(m *testing.M) {
	if address := os.Getenv("BRIDGE_TEST_CHILD"); address != "" {
		os.Exit(child(address))
	}
	os.Exit(m.Run())
}

func child(address string) int {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+address+"/v1/messages?beta=true", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("error:", err)
		return 1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("%d %s", resp.StatusCode, body)
	return 3
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_ = l.Close()
	return address
}

func TestExecForwardsToTheSocketAndReturnsTheExitCode(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "model.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var seen string
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = r.Method + " " + r.URL.RequestURI() + " " + string(body)
		_, _ = io.WriteString(w, "proxied")
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	address := freeAddress(t)
	t.Setenv("BRIDGE_TEST_CHILD", address)
	var stdout, stderr bytes.Buffer
	code := run([]string{"exec", "--listen", address, "--socket", socket, "--", os.Args[0]}, nil, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("exit code: want 3, got %d (%q, %q)", code, stdout.String(), stderr.String())
	}
	if stdout.String() != "200 proxied" || seen != `POST /v1/messages?beta=true {"model":"m"}` {
		t.Fatalf("forward: child %q, server saw %q", stdout.String(), seen)
	}
}

func TestExecRefusesBadUsage(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"exec", "--listen", "127.0.0.1:0"}, nil, io.Discard, &stderr); code != 2 {
		t.Fatalf("no socket and no command: %d", code)
	}
	if code := run([]string{"exec", "--listen", "127.0.0.1:0", "--socket", "/nonexistent.sock", "--", "/nonexistent/claude"}, nil, io.Discard, &stderr); code != 127 {
		t.Fatalf("missing command: %d", code)
	}
	if code := run([]string{"nope"}, nil, io.Discard, &stderr); code != 2 {
		t.Fatalf("unknown command: %d", code)
	}
}

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	if code := run([]string{"version"}, nil, &stdout, io.Discard); code != 0 || strings.TrimSpace(stdout.String()) != version {
		t.Fatalf("version: %d %q", code, stdout.String())
	}
}

func TestGuard(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"docs", "src", ".easycode", "outside"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "docs", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/target", filepath.Join(root, "docs", "dangling")); err != nil {
		t.Fatal(err)
	}
	denials := filepath.Join(root, ".easycode", "denials.jsonl")
	writable := filepath.Join(root, "docs") + ":" + filepath.Join(root, "README.md")
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"no path", `{"tool_name":"Bash","tool_input":{"command":"ls"}}`, 0},
		{"inside", `{"tool_name":"Write","tool_input":{"file_path":"` + root + `/docs/a/b.md"}}`, 0},
		{"relative inside", `{"tool_name":"Edit","tool_input":{"file_path":"docs/x.md"}}`, 0},
		{"the file itself", `{"tool_name":"Write","tool_input":{"file_path":"` + root + `/README.md"}}`, 0},
		{"notebook inside", `{"tool_name":"NotebookEdit","tool_input":{"notebook_path":"docs/n.ipynb"}}`, 0},
		{"outside", `{"tool_name":"Write","tool_input":{"file_path":"` + root + `/src/main.go"}}`, guardRefused},
		{"sibling prefix", `{"tool_name":"Write","tool_input":{"file_path":"` + root + `/docsevil/x"}}`, guardRefused},
		{"dot dot", `{"tool_name":"Write","tool_input":{"file_path":"` + root + `/docs/../docs/x.md"}}`, guardRefused},
		{"symlink out", `{"tool_name":"Write","tool_input":{"file_path":"` + root + `/docs/escape/x.md"}}`, guardRefused},
		{"dangling link", `{"tool_name":"Write","tool_input":{"file_path":"` + root + `/docs/dangling"}}`, guardRefused},
		{"bad input", `not json`, guardRefused},
	}
	for _, c := range cases {
		var stderr bytes.Buffer
		if got := guard(strings.NewReader(c.input), &stderr, writable, root, denials); got != c.want {
			t.Errorf("%s: want %d, got %d (%q)", c.name, c.want, got, stderr.String())
		}
		if c.want == guardRefused && c.name == "outside" && !strings.Contains(stderr.String(), "easycode: write to "+root+"/src/main.go is not allowed for this agent") {
			t.Errorf("message: %q", stderr.String())
		}
	}
	raw, err := os.ReadFile(denials)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 5 || lines[0] != `{"tool":"Write","path":"`+root+`/src/main.go","reason":"path_not_allowed"}` {
		t.Fatalf("denials: %q", raw)
	}
	if got := guard(strings.NewReader(`{"tool_name":"Write","tool_input":{"file_path":"/x"}}`), io.Discard, "", root, denials); got != guardRefused {
		t.Fatalf("no writable path: %d", got)
	}
}
