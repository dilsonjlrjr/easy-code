// Package files keeps platform files in the files volume (ARQ section 9: evidence and attachments): the
// content of the attachments of the business need (US-03), under paths the platform generates.
package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Store is the files volume under one root directory.
type Store struct {
	root string
}

// New builds the store. root is the directory of the files volume.
func New(root string) Store { return Store{root: root} }

// pathPattern is the only shape of path the platform writes (attachments/<project>/<attachment>).
var pathPattern = regexp.MustCompile(`^attachments/[0-9a-f-]{36}/[0-9a-f-]{36}$`)

func (s Store) resolve(path string) (string, error) {
	if !pathPattern.MatchString(path) {
		return "", fmt.Errorf("files: invalid path %q", path)
	}
	return filepath.Join(s.root, filepath.FromSlash(path)), nil
}

// Write stores the content (a new file: an existing one is never replaced).
func (s Store) Write(_ context.Context, path string, content []byte) error {
	full, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("files: %w", err)
	}
	file, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640) //nolint:gosec // G304: path built from the fixed pattern above
	if err != nil {
		return fmt.Errorf("files: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("files: %w", err)
	}
	return file.Close()
}

// Read returns the content of a stored file.
func (s Store) Read(_ context.Context, path string) ([]byte, error) {
	full, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full) //nolint:gosec // G304: path built from the fixed pattern above
}

// Remove deletes a stored file; a missing file is not an error.
func (s Store) Remove(_ context.Context, path string) error {
	full, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("files: %w", err)
	}
	return nil
}
