// Package keychain keeps the refresh token of easycode-desktop in the OS keychain: Secret Service over D-Bus
// on Linux, Keychain on macOS, Credential Manager on Windows (cliente-local.md section 1, ADR-12). There is
// no file fallback: without a keychain the sign-in fails with a message (ESP-28 ER-28-1).
package keychain

import (
	"errors"
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

// Service is the keychain entry name; the account is the server address, so two servers never share a token.
const Service = "easycode-desktop"

var (
	// ErrNotFound means there is no stored token (the user is signed out).
	ErrNotFound = errors.New("no token in the keychain")
	// ErrUnavailable means the OS keychain cannot be used; nothing was written anywhere else.
	ErrUnavailable = errors.New("OS keychain unavailable")
)

// Store holds one secret.
type Store interface {
	Get() (string, error)
	Set(secret string) error
	Delete() error
}

// backend is the part of go-keyring the adapter uses (replaced in tests).
type backend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type goKeyring struct{}

func (goKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (goKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (goKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

// OS is the Store over the operating system keychain for one server.
type OS struct {
	account string
	backend backend
}

// NewOS returns the keychain store for the given server address.
func NewOS(server string) *OS { return &OS{account: server, backend: goKeyring{}} }

// Get returns the stored token, ErrNotFound or ErrUnavailable.
func (k *OS) Get() (string, error) {
	secret, err := k.backend.Get(Service, k.account)
	if err != nil {
		return "", classify(err)
	}
	return secret, nil
}

// Set stores the token or returns ErrUnavailable.
func (k *OS) Set(secret string) error {
	if err := k.backend.Set(Service, k.account, secret); err != nil {
		return classify(err)
	}
	return nil
}

// Delete removes the token; a missing one is not an error.
func (k *OS) Delete() error {
	err := k.backend.Delete(Service, k.account)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return classify(err)
}

func classify(err error) error {
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	// Any other failure (no D-Bus session, no Secret Service, locked collection the user refused to open,
	// unsupported platform) is an unavailable keychain. The cause text never carries the secret.
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

// Memory is an in-memory Store for tests of the packages that use the keychain.
type Memory struct {
	mu          sync.Mutex
	secret      string
	present     bool
	Unavailable bool
}

// Get implements Store.
func (m *Memory) Get() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Unavailable {
		return "", ErrUnavailable
	}
	if !m.present {
		return "", ErrNotFound
	}
	return m.secret, nil
}

// Set implements Store.
func (m *Memory) Set(secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Unavailable {
		return ErrUnavailable
	}
	m.secret, m.present = secret, true
	return nil
}

// Delete implements Store.
func (m *Memory) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Unavailable {
		return ErrUnavailable
	}
	m.secret, m.present = "", false
	return nil
}
