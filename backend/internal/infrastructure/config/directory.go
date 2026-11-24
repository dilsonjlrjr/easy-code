package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EnvConfigDir names the directory written by easycode-installer (ADR-22). It holds FileName with
// the non-secret settings and SecretsDir with one file per secret; the variables of the environment
// still win over the file, so a deployment can override one value without editing it.
const EnvConfigDir = "EASYCODE_CONFIG_DIR"

// Files of the configuration directory.
const (
	FileName   = "easycode.env"
	SecretsDir = "secrets"
)

// Environment layers the configuration directory, when EASYCODE_CONFIG_DIR is set, under lookup.
// A *_FILE value that is a relative path is resolved against the directory, so the same directory
// works wherever it is mounted.
func Environment(lookup LookupFunc) (LookupFunc, error) {
	dir, ok := lookup(EnvConfigDir)
	dir = strings.TrimSpace(dir)
	if !ok || dir == "" {
		return lookup, nil
	}
	content, err := os.ReadFile(filepath.Join(dir, FileName)) //nolint:gosec // the directory is deployment configuration
	if err != nil {
		return nil, fmt.Errorf("%s: cannot read %s: %w", EnvConfigDir, FileName, err)
	}
	values, err := ParseFile(content)
	if err != nil {
		return nil, fmt.Errorf("%s/%s: %w", EnvConfigDir, FileName, err)
	}
	for key, value := range values {
		if strings.HasSuffix(key, SecretFileSuffix) && value != "" && !filepath.IsAbs(value) {
			values[key] = filepath.Join(dir, value)
		}
	}
	return func(key string) (string, bool) {
		if value, ok := lookup(key); ok && strings.TrimSpace(value) != "" {
			return value, true
		}
		value, ok := values[key]
		return value, ok
	}, nil
}

// ParseFile reads KEY=VALUE lines; blank lines and lines starting with # are skipped. Values are
// taken literally (no quotes, no expansion), which keeps the format the same for the installer and
// the processes.
func ParseFile(content []byte) (map[string]string, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d: want KEY=VALUE", line)
		}
		values[key] = strings.TrimSpace(value)
	}
	return values, scanner.Err()
}

// FormatFile writes values in the format ParseFile reads, sorted by key, after a header comment.
func FormatFile(header string, values map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if key == "" || strings.ContainsAny(key, "= \t\n") || strings.ContainsAny(value, "\n\r") {
			return nil, errors.New("configuration key or value with a separator or a line break")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	for _, line := range strings.Split(strings.TrimSpace(header), "\n") {
		out.WriteString("# " + strings.TrimSpace(line) + "\n")
	}
	for _, key := range keys {
		out.WriteString(key + "=" + values[key] + "\n")
	}
	return out.Bytes(), nil
}
