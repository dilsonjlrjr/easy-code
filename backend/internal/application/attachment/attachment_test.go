package attachment_test

import (
	"testing"

	"easycode/internal/application/attachment"
)

func TestFileNameKeepsOnlyTheBaseName(t *testing.T) {
	cases := map[string]string{`C:\atas\ata.md`: "ata.md", "../../etc/passwd": "passwd", "/tmp/requisitos.txt": "requisitos.txt"}
	for sent, want := range cases {
		if got, ok := attachment.FileName(sent); !ok || got != want {
			t.Errorf("%q: %q %v", sent, got, ok)
		}
	}
	for _, bad := range []string{"", "/", "a\nb"} {
		if _, ok := attachment.FileName(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

// The type comes from the content, never from the client (RNF-05); Markdown by extension for text.
func TestMediaTypeIsDetectedOnTheServer(t *testing.T) {
	cases := []struct{ name, content, want string }{
		{"ata.md", "# Ata", "text/markdown"}, {"ata.txt", "texto", "text/plain"}, {"falso.txt", "%PDF-1.7", "application/pdf"},
		{"pagina.md", "<html><body>x", "text/html"}, {"dados.csv", "a,b\n1,2", "text/csv"},
	}
	for _, c := range cases {
		if got := attachment.MediaType(c.name, []byte(c.content)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}
