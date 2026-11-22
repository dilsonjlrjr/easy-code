package i18n_test

import (
	"slices"
	"testing"

	"easycode/internal/infrastructure/i18n"
)

func newTranslator(t *testing.T) *i18n.Translator {
	t.Helper()
	translator, err := i18n.NewTranslator()
	if err != nil {
		t.Fatal(err)
	}
	return translator
}

func TestNegotiatePicksBestSupportedLanguage(t *testing.T) {
	translator := newTranslator(t)
	cases := map[string]string{
		"":                      i18n.PortugueseBrazil,
		"en":                    i18n.English,
		"en-US,en;q=0.9":        i18n.English,
		"pt-PT":                 i18n.PortugueseBrazil,
		"fr-FR, en;q=0.5":       i18n.English,
		"de":                    i18n.PortugueseBrazil,
		"en;q=0.3, pt-BR;q=0.8": i18n.PortugueseBrazil,
		"en;q=0, de":            i18n.PortugueseBrazil,
		"*":                     i18n.PortugueseBrazil,
	}
	for header, want := range cases {
		if got := translator.Negotiate(header); got != want {
			t.Errorf("Negotiate(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestTranslateReplacesPlaceholdersAndFallsBack(t *testing.T) {
	translator := newTranslator(t)

	got := translator.Translate(i18n.English, "problem.method-not-allowed.detail", map[string]string{"method": "POST"})
	if got != "The POST method is not accepted on this resource." {
		t.Fatalf("unexpected message %q", got)
	}
	if got := translator.Translate("es", "health.alive", nil); got != "Serviço no ar." {
		t.Fatalf("expected pt-BR fallback, got %q", got)
	}
	if got := translator.Translate(i18n.English, "missing.key", nil); got != "missing.key" {
		t.Fatalf("expected key fallback, got %q", got)
	}
}

func TestCatalogsHaveTheSameKeys(t *testing.T) {
	translator := newTranslator(t)

	if !slices.Equal(translator.Keys(i18n.PortugueseBrazil), translator.Keys(i18n.English)) {
		t.Fatal("pt-BR and en catalogs differ")
	}
}
