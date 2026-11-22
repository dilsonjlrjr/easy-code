// Package i18n translates user-facing messages. Catalogs are embedded; pt-BR is the default.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Supported languages.
const (
	PortugueseBrazil = "pt-BR"
	English          = "en"
)

// catalogFiles must be package level: go:embed only applies to package variables.
//
//go:embed locales/*.json
var catalogFiles embed.FS

// Translator resolves message keys for a language negotiated from Accept-Language.
type Translator struct {
	catalogs map[string]map[string]string
	fallback string
}

// NewTranslator loads the embedded catalogs.
func NewTranslator() (*Translator, error) {
	catalogs := map[string]map[string]string{}
	for _, language := range []string{PortugueseBrazil, English} {
		catalog, err := loadCatalog(language)
		if err != nil {
			return nil, err
		}
		catalogs[language] = catalog
	}
	return &Translator{catalogs: catalogs, fallback: PortugueseBrazil}, nil
}

func loadCatalog(language string) (map[string]string, error) {
	content, err := catalogFiles.ReadFile("locales/" + language + ".json")
	if err != nil {
		return nil, fmt.Errorf("read catalog %s: %w", language, err)
	}
	catalog := map[string]string{}
	if err := json.Unmarshal(content, &catalog); err != nil {
		return nil, fmt.Errorf("parse catalog %s: %w", language, err)
	}
	return catalog, nil
}

// Negotiate picks the supported language with the highest quality in an Accept-Language
// header, falling back to pt-BR.
func (t *Translator) Negotiate(acceptLanguage string) string {
	for _, candidate := range parseAcceptLanguage(acceptLanguage) {
		if language, ok := t.match(candidate); ok {
			return language
		}
	}
	return t.fallback
}

func (t *Translator) match(tag string) (string, bool) {
	base, _, _ := strings.Cut(strings.ToLower(tag), "-")
	switch base {
	case "pt":
		return PortugueseBrazil, true
	case "en":
		return English, true
	case "*":
		return t.fallback, true
	default:
		return "", false
	}
}

// Translate returns the message for key in language, replacing {name} placeholders.
// It falls back to pt-BR and then to the key itself.
func (t *Translator) Translate(language, key string, params map[string]string) string {
	message, ok := t.catalogs[language][key]
	if !ok {
		message, ok = t.catalogs[t.fallback][key]
	}
	if !ok {
		return key
	}
	for name, value := range params {
		message = strings.ReplaceAll(message, "{"+name+"}", value)
	}
	return message
}

// Keys lists the keys of a language catalog, sorted.
func (t *Translator) Keys(language string) []string {
	keys := make([]string, 0, len(t.catalogs[language]))
	for key := range t.catalogs[language] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type weightedTag struct {
	tag     string
	quality float64
}

func parseAcceptLanguage(header string) []string {
	var weighted []weightedTag
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if tag == "" {
			continue
		}
		quality := parseQuality(params)
		if quality > 0 {
			weighted = append(weighted, weightedTag{tag: strings.TrimSpace(tag), quality: quality})
		}
	}
	sort.SliceStable(weighted, func(i, j int) bool { return weighted[i].quality > weighted[j].quality })
	tags := make([]string, len(weighted))
	for i, item := range weighted {
		tags[i] = item.tag
	}
	return tags
}

func parseQuality(params string) float64 {
	name, value, found := strings.Cut(strings.TrimSpace(params), "=")
	if !found || strings.TrimSpace(name) != "q" {
		return 1
	}
	quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return quality
}
