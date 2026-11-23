package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appartifact "easycode/internal/application/artifact"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// ArtifactEdits implements appartifact.Edits (api role) and appartifact.EditWork (worker role) over
// artifact_edits and the edit_artifact commands (US-53).
type ArtifactEdits struct {
	pool *pgxpool.Pool
}

var (
	_ appartifact.Edits    = ArtifactEdits{}
	_ appartifact.EditWork = ArtifactEdits{}
)

// NewArtifactEdits builds the adapter.
func NewArtifactEdits(pool *pgxpool.Pool) ArtifactEdits { return ArtifactEdits{pool: pool} }

// State implements appartifact.Edits.
func (e ArtifactEdits) State(ctx context.Context, projectID, code, stage string) (appartifact.EditState, error) {
	var out appartifact.EditState
	var artifactState, artifactType *string
	var current *int
	err := conn(ctx, e.pool).QueryRow(WithOperation(ctx, "artifact_edits.state"), `
SELECT coalesce((SELECT s.state FROM easycode.stages s WHERE s.project_id = pr.id AND s.stage = $3), ''),
       (SELECT count(*) FROM easycode.waves w WHERE w.project_id = pr.id AND w.stage = $3
         AND w.state IN ('queued','running','awaiting_user','pausing','paused')),
       EXISTS (SELECT 1 FROM easycode.gate_rounds r WHERE r.project_id = pr.id AND r.stage = $3 AND r.state IN ('evaluating','awaiting_human')),
       a.state, a.type, a.current_version
  FROM easycode.projects pr LEFT JOIN easycode.artifacts a ON a.project_id = pr.id AND a.code = $2
 WHERE pr.public_id = $1::uuid`, projectID, code, stage).Scan(&out.StageState, &out.ActiveWaves, &out.OpenRound, &artifactState, &artifactType, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return out, fmt.Errorf("edit state: %w", classify(err))
	}
	if artifactState != nil {
		out.Exists, out.State, out.Type, out.CurrentVersion = true, *artifactState, *artifactType, *current
	}
	return out, nil
}

// Request implements appartifact.Edits: the command edit_artifact (target artifact) and the edit with its text.
func (e ArtifactEdits) Request(ctx context.Context, p identity.Principal, projectID string, edit appartifact.EditRequest, key, bodyHash string) (string, string, error) {
	commandID, err := uuid.NewV7()
	if err != nil {
		return "", "", err
	}
	editID := uuid.New().String()
	payload, _ := json.Marshal(map[string]string{"edit": editID})
	q := conn(ctx, e.pool)
	_, err = q.Exec(WithOperation(ctx, "artifact_edits.request"), `
WITH c AS (
  INSERT INTO easycode.commands (public_id, organization_id, project_id, target_type, artifact_id, type, payload, requested_by, origin, idempotency_key, body_hash)
  SELECT $4::uuid, pr.organization_id, pr.id, 'artifact', (SELECT a.id FROM easycode.artifacts a WHERE a.project_id = pr.id AND a.code = nullif($7, '')),
         'edit_artifact', $5::jsonb, `+principalUserIDSQL+`, 'web', $12::uuid, $13
    FROM easycode.projects pr WHERE pr.public_id = $3::uuid
  RETURNING id, project_id, requested_by)
INSERT INTO easycode.artifact_edits (public_id, project_id, command_id, operation, artifact_type, artifact_code, base_version, content, reason, requested_by)
SELECT $14::uuid, c.project_id, c.id, $6, $8, nullif($7, ''), nullif($9, 0), nullif($10, ''), $11, c.requested_by FROM c`,
		p.OrganizationID, p.Subject, projectID, commandID.String(), string(payload), edit.Operation, edit.Artifact, edit.Type, edit.BaseVersion,
		edit.Content, edit.Reason, key, bodyHash, editID)
	if err != nil {
		return "", "", fmt.Errorf("request artifact edit: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "commands.notify"), "SELECT pg_notify($1, $2)", ChannelCommands, projectID); err != nil {
		return "", "", fmt.Errorf("notify commands: %w", err)
	}
	return editID, commandID.String(), nil
}

// Get implements appartifact.Edits: the outcome is read from the command and the review wave — applied when the
// wave wrote a version, refused when the command was refused or the wave ended without writing (CA-2).
func (e ArtifactEdits) Get(ctx context.Context, projectID, id string) (appartifact.EditView, error) {
	var v appartifact.EditView
	var commandStatus, refusal, waveState, waveCode string
	var version int
	err := conn(ctx, e.pool).QueryRow(WithOperation(ctx, "artifact_edits.get"), `
SELECT ed.public_id::text, c.public_id::text, ed.operation, ed.artifact_type, coalesce(ed.artifact_code, ''), c.status, coalesce(c.refusal_reason, ''),
       coalesce(w.state, ''), coalesce(w.code, ''),
       coalesce((SELECT v.version FROM easycode.artifact_versions v WHERE v.wave_id = w.id AND v.author = 'human' ORDER BY v.version DESC LIMIT 1), 0)
  FROM easycode.artifact_edits ed JOIN easycode.projects pr ON pr.id = ed.project_id
  JOIN easycode.commands c ON c.id = ed.command_id LEFT JOIN easycode.waves w ON w.id = ed.wave_id
 WHERE pr.public_id = $1::uuid AND ed.public_id = $2::uuid`, projectID, id).
		Scan(&v.ID, &v.Command, &v.Operation, &v.Type, &v.Artifact, &commandStatus, &refusal, &waveState, &waveCode, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return v, fmt.Errorf("artifact edit: %w", classify(err))
	}
	v.Wave, v.Version = waveCode, version
	switch {
	case commandStatus == "refused":
		v.State, v.Refusal = appartifact.EditRefused, refusal
	case waveCode == "":
		v.State = appartifact.EditPending
	case version > 0 && waveState == "completed":
		v.State = appartifact.EditApplied
	case waveState == "completed" || waveState == "failed" || waveState == "canceled":
		v.State, v.Refusal = appartifact.EditRefused, "not_validated_by_agent"
	default:
		v.State = appartifact.EditReviewing
	}
	return v, nil
}

// Pending implements appartifact.EditWork.
func (e ArtifactEdits) Pending(ctx context.Context, id string) (appartifact.PendingEdit, error) {
	var p appartifact.PendingEdit
	err := conn(ctx, e.pool).QueryRow(WithOperation(ctx, "artifact_edits.pending"), `
SELECT ed.public_id::text, pr.public_id::text, ed.operation, ed.artifact_type, coalesce(ed.artifact_code, ''), coalesce(ed.base_version, 0), u.sub,
       coalesce(w.code, '')
  FROM easycode.artifact_edits ed JOIN easycode.projects pr ON pr.id = ed.project_id JOIN easycode.users u ON u.id = ed.requested_by
  LEFT JOIN easycode.waves w ON w.id = ed.wave_id
 WHERE ed.public_id = $1::uuid`, id).Scan(&p.ID, &p.ProjectID, &p.Operation, &p.Type, &p.Artifact, &p.BaseVersion, &p.Editor, &p.Wave)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return p, fmt.Errorf("pending artifact edit: %w", classify(err))
	}
	return p, nil
}

// InvalidateOpenRound implements appartifact.EditWork.
func (e ArtifactEdits) InvalidateOpenRound(ctx context.Context, projectID, stage string) (int, string, error) {
	var number int
	var from string
	err := conn(ctx, e.pool).QueryRow(WithOperation(ctx, "gate_rounds.invalidate_by_edit"), `
WITH open AS (SELECT x.id, x.state FROM easycode.gate_rounds x JOIN easycode.projects pr ON pr.id = x.project_id
               WHERE pr.public_id = $1::uuid AND x.stage = $2 AND x.state IN ('evaluating','awaiting_human') ORDER BY x.number DESC LIMIT 1 FOR UPDATE)
UPDATE easycode.gate_rounds r SET state = 'invalidated', invalidated_at = now() FROM open WHERE r.id = open.id RETURNING r.number, open.state`,
		projectID, stage).Scan(&number, &from)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("invalidate round by edit: %w", classify(err))
	}
	return number, from, nil
}

// LinkWave implements appartifact.EditWork.
func (e ArtifactEdits) LinkWave(ctx context.Context, editID, waveCode string) error {
	_, err := conn(ctx, e.pool).Exec(WithOperation(ctx, "artifact_edits.link_wave"), `
UPDATE easycode.artifact_edits ed SET wave_id = w.id FROM easycode.waves w
 WHERE ed.public_id = $1::uuid AND w.project_id = ed.project_id AND w.code = $2`, editID, waveCode)
	if err != nil {
		return fmt.Errorf("link edit wave: %w", classify(err))
	}
	return nil
}
