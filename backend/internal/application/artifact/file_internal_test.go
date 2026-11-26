package artifact

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A version without a commit is served as the Markdown file rebuilt from its header and body: YAML block
// front matter in the header key order, then the body (the human edit of US-53 starts from it).
func TestRebuildFileWritesFrontMatterAndBody(t *testing.T) {
	header := json.RawMessage(`{"id":"US-10","titulo":"Somar: dois números","criterios":[{"id":"CA-1","texto":"a"}],"tags":[]}`)
	content, err := rebuildFile(header, "# US-10\n\nTexto.\n")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if strings.Contains(text, "[object Object]") || strings.Contains(text, "{\"") {
		t.Fatalf("front matter must be YAML block, got:\n%s", text)
	}
	if !strings.HasPrefix(text, "---\nid: \"US-10\"\n") || !strings.HasSuffix(text, "---\n# US-10\n\nTexto.\n") {
		t.Fatalf("unexpected file:\n%s", text)
	}
	front := strings.TrimPrefix(strings.SplitN(text, "\n---\n", 2)[0], "---\n")
	var back map[string]any
	if err := yaml.Unmarshal([]byte(front), &back); err != nil || back["titulo"] != "Somar: dois números" {
		t.Fatalf("front matter must read back: %v %v", back, err)
	}
	if criteria, ok := back["criterios"].([]any); !ok || len(criteria) != 1 {
		t.Fatalf("nested list lost: %v", back["criterios"])
	}
}
