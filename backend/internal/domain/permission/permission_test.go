package permission

import (
	"testing"
	"time"
)

func TestGlobsOfPermissions(t *testing.T) {
	cases := []struct {
		glob, name string
		ok         bool
	}{
		{"docs/requisitos/**", "docs/requisitos/US-01.md", true},
		{"docs/requisitos/**", "docs/requisitos/sub/x.md", true},
		{"docs/requisitos/**", "docs/arquitetura/ARQ.md", false},
		{"docs/arquitetura/tarefas/T-*.md", "docs/arquitetura/tarefas/T-01.md", true},
		{"docs/arquitetura/tarefas/T-*.md", "docs/arquitetura/tarefas/sub/T-01.md", false},
		{"app/**", "app/main.go", true},
		{"app/**", "application/main.go", false},
		{"docs/qa/plano.md", "docs/qa/plano.md", true},
	}
	for _, c := range cases {
		if got := Match(c.glob, c.name); got != c.ok {
			t.Errorf("%s ~ %s: %v", c.glob, c.name, got)
		}
	}
}

func TestCleanPathRefusesEscapes(t *testing.T) {
	for raw, ok := range map[string]bool{"docs/a.md": true, "/docs/a.md": true, "../escape.md": false, "docs/../../x": false, "a\\b": false, "": false, ".": false} {
		if _, got := CleanPath(raw); got != ok {
			t.Errorf("%q: %v", raw, got)
		}
	}
}

// RNF-07: the tools come from the explicit lists; without execute_command the tool is not offered.
func TestPolicyToolsFollowTheLists(t *testing.T) {
	requirements := Policy{Agent: "requirements-agent", ReadPaths: []string{"docs/requisitos/**"}, WritePaths: []string{"docs/requisitos/**"}, AskUser: true}
	if requirements.Allows(ToolExecuteCommand) || !requirements.Allows(ToolWriteFile) || !requirements.Allows(ToolFinish) {
		t.Fatalf("requirements tools: %v", requirements.Tools())
	}
	developer := Policy{Execute: true, Categories: []string{"build"}, WritePaths: []string{"app/**"}}
	if !developer.Allows(ToolExecuteCommand) || developer.Allows(ToolAskUser) {
		t.Fatalf("developer tools: %v", developer.Tools())
	}
	if _, ok := developer.Category("tests"); ok {
		t.Fatal("category outside the list")
	}
}

func TestISODurations(t *testing.T) {
	for iso, want := range map[string]time.Duration{"PT2M": 2 * time.Minute, "PT1H30M": 90 * time.Minute, "PT45S": 45 * time.Second, "P1D": 0} {
		if got := ParseDuration(iso); got != want {
			t.Errorf("%s: %v", iso, got)
		}
	}
}
