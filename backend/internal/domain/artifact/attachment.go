package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// Attachment is one other file of an artifact version (Artifact.attachments, PL-24): a file of the
// artifact folder that is not an artifact itself (openapi.yaml, estrutura.md, prototipo/). The content
// stays in the git commit of the version.
type Attachment struct {
	// Path is relative to the folder of the artifact file when the file is inside it (api/openapi.yaml,
	// prototipo/index.html), else the repository path (docs/qa/plano.md of a CT in docs/qa/cenarios/).
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	SizeBytes int    `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	// RepositoryPath is the path in the project repository, to read the content (not in the api).
	RepositoryPath string `json:"repository_path"`
}

// AttachmentSpec is an attachment output of the workflow: the artifact type it belongs to and its
// repository path; a path ending in "/" is a folder (every file under it).
type AttachmentSpec struct {
	Type string
	Path string
}

// VersionAttachments lists the attachments of a version of an artifact of artifactType written at
// artifactPath, from the files of the repository after the wave (path → content), ordered by path.
func VersionAttachments(artifactType, artifactPath string, specs []AttachmentSpec, files map[string][]byte) []Attachment {
	folder := path.Dir(artifactPath) + "/"
	out := []Attachment{}
	for file, content := range files {
		if file == artifactPath || !attached(artifactType, file, specs) {
			continue
		}
		sum := sha256.Sum256(content)
		shown := file
		if strings.HasPrefix(file, folder) {
			shown = strings.TrimPrefix(file, folder)
		}
		out = append(out, Attachment{Path: shown, MediaType: MediaTypeOf(file, content), SizeBytes: len(content),
			SHA256: hex.EncodeToString(sum[:]), RepositoryPath: file})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func attached(artifactType, file string, specs []AttachmentSpec) bool {
	for _, spec := range specs {
		if spec.Type != artifactType {
			continue
		}
		if spec.Path == file || (strings.HasSuffix(spec.Path, "/") && strings.HasPrefix(file, spec.Path)) {
			return true
		}
	}
	return false
}

var mediaTypes = map[string]string{
	".md": "text/markdown", ".markdown": "text/markdown", ".txt": "text/plain", ".yaml": "application/yaml",
	".yml": "application/yaml", ".json": "application/json", ".sql": "application/sql", ".dbml": "text/plain",
	".mmd": "text/plain", ".csv": "text/csv", ".html": "text/html", ".htm": "text/html", ".css": "text/css",
	".js": "text/javascript", ".mjs": "text/javascript", ".svg": "image/svg+xml", ".png": "image/png",
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".woff": "font/woff",
	".woff2": "font/woff2", ".ico": "image/x-icon",
}

// MediaTypeOf is the media type of an attachment by its extension; an unknown extension is text/plain
// for UTF-8 text, else application/octet-stream.
func MediaTypeOf(file string, content []byte) string {
	if media, ok := mediaTypes[strings.ToLower(path.Ext(file))]; ok {
		return media
	}
	if utf8.Valid(content) {
		return "text/plain"
	}
	return "application/octet-stream"
}

// textAttachmentTypes are the attachments getArtifactAttachment serves as text; HTML, scripts, styles,
// images and fonts of the prototype only leave through the content origin (ADR-19, ER-21-5).
var textAttachmentTypes = map[string]bool{
	"text/markdown": true, "text/plain": true, "application/yaml": true, "application/json": true,
	"application/sql": true, "text/csv": true,
}

// TextAttachment reports whether the attachment is served as text by getArtifactAttachment.
func TextAttachment(mediaType string) bool { return textAttachmentTypes[mediaType] }

// FindAttachment returns the attachment of the version with the shown path.
func FindAttachment(attachments []Attachment, shown string) (Attachment, bool) {
	for _, a := range attachments {
		if a.Path == shown {
			return a, true
		}
	}
	return Attachment{}, false
}
