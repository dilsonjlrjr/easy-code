package main

import (
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/infrastructure/config"
)

func TestInstallBridgeCopiesTheBinaryToTheSharedVolume(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "agent-bridge")
	if err := os.WriteFile(source, []byte("#!bridge"), 0o600); err != nil {
		t.Fatal(err)
	}
	volume := filepath.Join(dir, "volume")
	for range 2 { // a restart replaces the copy
		if err := installBridge(source, volume); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(volume, "bin", "agent-bridge"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("bridge: %v %v", info, err)
	}
	entries, _ := os.ReadDir(filepath.Join(volume, "bin"))
	if len(entries) != 1 {
		t.Fatalf("temporary files left: %v", entries)
	}
	if err := installBridge(filepath.Join(dir, "missing"), volume); err == nil {
		t.Fatal("a missing bridge must fail")
	}
}

func TestRunnerConfigTurnsOnAgentSessions(t *testing.T) {
	runner := runnerConfig(config.Sandbox{Image: "go", ClaudeCodeBin: "/usr/local/bin/claude", AgentProxyVolume: "easycode-agent-proxy"})
	if runner.AgentSession.ClaudeCodeBin != "/usr/local/bin/claude" || runner.AgentSession.ProxyVolume != "easycode-agent-proxy" || runner.AgentSession.DetectImage != "go" {
		t.Fatalf("agent session: %+v", runner.AgentSession)
	}
}
