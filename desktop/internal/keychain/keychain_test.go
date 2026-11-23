package keychain

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

type fakeBackend struct {
	entries map[string]string
	err     error
}

func (f *fakeBackend) Get(service, user string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	secret, ok := f.entries[service+"|"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return secret, nil
}

func (f *fakeBackend) Set(service, user, password string) error {
	if f.err != nil {
		return f.err
	}
	f.entries[service+"|"+user] = password
	return nil
}

func (f *fakeBackend) Delete(service, user string) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.entries[service+"|"+user]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.entries, service+"|"+user)
	return nil
}

func TestOSStoreKeepsOneTokenPerServer(t *testing.T) {
	backend := &fakeBackend{entries: map[string]string{}}
	first := &OS{account: "https://a.example", backend: backend}
	second := &OS{account: "https://b.example", backend: backend}

	if _, err := first.Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty keychain: %v", err)
	}
	if err := first.Set("refresh-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token leaked to another server: %v", err)
	}
	if got, err := first.Get(); err != nil || got != "refresh-a" {
		t.Fatalf("got %q %v", got, err)
	}
	if backend.entries[Service+"|https://a.example"] != "refresh-a" {
		t.Fatalf("entry not under the service and server account: %v", backend.entries)
	}
	if err := first.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := first.Delete(); err != nil {
		t.Fatalf("deleting a missing token must not fail: %v", err)
	}
}

func TestOSStoreReportsUnavailableKeychainWithoutFallback(t *testing.T) {
	backend := &fakeBackend{entries: map[string]string{}, err: errors.New("org.freedesktop.DBus.Error.ServiceUnknown")}
	store := &OS{account: "https://a.example", backend: backend}
	for name, err := range map[string]error{
		"set":    store.Set("refresh"),
		"delete": store.Delete(),
	} {
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: want ErrUnavailable, got %v", name, err)
		}
	}
	if _, err := store.Get(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("get: want ErrUnavailable, got %v", err)
	}
	if len(backend.entries) != 0 {
		t.Fatal("nothing may be stored when the keychain fails")
	}
}

func TestOSStoreWithGoKeyringMock(t *testing.T) {
	keyring.MockInit()
	store := NewOS("https://a.example")
	if err := store.Set("refresh"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(); err != nil || got != "refresh" {
		t.Fatalf("got %q %v", got, err)
	}
	keyring.MockInitWithError(errors.New("no secret service"))
	if err := store.Set("other"); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "other") {
		t.Fatalf("want ErrUnavailable without the secret, got %v", err)
	}
}

func TestMemoryStore(t *testing.T) {
	store := &Memory{}
	if _, err := store.Get(); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_ = store.Set("x")
	if got, _ := store.Get(); got != "x" {
		t.Fatal(got)
	}
	store.Unavailable = true
	if err := store.Set("y"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
