package config_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/infrastructure/config"
)

func lookupFrom(values map[string]string) config.LookupFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := config.Load(lookupFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.Telemetry.Enabled {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if err := cfg.RequireDatabase(); err == nil {
		t.Fatal("expected missing database URL to be reported")
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	cfg, err := config.Load(lookupFrom(map[string]string{
		config.EnvLogLevel:     "debug",
		config.EnvHTTPAddr:     "127.0.0.1:9000",
		config.EnvDatabaseURL:  "postgres://app@db/easycode",
		config.EnvProbeTimeout: "500ms",
		config.EnvOTelEnabled:  "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != slog.LevelDebug || cfg.HTTP.Addr != "127.0.0.1:9000" || !cfg.Telemetry.Enabled {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Database.ProbeTimeout != 500*time.Millisecond {
		t.Fatalf("unexpected probe timeout %v", cfg.Database.ProbeTimeout)
	}
	if cfg.Database.MigrationURL != cfg.Database.URL {
		t.Fatal("migration URL must fall back to the database URL")
	}
}

func TestLoadReportsInvalidValues(t *testing.T) {
	_, err := config.Load(lookupFrom(map[string]string{
		config.EnvHTTPReadTimeout: "soon",
		config.EnvOTelSampleRatio: "2",
	}))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// The marco1 profile no longer exists (PLANO MARCO-2): its variables are refused, never ignored.
func TestMarco1ProfileIsRefused(t *testing.T) {
	for _, key := range []string{"EASYCODE_PROFILE", "EASYCODE_DEV_SESSION_SECRET", "EASYCODE_DEV_USER_SUB"} {
		if _, err := config.Load(lookupFrom(map[string]string{key: "marco1"})); err == nil {
			t.Errorf("%s must be refused", key)
		}
	}
}

// The api needs the realm and both secrets; a secret may come from a file (NAME_FILE).
func TestIdentityNeedsTheRealmAndReadsSecretFiles(t *testing.T) {
	cfg, err := config.Load(lookupFrom(map[string]string{}))
	if err != nil || cfg.RequireIdentity() == nil {
		t.Fatalf("identity without realm must be refused at serve: %v", err)
	}
	if _, err := config.Load(lookupFrom(map[string]string{config.EnvSessionSecret: "short"})); err == nil {
		t.Fatal("a short session secret must be refused")
	}
	file := filepath.Join(t.TempDir(), "client-secret")
	if err := os.WriteFile(file, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(lookupFrom(map[string]string{
		config.EnvPublicOrigin: "https://easycode.test/", config.EnvOIDCIssuer: "https://sso.test/realms/easycode/",
		config.EnvOIDCClientSecret + config.SecretFileSuffix: file, config.EnvSessionSecret: strings.Repeat("s", 32),
	}))
	if err != nil || cfg.RequireIdentity() != nil {
		t.Fatalf("complete identity: %v %v", err, cfg.RequireIdentity())
	}
	if cfg.Identity.ClientSecret != "from-file" || cfg.Identity.Issuer != "https://sso.test/realms/easycode" ||
		cfg.Identity.PublicOrigin != "https://easycode.test" || cfg.Identity.ClientID != "easycode-api" {
		t.Fatalf("identity: %+v", cfg.Identity)
	}
}
