package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/domain/failure"
)

// Artifacts stores artifact versions (worker role) and reads them (api role).
type Artifacts struct {
	pool *pgxpool.Pool
}

var (
	_ appartifact.Repository = Artifacts{}
	_ appartifact.Reader     = Artifacts{}
)

// NewArtifacts builds the adapter.
func NewArtifacts(pool *pgxpool.Pool) Artifacts { return Artifacts{pool: pool} }

// Latest implements appartifact.Repository.
func (a Artifacts) Latest(ctx context.Context, projectID, code string) (*appartifact.Latest, error) {
	var latest appartifact.Latest
	err := conn(ctx, a.pool).QueryRow(WithOperation(ctx, "artifacts.latest"), `
SELECT v.version, v.wave_code, v.git_commit IS NOT NULL, ar.type
  FROM easycode.artifacts ar JOIN easycode.projects pr ON pr.id = ar.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = ar.id AND v.version = ar.current_version
 WHERE pr.public_id = $1::uuid AND ar.code = $2
 FOR UPDATE OF ar`, projectID, code).Scan(&latest.Version, &latest.WaveCode, &latest.Committed, &latest.Type)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest artifact version: %w", classify(err))
	}
	return &latest, nil
}

// Save implements appartifact.Repository.
func (a Artifacts) Save(ctx context.Context, v appartifact.NewVersion) error {
	q := conn(ctx, a.pool)
	var projectID, waveID int64
	err := q.QueryRow(WithOperation(ctx, "artifacts.resolve_wave"), `
SELECT pr.id, w.id FROM easycode.projects pr JOIN easycode.waves w ON w.project_id = pr.id AND w.code = $2
 WHERE pr.public_id = $1::uuid`, v.ProjectID, v.WaveCode).Scan(&projectID, &waveID)
	if errors.Is(err, pgx.ErrNoRows) {
		return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return fmt.Errorf("resolve wave: %w", classify(err))
	}
	var artifactID int64
	err = q.QueryRow(WithOperation(ctx, "artifacts.upsert"), `
INSERT INTO easycode.artifacts (project_id, code, type, title, current_version) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (project_id, code) DO UPDATE SET title = excluded.title, current_version = excluded.current_version, updated_at = now()
RETURNING id`, projectID, v.Code, v.Type, v.Title, v.Version).Scan(&artifactID)
	if err != nil {
		return fmt.Errorf("save artifact: %w", classify(err))
	}
	if v.Rewrite {
		_, err = q.Exec(WithOperation(ctx, "artifact_versions.rewrite"), `
UPDATE easycode.artifact_versions SET description = $3, path = $4, header = $5::jsonb, body = $6, content_hash = $7, created_at = now()
 WHERE artifact_id = $1 AND version = $2 AND git_commit IS NULL`, artifactID, v.Version, v.Description, v.Path, string(v.Header), v.Body, v.ContentHash)
	} else {
		_, err = q.Exec(WithOperation(ctx, "artifact_versions.insert"), `
INSERT INTO easycode.artifact_versions (artifact_id, version, wave_id, author, wave_code, description, path, header, body, content_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)`,
			artifactID, v.Version, waveID, v.Author, v.WaveCode, v.Description, v.Path, string(v.Header), v.Body, v.ContentHash)
	}
	if err != nil {
		return fmt.Errorf("save artifact version: %w", classify(err))
	}
	return nil
}

const summaryColumns = `ar.code, ar.type, ar.title, ar.state, v.version, coalesce(ar.owner_stage, ''), v.author, v.wave_code, v.path`

// List implements appartifact.Reader (order by type and id).
func (a Artifacts) List(ctx context.Context, projectID string, filter appartifact.Filter) (appartifact.Page, error) {
	afterType, afterCode := "", ""
	if filter.Cursor != "" {
		_, raw, err := decodeCursor(filter.Cursor)
		if err != nil {
			return appartifact.Page{}, failure.Invalid(failure.FieldError{Field: "cursor", Message: "invalid"})
		}
		afterType, afterCode = splitPair(raw)
	}
	rows, err := conn(ctx, a.pool).Query(WithOperation(ctx, "artifacts.list"), `
SELECT `+summaryColumns+`, ar.updated_at, CASE WHEN ar.type = 'T' THEN coalesce(v.header->>'situacao', '') ELSE '' END
  FROM easycode.artifacts ar JOIN easycode.projects pr ON pr.id = ar.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = ar.id AND v.version = ar.current_version
 WHERE pr.public_id = $1::uuid
   AND (coalesce(cardinality($2::text[]), 0) = 0 OR ar.type = ANY($2::text[]))
   AND ($3 = '' OR ar.owner_stage = $3)
   AND (coalesce(cardinality($4::text[]), 0) = 0 OR ar.state = ANY($4::text[]))
   AND ($5 = '' OR ar.title ILIKE '%' || $5 || '%' OR v.body_tsv @@ plainto_tsquery('portuguese', $5))
   AND ($6 = '' OR (ar.type, ar.code) > ($6, $7))
 ORDER BY ar.type, ar.code
 LIMIT $8`, projectID, filter.Types, filter.Stage, filter.States, filter.Search, afterType, afterCode, filter.Limit+1)
	if err != nil {
		return appartifact.Page{}, fmt.Errorf("list artifacts: %w", classify(err))
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appartifact.Summary, error) {
		var s appartifact.Summary
		err := row.Scan(&s.Code, &s.Type, &s.Title, &s.State, &s.Version, &s.Stage, &s.Author, &s.WaveCode, &s.Path, &s.UpdatedAt, &s.Situation)
		return s, err
	})
	if err != nil {
		return appartifact.Page{}, fmt.Errorf("scan artifacts: %w", err)
	}
	page := appartifact.Page{Items: items}
	if len(items) > filter.Limit {
		last := items[filter.Limit-1]
		page.Items = items[:filter.Limit]
		page.NextCursor = encodeCursor(time.Unix(0, 0), last.Type+"|"+last.Code)
	}
	return page, nil
}

func splitPair(raw string) (string, string) {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '|' {
			return raw[:i], raw[i+1:]
		}
	}
	return raw, ""
}

// Version implements appartifact.Reader (0 = current version).
func (a Artifacts) Version(ctx context.Context, projectID, code string, version int) (appartifact.View, error) {
	var view appartifact.View
	var commit *string
	err := conn(ctx, a.pool).QueryRow(WithOperation(ctx, "artifacts.version"), `
SELECT `+summaryColumns+`, ar.updated_at, v.header, v.body, v.git_commit
  FROM easycode.artifacts ar JOIN easycode.projects pr ON pr.id = ar.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = ar.id AND v.version = CASE WHEN $3 = 0 THEN ar.current_version ELSE $3 END
 WHERE pr.public_id = $1::uuid AND ar.code = $2`, projectID, code, version).Scan(&view.Code, &view.Type, &view.Title, &view.State,
		&view.Version, &view.Stage, &view.Author, &view.WaveCode, &view.Path, &view.UpdatedAt, &view.Header, &view.Body, &commit)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return view, fmt.Errorf("read artifact version: %w", classify(err))
	}
	if commit != nil {
		view.Commit = *commit
	}
	return view, nil
}

// Versions implements appartifact.Reader (descending version).
func (a Artifacts) Versions(ctx context.Context, projectID, code string) ([]appartifact.VersionSummary, error) {
	rows, err := conn(ctx, a.pool).Query(WithOperation(ctx, "artifacts.versions"), `
SELECT v.version, v.created_at, v.wave_code, v.author, v.description,
       ar.state = 'approved' AND v.version = ar.current_version, coalesce(v.git_commit, '')
  FROM easycode.artifacts ar JOIN easycode.projects pr ON pr.id = ar.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = ar.id
 WHERE pr.public_id = $1::uuid AND ar.code = $2
 ORDER BY v.version DESC`, projectID, code)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", classify(err))
	}
	versions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appartifact.VersionSummary, error) {
		var s appartifact.VersionSummary
		err := row.Scan(&s.Version, &s.CreatedAt, &s.WaveCode, &s.Author, &s.Description, &s.Approved, &s.Commit)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan versions: %w", err)
	}
	if len(versions) == 0 {
		return nil, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return versions, nil
}

// SetState implements appartifact.Repository.
func (a Artifacts) SetState(ctx context.Context, projectID, code, state, headerState string) error {
	q := conn(ctx, a.pool)
	var artifactID int64
	var version int
	err := q.QueryRow(WithOperation(ctx, "artifacts.set_state"), `
UPDATE easycode.artifacts ar SET state = $3, updated_at = now() FROM easycode.projects pr
 WHERE pr.id = ar.project_id AND pr.public_id = $1::uuid AND ar.code = $2 AND ar.state <> $3
RETURNING ar.id, ar.current_version`, projectID, code, state).Scan(&artifactID, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already in the state
	}
	if err != nil {
		return fmt.Errorf("artifact %s to %s: %w", code, state, classify(err))
	}
	if headerState == "" {
		return nil
	}
	_, err = q.Exec(WithOperation(ctx, "artifact_versions.header_state"),
		"UPDATE easycode.artifact_versions SET header = jsonb_set(header, '{estado}', to_jsonb($3::text)) WHERE artifact_id = $1 AND version = $2",
		artifactID, version, headerState)
	if err != nil {
		return fmt.Errorf("artifact header state: %w", classify(err))
	}
	return nil
}

// InReview implements appartifact.Repository.
func (a Artifacts) InReview(ctx context.Context, projectID, stage string) ([]appartifact.Stored, error) {
	rows, err := conn(ctx, a.pool).Query(WithOperation(ctx, "artifacts.in_review"), `
SELECT ar.code, ar.type, ar.current_version FROM easycode.artifacts ar JOIN easycode.projects pr ON pr.id = ar.project_id
 WHERE pr.public_id = $1::uuid AND ar.owner_stage = $2 AND ar.state = 'in_review' ORDER BY ar.type, ar.code`, projectID, stage)
	if err != nil {
		return nil, fmt.Errorf("artifacts in review: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appartifact.Stored, error) {
		var s appartifact.Stored
		return s, row.Scan(&s.Code, &s.Type, &s.Version)
	})
}

// CurrentHeader implements appartifact.Repository.
func (a Artifacts) CurrentHeader(ctx context.Context, projectID, code string) (map[string]any, error) {
	view, err := a.Version(ctx, projectID, code, 0)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(view.Header))
	decoder.UseNumber()
	var header map[string]any
	if err := decoder.Decode(&header); err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	return header, nil
}

// CurrentHeaders implements appartifact.Repository.
func (a Artifacts) CurrentHeaders(ctx context.Context, projectID, artifactType string) (map[string]map[string]any, error) {
	rows, err := conn(ctx, a.pool).Query(WithOperation(ctx, "artifacts.current_headers"), `
SELECT ar.code, v.header FROM easycode.artifacts ar JOIN easycode.projects pr ON pr.id = ar.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = ar.id AND v.version = ar.current_version
 WHERE pr.public_id = $1::uuid AND ar.type = $2 AND ar.state <> 'replaced'`, projectID, artifactType)
	if err != nil {
		return nil, fmt.Errorf("current headers: %w", classify(err))
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		var code string
		var raw []byte
		if err := rows.Scan(&code, &raw); err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var header map[string]any
		if err := decoder.Decode(&header); err != nil {
			return nil, fmt.Errorf("decode header: %w", err)
		}
		out[code] = header
	}
	return out, rows.Err()
}
