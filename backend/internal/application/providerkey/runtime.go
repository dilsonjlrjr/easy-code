package providerkey

import (
	"context"
	"strings"
)

// RuntimeProbe asks easycode-sandbox whether agent sessions can run (GET /v1/claude-code).
type RuntimeProbe interface {
	ClaudeCode(ctx context.Context) (available bool, version string, err error)
}

// RuntimeStore records the detection in claude_code_runtime (worker role).
type RuntimeStore interface {
	RecordClaudeCodeRuntime(ctx context.Context, detected bool, version string) error
}

// maxVersionLength bounds the version kept (claude_code_runtime_version_ck).
const maxVersionLength = 128

// SyncRuntime is the sync_claude_code_runtime job (US-58 CA-1, CA-2): it records what the
// runner reports. A runner that does not answer means agent sessions cannot run, so it records
// "not detected" and still returns the probe error for the job log.
func SyncRuntime(ctx context.Context, probe RuntimeProbe, store RuntimeStore) (Runtime, error) {
	available, version, probeErr := probe.ClaudeCode(ctx)
	if probeErr != nil {
		available, version = false, ""
	}
	version = strings.TrimSpace(version)
	if !available {
		version = ""
	}
	if len(version) > maxVersionLength {
		version = version[:maxVersionLength]
	}
	if err := store.RecordClaudeCodeRuntime(ctx, available, version); err != nil {
		return Runtime{}, err
	}
	return Runtime{Detected: available, Version: version}, probeErr
}

// Usable reports whether a provider may be chosen in a model pair: claude-code needs the
// binary detected and an active key (US-58 CA-2); the other providers are always usable here
// (a wave without key fails with provider-without-key, US-15 CA-4).
func Usable(providerName string, runtime Runtime, hasActiveKey bool) bool {
	if providerName != ProviderClaudeCode {
		return true
	}
	return runtime.Detected && hasActiveKey
}
