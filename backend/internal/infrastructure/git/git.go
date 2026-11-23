// Package git keeps the git repository of each project (US-42, D-17, ARQ section 9: volume repos):
// a bare repository per project, written only by the worker (one commit per completed wave, with the
// artifacts in docs/ and the code in app/) and read by the api (clone over smart HTTP, last commit).
// It drives the git command line with a minimal environment; nothing of the host configuration or
// of the process environment reaches git.
package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"easycode/internal/application/gate"
	apprepository "easycode/internal/application/repository"
	"easycode/internal/application/runtime"
)

// MainBranch is the branch of every project repository.
const MainBranch = "main"

const maxBaseFileBytes = 512 << 10

// ErrNoRepository means the project has no commit yet.
var ErrNoRepository = apprepository.ErrEmpty

// Repositories are the project repositories under one directory (<root>/<project uuid>.git).
type Repositories struct {
	root string
	now  func() time.Time
}

// New builds the adapter. root is the directory of the repos volume.
func New(root string) Repositories { return Repositories{root: root, now: time.Now} }

// Path returns the repository directory of a project; the id must be a UUID (no path from outside).
func (r Repositories) Path(projectID string) (string, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return "", fmt.Errorf("project id: %w", err)
	}
	return filepath.Join(r.root, id.String()+".git"), nil
}

func (r Repositories) command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	all := append([]string{"-c", "safe.directory=*", "-c", "core.hooksPath=/dev/null", "-c", "protocol.allow=never"}, args...)
	cmd := exec.CommandContext(ctx, "git", all...) //nolint:gosec // G204: fixed git subcommands built here; paths come from Path (UUID only)
	cmd.Dir = dir
	home := os.TempDir()
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LANG=C"}
	return cmd
}

func (r Repositories) run(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := r.command(ctx, dir, args...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (r Repositories) hasHead(ctx context.Context, dir string) bool {
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	_, err := r.run(ctx, dir, nil, "rev-parse", "--verify", "-q", "refs/heads/"+MainBranch)
	return err == nil
}

func (r Repositories) ensure(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	_, err := r.run(ctx, dir, nil, "init", "-q", "--bare", "-b", MainBranch, ".")
	return err
}

// Files implements runtime.Repository: the text files of the main branch (the base of a wave).
func (r Repositories) Files(ctx context.Context, projectID string) (map[string]string, error) {
	dir, err := r.Path(projectID)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	if !r.hasHead(ctx, dir) {
		return files, nil
	}
	listing, err := r.run(ctx, dir, nil, "ls-tree", "-r", "-z", "--full-tree", "-l", "refs/heads/"+MainBranch)
	if err != nil {
		return nil, err
	}
	var shas, paths []string
	for _, entry := range strings.Split(string(listing), "\x00") {
		meta, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 || fields[1] != "blob" || fields[0] != "100644" {
			continue
		}
		if size, err := strconv.Atoi(fields[3]); err != nil || size > maxBaseFileBytes {
			continue
		}
		shas, paths = append(shas, fields[2]), append(paths, name)
	}
	if len(shas) == 0 {
		return files, nil
	}
	out, err := r.run(ctx, dir, strings.NewReader(strings.Join(shas, "\n")+"\n"), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(bytes.NewReader(out))
	for _, name := range paths {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file: unexpected header %q", header)
		}
		size, _ := strconv.Atoi(fields[2])
		content := make([]byte, size+1)
		if _, err := io.ReadFull(reader, content); err != nil {
			return nil, err
		}
		if utf8.Valid(content[:size]) {
			files[name] = string(content[:size])
		}
	}
	return files, nil
}

var trailerWave = regexp.MustCompile(`(?m)^Easycode-Wave: (\S+)$`)

// Commit implements runtime.Repository: one commit on main with the files of the wave. Repeating the
// commit of the same wave (the transaction that records it failed) returns the existing commit.
func (r Repositories) Commit(ctx context.Context, projectID string, c runtime.Commit) (string, error) {
	dir, err := r.Path(projectID)
	if err != nil {
		return "", err
	}
	if err := r.ensure(ctx, dir); err != nil {
		return "", err
	}
	if r.hasHead(ctx, dir) {
		if head, err := r.Head(ctx, projectID); err == nil && head.Wave == c.Wave {
			return head.SHA, nil
		}
	}
	return r.fastImport(ctx, dir, c)
}

// fastImport writes one commit on main with the files (the parent is the current main, if any).
func (r Repositories) fastImport(ctx context.Context, dir string, c runtime.Commit) (string, error) {
	parent := r.hasHead(ctx, dir)
	paths := make([]string, 0, len(c.Files))
	for p := range c.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var stream bytes.Buffer
	when := r.now().Unix()
	identity := fmt.Sprintf("%s <%s@agents.easycode.local> %d +0000", c.Agent, c.Agent, when)
	fmt.Fprintf(&stream, "commit refs/heads/%s\nauthor %s\ncommitter Easy Code <orchestrator@agents.easycode.local> %d +0000\ndata %d\n%s\n",
		MainBranch, identity, when, len(c.Message), c.Message)
	if parent {
		fmt.Fprintf(&stream, "from refs/heads/%s^0\n", MainBranch)
	}
	for _, p := range paths {
		if strings.ContainsAny(p, "\n\"") || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			return "", fmt.Errorf("invalid path in commit: %q", p)
		}
		fmt.Fprintf(&stream, "M 100644 inline %s\ndata %d\n", p, len(c.Files[p]))
		stream.Write(c.Files[p])
		stream.WriteByte('\n')
	}
	stream.WriteString("done\n")
	if _, err := r.run(ctx, dir, &stream, "fast-import", "--quiet", "--done"); err != nil {
		return "", err
	}
	sha, err := r.run(ctx, dir, nil, "rev-parse", "refs/heads/"+MainBranch)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(sha)), nil
}

// HeadCommit is the last commit of main.
type HeadCommit = apprepository.Commit

var _ apprepository.Git = Repositories{}

// Head returns the last commit of main, or ErrNoRepository.
func (r Repositories) Head(ctx context.Context, projectID string) (HeadCommit, error) {
	dir, err := r.Path(projectID)
	if err != nil {
		return HeadCommit{}, err
	}
	if !r.hasHead(ctx, dir) {
		return HeadCommit{}, ErrNoRepository
	}
	out, err := r.run(ctx, dir, nil, "log", "-1", "--format=%H%x00%cI%x00%B", "refs/heads/"+MainBranch)
	if err != nil {
		return HeadCommit{}, err
	}
	parts := strings.SplitN(string(out), "\x00", 3)
	if len(parts) != 3 {
		return HeadCommit{}, fmt.Errorf("git log: unexpected output")
	}
	when, _ := time.Parse(time.RFC3339, parts[1])
	head := HeadCommit{SHA: parts[0], When: when}
	if m := trailerWave.FindStringSubmatch(parts[2]); m != nil {
		head.Wave = m[1]
	}
	return head, nil
}

// Exists reports whether the project repository has a commit.
func (r Repositories) Exists(ctx context.Context, projectID string) bool {
	dir, err := r.Path(projectID)
	return err == nil && r.hasHead(ctx, dir)
}

// UploadPack serves git-upload-pack in stateless RPC mode (smart HTTP, read-only): with advertise it
// writes the reference advertisement, else it answers the negotiation read from in.
func (r Repositories) UploadPack(ctx context.Context, projectID string, advertise bool, in io.Reader, out io.Writer) error {
	dir, err := r.Path(projectID)
	if err != nil {
		return err
	}
	if !r.hasHead(ctx, dir) {
		return ErrNoRepository
	}
	args := []string{"upload-pack", "--stateless-rpc", "--strict"}
	if advertise {
		args = append(args, "--advertise-refs")
	}
	cmd := r.command(ctx, dir, append(args, ".")...)
	cmd.Stdin, cmd.Stdout = in, out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git upload-pack: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// shaPattern is a full commit id (SHA-1 or SHA-256).
var shaPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// Reachable reports whether a recorded commit is still an ancestor of main (project.history_preserved,
// t.dev_record_present): history rewritten or a commit unknown to the repository is false.
func (r Repositories) Reachable(ctx context.Context, projectID, sha string) (bool, error) {
	if !shaPattern.MatchString(sha) {
		return false, nil
	}
	dir, err := r.Path(projectID)
	if err != nil {
		return false, err
	}
	if !r.hasHead(ctx, dir) {
		return false, nil
	}
	cmd := r.command(ctx, dir, "merge-base", "--is-ancestor", sha, "refs/heads/"+MainBranch)
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return false, nil // 1: not an ancestor; 128: unknown commit
		}
		return false, err
	}
	return true, nil
}

// tagPattern is the name of a release mark (gate.ReleaseTag: lower case id of the RELEASE).
var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// HeadCommit implements gate.ReleaseRepository: the last commit of main, "" when none.
func (r Repositories) HeadCommit(ctx context.Context, projectID string) (string, error) {
	head, err := r.Head(ctx, projectID)
	if errors.Is(err, ErrNoRepository) {
		return "", nil
	}
	return head.SHA, err
}

// IsAncestor implements gate.ReleaseRepository (traceability.release_commit_contains_work).
func (r Repositories) IsAncestor(ctx context.Context, projectID, sha, commit string) (bool, error) {
	if !shaPattern.MatchString(sha) || !shaPattern.MatchString(commit) {
		return false, nil
	}
	dir, err := r.Path(projectID)
	if err != nil {
		return false, err
	}
	if !r.hasHead(ctx, dir) {
		return false, nil
	}
	if err := r.command(ctx, dir, "merge-base", "--is-ancestor", sha, commit).Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Mark implements gate.ReleaseRepository (US-08 CA-3): the approved RELEASE is committed on main and an
// annotated tag marks that commit. Repeating it returns the commit of the existing tag; a commit of the mark
// left without its tag (the worker fell between both) is reused.
func (r Repositories) Mark(ctx context.Context, projectID string, mark gate.ReleaseMark) (string, error) {
	if !tagPattern.MatchString(mark.Tag) {
		return "", fmt.Errorf("invalid release tag %q", mark.Tag)
	}
	dir, err := r.Path(projectID)
	if err != nil {
		return "", err
	}
	if !r.hasHead(ctx, dir) {
		return "", ErrNoRepository
	}
	if out, err := r.run(ctx, dir, nil, "rev-parse", "-q", "--verify", "refs/tags/"+mark.Tag+"^{commit}"); err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	sha := ""
	if out, err := r.run(ctx, dir, nil, "log", "-1", "--format=%H%x00%B", "refs/heads/"+MainBranch); err == nil {
		if head, body, ok := strings.Cut(string(out), "\x00"); ok && strings.Contains(body, "Easycode-Tag: "+mark.Tag+"\n") {
			sha = head
		}
	}
	if sha == "" {
		if sha, err = r.fastImport(ctx, dir, runtime.Commit{Agent: "orchestrator", Message: mark.Message, Files: map[string][]byte{mark.Path: mark.Content}}); err != nil {
			return "", err
		}
	}
	if _, err := r.run(ctx, dir, nil, "-c", "user.name=Easy Code", "-c", "user.email=orchestrator@agents.easycode.local",
		"tag", "-a", mark.Tag, "-m", "Release "+mark.Tag, sha); err != nil {
		return "", err
	}
	return sha, nil
}

// Marks implements gate.ReleaseRepository: the tags of the repository with their commit (getRepository.marks).
func (r Repositories) Marks(ctx context.Context, projectID string) ([]gate.Mark, error) {
	dir, err := r.Path(projectID)
	if err != nil {
		return nil, err
	}
	if !r.hasHead(ctx, dir) {
		return []gate.Mark{}, nil
	}
	out, err := r.run(ctx, dir, nil, "for-each-ref", "--sort=creatordate",
		"--format=%(refname:short)%00%(*objectname)%00%(objectname)%00%(creatordate:iso-strict)", "refs/tags")
	if err != nil {
		return nil, err
	}
	marks := []gate.Mark{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) != 4 {
			continue
		}
		commit := parts[1]
		if commit == "" {
			commit = parts[2]
		}
		when, _ := time.Parse(time.RFC3339, parts[3])
		marks = append(marks, gate.Mark{Name: parts[0], Commit: commit, When: when})
	}
	return marks, nil
}
