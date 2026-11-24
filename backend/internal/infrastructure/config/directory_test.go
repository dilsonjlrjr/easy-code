package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/infrastructure/config"
)

func writeDirectory(t *testing.T, values map[string]string, secrets map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	content, err := config.FormatFile("written by the test", values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.FileName), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, config.SecretsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range secrets {
		if err := os.WriteFile(filepath.Join(dir, config.SecretsDir, name), []byte(value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDirectoryFeedsTheConfigurationAndTheEnvironmentWins(t *testing.T) {
	dir := writeDirectory(t, map[string]string{
		config.EnvOIDCIssuer:                                 "https://sso.example/auth/realms/easycode",
		config.EnvOrganizationName:                           "Empresa",
		config.EnvHTTPAddr:                                   ":9000",
		config.EnvDatabaseURL + config.SecretFileSuffix:      "secrets/database-url",
		config.EnvSessionSecret + config.SecretFileSuffix:    "secrets/session-secret",
		config.EnvOIDCClientSecret + config.SecretFileSuffix: "secrets/oidc-client-secret",
	}, map[string]string{
		"database-url":       "postgres://easycode_api_login:pw@db/easycode",
		"session-secret":     strings.Repeat("s", 40),
		"oidc-client-secret": "client-secret",
	})
	lookup, err := config.Environment(lookupFrom(map[string]string{
		config.EnvConfigDir: dir,
		config.EnvHTTPAddr:  ":7000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != ":7000" {
		t.Fatalf("environment should win over the file, got %q", cfg.HTTP.Addr)
	}
	if cfg.Identity.Issuer != "https://sso.example/auth/realms/easycode" || cfg.Identity.OrganizationName != "Empresa" {
		t.Fatalf("file values not read: %+v", cfg.Identity)
	}
	if cfg.Database.URL != "postgres://easycode_api_login:pw@db/easycode" || cfg.Database.MigrationURL != cfg.Database.URL {
		t.Fatalf("database URL secret file not read: %q / %q", cfg.Database.URL, cfg.Database.MigrationURL)
	}
	if cfg.Identity.ClientSecret != "client-secret" || len(cfg.Identity.SessionSecret) != 40 {
		t.Fatal("secret files relative to the directory not read")
	}
}

func TestDirectoryWithoutTheVariableIsIgnored(t *testing.T) {
	lookup, err := config.Environment(lookupFrom(map[string]string{config.EnvHTTPAddr: ":7000"}))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := lookup(config.EnvHTTPAddr); value != ":7000" {
		t.Fatalf("got %q", value)
	}
}

func TestDirectoryMissingFileIsAnError(t *testing.T) {
	if _, err := config.Environment(lookupFrom(map[string]string{config.EnvConfigDir: t.TempDir()})); err == nil {
		t.Fatal("expected an error for a directory without the file")
	}
}

func TestParseFileRefusesLinesWithoutKey(t *testing.T) {
	if _, err := config.ParseFile([]byte("# comment\n\nVALID=1\n=nokey\n")); err == nil {
		t.Fatal("expected an error")
	}
	values, err := config.ParseFile([]byte("# comment\nA=1=2\n B = x \n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != "1=2" || values["B"] != "x" {
		t.Fatalf("got %v", values)
	}
}

func TestFormatFileRefusesLineBreaks(t *testing.T) {
	if _, err := config.FormatFile("h", map[string]string{"A": "x\ny"}); err == nil {
		t.Fatal("expected an error")
	}
}
