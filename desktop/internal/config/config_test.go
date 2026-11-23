package config

import (
	"errors"
	"os"
	"testing"
)

func TestLoadWithoutFileGivesDefaults(t *testing.T) {
	store := NewStore(t.TempDir(), func(string) string { return "" })
	prefs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if prefs.Locale != LocalePtBR || prefs.Server != "" || prefs.MuteSound {
		t.Fatalf("unexpected defaults: %+v", prefs)
	}
}

func TestSaveAndLoadRoundTripWithPrivateFile(t *testing.T) {
	store := NewStore(t.TempDir()+"/nested", func(string) string { return "" })
	want := Preferences{Server: "https://easycode.example", Locale: LocaleEn, MuteSound: true}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("preferences file mode %v, want 0600", info.Mode().Perm())
	}
}

func TestServerPrefersTheEnvironment(t *testing.T) {
	env := map[string]string{ServerEnv: "https://env.example/"}
	store := NewStore(t.TempDir(), func(key string) string { return env[key] })
	server, fromEnv, err := store.Server(Preferences{Server: "https://stored.example"})
	if err != nil || !fromEnv || server != "https://env.example" {
		t.Fatalf("got %q %v %v", server, fromEnv, err)
	}
	delete(env, ServerEnv)
	server, fromEnv, err = store.Server(Preferences{Server: "https://stored.example"})
	if err != nil || fromEnv || server != "https://stored.example" {
		t.Fatalf("got %q %v %v", server, fromEnv, err)
	}
	server, _, err = store.Server(Preferences{})
	if err != nil || server != "" {
		t.Fatalf("no server expected, got %q %v", server, err)
	}
}

func TestNormalizeServer(t *testing.T) {
	cases := map[string]string{
		"https://easycode.example":       "https://easycode.example",
		" https://easycode.example/ ":    "https://easycode.example",
		"https://easycode.example/base/": "https://easycode.example/base",
		"http://localhost:8080":          "http://localhost:8080",
		"http://127.0.0.1:8080/":         "http://127.0.0.1:8080",
		"http://[::1]:8080":              "http://[::1]:8080",
	}
	for in, want := range cases {
		got, err := NormalizeServer(in)
		if err != nil || got != want {
			t.Errorf("NormalizeServer(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", "easycode.example", "ftp://easycode.example", "http://easycode.example",
		"https://user:pass@easycode.example", "https://easycode.example/?a=1", "https://easycode.example/#x",
	} {
		if _, err := NormalizeServer(bad); !errors.Is(err, ErrInvalidServer) {
			t.Errorf("NormalizeServer(%q) accepted", bad)
		}
	}
}

func TestNormalizeLocale(t *testing.T) {
	for in, want := range map[string]string{"": LocalePtBR, "pt-BR": LocalePtBR, "en": LocaleEn, "en-US": LocaleEn, "fr": LocalePtBR} {
		if got := NormalizeLocale(in); got != want {
			t.Errorf("NormalizeLocale(%q) = %q, want %q", in, got, want)
		}
	}
}
