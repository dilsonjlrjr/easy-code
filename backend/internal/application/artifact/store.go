// Package artifact holds the artifact use cases: the store the agent runtime calls to record a
// version, which validates it against the schema of its type first (US-29), and the reads of
// the api (US-26).
package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"easycode/internal/application/event"
	"easycode/internal/application/transaction"
	domainartifact "easycode/internal/domain/artifact"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
)

// Validator checks a header against the JSON schema of its type (specs/dominio/artefatos).
type Validator interface {
	// Validate returns the schema errors; an unknown type is an error.
	Validate(artifactType string, header map[string]any) ([]domainartifact.SchemaError, error)
}

// HeaderParser turns the YAML front matter into a JSON object (dates stay text).
type HeaderParser interface {
	ParseHeader(yamlText string) (map[string]any, error)
}

// Latest is the current version of an artifact in a project.
type Latest struct {
	Version   int
	WaveCode  string
	Committed bool // has a git commit: immutable
	Type      string
}

// NewVersion is what the repository stores.
type NewVersion struct {
	ProjectID   string
	Code        string
	Type        string
	Title       string
	Version     int
	Rewrite     bool // same uncommitted version of the same wave: rewritten in place
	WaveCode    string
	Author      string
	Description string
	Path        string
	Header      json.RawMessage
	Body        string
	ContentHash string
}

// Repository stores artifacts and versions (worker role).
type Repository interface {
	// Latest returns the current version, or nil when the artifact does not exist yet.
	Latest(ctx context.Context, projectID, code string) (*Latest, error)
	// Save creates or updates the artifact index and writes the version.
	Save(ctx context.Context, version NewVersion) error
}

// Input is one artifact file written by an agent in a wave.
type Input struct {
	ProjectID   string
	WaveCode    string // REQ-01
	Agent       string // author: requirements-agent…
	Path        string // path in the project repository
	Content     []byte // the whole file: YAML front matter and Markdown body
	Description string // historico entry of this version
	// CorrelationID ties the artifact.created event to the wave flow; empty creates a new one.
	CorrelationID string
}

// Stored is the recorded version.
type Stored struct {
	Code    string
	Type    string
	Version int
	Created bool
}

// Store validates and records artifact versions. It is the port the agent runtime calls when
// an agent writes an artifact (US-17 → US-29 → US-26).
type Store struct {
	tx        transaction.Runner
	parser    HeaderParser
	validator Validator
	repo      Repository
	outbox    event.Outbox
	now       func() time.Time
}

// NewStore builds the store.
func NewStore(tx transaction.Runner, parser HeaderParser, validator Validator, repo Repository, outbox event.Outbox) *Store {
	return &Store{tx: tx, parser: parser, validator: validator, repo: repo, outbox: outbox, now: time.Now}
}

// Parse splits and validates a file without storing it. A file out of its schema fails with
// artifact-invalid and the list of errors in the "schema_errors" extension (US-29 CA-1).
func (s *Store) Parse(content []byte) (domainartifact.Document, string, error) {
	headerText, body, ok := domainartifact.Split(string(content))
	if !ok {
		return domainartifact.Document{}, "", invalid(domainartifact.SchemaError{Message: "front matter (---) with the artifact header is missing"})
	}
	header, err := s.parser.ParseHeader(headerText)
	if err != nil {
		return domainartifact.Document{}, "", invalid(domainartifact.SchemaError{Message: "header is not valid YAML: " + err.Error()})
	}
	artifactType, _ := header["tipo"].(string)
	if domainartifact.OwnerStage(artifactType) == "" && artifactType != "ACH" {
		return domainartifact.Document{}, "", invalid(domainartifact.SchemaError{Path: "/tipo", Message: fmt.Sprintf("unknown artifact type %q", artifactType)})
	}
	errors, err := s.validator.Validate(artifactType, header)
	if err != nil {
		return domainartifact.Document{}, "", err
	}
	if len(errors) > 0 {
		return domainartifact.Document{}, "", invalid(errors...)
	}
	return domainartifact.Document{Header: header, Body: body}, artifactType, nil
}

// StoreVersion validates the file and records it as the next version of its artifact (US-29
// CA-3): a new artifact starts at version 1; a version of the same wave not yet committed is
// rewritten in place (the agent may correct it in the wave, CA-1). The header "versao" must be
// the version being written. Call it inside the wave transaction or alone.
func (s *Store) StoreVersion(ctx context.Context, in Input) (Stored, error) {
	document, artifactType, err := s.Parse(in.Content)
	if err != nil {
		return Stored{}, err
	}
	code, _ := document.Header["id"].(string)
	title, _ := document.Header["titulo"].(string)
	var stored Stored
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		latest, err := s.repo.Latest(ctx, in.ProjectID, code)
		if err != nil {
			return err
		}
		next := NewVersion{
			ProjectID: in.ProjectID, Code: code, Type: artifactType, Title: title, Version: 1, WaveCode: in.WaveCode,
			Author: in.Agent, Description: strings.TrimSpace(in.Description), Path: in.Path, Body: document.Body,
		}
		switch {
		case latest == nil:
		case latest.Type != artifactType:
			return invalid(domainartifact.SchemaError{Path: "/tipo", Message: "type differs from the existing artifact " + code})
		case !latest.Committed && latest.WaveCode == in.WaveCode:
			next.Version, next.Rewrite = latest.Version, true
		default:
			next.Version = latest.Version + 1
		}
		if version, ok := number(document.Header["versao"]); !ok || version != next.Version {
			return invalid(domainartifact.SchemaError{Path: "/versao", Message: fmt.Sprintf("must be %d, the version being written", next.Version)})
		}
		if len(next.Description) < 3 {
			next.Description = historyDescription(document.Header, next.Version)
		}
		if next.Header, err = json.Marshal(document.Header); err != nil {
			return err
		}
		sum := sha256.Sum256(in.Content)
		next.ContentHash = hex.EncodeToString(sum[:])
		if err := s.repo.Save(ctx, next); err != nil {
			return err
		}
		stored = Stored{Code: code, Type: artifactType, Version: next.Version, Created: latest == nil}
		if latest != nil {
			return nil
		}
		return s.created(ctx, in, stored)
	})
	return stored, err
}

func (s *Store) created(ctx context.Context, in Input, stored Stored) error {
	data, err := json.Marshal(map[string]any{
		"artifact": stored.Code, "type": stored.Type, "version": stored.Version, "stage": domainartifact.OwnerStage(stored.Type),
		"transition": map[string]any{"machine": "artifact", "from_state": nil, "to_state": domainartifact.StateDraft},
		"actor":      map[string]any{"type": "agent", "id": in.Agent}, "author": in.Agent, "wave": in.WaveCode,
	})
	if err != nil {
		return err
	}
	correlation := in.CorrelationID
	if correlation == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		correlation = id.String()
	}
	_, err = s.outbox.Append(ctx, domainevent.Draft{
		Type: "br.easycode.artifact.created.v1", Subject: "artifacts/" + stored.Code, ProjectID: in.ProjectID,
		WaveID: in.WaveCode, CorrelationID: correlation, Data: data,
	})
	return err
}

// historyDescription takes the description of the header history entry of the version.
func historyDescription(header map[string]any, version int) string {
	entries, _ := header["historico"].([]any)
	for _, entry := range entries {
		item, _ := entry.(map[string]any)
		if n, ok := number(item["versao"]); ok && n == version {
			if text, _ := item["descricao"].(string); len(strings.TrimSpace(text)) >= 3 {
				return strings.TrimSpace(text)
			}
		}
	}
	return fmt.Sprintf("versão %d", version)
}

func number(value any) (int, bool) {
	switch n := value.(type) {
	case float64:
		return int(n), n == float64(int(n))
	case int64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func invalid(errors ...domainartifact.SchemaError) error {
	return failure.New(failure.KindInvalidInput, domainartifact.CodeInvalid, nil).With("schema_errors", errors)
}
