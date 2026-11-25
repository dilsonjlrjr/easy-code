package git

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"easycode/internal/application/content"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

var _ content.Files = Repositories{}

// ContentFile implements content.Files: one file of the repository at a commit, for the content origin
// (ADR-19). A missing repository, commit or path, a path that is not a regular file and a file above
// content.MaxFileBytes are content.ErrNoFile.
func (r Repositories) ContentFile(ctx context.Context, projectID, commit, path string) ([]byte, error) {
	dir, err := r.Path(projectID)
	if err != nil || !commitPattern.MatchString(commit) || path == "" || strings.ContainsAny(path, "\x00\r\n") {
		return nil, content.ErrNoFile
	}
	if !r.hasHead(ctx, dir) {
		return nil, content.ErrNoFile
	}
	object := commit + ":" + path
	check, err := r.run(ctx, dir, strings.NewReader(object+"\n"), "cat-file", "--batch-check=%(objecttype) %(objectsize)")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(check))
	if len(fields) != 2 || fields[0] != "blob" {
		return nil, content.ErrNoFile
	}
	size, err := strconv.Atoi(fields[1])
	if err != nil || size > content.MaxFileBytes {
		return nil, content.ErrNoFile
	}
	body, err := r.run(ctx, dir, nil, "cat-file", "blob", object)
	if err != nil {
		return nil, fmt.Errorf("read content file: %w", err)
	}
	return body, nil
}
