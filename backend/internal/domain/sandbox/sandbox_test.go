package sandbox

import (
	"testing"
	"time"
)

func TestCategoryAllowlistChecksArgv0AndPatterns(t *testing.T) {
	build := Category{Argv0: []string{"go", "npm"}, Shell: true}
	deps := Category{Argv0: []string{"go", "npm"}, Defaults: []string{"mod download*", "ci --ignore-scripts*"}}
	cases := []struct {
		name     string
		category Category
		argv     []string
		ok       bool
	}{
		{"argv0 in the list", build, []string{"go", "build", "./..."}, true},
		{"argv0 outside the list", build, []string{"curl", "http://postgres:5432"}, false},
		{"argv0 with a path", build, []string{"/usr/bin/go", "build"}, false},
		{"shell where allowed", build, []string{"sh", "-c", "go vet ./... && go build ./..."}, true},
		{"shell without -c", build, []string{"sh", "script.sh"}, false},
		{"shell where not allowed", deps, []string{"sh", "-c", "go mod download"}, false},
		{"pattern matches", deps, []string{"go", "mod", "download", "-x"}, true},
		{"pattern does not match", deps, []string{"go", "run", "./evil"}, false},
		{"npm without ignore-scripts", deps, []string{"npm", "ci"}, false},
		{"empty", build, nil, false},
	}
	for _, c := range cases {
		if err := c.category.Allowed(c.argv); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestEffectiveLimitsTakeTheMinimumAndCheckTheCeiling(t *testing.T) {
	agent := Limits{CPU: 4, MemoryMiB: 8192, DiskMiB: 20480, Time: 15 * time.Minute}
	project := Limits{CPU: 2, MemoryMiB: 4096, DiskMiB: 10240, Time: 30 * time.Minute}
	got := agent.Min(project)
	if got.CPU != 2 || got.MemoryMiB != 4096 || got.DiskMiB != 10240 || got.Time != 15*time.Minute {
		t.Fatalf("min: %+v", got)
	}
	if !got.Within(agent) || (Limits{CPU: 8}).Within(agent) {
		t.Fatal("ceiling")
	}
}

func TestDirectoryStaysInsideTheWorkspace(t *testing.T) {
	for dir, ok := range map[string]bool{"": true, "app": true, "app/../app": true, "../etc": false, "/etc": false, "app/../../x": false} {
		if _, got := CleanDirectory(dir); got != ok {
			t.Errorf("%q: %v", dir, got)
		}
	}
}

// US-41 CA-2: total, passed and failed from the Go test output.
func TestParseTestsCountsGoTests(t *testing.T) {
	verbose := "=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestB\n--- FAIL: TestB (0.00s)\n    --- PASS: TestB/sub (0.00s)\n--- SKIP: TestC\nFAIL\n"
	got := ParseTests(verbose)
	if got == nil || got.Total != 4 || got.Passed != 2 || got.Failed != 1 || got.Skipped != 1 {
		t.Fatalf("verbose: %+v", got)
	}
	events := `{"Action":"run","Test":"TestA"}` + "\n" + `{"Action":"pass","Test":"TestA"}` + "\n" + `{"Action":"pass","Package":"x"}` + "\n"
	if got := ParseTests(events); got == nil || got.Total != 1 || got.Passed != 1 {
		t.Fatalf("json: %+v", got)
	}
	if ParseTests("ok  \texample.com/app\t0.01s\n") != nil {
		t.Fatal("no test lines: no summary")
	}
}
