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
	EnvWorkerOutboxPoll     = "EASYCODE_WORKER_OUTBOX_POLL_INTERVAL"
	EnvGatewaySocket        = "EASYCODE_GATEWAY_SOCKET"
	EnvGatewayProviders     = "EASYCODE_GATEWAY_PROVIDERS"
	EnvGatewaySessionIdle   = "EASYCODE_GATEWAY_SESSION_IDLE"
	EnvGatewayPrivateKey    = "EASYCODE_GATEWAY_PRIVATE_KEY_FILE"
	EnvAnthropicBaseURL     = "EASYCODE_ANTHROPIC_BASE_URL"
	EnvAnthropicTestModel   = "EASYCODE_ANTHROPIC_TEST_MODEL"
	EnvProviderCallTimeout  = "EASYCODE_PROVIDER_CALL_TIMEOUT"
	EnvProfile              = "EASYCODE_PROFILE"
	EnvDevUserSubject       = "EASYCODE_DEV_USER_SUB"
	EnvDevUserName          = "EASYCODE_DEV_USER_NAME"
	EnvDevSessionSecret     = "EASYCODE_DEV_SESSION_SECRET"
	EnvPublicOrigin         = "EASYCODE_PUBLIC_ORIGIN"
	EnvSealingPublicKey     = "EASYCODE_SEALING_PUBLIC_KEY_FILE"
	EnvKeyTestTimeout       = "EASYCODE_PROVIDER_KEY_TEST_TIMEOUT"
	EnvAttachmentLimit      = "EASYCODE_ATTACHMENT_LIMIT_BYTES"
	EnvWorkerCommandPoll    = "EASYCODE_WORKER_COMMAND_POLL_INTERVAL"
	EnvWorkerLease          = "EASYCODE_WORKER_WAVE_LEASE"
	EnvWorkerCorrections    = "EASYCODE_WORKER_MAX_ARTIFACT_CORRECTIONS"
	EnvWorkerMaxTokens      = "EASYCODE_WORKER_MAX_OUTPUT_TOKENS"
	EnvWorkerID             = "EASYCODE_WORKER_ID"
)

// ProfileMarco1 is the development profile of Marco 1: fixed "dev" identity, no Keycloak (ARQ section 9).
const ProfileMarco1 = "marco1"

// LookupFunc reads one environment variable. os.LookupEnv satisfies it.
type LookupFunc func(key string) (string, bool)

// Config holds every setting a process needs.
type Config struct {
	LogLevel  slog.Level
	HTTP      HTTP
	Database  Database
	Telemetry Telemetry
	Worker    Worker
	Gateway   Gateway
	Identity  Identity
	Providers Providers
}

// Identity configures who may use the api. Until Keycloak (MARCO-2) only the fixed development
// identity of the marco1 profile exists.
type Identity struct {
	Profile string
	// DevUserSubject and DevUserName describe the fixed "dev" user (marco1 only).
	DevUserSubject string
	DevUserName    string
	// DevSessionSecret signs the development session cookie (marco1 only, at least 32 bytes).
	DevSessionSecret string
	// PublicOrigin is the web origin; writes with the session cookie must send it in Origin.
	PublicOrigin string
}

// DevIdentity reports whether the fixed development identity is enabled.
func (i Identity) DevIdentity() bool { return i.Profile == ProfileMarco1 }

// Providers configures provider keys and limits seen by the api and the worker.
type Providers struct {
	// SealingPublicKeyFile holds the platform public key the api seals keys with (ADR-13).
	SealingPublicKeyFile string
	// KeyTestTimeout bounds the key test (US-15: 15 s).
	KeyTestTimeout time.Duration
	// AttachmentLimitBytes is the maximum attachment size (getCurrentUser.attachment_limit_bytes).
	AttachmentLimitBytes int
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
	// OutboxPollInterval is the fallback poll of the outbox publisher; a NOTIFY wakes it earlier.
	OutboxPollInterval time.Duration
	// CommandPollInterval is the fallback poll of the command queue; a NOTIFY wakes it earlier.
	CommandPollInterval time.Duration
	// WaveLease is the lease of a running wave; a dead worker's wave resumes after it (RNF-12).
	WaveLease time.Duration
	// MaxArtifactCorrections is how many rejected deliveries a wave gets before failing (US-29 CA-2).
	MaxArtifactCorrections int
	// MaxOutputTokens bounds each model answer.
	MaxOutputTokens int
	// ID names this worker in leases and checkpoints ("" = host name).
	ID string
}

// Gateway configures easycode-gateway (interno.md section 3).
type Gateway struct {
	// Socket is the Unix socket shared only with the worker.
	Socket string
	// Providers lists the enabled provider adapters, comma separated ("fake" until Onda 1).
	Providers []string
	// SessionIdle closes a wave session after this inactivity.
	SessionIdle time.Duration
	// PrivateKeyFile holds the platform private key that opens sealed provider keys (ADR-13).
	PrivateKeyFile string
	// AnthropicBaseURL is the Anthropic API endpoint (egress proxy or test server).
	AnthropicBaseURL string
	// AnthropicTestModel is the model id of the minimal key test call (configured, never fixed in code).
	AnthropicTestModel string
	// CallTimeout bounds one provider HTTP call.
	CallTimeout time.Duration
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
		Worker: Worker{
			HeartbeatInterval:      r.duration(EnvWorkerHeartbeat, 30*time.Second),
			OutboxPollInterval:     r.duration(EnvWorkerOutboxPoll, 5*time.Second),
			CommandPollInterval:    r.duration(EnvWorkerCommandPoll, 2*time.Second),
			WaveLease:              r.duration(EnvWorkerLease, 30*time.Second),
			MaxArtifactCorrections: r.integer(EnvWorkerCorrections, 3),
			MaxOutputTokens:        r.integer(EnvWorkerMaxTokens, 16000),
			ID:                     r.text(EnvWorkerID, ""),
		},
		Gateway: Gateway{
			Socket:             r.text(EnvGatewaySocket, "/run/easycode-gateway/gateway.sock"),
			Providers:          r.list(EnvGatewayProviders),
			SessionIdle:        r.duration(EnvGatewaySessionIdle, 15*time.Minute),
			PrivateKeyFile:     r.text(EnvGatewayPrivateKey, ""),
			AnthropicBaseURL:   r.text(EnvAnthropicBaseURL, "https://api.anthropic.com"),
			AnthropicTestModel: r.text(EnvAnthropicTestModel, ""),
			CallTimeout:        r.duration(EnvProviderCallTimeout, 10*time.Minute),
		},
		Identity: Identity{
			Profile:          r.text(EnvProfile, ""),
			DevUserSubject:   r.text(EnvDevUserSubject, "dev"),
			DevUserName:      r.text(EnvDevUserName, "Dev"),
			DevSessionSecret: r.text(EnvDevSessionSecret, ""),
			PublicOrigin:     r.text(EnvPublicOrigin, ""),
		},
		Providers: Providers{
			SealingPublicKeyFile: r.text(EnvSealingPublicKey, ""),
			KeyTestTimeout:       r.duration(EnvKeyTestTimeout, 15*time.Second),
			AttachmentLimitBytes: r.integer(EnvAttachmentLimit, 10<<20),
		},
	}
	r.checkIdentity(cfg.Identity)
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

// checkIdentity refuses the development identity outside the marco1 profile (ADR-12).
func (r *reader) checkIdentity(identity Identity) {
	switch {
	case identity.Profile != "" && identity.Profile != ProfileMarco1:
		r.fail(EnvProfile, identity.Profile, errors.New("unknown profile (available: marco1)"))
	case !identity.DevIdentity() && identity.DevSessionSecret != "":
		r.problems = append(r.problems, fmt.Errorf("%s is only accepted with %s=%s", EnvDevSessionSecret, EnvProfile, ProfileMarco1))
	case identity.DevIdentity() && len(identity.DevSessionSecret) < 32:
		r.problems = append(r.problems, fmt.Errorf("%s must have at least 32 characters in the %s profile", EnvDevSessionSecret, ProfileMarco1))
	}
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

func (r *reader) list(key string) []string {
	value, ok := r.raw(key)
	if !ok {
		return nil
	}
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
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
