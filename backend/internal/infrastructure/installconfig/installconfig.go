// Package installconfig writes the configuration volume of easycode-installer (ADR-22): easycode.env
// with the settings without secrets, one file per secret under secrets/, and the lock installed.json.
// The processes read the same directory through EASYCODE_CONFIG_DIR (config.Environment).
package installconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"easycode/internal/application/install"
	"easycode/internal/infrastructure/config"
)

// LockFile is the lock of the installation (US-55 RN-1). Removing it, and the row of
// platform_installation, is the only way to run the wizard again.
const LockFile = "installed.json"

// Store implements install.ConfigStore over a directory.
type Store struct {
	dir string
}

var _ install.ConfigStore = Store{}

// New builds the store; the directory must exist and be writable.
func New(dir string) Store { return Store{dir: dir} }

// Writable checks the directory before the wizard starts.
func (s Store) Writable() error {
	probe := filepath.Join(s.dir, ".write-test")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return fmt.Errorf("configuration directory %s is not writable", s.dir)
	}
	return os.Remove(probe)
}

// Installed implements install.ConfigStore.
func (s Store) Installed() (bool, error) {
	_, err := os.Stat(filepath.Join(s.dir, LockFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Write implements install.ConfigStore: every file is replaced atomically.
func (s Store) Write(settings install.Settings) error {
	secrets := filepath.Join(s.dir, config.SecretsDir)
	if err := os.MkdirAll(secrets, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", config.SecretsDir, err)
	}
	for name, value := range settings.Secrets {
		if filepath.Base(name) != name {
			return fmt.Errorf("invalid secret name %q", name)
		}
		if err := writeFile(filepath.Join(secrets, name), []byte(value+"\n")); err != nil {
			return err
		}
	}
	content, err := config.FormatFile("Written by easycode-installer (ADR-22). Secrets live in secrets/, one file each.\n"+
		"A variable set in the environment of a service wins over this file.", settings.Values)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.dir, config.FileName), content)
}

// Lock implements install.ConfigStore.
func (s Store) Lock(installation install.Installation) error {
	content, err := json.MarshalIndent(installation, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.dir, LockFile), append(content, '\n'))
}

func writeFile(path string, content []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}
