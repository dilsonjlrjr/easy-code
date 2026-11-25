package runtime_test

import (
	"testing"

	"easycode/internal/application/runtime"
	"easycode/internal/domain/provider"
	"easycode/internal/domain/sandbox"
)

// US-23, ADR-05/07: the scans of the Segurança run in the security image (scanners and their offline
// databases); every other command of the same wave stays in the image of the project stack.
func TestScannerCommandsRunInTheSecurityImage(t *testing.T) {
	scan := args(map[string]any{"category": "scanner", "argv": []string{"trivy", "fs", "app"}})
	probe := args(map[string]any{"category": "local_http_client", "argv": []string{"curl", "http://localhost:8080/health"}})
	model := &cannedModel{answers: []provider.Message{{Calls: []provider.ToolCall{
		{ID: "s", Name: runtime.ToolExecuteCommand, Args: scan},
		{ID: "p", Name: runtime.ToolExecuteCommand, Args: probe},
	}}}}
	runner := &fakeRunner{results: []sandbox.ExecutionResult{exit(0), exit(0)}}
	w := runtime.WaveContext{WaveID: "w1", Code: "SEG-01", Agent: "security-agent", Attempt: 1, Instruction: "i", Image: "go",
		Policy: policy(t, "security-agent"), Limits: sandbox.Limits{CPU: 1}}
	steps(t, runtime.NewToolAgent(model, 1000, runner), w, 3)
	if len(runner.requests) != 2 {
		t.Fatalf("want two executions, got %+v", runner.requests)
	}
	if got := runner.requests[0]; got.Category != sandbox.CategoryScanner || got.Image != sandbox.ImageSecurity {
		t.Fatalf("scanner in the security image, got %q in %q", got.Category, got.Image)
	}
	if got := runner.requests[1].Image; got != "go" {
		t.Fatalf("local_http_client in the stack image, got %q", got)
	}
}

func TestImageForCategory(t *testing.T) {
	cases := map[string]string{sandbox.CategoryScanner: sandbox.ImageSecurity, sandbox.CategoryBuild: "go", sandbox.CategoryTests: "go", "lint": "go"}
	for category, want := range cases {
		if got := sandbox.ImageFor(category, "go"); got != want {
			t.Errorf("%s: want %q, got %q", category, want, got)
		}
	}
}
