// Package config loads process settings from environment variables (RNF-17: no fixed addresses).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Environment variable names.
//
//nolint:gosec // G101: these are variable names, not credentials.
const (
	EnvLogLevel             = "EASYCODE_LOG_LEVEL"
	EnvHTTPAddr             = "EASYCODE_HTTP_ADDR"
	EnvHTTPReadTimeout      = "EASYCODE_HTTP_READ_TIMEOUT"
	EnvHTTPWriteTimeout     = "EASYCODE_HTTP_WRITE_TIMEOUT"
	EnvHTTPIdleTimeout      = "EASYCODE_HTTP_IDLE_TIMEOUT"
	EnvShutdownTimeout      = "EASYCODE_SHUTDOWN_TIMEOUT"
	EnvDatabaseURL          = "EASYCODE_DATABASE_URL"
	EnvMigrationDatabaseURL = "EASYCODE_MIGRATION_DATABASE_URL"
	EnvDatabaseMaxConns     = "EASYCODE_DATABASE_MAX_CONNS"
	EnvProbeTimeout         = "EASYCODE_PROBE_TIMEOUT"
	EnvOTelEnabled          = "EASYCODE_OTEL_ENABLED"
	EnvOTelEndpoint         = "EASYCODE_OTEL_ENDPOINT"
	EnvOTelInsecure         = "EASYCODE_OTEL_INSECURE"
	EnvOTelSampleRatio      = "EASYCODE_OTEL_SAMPLE_RATIO"
	EnvWorkerHeartbeat      = "EASYCODE_WORKER_HEARTBEAT_INTERVAL"
)

// LookupFunc reads one environment variable. os.LookupEnv satisfies it.
type LookupFunc func(key string) (string, bool)

// Config holds every setting a process needs.
type Config struct {
	LogLevel  slog.Level
	HTTP      HTTP
	Database  Database
	Telemetry Telemetry
	Worker    Worker
}

// HTTP configures the api listener.
type HTTP struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

// Database configures PostgreSQL access.
type Database struct {
	URL          string
	MigrationURL string
	MaxConns     int32
	ProbeTimeout time.Duration
}

// Telemetry configures OpenTelemetry export (ADR-15). Disabled by default.
type Telemetry struct {
	Enabled     bool
	Endpoint    string
	Insecure    bool
	SampleRatio float64
}

// Worker configures the background worker loop.
type Worker struct {
	HeartbeatInterval time.Duration
}

// Load reads the configuration. It reports every invalid variable at once.
func Load(lookup LookupFunc) (Config, error) {
	r := reader{lookup: lookup}
	cfg := Config{
		LogLevel: r.level(EnvLogLevel, slog.LevelInfo),
		HTTP: HTTP{
			Addr:            r.text(EnvHTTPAddr, ":8080"),
			ReadTimeout:     r.duration(EnvHTTPReadTimeout, 15*time.Second),
			WriteTimeout:    r.duration(EnvHTTPWriteTimeout, 30*time.Second),
			IdleTimeout:     r.duration(EnvHTTPIdleTimeout, 60*time.Second),
			ShutdownTimeout: r.duration(EnvShutdownTimeout, 20*time.Second),
		},
		Database: Database{
			URL:          r.text(EnvDatabaseURL, ""),
			MaxConns:     int32(r.integer(EnvDatabaseMaxConns, 10)),
			ProbeTimeout: r.duration(EnvProbeTimeout, 2*time.Second),
		},
		Telemetry: Telemetry{
			Enabled:     r.boolean(EnvOTelEnabled, false),
			Endpoint:    r.text(EnvOTelEndpoint, ""),
			Insecure:    r.boolean(EnvOTelInsecure, false),
			SampleRatio: r.ratio(EnvOTelSampleRatio, 1.0),
		},
		Worker: Worker{HeartbeatInterval: r.duration(EnvWorkerHeartbeat, 30*time.Second)},
	}
	cfg.Database.MigrationURL = r.text(EnvMigrationDatabaseURL, cfg.Database.URL)
	return cfg, errors.Join(r.problems...)
}

// RequireDatabase fails when the processes that need PostgreSQL have no URL.
func (c Config) RequireDatabase() error {
	if c.Database.URL == "" {
		return fmt.Errorf("%s is required", EnvDatabaseURL)
	}
	return nil
}

type reader struct {
	lookup   LookupFunc
	problems []error
}

func (r *reader) raw(key string) (string, bool) {
	value, ok := r.lookup(key)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func (r *reader) fail(key, value string, err error) {
	r.problems = append(r.problems, fmt.Errorf("%s=%q: %w", key, value, err))
}

func (r *reader) text(key, fallback string) string {
	if value, ok := r.raw(key); ok {
		return value
	}
	return fallback
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err == nil && parsed <= 0 {
		err = errors.New("must be positive")
	}
	if err != nil {
		r.fail(key, value, err)
		return fallback
	}
	return parsed
}

func (r *reader) integer(key string, fallback int) int {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err == nil && parsed <= 0 {
		err = errors.New("must be positive")
	}
	if err != nil {
		r.fail(key, value, err)
		return fallback
	}
	return int(parsed)
}

func (r *reader) boolean(key string, fallback bool) bool {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		r.fail(key, value, err)
		return fallback
	}
	return parsed
}

func (r *reader) ratio(key string, fallback float64) float64 {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err == nil && (parsed < 0 || parsed > 1) {
		err = errors.New("must be between 0 and 1")
	}
	if err != nil {
		r.fail(key, value, err)
		return fallback
	}
	return parsed
}

func (r *reader) level(key string, fallback slog.Level) slog.Level {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(value)); err != nil {
		r.fail(key, value, err)
		return fallback
	}
	return parsed
}
