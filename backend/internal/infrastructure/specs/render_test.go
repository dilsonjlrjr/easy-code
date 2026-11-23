package specs_test

import (
	"strings"
	"testing"

	"easycode/internal/infrastructure/specs"
)

// The RELEASE of the orchestrator reads back as the same header (dates stay text, order is the template's).
func TestArtifactRendererRoundTrip(t *testing.T) {
	header := map[string]any{"versao": 1, "id": "RELEASE-01", "tipo": "RELEASE", "titulo": "Release 01 — Reserva: salas",
		"historico": []any{map[string]any{"versao": 1, "data": "2026-11-22", "onda": "REL-01", "descricao": "criação"}},
		"custos":    []any{map[string]any{"etapa": "qa", "custo": 0.25, "tokens": int64(10)}}, "riscos_aceitos": []any{}, "situacao": "rascunho"}
	content, err := specs.ArtifactRenderer{}.Render(header, "# Corpo\n")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.HasPrefix(text, "---\nid: RELEASE-01\ntipo: RELEASE\n") || !strings.HasSuffix(text, "---\n\n# Corpo\n") {
		t.Fatalf("layout:\n%s", text)
	}
	head := strings.SplitN(strings.TrimPrefix(text, "---\n"), "---\n", 2)[0]
	parsed, err := specs.HeaderParser{}.ParseHeader(head)
	if err != nil {
		t.Fatal(err)
	}
	history := parsed["historico"].([]any)[0].(map[string]any)
	if history["data"] != "2026-11-22" || parsed["titulo"] != "Release 01 — Reserva: salas" {
		t.Fatalf("read back: %v", parsed)
	}
}
