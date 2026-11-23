// Package config keeps the few local preferences of easycode-desktop: the server address, the interface
// language and whether notifications play a sound. It never holds a token or any wave state (US-46 RN-1):
// the refresh token lives only in the OS keychain (package keychain) and every view is read from the server.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ServerEnv overrides the stored server address (cliente-local.md section 1, RNF-17).
const ServerEnv = "EASYCODE_SERVER"

// Supported interface languages; pt-BR is the default (RNF-21).
const (
	LocalePtBR = "pt-BR"
	LocaleEn   = "en"
)

const fileName = "desktop.json"

// ErrInvalidServer is returned for an address that is not an absolute http(s) URL, or that uses plain http
// for a host other than the loopback (the token would travel in clear text).
var ErrInvalidServer = errors.New("invalid server address")

// Preferences is what the app writes to the user configuration folder.
type Preferences struct {
	Server    string `json:"server,omitempty"`
	Locale    string `json:"locale,omitempty"`
	MuteSound bool   `json:"mute_sound,omitempty"`
}

// Store reads and writes Preferences in one JSON file of a folder.
type Store struct {
	dir    string
	getenv func(string) string
}

// DefaultDir is the per-user configuration folder: XDG on Linux, Application Support on macOS and
// %APPDATA%\easycode on Windows (os.UserConfigDir covers the three).
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(base, "easycode"), nil
}

// NewStore returns a store over dir; getenv is os.Getenv outside tests.
func NewStore(dir string, getenv func(string) string) *Store {
	if getenv == nil {
		getenv = os.Getenv
	}
	return &Store{dir: dir, getenv: getenv}
}

// Path is the file the preferences are written to.
func (s *Store) Path() string { return filepath.Join(s.dir, fileName) }

// Load reads the preferences; a missing file gives the defaults.
func (s *Store) Load() (Preferences, error) {
	raw, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return Preferences{Locale: LocalePtBR}, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("read preferences: %w", err)
	}
	var prefs Preferences
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return Preferences{}, fmt.Errorf("parse preferences: %w", err)
	}
	prefs.Locale = NormalizeLocale(prefs.Locale)
	return prefs, nil
}

// Save writes the preferences atomically, readable only by the user.
func (s *Store) Save(prefs Preferences) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	raw, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, fileName+".*")
	if err != nil {
		return fmt.Errorf("write preferences: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write preferences: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write preferences: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write preferences: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.Path()); err != nil {
		return fmt.Errorf("write preferences: %w", err)
	}
	return nil
}

// Server is the address in use: EASYCODE_SERVER when set, otherwise the stored one. The second result
// says whether it came from the environment (the sign-in view then does not ask for it).
func (s *Store) Server(prefs Preferences) (string, bool, error) {
	if env := strings.TrimSpace(s.getenv(ServerEnv)); env != "" {
		server, err := NormalizeServer(env)
		return server, true, err
	}
	if prefs.Server == "" {
		return "", false, nil
	}
	server, err := NormalizeServer(prefs.Server)
	return server, false, err
}

// NormalizeServer validates an address typed by the user and returns it without a trailing slash.
// https is required, except for the loopback, where the development stack runs on plain http.
func NormalizeServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrInvalidServer
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !IsLoopback(parsed.Hostname()) {
			return "", ErrInvalidServer
		}
	default:
		return "", ErrInvalidServer
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

// IsLoopback says whether host names this machine.
func IsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NormalizeLocale maps any value to a supported language, pt-BR by default.
func NormalizeLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		return LocaleEn
	}
	return LocalePtBR
}
