package docker_test

import (
	"os"
	"strings"
	"testing"
	"time"

	appsandbox "easycode/internal/application/sandbox"
	"easycode/internal/domain/sandbox"
	"easycode/internal/infrastructure/docker"
	"easycode/internal/infrastructure/specs"
)

// US-23 CA-2, RNF-02, RNF-03: the scans of the Segurança run in the security image (infra/sandbox/security)
// through the same hardened runner profile as the stack images: no network, read-only root, non-root user,
// resource limits. Opengrep uses the rules baked into the image and Trivy the preloaded database, so each
// tool finds what it should without any network. It needs the daemon socket and the security image
// (make sandbox-image builds it); without them it is skipped.

const securityAgent = "security-agent"

func newSecurityRunner(t *testing.T) *appsandbox.Runner {
	t.Helper()
	socket, image := os.Getenv("EASYCODE_TEST_DOCKER_SOCKET"), os.Getenv("EASYCODE_TEST_SECURITY_IMAGE")
	if socket == "" || image == "" {
		t.Skip("EASYCODE_TEST_DOCKER_SOCKET and EASYCODE_TEST_SECURITY_IMAGE not set")
	}
	config := appsandbox.Config{Images: map[string]string{sandbox.ImageSecurity: image}, Runtime: "runc",
		Defaults: sandbox.Limits{CPU: 2, MemoryMiB: 2048, DiskMiB: 256, Time: 5 * time.Minute, Processes: 256, MaxOutputBytes: 1 << 20}}
	return appsandbox.NewRunner(docker.New(socket), specs.AgentPolicy, config)
}

func scan(argv ...string) sandbox.ExecutionRequest {
	req := request(sandbox.CategoryScanner, argv...)
	req.Agent, req.Image = securityAgent, sandbox.ImageSecurity
	return req
}

// The application under audit: a concatenated SQL query and TLS without verification (Opengrep), a module
// with known vulnerabilities (Trivy) and a token in a configuration file (Gitleaks).
var audited = map[string]string{
	"app/go.mod": "module example.com/reserva\n\ngo 1.26\n\nrequire golang.org/x/net v0.0.1\n",
	"app/go.sum": "golang.org/x/net v0.0.1 h1:abc=\ngolang.org/x/net v0.0.1/go.mod h1:def=\n",
	"app/main.go": "package main\n\nimport (\n\t\"crypto/tls\"\n\t\"database/sql\"\n)\n\n" +
		"func find(db *sql.DB, name string) {\n\tdb.Query(\"SELECT * FROM rooms WHERE name = '\" + name + \"'\")\n\t_ = &tls.Config{InsecureSkipVerify: true}\n}\n\nfunc main() {}\n",
	"app/config.env": "PARTNER_TOKEN=ghp_1234567890abcdefghijABCDEFGHIJ123456\n",
}

func TestSecurityScansRunOfflineInTheSecurityImage(t *testing.T) {
	runner := newSecurityRunner(t)
	cases := []struct {
		name  string
		req   sandbox.ExecutionRequest
		exit  int
		wants []string
	}{
		{"opengrep", scan("opengrep", "scan", "--config", "/opt/easycode/opengrep-rules", "--error", "app"), 1,
			[]string{"go-sql-string-concatenation", "go-tls-insecure-skip-verify"}},
		{"trivy", scan("trivy", "fs", "--scanners", "vuln", "--exit-code", "1", "app"), 1, []string{"golang.org/x/net", "CVE-"}},
		{"gitleaks", scan("gitleaks", "dir", "--redact", "app"), 1, []string{"leaks found"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := run(t, runner, audited, c.req)
			if result.EndReason != sandbox.EndCompleted || result.ExitCode == nil || *result.ExitCode != c.exit {
				t.Fatalf("%s: want exit %d, got %+v\n%s", c.name, c.exit, result, result.OutputStart)
			}
			for _, want := range c.wants {
				if !strings.Contains(result.OutputStart, want) {
					t.Fatalf("%s: output without %q:\n%s", c.name, want, result.OutputStart)
				}
			}
			if !strings.HasPrefix(result.ImageDigest, "sha256:") {
				t.Fatalf("RNF-15: image digest recorded, got %q", result.ImageDigest)
			}
		})
	}
	// --redact (varreduras-opensource.md): the secret never reaches the recorded output.
	result := run(t, runner, audited, scan("gitleaks", "dir", "--redact", "--verbose", "app"))
	if strings.Contains(result.OutputStart, "ghp_1234567890") {
		t.Fatalf("the secret leaked into the output:\n%s", result.OutputStart)
	}
}

// RNF-02: the scanners have no network either; a scan that needs it fails instead of reaching out, and
// the security image refuses the commands of other categories (only the scanner allowlist applies).
func TestSecurityImageKeepsTheSandboxRules(t *testing.T) {
	runner := newSecurityRunner(t)
	result := run(t, runner, audited, scan("trivy", "image", "--skip-db-update", "--offline-scan=false", "alpine:3"))
	if result.Succeeded() {
		t.Fatalf("RNF-02: pulling an image needs the network and must fail:\n%s", result.OutputStart)
	}
	result = run(t, runner, audited, scan("opengrep", "scan", "--config", "auto", "app"))
	if result.Succeeded() {
		t.Fatalf("RNF-02: the rule registry is out of reach:\n%s", result.OutputStart)
	}
	refused := scan("sh", "-c", "id")
	refused.ExecutionID = uniqueID(t)
	if _, err := runner.Execute(t.Context(), refused); err == nil {
		t.Fatal("D-38: a shell is not a scanner")
	}
}
