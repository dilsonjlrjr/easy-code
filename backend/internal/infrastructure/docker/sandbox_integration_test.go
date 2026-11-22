package docker_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appsandbox "easycode/internal/application/sandbox"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/sandbox"
	"easycode/internal/infrastructure/docker"
	"easycode/internal/infrastructure/specs"
)

// The sandbox escape and limit suite (ADR-05 "suíte de fuga", RNF-02, RNF-03, US-41) against the real
// Docker daemon, through the runner of easycode-sandbox. It needs the daemon socket and the Go sandbox
// image (make test-integration builds it); without them it is skipped.

const developer = "developer-agent"

func newRunner(t *testing.T) *appsandbox.Runner {
	t.Helper()
	socket, image := os.Getenv("EASYCODE_TEST_DOCKER_SOCKET"), os.Getenv("EASYCODE_TEST_SANDBOX_IMAGE")
	if socket == "" || image == "" {
		t.Skip("EASYCODE_TEST_DOCKER_SOCKET and EASYCODE_TEST_SANDBOX_IMAGE not set")
	}
	config := appsandbox.Config{Images: map[string]string{"go": image}, Runtime: "runc",
		Defaults: sandbox.Limits{CPU: 1, MemoryMiB: 1024, DiskMiB: 256, Time: 3 * time.Minute, Processes: 128, MaxOutputBytes: 1 << 20}}
	return appsandbox.NewRunner(docker.New(socket), specs.AgentPolicy, config)
}

func request(category string, argv ...string) sandbox.ExecutionRequest {
	return sandbox.ExecutionRequest{ExecutionID: uuid.NewString(),
		ProjectID: "3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14", WaveID: "0193a5c0-44aa-7e01-b2c9-000000000001", Agent: developer, Attempt: 1,
		Image: "go", Category: category, Argv: argv}
}

func uniqueID(t *testing.T) string {
	t.Helper()
	return uuid.NewString()
}

func run(t *testing.T, runner *appsandbox.Runner, files map[string]string, req sandbox.ExecutionRequest) sandbox.ExecutionResult {
	t.Helper()
	req.ExecutionID = uniqueID(t)
	contents := map[string][]byte{}
	for p, c := range files {
		contents[p] = []byte(c)
	}
	archive, err := appsandbox.Archive(contents)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.PrepareWorkspace(req.ProjectID, archive); err != nil {
		t.Fatal(err)
	}
	result, err := runner.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("%v: %v", req.Argv, err)
	}
	return result
}

func shell(category, script string) sandbox.ExecutionRequest {
	return request(category, "sh", "-c", script)
}

var app = map[string]string{
	"app/go.mod":       "module example.com/reserva\n\ngo 1.26\n",
	"app/main.go":      "package main\n\nimport \"fmt\"\n\nfunc Sum(a, b int) int { return a + b }\n\nfunc main() { fmt.Println(Sum(1, 2)) }\n",
	"app/main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestSum(t *testing.T) {\n\tif Sum(1, 2) != 3 {\n\t\tt.Fatal(\"sum\")\n\t}\n}\n",
}

// US-41 CA-1, CA-2, US-30 CA-3: build and tests run, with exit code, duration, output and the test counts.
func TestSandboxRunsBuildAndTestsWithEvidence(t *testing.T) {
	runner := newRunner(t)
	build := request(sandbox.CategoryBuild, "go", "build", "./...")
	build.Directory = "app"
	result := run(t, runner, app, build)
	if !result.Succeeded() || result.Duration <= 0 || !strings.HasPrefix(result.ImageDigest, "sha256:") || len(result.OutputSHA256) != 64 {
		t.Fatalf("build: %+v %q", result, result.OutputStart)
	}
	tests := request(sandbox.CategoryTests, "go", "test", "-count=1", "-v", "./...")
	tests.Directory = "app"
	result = run(t, runner, app, tests)
	if !result.Succeeded() || result.Tests == nil || result.Tests.Total != 1 || result.Tests.Passed != 1 || result.Tests.Failed != 0 {
		t.Fatalf("CA-2 tests: %+v %q", result.Tests, result.OutputStart)
	}
	files, _, err := runner.Collect(tests.ProjectID)
	if err != nil || string(files["app/main.go"]) != app["app/main.go"] {
		t.Fatalf("collect: %v", err)
	}
	failing := map[string]string{}
	for p, c := range app {
		failing[p] = c
	}
	failing["app/main_test.go"] = strings.Replace(app["app/main_test.go"], "!= 3", "!= 4", 1)
	result = run(t, runner, failing, tests)
	if result.Succeeded() || result.ExitCode == nil || *result.ExitCode == 0 || result.Tests == nil || result.Tests.Failed != 1 {
		t.Fatalf("a failing test is a normal result with exit code: %+v", result)
	}
}

// RNF-02: no route to the platform (database of the tests), to the host or to the internet; only lo.
func TestSandboxHasNoNetwork(t *testing.T) {
	runner := newRunner(t)
	target := "10.0.0.1:5432"
	if raw := os.Getenv("EASYCODE_TEST_DATABASE_URL"); raw != "" {
		if parsed, err := url.Parse(raw); err == nil {
			if addrs, err := net.LookupHost(parsed.Hostname()); err == nil && len(addrs) > 0 {
				target = net.JoinHostPort(addrs[0], "5432")
			}
		}
	}
	probe := map[string]string{
		"app/go.mod": "module example.com/probe\n\ngo 1.26\n",
		"app/net_test.go": "package probe\n\nimport (\n\t\"net\"\n\t\"net/http\"\n\t\"testing\"\n\t\"time\"\n)\n\n" +
			"func TestReachesPlatformDatabase(t *testing.T) {\n\tc, err := net.DialTimeout(\"tcp\", \"" + target + "\", 3*time.Second)\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tc.Close()\n}\n\n" +
			"func TestReachesInternet(t *testing.T) {\n\tclient := http.Client{Timeout: 3 * time.Second}\n\tif _, err := client.Get(\"https://proxy.golang.org\"); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n",
	}
	tests := request(sandbox.CategoryTests, "go", "test", "-count=1", "-v", "./...")
	tests.Directory = "app"
	result := run(t, runner, probe, tests)
	if result.Succeeded() || result.Tests == nil || result.Tests.Failed != 2 || result.Tests.Passed != 0 {
		t.Fatalf("RNF-02: tests that leave the sandbox must fail: %+v %q", result.Tests, result.OutputStart)
	}
	result = run(t, runner, nil, shell(sandbox.CategoryTests, "ls /sys/class/net; getent hosts postgres keycloak api || echo no-dns"))
	if !result.Succeeded() || strings.TrimSpace(strings.ReplaceAll(result.OutputStart, "no-dns", "")) != "lo" {
		t.Fatalf("RNF-02: only the loopback interface: %q", result.OutputStart)
	}
}

// ADR-05 profile: read-only root, non-root user, no capability, no new privileges, seccomp, no Docker
// socket and nothing of the host environment.
func TestSandboxProfileIsHardened(t *testing.T) {
	runner := newRunner(t)
	script := `id -u; grep -E '^(CapEff|NoNewPrivs|Seccomp):' /proc/self/status; touch /etc/x 2>/dev/null && echo root-writable; ` +
		`test -e /var/run/docker.sock && echo docker-socket; env | grep -c EASYCODE_ || true; cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/memory.max /sys/fs/cgroup/pids.max`
	req := shell(sandbox.CategoryTests, script)
	req.Limits = sandbox.Limits{CPU: 1, MemoryMiB: 512, Processes: 64}
	out := run(t, runner, nil, req).OutputStart
	for _, want := range []string{"10001\n", "CapEff:\t0000000000000000", "NoNewPrivs:\t1", "Seccomp:\t2", "\n0\n", "100000 100000", "536870912", "64"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile: want %q in %q", want, out)
		}
	}
	if strings.Contains(out, "root-writable") || strings.Contains(out, "docker-socket") {
		t.Fatalf("profile: %q", out)
	}
}

// RNF-03, US-41 CA-3: each limit ends the command within 10 s and is recorded as "limit exceeded".
func TestSandboxLimitsEndTheCommand(t *testing.T) {
	runner := newRunner(t)
	cases := []struct {
		name   string
		req    sandbox.ExecutionRequest
		reason string
	}{
		{"time", func() sandbox.ExecutionRequest {
			r := shell(sandbox.CategoryTests, "sleep 60")
			r.Limits.Time = 2 * time.Second
			return r
		}(), sandbox.EndTimeLimit},
		{"memory", func() sandbox.ExecutionRequest {
			r := shell(sandbox.CategoryTests, "head -c 2000m /dev/zero | tail")
			r.Limits.MemoryMiB = 256
			return r
		}(), sandbox.EndMemoryLimit},
		{"disk", func() sandbox.ExecutionRequest {
			r := shell(sandbox.CategoryTests, "head -c 64m /dev/zero > big")
			r.Limits.DiskMiB = 16
			return r
		}(), sandbox.EndDiskLimit},
		{"processes", func() sandbox.ExecutionRequest {
			r := shell(sandbox.CategoryTests, "for i in $(seq 1 60); do sleep 20 & done; wait")
			r.Limits.Processes = 16
			return r
		}(), sandbox.EndProcessLimit},
		{"output", func() sandbox.ExecutionRequest {
			r := shell(sandbox.CategoryTests, "yes | head -c 100000")
			r.Limits.MaxOutputBytes = 1024
			return r
		}(), sandbox.EndOutputLimit},
	}
	for _, c := range cases {
		started := time.Now()
		result := run(t, runner, nil, c.req)
		if result.EndReason != c.reason || result.ExitCode != nil || !sandbox.LimitExceeded(result.EndReason) {
			t.Errorf("%s: want %s, got %s (exit %v) %q", c.name, c.reason, result.EndReason, result.ExitCode, result.OutputStart)
		}
		if c.name == "time" && time.Since(started) > c.req.Limits.Time+10*time.Second+5*time.Second {
			t.Errorf("time limit: killed after %v", time.Since(started))
		}
	}
}

// S-02: collect_output accepts only regular files; links are refused and never followed.
func TestSandboxCollectRefusesLinks(t *testing.T) {
	runner := newRunner(t)
	req := shell(sandbox.CategoryBuild, "ln -s /etc/passwd app/link && ln app/go.mod app/hard && echo generated > app/out.txt")
	result := run(t, runner, map[string]string{"app/go.mod": "module x\n"}, req)
	if !result.Succeeded() {
		t.Fatalf("command: %+v", result)
	}
	files, refused, err := runner.Collect(req.ProjectID)
	if err != nil || string(files["app/out.txt"]) != "generated\n" || files["app/link"] != nil {
		t.Fatalf("collect: %v %v", files, err)
	}
	reasons := map[string]string{}
	for _, r := range refused {
		reasons[r.Path] = r.Reason
	}
	if reasons["app/link"] != "symlink" || (reasons["app/hard"] != "hardlink" && reasons["app/go.mod"] != "hardlink") {
		t.Fatalf("refused: %v", refused)
	}
}

// US-30, S-05, RNF-07: the runner checks the request again, whatever the worker sent.
func TestRunnerRechecksThePolicy(t *testing.T) {
	runner := newRunner(t)
	archive, _ := appsandbox.Archive(map[string][]byte{})
	_ = runner.PrepareWorkspace("3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14", archive)
	cases := map[string]struct {
		req  sandbox.ExecutionRequest
		code string
	}{
		"argv0 outside the category": {request(sandbox.CategoryBuild, "curl", "http://api:8080"), sandbox.ProblemCommandNotAllowed},
		"requirements agent": {func() sandbox.ExecutionRequest {
			r := request(sandbox.CategoryBuild, "go", "build")
			r.Agent = "requirements-agent"
			return r
		}(), sandbox.ProblemCommandNotAllowed},
		"image outside the list": {func() sandbox.ExecutionRequest {
			r := request(sandbox.CategoryBuild, "go", "build")
			r.Image = "alpine"
			return r
		}(), sandbox.ProblemImageNotAllowed},
		"limit above the ceiling": {func() sandbox.ExecutionRequest {
			r := request(sandbox.CategoryBuild, "go", "build")
			r.Limits.CPU = 64
			return r
		}(), sandbox.ProblemLimitAboveCeiling},
		"secret variable": {func() sandbox.ExecutionRequest {
			r := request(sandbox.CategoryBuild, "go", "build")
			r.Environment = map[string]string{"ANTHROPIC_API_KEY": "x"}
			return r
		}(), sandbox.ProblemForbiddenVariable},
		"support service": {func() sandbox.ExecutionRequest {
			r := request(sandbox.CategoryTests, "go", "test")
			r.Services = []string{"ephemeral_postgresql"}
			return r
		}(), sandbox.ProblemServiceNotAllowed},
	}
	for name, c := range cases {
		c.req.ExecutionID = uniqueID(t)
		_, err := runner.Execute(context.Background(), c.req)
		var classified *failure.Error
		if !errors.As(err, &classified) || classified.Code != c.code {
			t.Errorf("%s: want %s, got %v", name, c.code, err)
		}
	}
}

// RNF-12: the same execution id never runs the command twice.
func TestSandboxExecutionIsIdempotent(t *testing.T) {
	runner := newRunner(t)
	archive, _ := appsandbox.Archive(map[string][]byte{})
	req := shell(sandbox.CategoryTests, "date +%s%N")
	req.ExecutionID = uniqueID(t)
	_ = runner.PrepareWorkspace(req.ProjectID, archive)
	first, err := runner.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Execute(context.Background(), req)
	if err != nil || second.OutputStart != first.OutputStart || !second.Start.Equal(first.Start) {
		t.Fatalf("repeated execution ran again: %q %q %v", first.OutputStart, second.OutputStart, err)
	}
}
