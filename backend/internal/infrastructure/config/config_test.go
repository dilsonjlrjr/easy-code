package config_test

import (
	"log/slog"
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
