// Package config loads process settings from environment variables (RNF-17: no fixed addresses).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
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
	EnvOpenAIBaseURL        = "EASYCODE_OPENAI_BASE_URL"
	EnvProviderCallTimeout  = "EASYCODE_PROVIDER_CALL_TIMEOUT"
	EnvPublicOrigin         = "EASYCODE_PUBLIC_ORIGIN"
	EnvOIDCIssuer           = "EASYCODE_OIDC_ISSUER"
	EnvOIDCInternalURL      = "EASYCODE_OIDC_INTERNAL_URL"
	EnvOIDCClientID         = "EASYCODE_OIDC_CLIENT_ID"
	EnvOIDCClientSecret     = "EASYCODE_OIDC_CLIENT_SECRET"
	EnvOIDCAdminRole        = "EASYCODE_OIDC_ADMINISTRATOR_ROLE"
	EnvSessionSecret        = "EASYCODE_SESSION_SECRET"
	EnvOrganizationName     = "EASYCODE_ORGANIZATION_NAME"
	EnvEventsClientID       = "EASYCODE_SSO_EVENTS_CLIENT_ID"
	EnvEventsClientSecret   = "EASYCODE_SSO_EVENTS_CLIENT_SECRET"
	EnvWorkerSSOPoll        = "EASYCODE_WORKER_SSO_POLL_INTERVAL"
	EnvSealingPublicKey     = "EASYCODE_SEALING_PUBLIC_KEY_FILE"
	EnvKeyTestTimeout       = "EASYCODE_PROVIDER_KEY_TEST_TIMEOUT"
	EnvAttachmentLimit      = "EASYCODE_ATTACHMENT_LIMIT_BYTES"
	EnvWorkerCommandPoll    = "EASYCODE_WORKER_COMMAND_POLL_INTERVAL"
	EnvWorkerLease          = "EASYCODE_WORKER_WAVE_LEASE"
	EnvWorkerCorrections    = "EASYCODE_WORKER_MAX_ARTIFACT_CORRECTIONS"
	EnvWorkerMaxTokens      = "EASYCODE_WORKER_MAX_OUTPUT_TOKENS"
	EnvWorkerID             = "EASYCODE_WORKER_ID"
	EnvReposDir             = "EASYCODE_REPOS_DIR"
	EnvGitBaseURL           = "EASYCODE_GIT_BASE_URL"
	EnvSandboxSocket        = "EASYCODE_SANDBOX_SOCKET"
	EnvSandboxImage         = "EASYCODE_SANDBOX_IMAGE"
	EnvSandboxImages        = "EASYCODE_SANDBOX_IMAGES"
	EnvSandboxRuntime       = "EASYCODE_SANDBOX_RUNTIME"
	EnvDockerSocket         = "EASYCODE_DOCKER_SOCKET"
	EnvSandboxDepsNetwork   = "EASYCODE_SANDBOX_DEPENDENCIES_NETWORK"
	EnvSandboxDepsProxy     = "EASYCODE_SANDBOX_DEPENDENCIES_PROXY"
	EnvProjectCPULimit      = "EASYCODE_PROJECT_CPU_LIMIT"
	EnvProjectMemoryLimit   = "EASYCODE_PROJECT_MEMORY_LIMIT_MIB"
	EnvProjectDiskLimit     = "EASYCODE_PROJECT_DISK_LIMIT_MIB"
	EnvProjectCommandTime   = "EASYCODE_PROJECT_COMMAND_TIME"
)

// removedVariables belonged to the marco1 development profile (fixed "dev" identity), which no
// longer exists since MARCO-2: setting them is a configuration error, never a silent fallback.
var removedVariables = []string{"EASYCODE_PROFILE", "EASYCODE_DEV_USER_SUB", "EASYCODE_DEV_USER_NAME", "EASYCODE_DEV_SESSION_SECRET"}

// SecretFileSuffix marks the file form of a secret: a secret variable may instead name a file with the value (NAME_FILE), the
// production form (ARQ section 8.1: secret files mounted in the service).
const SecretFileSuffix = "_FILE"

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
	Sandbox   Sandbox
	// ReposDir is the directory of the project git repositories (volume repos, US-42).
	ReposDir string
	// GitBaseURL is the public base of the clone URLs (Repository.clone_url); "" = PublicOrigin.
	GitBaseURL string
}

// Sandbox configures easycode-sandbox and its use by the worker (ADR-05, ADR-06, RNF-03).
type Sandbox struct {
	// Socket is the Unix socket shared only by the worker and the runner.
	Socket string
	// Image is the logical image of the project stack the worker asks for (Phase 1: Go).
	Image string
	// Images is the allowlist of the runner: logical id → local image reference ("go=easycode-sandbox-go:dev").
	Images map[string]string
	// Runtime is the OCI runtime of the containers (runc in Phase 1, runsc later).
	Runtime string
	// DockerSocket is the daemon socket, only mounted in the runner.
	DockerSocket string
	// DependenciesNetwork is the internal network of the dependency action, whose only exit is the deps-proxy (ADR-07).
	DependenciesNetwork string
	// DependenciesProxy is the deps-proxy URL the package managers use in the dependency action.
	DependenciesProxy string
	// Project limits of RNF-03 (2 vCPU, 4 GiB, 10 GiB, 30 min by default).
	CPU         float64
	MemoryMiB   int
	DiskMiB     int
	CommandTime time.Duration
}

// Identity configures the SSO (Keycloak realm, ADR-12) and the web session.
type Identity struct {
	// PublicOrigin is the web origin; writes with the session cookie must send it in Origin and
	// the SSO returns to PublicOrigin + /api/v1/session/oidc/callback.
	PublicOrigin string
	// Issuer is the public issuer of the realm; InternalURL reaches the same realm from the
	// containers ("" = Issuer).
	Issuer       string
	InternalURL  string
	ClientID     string
	ClientSecret string
	// AdministratorRole is the realm role of the organization administrator.
	AdministratorRole string
	// SessionSecret derives the keys of the login flow cookie and of the sealed refresh token.
	SessionSecret string
	// OrganizationName names the organization of the issuer, created at migrate when missing.
	OrganizationName string
	// EventsClientID and EventsClientSecret are the read-only service account of the worker
	// (login events PL-36 and offboarding D-31).
	EventsClientID     string
	EventsClientSecret string
}

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
	// SSOPollInterval is how often the worker reads the login events and the offboarded people of
	// the realm (interno.md section 7).
	SSOPollInterval time.Duration
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
	// OpenAIBaseURL is the OpenAI API endpoint (egress proxy or test server; US-24).
	OpenAIBaseURL string
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
			SSOPollInterval:        r.duration(EnvWorkerSSOPoll, 60*time.Second),
		},
		Gateway: Gateway{
			Socket:             r.text(EnvGatewaySocket, "/run/easycode-gateway/gateway.sock"),
			Providers:          r.list(EnvGatewayProviders),
			SessionIdle:        r.duration(EnvGatewaySessionIdle, 15*time.Minute),
			PrivateKeyFile:     r.text(EnvGatewayPrivateKey, ""),
			AnthropicBaseURL:   r.text(EnvAnthropicBaseURL, "https://api.anthropic.com"),
			AnthropicTestModel: r.text(EnvAnthropicTestModel, ""),
			OpenAIBaseURL:      r.text(EnvOpenAIBaseURL, "https://api.openai.com"),
			CallTimeout:        r.duration(EnvProviderCallTimeout, 10*time.Minute),
		},
		Identity: Identity{
			PublicOrigin:       strings.TrimRight(r.text(EnvPublicOrigin, ""), "/"),
			Issuer:             strings.TrimRight(r.text(EnvOIDCIssuer, ""), "/"),
			InternalURL:        strings.TrimRight(r.text(EnvOIDCInternalURL, ""), "/"),
			ClientID:           r.text(EnvOIDCClientID, "easycode-api"),
			ClientSecret:       r.secret(EnvOIDCClientSecret),
			AdministratorRole:  r.text(EnvOIDCAdminRole, "easycode-administrator"),
			SessionSecret:      r.secret(EnvSessionSecret),
			OrganizationName:   r.text(EnvOrganizationName, ""),
			EventsClientID:     r.text(EnvEventsClientID, ""),
			EventsClientSecret: r.secret(EnvEventsClientSecret),
		},
		Providers: Providers{
			SealingPublicKeyFile: r.text(EnvSealingPublicKey, ""),
			KeyTestTimeout:       r.duration(EnvKeyTestTimeout, 15*time.Second),
			AttachmentLimitBytes: r.integer(EnvAttachmentLimit, 10<<20),
		},
	}
	cfg.ReposDir = r.text(EnvReposDir, "/var/lib/easycode/repos")
	cfg.GitBaseURL = strings.TrimRight(r.text(EnvGitBaseURL, cfg.Identity.PublicOrigin), "/")
	cfg.Sandbox = Sandbox{
		Socket: r.text(EnvSandboxSocket, "/run/easycode-sandbox/runner.sock"), Image: r.text(EnvSandboxImage, "go"),
		Images: map[string]string{}, Runtime: r.text(EnvSandboxRuntime, "runc"), DockerSocket: r.text(EnvDockerSocket, "/var/run/docker.sock"),
		DependenciesNetwork: r.text(EnvSandboxDepsNetwork, ""), DependenciesProxy: r.text(EnvSandboxDepsProxy, ""),
		CPU: float64(r.integer(EnvProjectCPULimit, 2)), MemoryMiB: r.integer(EnvProjectMemoryLimit, 4096),
		DiskMiB: r.integer(EnvProjectDiskLimit, 10240), CommandTime: r.duration(EnvProjectCommandTime, 30*time.Minute),
	}
	for _, item := range r.list(EnvSandboxImages) {
		logical, ref, ok := strings.Cut(item, "=")
		if !ok || logical == "" || ref == "" {
			r.fail(EnvSandboxImages, item, errors.New("want logical=image"))
			continue
		}
		cfg.Sandbox.Images[logical] = ref
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

// checkIdentity refuses the variables of the removed marco1 profile and a short session key.
func (r *reader) checkIdentity(identity Identity) {
	for _, key := range removedVariables {
		if _, set := r.raw(key); set {
			r.problems = append(r.problems, fmt.Errorf("%s belongs to the removed marco1 profile: sign in through the SSO (EASYCODE_OIDC_*)", key))
		}
	}
	if identity.SessionSecret != "" && len(identity.SessionSecret) < 32 {
		r.problems = append(r.problems, fmt.Errorf("%s must have at least 32 characters", EnvSessionSecret))
	}
}

// RequireIdentity checks what the api needs to sign people in (serve only).
func (c Config) RequireIdentity() error {
	var problems []error
	for key, value := range map[string]string{
		EnvPublicOrigin: c.Identity.PublicOrigin, EnvOIDCIssuer: c.Identity.Issuer,
		EnvOIDCClientSecret: c.Identity.ClientSecret, EnvSessionSecret: c.Identity.SessionSecret,
	} {
		if value == "" {
			problems = append(problems, fmt.Errorf("%s is required", key))
		}
	}
	return errors.Join(problems...)
}

// secret reads a secret from NAME or, in production, from the file named by NAME_FILE.
func (r *reader) secret(key string) string {
	if path, ok := r.raw(key + SecretFileSuffix); ok {
		content, err := os.ReadFile(path) //nolint:gosec // the path is deployment configuration (mounted secret file), not user input
		if err != nil {
			r.problems = append(r.problems, fmt.Errorf("%s%s: cannot read the file", key, SecretFileSuffix))
			return ""
		}
		return strings.TrimSpace(string(content))
	}
	return r.text(key, "")
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
