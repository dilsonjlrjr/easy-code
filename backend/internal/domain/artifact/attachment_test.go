package artifact_test

import (
	"testing"

	"easycode/internal/domain/artifact"
)

// PL-24: the attachments of a version are the attachment outputs of its type, shown relative to the
// artifact folder (or by repository path outside it), with type, size and hash.
func TestVersionAttachmentsOfTheType(t *testing.T) {
	specs := []artifact.AttachmentSpec{
		{Type: "ARQ", Path: "docs/arquitetura/estrutura.md"}, {Type: "ARQ", Path: "docs/arquitetura/api/"},
		{Type: "TELAS", Path: "docs/design/prototipo/"}, {Type: "CT", Path: "docs/qa/plano.md"},
	}
	files := map[string][]byte{
		"docs/arquitetura/ARQ.md": []byte("---\n"), "docs/arquitetura/estrutura.md": []byte("# Estrutura"),
		"docs/arquitetura/api/openapi.yaml": []byte("openapi: 3.1.0"), "docs/arquitetura/adr/ADR-01.md": []byte("x"),
		"docs/design/prototipo/index.html": []byte("<html>"), "docs/qa/plano.md": []byte("# Plano"),
	}
	got := artifact.VersionAttachments("ARQ", "docs/arquitetura/ARQ.md", specs, files)
	if len(got) != 2 || got[0].Path != "api/openapi.yaml" || got[0].MediaType != "application/yaml" || got[1].Path != "estrutura.md" ||
		got[1].MediaType != "text/markdown" || got[1].SizeBytes != 11 || len(got[1].SHA256) != 64 || got[1].RepositoryPath != "docs/arquitetura/estrutura.md" {
		t.Fatalf("ARQ: %+v", got)
	}
	if telas := artifact.VersionAttachments("TELAS", "docs/design/TELAS.md", specs, files); len(telas) != 1 || telas[0].Path != "prototipo/index.html" ||
		artifact.TextAttachment(telas[0].MediaType) {
		t.Fatalf("TELAS: %+v", telas)
	}
	if ct := artifact.VersionAttachments("CT", "docs/qa/cenarios/CT-01.md", specs, files); len(ct) != 1 || ct[0].Path != "docs/qa/plano.md" {
		t.Fatalf("CT outside its folder keeps the repository path: %+v", ct)
	}
	if none := artifact.VersionAttachments("US", "docs/requisitos/US-01-x.md", specs, files); len(none) != 0 {
		t.Fatalf("US: %+v", none)
	}
	if found, ok := artifact.FindAttachment(got, "estrutura.md"); !ok || found.SizeBytes != 11 {
		t.Fatal("FindAttachment")
	}
	if artifact.MediaTypeOf("x.bin", []byte{0xff, 0xfe}) != "application/octet-stream" || artifact.MediaTypeOf("LEIAME", []byte("ok")) != "text/plain" {
		t.Fatal("MediaTypeOf")
	}
}
