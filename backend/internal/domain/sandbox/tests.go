package sandbox

import (
	"bufio"
	"encoding/json"
	"strings"
)

// ParseTests counts the tests of a test command output (US-41 CA-2). It knows the Go formats
// (`go test -v` lines and `go test -json` events); nil when nothing is recognized.
func ParseTests(output string) *TestSummary {
	var summary TestSummary
	seen := false
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "{") {
			var event struct {
				Action string `json:"Action"`
				Test   string `json:"Test"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Test != "" {
				switch event.Action {
				case "pass":
					summary.Passed++
				case "fail":
					summary.Failed++
				case "skip":
					summary.Skipped++
				default:
					continue
				}
				seen = true
				continue
			}
		}
		switch {
		case strings.HasPrefix(line, "--- PASS:"):
			summary.Passed++
		case strings.HasPrefix(line, "--- FAIL:"):
			summary.Failed++
		case strings.HasPrefix(line, "--- SKIP:"):
			summary.Skipped++
		default:
			continue
		}
		seen = true
	}
	if !seen {
		return nil
	}
	summary.Total = summary.Passed + summary.Failed + summary.Skipped
	return &summary
}
