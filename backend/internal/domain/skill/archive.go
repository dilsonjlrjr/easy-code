package skill

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Limits of an uploaded package (policy.imported_skill: ceilings of bytes and entries, safe extraction).
const (
	MaxPackageBytes   = 2 << 20 // transport limit of the zip (US-44 CA-3: 413 too-large-package)
	MaxPackageEntries = 200
	MaxExpandedBytes  = 8 << 20
	// DefaultMaxSizeTokens is policy.imported_skill max_size_tokens.
	DefaultMaxSizeTokens = 50000
)

// Problem codes of the import (openapi importSkill).
const (
	CodeInvalidPackage  = "invalid-skill-package"
	CodeTooLargePackage = "too-large-package"
)

// Statuses and block reasons of the machine skill (D-26).
const (
	StatusImported    = "imported"
	StatusScanning    = "scanning"
	StatusBlocked     = "blocked"
	StatusDeactivated = "deactivated"

	ReasonFormat    = "format"
	ReasonSize      = "size"
	ReasonMalicious = "malicious_content"
)

// ErrInvalidPackage is a refusal at the submission: no version is created (US-44 CA-2).
var ErrInvalidPackage = errors.New("invalid skill package")

// Invalid is a refusal with the reason shown to the administrator.
type Invalid struct{ Reason string }

func (e Invalid) Error() string { return "invalid skill package: " + e.Reason }
func (e Invalid) Unwrap() error { return ErrInvalidPackage }

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// ParseArchive reads a zipped skill folder: SKILL.md at the root (or inside a single top folder) with name and
// description in the front matter (CA-2). Unsafe entries (absolute path, "..", links, special files) or a
// package beyond the ceilings make the version blocked by format, so they are reported as Unsafe, not refused.
func ParseArchive(archive []byte, readYAML func([]byte) ([]byte, error)) (Package, bool, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return Package{}, false, Invalid{Reason: "not_a_zip"}
	}
	if len(reader.File) > MaxPackageEntries {
		return Package{}, false, Invalid{Reason: "too_many_entries"}
	}
	unsafe := false
	files := map[string]string{}
	total := 0
	for _, f := range reader.File {
		name := f.Name
		if f.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(name)
		if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || clean != name || strings.HasPrefix(clean, "../") || clean == ".." ||
			!f.Mode().IsRegular() {
			unsafe = true
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Package{}, false, Invalid{Reason: "unreadable_entry"}
		}
		content, err := io.ReadAll(io.LimitReader(rc, MaxExpandedBytes+1))
		_ = rc.Close()
		if err != nil {
			return Package{}, false, Invalid{Reason: "unreadable_entry"}
		}
		total += len(content)
		if total > MaxExpandedBytes {
			return Package{}, false, Invalid{Reason: "expanded_too_large"}
		}
		if !utf8.Valid(content) {
			unsafe = true // binary file: a skill is text (scripts are never executed, allow_executable_scripts false)
			continue
		}
		files[clean] = string(content)
	}
	files = stripTopFolder(files)
	skillMD, ok := files["SKILL.md"]
	if !ok {
		return Package{}, false, Invalid{Reason: "missing_skill_md"}
	}
	_, header, found := strings.Cut(skillMD, "---\n")
	if found {
		header, _, found = strings.Cut(header, "\n---")
	}
	if !found {
		return Package{}, false, Invalid{Reason: "missing_front_matter"}
	}
	encoded, err := readYAML([]byte(header))
	if err != nil {
		return Package{}, false, Invalid{Reason: "invalid_front_matter"}
	}
	var front struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Agents      []string `json:"agents"`
	}
	if json.Unmarshal(encoded, &front) != nil || strings.TrimSpace(front.Name) == "" {
		return Package{}, false, Invalid{Reason: "missing_name"}
	}
	if strings.TrimSpace(front.Description) == "" {
		return Package{}, false, Invalid{Reason: "missing_description"}
	}
	if !namePattern.MatchString(front.Name) {
		return Package{}, false, Invalid{Reason: "invalid_name"}
	}
	return Package{Name: front.Name, Description: strings.TrimSpace(front.Description), Agents: front.Agents, Files: files}, unsafe, nil
}

// stripTopFolder removes a single top folder that holds every file (a zipped folder).
func stripTopFolder(files map[string]string) map[string]string {
	top := ""
	for name := range files {
		head, _, nested := strings.Cut(name, "/")
		if !nested || (top != "" && head != top) {
			return files
		}
		top = head
	}
	if top == "" {
		return files
	}
	out := make(map[string]string, len(files))
	for name, content := range files {
		out[strings.TrimPrefix(name, top+"/")] = content
	}
	return out
}

// suspicious are instructions a skill must never carry: asking for keys or secrets, or overriding the system
// (PROJETO 8.3, RNF-05). A match blocks the version until an administrator unblocks it (US-44 CA-4).
var suspicious = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+|any\s+)?(the\s+)?(previous|prior|above|system)\s+(instructions|prompts?|rules)`),
	regexp.MustCompile(`(?i)ignor[ea]r?\s+(todas\s+)?(as\s+)?(instru[çc][õo]es|regras)\s+(anteriores|do\s+sistema)`),
	regexp.MustCompile(`(?i)disregard\s+(the\s+)?(system|previous)`),
	regexp.MustCompile(`(?i)(reveal|print|show|send|exfiltrat\w*|envie|mostre|revele)\s+(me\s+)?(the\s+|a\s+|o\s+|as\s+|os\s+)?(api[\s_-]?keys?|secret|secrets|credentials|tokens?|chaves?(\s+de\s+api)?|senhas?|system\s+prompt)`),
	regexp.MustCompile(`(?i)(sk-ant-|sk-proj-|AKIA[0-9A-Z]{12})`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+(in\s+)?(developer|dan|jailbreak)`),
}

// Scan is the result of the scan of a version (policy.imported_skill: format, size, malicious content).
type Scan struct {
	Status  string
	Reasons []string
	Report  map[string]any
}

// ScanPackage checks the package: unsafe entries → format, estimated size above maxTokens → size, suspicious
// instruction → malicious_content. No reason → unblocked (CA-1).
func ScanPackage(pkg Package, unsafe bool, maxTokens int) Scan {
	var reasons []string
	report := map[string]any{"size_tokens": pkg.SizeTokens(), "files": len(pkg.Files)}
	if unsafe {
		reasons = append(reasons, ReasonFormat)
	}
	if pkg.SizeTokens() > maxTokens {
		reasons = append(reasons, ReasonSize)
		report["max_size_tokens"] = maxTokens
	}
	var findings []string
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, pattern := range suspicious {
			if pattern.MatchString(pkg.Files[name]) {
				findings = append(findings, fmt.Sprintf("%s: %s", name, pattern.String()[:min(40, len(pattern.String()))]))
				break
			}
		}
	}
	if len(findings) > 0 {
		reasons = append(reasons, ReasonMalicious)
		report["suspicious"] = findings
	}
	if len(reasons) == 0 {
		return Scan{Status: StatusUnblocked, Report: report}
	}
	return Scan{Status: StatusBlocked, Reasons: reasons, Report: report}
}
