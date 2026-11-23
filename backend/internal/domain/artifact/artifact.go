// Package artifact holds the artifact rules shared by the store and the reads (US-29, US-26):
// types and their owner stage, the document split into YAML header and Markdown body, and the
// schema errors returned to the agent when an artifact is out of its schema.
package artifact

import (
	"regexp"
	"strings"
)

// Failure codes.
const (
	// CodeInvalid refuses an artifact out of its schema; the extension "schema_errors" lists the
	// errors so the agent corrects it in the same wave (US-29 CA-1).
	CodeInvalid = "artifact-invalid"
)

// Artifact states (machine "artifact" of estados.yaml).
const (
	StateDraft    = "draft"
	StateInReview = "in_review"
	StateApproved = "approved"
	StateReplaced = "replaced"
)

// Types of the common header (ArtifactType).
var Types = []string{"VISAO", "US", "RNF", "BACKLOG", "ARQ", "ADR", "T", "DIC", "PERSIST", "VALIDACAO", "CT", "AMEACAS", "ACH", "REL-QA", "REL-SEG", "RELEASE", "TELAS"}

var codePattern = regexp.MustCompile(`^([A-Z]+(-[A-Z]+)?-[0-9]{2,}|VISAO|ARQ|DIC|PERSIST|AMEACAS|BACKLOG|TELAS)$`)

// ValidCode reports whether code is an artifact id of the common header.
func ValidCode(code string) bool { return codePattern.MatchString(code) }

// SchemaError is one violation of the artifact schema, safe to show to the agent and the user.
type SchemaError struct {
	Path    string `json:"path"`    // JSON pointer in the header ("" = the document)
	Message string `json:"message"` // validator message, without the content
}

// Document is an artifact file: the YAML front matter as JSON and the Markdown body.
type Document struct {
	Header map[string]any
	Body   string
}

// Split separates the front matter ("---" lines) from the body. ok is false without front matter.
func Split(content string) (header, body string, ok bool) {
	content = strings.TrimPrefix(content, "\uFEFF")
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return "", content, false
	}
	rest := content[strings.Index(content, "\n")+1:]
	for offset := 0; offset < len(rest); {
		end := strings.Index(rest[offset:], "\n")
		line := rest[offset:]
		if end >= 0 {
			line = rest[offset : offset+end]
		}
		if strings.TrimRight(line, "\r") == "---" {
			body := ""
			if end >= 0 {
				body = rest[offset+end+1:]
			}
			return rest[:offset], body, true
		}
		if end < 0 {
			break
		}
		offset += end + 1
	}
	return "", content, false
}

// SchemaFile is the schema file name of a type (us.schema.json, rel-qa.schema.json).
func SchemaFile(artifactType string) string {
	return strings.ToLower(artifactType) + ".schema.json"
}

var ownerStages = map[string]string{
	"VISAO": "requirements", "US": "requirements", "RNF": "requirements", "BACKLOG": "requirements",
	"ARQ": "architecture", "ADR": "architecture", "T": "architecture", "DIC": "data", "PERSIST": "data", "TELAS": "design",
	"VALIDACAO": "validation", "CT": "validation", "AMEACAS": "validation", "REL-QA": "qa", "REL-SEG": "security", "RELEASE": "release",
}

// TypeOf is the type of an artifact id: "US-03" → "US", "VISAO" → "VISAO", "REL-QA-01" → "REL-QA".
func TypeOf(code string) string {
	if i := strings.LastIndex(code, "-"); i > 0 && strings.Trim(code[i+1:], "0123456789") == "" {
		return code[:i]
	}
	return code
}

// OwnerStage is the stage whose gate approves the type (artifacts.owner_stage); "" for ACH.
func OwnerStage(artifactType string) string { return ownerStages[artifactType] }
