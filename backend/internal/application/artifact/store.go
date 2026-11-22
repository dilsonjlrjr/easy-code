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
	// SetState moves the artifact (machine artifact) and keeps the header "estado" of its current
	// version in step (a follow-up field, mutable even after the commit).
	SetState(ctx context.Context, projectID, code, state, headerState string) error
	// InReview lists the current versions in review of a stage.
	InReview(ctx context.Context, projectID, stage string) ([]Stored, error)
	// CurrentHeader returns the header of the current version.
	CurrentHeader(ctx context.Context, projectID, code string) (map[string]any, error)
	// CurrentHeaders returns code → header of the current version of every artifact of the type that is
	// not superseded (traceability checks of the gate).
	CurrentHeaders(ctx context.Context, projectID, artifactType string) (map[string]map[string]any, error)
	// Current returns the current version (nil when the artifact does not exist).
	Current(ctx context.Context, projectID, code string) (*Current, error)
	// MergeHeader merges fields into the header of the current version (tracking fields, allowed by
	// artifact_versions_immutable even after the commit).
	MergeHeader(ctx context.Context, projectID, code string, fields map[string]any) error
	// Reopen creates the next version of an approved artifact in draft for a finding rework (copy of the
	// current one with the history entry of the finding, owned by the rework wave, US-34 CA-1).
	Reopen(ctx context.Context, version NewVersion, finding string) error
}

// Current is the current version of an artifact.
type Current struct {
	Code    string
	Type    string
	State   string
	Version int
	Path    string
	Author  string
	Header  map[string]any
	Body    string
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

// Header states of the common header (cabecalho-comum: estado).
var headerStates = map[string]string{
	domainartifact.StateDraft: "rascunho", domainartifact.StateInReview: "em_revisao",
	domainartifact.StateApproved: "aprovado", domainartifact.StateReplaced: "substituido",
}

// SetState applies an artifact transition decided by the orchestrator (gate approved or rejected).
func (s *Store) SetState(ctx context.Context, projectID, code, state string) error {
	return s.repo.SetState(ctx, projectID, code, state, headerStates[state])
}

// InReview lists the versions in review of a stage (outputs of the gate round).
func (s *Store) InReview(ctx context.Context, projectID, stage string) ([]Stored, error) {
	return s.repo.InReview(ctx, projectID, stage)
}

// SubmitForReview moves a stored version to in_review when its wave completes (draft →
// in_review, guard: valid header, history entry of the version; US-17 CA-1).
func (s *Store) SubmitForReview(ctx context.Context, projectID, waveCode, agent string, stored Stored, correlation string) error {
	if err := s.SetState(ctx, projectID, stored.Code, domainartifact.StateInReview); err != nil {
		return err
	}
	data, err := json.Marshal(map[string]any{
		"artifact": stored.Code, "type": stored.Type, "version": stored.Version, "stage": domainartifact.OwnerStage(stored.Type),
		"transition": map[string]any{"machine": "artifact", "from_state": domainartifact.StateDraft, "to_state": domainartifact.StateInReview},
		"actor":      map[string]any{"type": "agent", "id": agent}, "wave": waveCode,
	})
	if err != nil {
		return err
	}
	_, err = s.outbox.Append(ctx, domainevent.Draft{Type: "br.easycode.artifact.submitted_for_review.v1", Subject: "artifacts/" + stored.Code,
		ProjectID: projectID, WaveID: waveCode, CorrelationID: correlation, Data: data})
	return err
}

// Headers returns the current headers of the artifacts of a type (traceability checks of the gate round).
func (s *Store) Headers(ctx context.Context, projectID, artifactType string) (map[string]map[string]any, error) {
	return s.repo.CurrentHeaders(ctx, projectID, artifactType)
}

// Header returns the header of the current version (checks of the gate round).
func (s *Store) Header(ctx context.Context, projectID, code string) (map[string]any, error) {
	return s.repo.CurrentHeader(ctx, projectID, code)
}

// Current returns the current version of an artifact (nil when it does not exist).
func (s *Store) Current(ctx context.Context, projectID, code string) (*Current, error) {
	return s.repo.Current(ctx, projectID, code)
}

// UpdateTracking merges tracking fields into the current version (T of the Dev, CT.execucao), without a
// new version: the artifact stays approved (estados.yaml tracking_fields).
func (s *Store) UpdateTracking(ctx context.Context, projectID, code string, fields map[string]any) error {
	return s.repo.MergeHeader(ctx, projectID, code, fields)
}

// Reopen takes an approved artifact back to draft in a new version for the rework of a finding: the
// version copies the current one, cites the finding in the history and belongs to the rework wave, which
// rewrites it (artifact machine approved → draft, US-34 CA-1). It returns the new version.
func (s *Store) Reopen(ctx context.Context, projectID, code, waveCode, agent, finding string) (int, error) {
	current, err := s.repo.Current(ctx, projectID, code)
	if err != nil || current == nil {
		return 0, err
	}
	header := map[string]any{}
	for k, v := range current.Header {
		header[k] = v
	}
	next := current.Version + 1
	history, _ := header["historico"].([]any)
	header["historico"] = append(append([]any{}, history...), map[string]any{"versao": next, "data": s.now().UTC().Format("2006-01-02"),
		"onda": waveCode, "descricao": "reaberto pelo achado " + finding, "achado": finding})
	header["versao"], header["estado"] = next, headerStates[domainartifact.StateDraft]
	encoded, err := json.Marshal(header)
	if err != nil {
		return 0, err
	}
	sum := sha256.Sum256(append(encoded, []byte(current.Body)...))
	return next, s.repo.Reopen(ctx, NewVersion{ProjectID: projectID, Code: code, Type: current.Type, Version: next, WaveCode: waveCode, Author: agent,
		Description: "reaberto pelo achado " + finding, Path: current.Path, Header: encoded, Body: current.Body, ContentHash: hex.EncodeToString(sum[:])}, finding)
}
