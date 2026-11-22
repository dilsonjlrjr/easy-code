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
	parent := r.hasHead(ctx, dir)
	if parent {
		if head, err := r.Head(ctx, projectID); err == nil && head.Wave == c.Wave {
			return head.SHA, nil
		}
	}
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
