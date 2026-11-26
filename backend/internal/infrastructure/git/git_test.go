package git_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/application/runtime"
	"easycode/internal/infrastructure/git"
)

const project = "3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14"

func TestCommitsAreReadBackAndIdempotentPerWave(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	repos := git.New(t.TempDir())
	if files, err := repos.Files(ctx, project); err != nil || len(files) != 0 {
		t.Fatalf("empty repository: %v %v", files, err)
	}
	first, err := repos.Commit(ctx, project, runtime.Commit{Wave: "REQ-01", Agent: "requirements-agent", Message: "REQ-01: requisitos\n\nEasycode-Wave: REQ-01\n",
		Files: map[string][]byte{"docs/requisitos/VISAO.md": []byte("---\nid: VISAO\n---\n# Visão\n")}})
	if err != nil || len(first) != 40 {
		t.Fatalf("first commit: %q %v", first, err)
	}
	again, err := repos.Commit(ctx, project, runtime.Commit{Wave: "REQ-01", Agent: "requirements-agent", Message: "REQ-01\n\nEasycode-Wave: REQ-01\n",
		Files: map[string][]byte{"docs/requisitos/VISAO.md": []byte("x")}})
	if err != nil || again != first {
		t.Fatalf("same wave commits once: %s %s %v", first, again, err)
	}
	second, err := repos.Commit(ctx, project, runtime.Commit{Wave: "DEV-01", Agent: "developer-agent", Message: "T-01: implementação\n\nEasycode-Wave: DEV-01\nEasycode-Task: T-01\n",
		Files: map[string][]byte{"app/main.go": []byte("package main\n")}})
	if err != nil || second == first {
		t.Fatalf("second commit: %v", err)
	}
	files, err := repos.Files(ctx, project)
	if err != nil || files["docs/requisitos/VISAO.md"] == "" || files["app/main.go"] != "package main\n" {
		t.Fatalf("files keep the history of the earlier waves: %v %v", files, err)
	}
	head, err := repos.Head(ctx, project)
	if err != nil || head.SHA != second || head.Wave != "DEV-01" || head.When.IsZero() {
		t.Fatalf("head: %+v %v", head, err)
	}
	var advert bytes.Buffer
	if err := repos.UploadPack(ctx, project, true, strings.NewReader(""), &advert); err != nil || !strings.Contains(advert.String(), "refs/heads/main") {
		t.Fatalf("advertisement: %v %q", err, advert.String())
	}
	if _, err := repos.Path("../../etc"); err == nil {
		t.Fatal("project id must be a UUID")
	}
}

// A repositories root the worker cannot write fails Prepare with a permission error (the wave fails
// before the agent runs, instead of at the commit).
func TestPrepareCreatesTheRepositoryAndReportsAnUnwritableRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	repos := git.New(t.TempDir())
	if err := repos.Prepare(ctx, project); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	dir, _ := repos.Path(project)
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		t.Fatalf("prepare creates the bare repository: %v", err)
	}
	if err := repos.Prepare(ctx, project); err != nil {
		t.Fatalf("prepare is idempotent: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if err := git.New(root).Prepare(ctx, project); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("unwritable root must report a permission error, got %v", err)
	}
}
