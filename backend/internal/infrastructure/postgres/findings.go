package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appfinding "easycode/internal/application/finding"
	"easycode/internal/domain/failure"
	domainfinding "easycode/internal/domain/finding"
	"easycode/internal/domain/identity"
)

// Findings stores the finding machine (findings, finding_cycles, finding_escalations) for the worker
// and reads it for the api.
type Findings struct {
	pool *pgxpool.Pool
}

var _ appfinding.Store = Findings{}

// NewFindings builds the adapter.
func NewFindings(pool *pgxpool.Pool) Findings { return Findings{pool: pool} }

const findingByCode = `(SELECT f.artifact_id FROM easycode.findings f JOIN easycode.artifacts a ON a.id = f.artifact_id
  JOIN easycode.projects pr ON pr.id = f.project_id WHERE pr.public_id = $1::uuid AND a.code = $2)`

const waveByCode = `(SELECT w.id FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id WHERE pr.public_id = $1::uuid AND w.code = `

// Insert implements appfinding.Store: the finding of an ACH artifact, open, with its affected artifacts.
func (s Findings) Insert(ctx context.Context, f appfinding.NewFinding) error {
	q := conn(ctx, s.pool)
	var id int64
	err := q.QueryRow(WithOperation(ctx, "findings.insert"), `
INSERT INTO easycode.findings (artifact_id, project_id, origin_wave_id, origin_stage, destination_stage, severity, category)
SELECT a.id, pr.id, w.id, $4, $5, $6, $7 FROM easycode.projects pr JOIN easycode.artifacts a ON a.project_id = pr.id AND a.code = $2
  JOIN easycode.waves w ON w.project_id = pr.id AND w.code = $3
 WHERE pr.public_id = $1::uuid RETURNING artifact_id`, f.ProjectID, f.Code, f.OriginWave, f.OriginStage, f.Destination, f.Severity, f.Category).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert finding: %w", classify(err))
	}
	for _, ref := range f.Affected {
		_, err := q.Exec(WithOperation(ctx, "finding_affected_artifacts.insert"), `
INSERT INTO easycode.finding_affected_artifacts (finding_id, reference, artifact_id)
VALUES ($1, $2, (SELECT a.id FROM easycode.artifacts a JOIN easycode.findings f ON f.project_id = a.project_id WHERE f.artifact_id = $1 AND a.code = $3))
ON CONFLICT (finding_id, reference) DO NOTHING`, id, ref, domainfinding.ArtifactOf(ref))
		if err != nil {
			return fmt.Errorf("affected artifact: %w", classify(err))
		}
	}
	return nil
}

// Get implements appfinding.Store.
func (s Findings) Get(ctx context.Context, projectID, code string) (*domainfinding.Finding, error) {
	q := conn(ctx, s.pool)
	if _, err := q.Exec(WithOperation(ctx, "findings.lock"), `SELECT 1 FROM easycode.findings WHERE artifact_id = `+findingByCode+` FOR UPDATE`, projectID, code); err != nil {
		return nil, fmt.Errorf("lock finding: %w", classify(err))
	}
	findings, err := loadFindings(ctx, q, "findings.get", projectID, "ar.code = $2", code)
	if err != nil || len(findings) == 0 {
		return nil, err
	}
	return &findings[0], nil
}

// SetStatus implements appfinding.Store (orchestrator transitions of the machine finding).
func (s Findings) SetStatus(ctx context.Context, projectID, code, status, resolution, discardReason string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "findings.set_status"), `
UPDATE easycode.findings SET status = $3, resolution = nullif($4, ''), discard_reason = coalesce(nullif($5, ''), discard_reason), updated_at = now()
 WHERE artifact_id = `+findingByCode, projectID, code, status, resolution, discardReason)
	if err != nil {
		return fmt.Errorf("finding %s to %s: %w", code, status, classify(err))
	}
	return nil
}

// StartCycle implements appfinding.Store.
func (s Findings) StartCycle(ctx context.Context, projectID, code string, number int, reworkWave string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "finding_cycles.start"), `
INSERT INTO easycode.finding_cycles (finding_id, number, rework_wave_id) VALUES (`+findingByCode+`, $3, `+waveByCode+`$4))`,
		projectID, code, number, reworkWave)
	if err != nil {
		return fmt.Errorf("start cycle: %w", classify(err))
	}
	return nil
}

// ReplaceRework implements appfinding.Store.
func (s Findings) ReplaceRework(ctx context.Context, projectID, code string, number int, reworkWave string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "finding_cycles.replace_rework"), `
UPDATE easycode.finding_cycles SET rework_wave_id = `+waveByCode+`$4) WHERE finding_id = `+findingByCode+` AND number = $3`,
		projectID, code, number, reworkWave)
	if err != nil {
		return fmt.Errorf("replace rework wave: %w", classify(err))
	}
	return nil
}

// SendToRevalidation implements appfinding.Store.
func (s Findings) SendToRevalidation(ctx context.Context, projectID, code string, number int, commit, revalidationWave string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "finding_cycles.revalidation"), `
UPDATE easycode.finding_cycles SET commit_or_version = $4, revalidation_wave_id = `+waveByCode+`$5)
 WHERE finding_id = `+findingByCode+` AND number = $3`, projectID, code, number, commit, revalidationWave)
	if err != nil {
		return fmt.Errorf("send to revalidation: %w", classify(err))
	}
	return nil
}

// RecordResult implements appfinding.Store.
func (s Findings) RecordResult(ctx context.Context, projectID, code string, number int, result string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "finding_cycles.result"), `
UPDATE easycode.finding_cycles SET result = $4, revalidated_at = now() WHERE finding_id = `+findingByCode+` AND number = $3`,
		projectID, code, number, result)
	if err != nil {
		return fmt.Errorf("record cycle result: %w", classify(err))
	}
	return nil
}

// Escalate implements appfinding.Store: a new escalation item without decision.
func (s Findings) Escalate(ctx context.Context, projectID, code, reason string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "finding_escalations.insert"), `
INSERT INTO easycode.finding_escalations (finding_id, number, reason)
SELECT f.id, coalesce((SELECT max(e.number) FROM easycode.finding_escalations e WHERE e.finding_id = f.id), 0) + 1, $3
  FROM (SELECT `+findingByCode+` AS id) f`, projectID, code, reason)
	if err != nil {
		return fmt.Errorf("escalate finding: %w", classify(err))
	}
	return nil
}

// SetModifiedArtifacts implements appfinding.Store.
func (s Findings) SetModifiedArtifacts(ctx context.Context, projectID, code string, artifacts []string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "findings.modified_artifacts"),
		`UPDATE easycode.findings SET modified_artifacts = $3 WHERE artifact_id = `+findingByCode, projectID, code, artifacts)
	if err != nil {
		return fmt.Errorf("modified artifacts: %w", classify(err))
	}
	return nil
}

// WaveFinding implements appfinding.Store.
func (s Findings) WaveFinding(ctx context.Context, projectID, wave string) (string, error) {
	var code string
	err := conn(ctx, s.pool).QueryRow(WithOperation(ctx, "waves.finding_code"), `
SELECT a.code FROM easycode.waves w JOIN easycode.projects pr ON pr.id = w.project_id JOIN easycode.artifacts a ON a.id = w.finding_id
 WHERE pr.public_id = $1::uuid AND w.code = $2`, projectID, wave).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("wave finding: %w", classify(err))
	}
	return code, nil
}

// ReworkWaves implements appfinding.Store.
func (s Findings) ReworkWaves(ctx context.Context, projectID, code string) (int, error) {
	var n int
	err := conn(ctx, s.pool).QueryRow(WithOperation(ctx, "waves.rework_count"),
		`SELECT count(*) FROM easycode.waves WHERE purpose = 'rework' AND finding_id = `+findingByCode, projectID, code).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("rework waves: %w", classify(err))
	}
	return n, nil
}

// StageState implements appfinding.Store.
func (s Findings) StageState(ctx context.Context, projectID, stage string) (string, error) {
	var state string
	err := conn(ctx, s.pool).QueryRow(WithOperation(ctx, "stages.state"), `
SELECT s.state FROM easycode.stages s JOIN easycode.projects pr ON pr.id = s.project_id WHERE pr.public_id = $1::uuid AND s.stage = $2`,
		projectID, stage).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("stage state: %w", classify(err))
	}
	return state, nil
}

// ReopenStage implements appfinding.Store.
func (s Findings) ReopenStage(ctx context.Context, projectID, stage, finding string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "stages.reopen"), `
UPDATE easycode.stages s SET state = 'reopened', reopened_by_finding_id = `+findingByCode+`, reevaluation_pending = false, updated_at = now()
  FROM easycode.projects pr WHERE pr.id = s.project_id AND pr.public_id = $1::uuid AND s.stage = $3`, projectID, finding, stage)
	if err != nil {
		return fmt.Errorf("reopen stage: %w", classify(err))
	}
	return nil
}

// InvalidateRound implements appfinding.Store.
func (s Findings) InvalidateRound(ctx context.Context, projectID, stage string, states []string) (int, error) {
	var number int
	err := conn(ctx, s.pool).QueryRow(WithOperation(ctx, "gate_rounds.invalidate"), `
UPDATE easycode.gate_rounds r SET state = 'invalidated', invalidated_at = now()
 WHERE r.id = (SELECT x.id FROM easycode.gate_rounds x JOIN easycode.projects pr ON pr.id = x.project_id
                WHERE pr.public_id = $1::uuid AND x.stage = $2 ORDER BY x.number DESC LIMIT 1)
   AND r.state = ANY($3) RETURNING r.number`, projectID, stage, states).Scan(&number)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("invalidate round: %w", classify(err))
	}
	return number, nil
}

// BlockStage implements appfinding.Store: the stage goes to blocked and its running waves to pausing
// (cooperative pause by policy at the next checkpoint).
func (s Findings) BlockStage(ctx context.Context, projectID, stage string) ([]string, error) {
	q := conn(ctx, s.pool)
	_, err := q.Exec(WithOperation(ctx, "stages.block"), `
UPDATE easycode.stages s SET state = 'blocked', updated_at = now() FROM easycode.projects pr
 WHERE pr.id = s.project_id AND pr.public_id = $1::uuid AND s.stage = $2`, projectID, stage)
	if err != nil {
		return nil, fmt.Errorf("block stage: %w", classify(err))
	}
	rows, err := q.Query(WithOperation(ctx, "waves.pause_by_policy"), `
UPDATE easycode.waves w SET state = 'pausing', pause_cause = 'dependency_reopened'
  FROM easycode.projects pr WHERE pr.id = w.project_id AND pr.public_id = $1::uuid AND w.stage = $2 AND w.state = 'running'
RETURNING w.code`, projectID, stage)
	if err != nil {
		return nil, fmt.Errorf("pause waves: %w", classify(err))
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// SetReevaluation implements appfinding.Store.
func (s Findings) SetReevaluation(ctx context.Context, projectID, stage string, pending bool) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "stages.reevaluation"), `
UPDATE easycode.stages s SET reevaluation_pending = $3, updated_at = now() FROM easycode.projects pr
 WHERE pr.id = s.project_id AND pr.public_id = $1::uuid AND s.stage = $2 AND (s.state = 'approved' OR NOT $3)`, projectID, stage, pending)
	if err != nil {
		return fmt.Errorf("reevaluation flag: %w", classify(err))
	}
	return nil
}

// ActiveWaves implements appfinding.Store.
func (s Findings) ActiveWaves(ctx context.Context, projectID, code string) ([]string, error) {
	rows, err := conn(ctx, s.pool).Query(WithOperation(ctx, "waves.of_finding_active"), `
SELECT w.code FROM easycode.waves w WHERE w.finding_id = `+findingByCode+`
   AND w.state IN ('queued','running','awaiting_user','pausing','paused','failed') ORDER BY w.id`, projectID, code)
	if err != nil {
		return nil, fmt.Errorf("finding waves: %w", classify(err))
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Pending implements appfinding.Store.
func (s Findings) Pending(ctx context.Context) (extra, closed, scope, returned []appfinding.Waiting, err error) {
	q := conn(ctx, s.pool)
	collect := func(operation, sql string) ([]appfinding.Waiting, error) {
		rows, err := q.Query(WithOperation(ctx, operation), sql)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", operation, classify(err))
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appfinding.Waiting, error) {
			var w appfinding.Waiting
			return w, row.Scan(&w.ProjectID, &w.Stage, &w.Finding)
		})
	}
	if extra, err = collect("findings.pending_extra_round", `
SELECT pr.public_id::text, f.destination_stage, a.code FROM easycode.findings f JOIN easycode.artifacts a ON a.id = f.artifact_id
  JOIN easycode.projects pr ON pr.id = f.project_id
 WHERE pr.state = 'in_progress' AND f.status = 'in_rework'
   AND NOT EXISTS (SELECT 1 FROM easycode.finding_cycles c WHERE c.finding_id = f.artifact_id AND c.result IS NULL)`); err != nil {
		return
	}
	if closed, err = collect("findings.pending_cancel", `
SELECT DISTINCT pr.public_id::text, f.origin_stage, a.code FROM easycode.findings f JOIN easycode.artifacts a ON a.id = f.artifact_id
  JOIN easycode.projects pr ON pr.id = f.project_id JOIN easycode.waves w ON w.finding_id = f.artifact_id
 WHERE f.status IN ('accepted','discarded') AND w.state IN ('queued','running','awaiting_user','pausing','paused','failed')`); err != nil {
		return
	}
	if scope, err = collect("findings.pending_scope", `
SELECT pr.public_id::text, 'requirements', a.code FROM easycode.findings f JOIN easycode.artifacts a ON a.id = f.artifact_id
  JOIN easycode.projects pr ON pr.id = f.project_id JOIN easycode.stages s ON s.project_id = pr.id AND s.stage = 'requirements'
 WHERE pr.state = 'in_progress' AND f.status = 'awaiting_scope' AND s.state = 'approved'`); err != nil {
		return
	}
	returned, err = collect("stages.returned_waiting", `
SELECT pr.public_id::text, s.stage, '' FROM easycode.stages s JOIN easycode.projects pr ON pr.id = s.project_id
 WHERE pr.state = 'in_progress' AND s.state = 'returned'
   AND NOT EXISTS (SELECT 1 FROM easycode.waves w WHERE w.stage_id = s.id AND w.state NOT IN ('completed','canceled'))`)
	return
}

// RoundOutputs implements appfinding.Store.
func (s Findings) RoundOutputs(ctx context.Context, projectID, stage string, round int) ([]string, error) {
	rows, err := conn(ctx, s.pool).Query(WithOperation(ctx, "gate_round_artifacts.codes"), `
SELECT ar.code FROM easycode.gate_round_artifacts ga JOIN easycode.artifact_versions v ON v.id = ga.artifact_version_id
  JOIN easycode.artifacts ar ON ar.id = v.artifact_id WHERE ga.round_id = `+roundByKey+` ORDER BY ar.code`, projectID, stage, round)
	if err != nil {
		return nil, fmt.Errorf("round outputs: %w", classify(err))
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// AwaitingScope implements appfinding.Store.
func (s Findings) AwaitingScope(ctx context.Context, projectID, stage string, round int) ([]string, error) {
	rows, err := conn(ctx, s.pool).Query(WithOperation(ctx, "findings.awaiting_scope"), `
SELECT a.code FROM easycode.findings f JOIN easycode.artifacts a ON a.id = f.artifact_id JOIN easycode.projects pr ON pr.id = f.project_id
  JOIN easycode.gate_rounds r ON r.project_id = pr.id AND r.stage = $2 AND r.number = $3
 WHERE pr.public_id = $1::uuid AND f.status = 'awaiting_scope'
   AND EXISTS (SELECT 1 FROM easycode.finding_escalations e WHERE e.finding_id = f.artifact_id AND e.decision = 'scope_change' AND e.decided_at < r.opened_at)
 ORDER BY a.code`, projectID, stage, round)
	if err != nil {
		return nil, fmt.Errorf("awaiting scope: %w", classify(err))
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

var _ appfinding.DecisionStore = Findings{}

const userOfPrincipal = `(SELECT uu.id FROM easycode.users uu JOIN easycode.organizations oo ON oo.id = uu.organization_id WHERE oo.public_id = $3::uuid AND uu.sub = $4)`

// List implements appfinding.DecisionStore: severity first, then the opening date; the cursor is the offset.
func (s Findings) List(ctx context.Context, projectID string, filter appfinding.Filter) (appfinding.Page, error) {
	conditions := []string{}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)+1))
	}
	if len(filter.Statuses) > 0 {
		add("f.status = ANY($%d)", filter.Statuses)
	}
	if len(filter.Severities) > 0 {
		add("f.severity = ANY($%d)", filter.Severities)
	}
	if filter.Origin != "" {
		add("f.origin_stage = $%d", filter.Origin)
	}
	if filter.Destination != "" {
		add("f.destination_stage = $%d", filter.Destination)
	}
	if filter.Author != "" {
		add("ow.agent = $%d", filter.Author)
	}
	condition := ""
	for i, c := range conditions {
		if i > 0 {
			condition += " AND "
		}
		condition += c
	}
	findings, err := loadFindings(ctx, conn(ctx, s.pool), "findings.list", projectID, condition, args...)
	if err != nil {
		return appfinding.Page{}, err
	}
	sortFindings(findings)
	offset := 0
	if filter.Cursor != "" {
		if _, err := fmt.Sscanf(filter.Cursor, "%d", &offset); err != nil || offset < 0 {
			return appfinding.Page{}, failureInvalidCursor()
		}
	}
	page := appfinding.Page{}
	if offset < len(findings) {
		end := offset + filter.Limit
		if end > len(findings) {
			end = len(findings)
		}
		page.Items = findings[offset:end]
		if end < len(findings) {
			page.NextCursor = fmt.Sprintf("%d", end)
		}
	}
	return page, nil
}

// Find implements appfinding.DecisionStore.
func (s Findings) Find(ctx context.Context, projectID, code string, lock bool) (*domainfinding.Finding, error) {
	if lock {
		return s.Get(ctx, projectID, code)
	}
	findings, err := loadFindings(ctx, conn(ctx, s.pool), "findings.find", projectID, "ar.code = $2", code)
	if err != nil || len(findings) == 0 {
		return nil, err
	}
	return &findings[0], nil
}

// Accept implements appfinding.DecisionStore (accept_risk, or the renewal of an accepted finding).
func (s Findings) Accept(ctx context.Context, p identity.Principal, projectID, code, justification string, validUntil *time.Time, from string) error {
	q := conn(ctx, s.pool)
	_, err := q.Exec(WithOperation(ctx, "findings.accept"), `
UPDATE easycode.findings SET status = 'accepted', accepted_by = `+userOfPrincipal+`, accepted_at = now(), acceptance_justification = $5,
       acceptance_valid_until = $6, updated_at = now()
 WHERE artifact_id = `+findingByCode, projectID, code, p.OrganizationID, p.Subject, justification, validUntil)
	if err != nil {
		return fmt.Errorf("accept risk: %w", classify(err))
	}
	if from != domainfinding.StatusEscalated {
		return nil
	}
	_, err = q.Exec(WithOperation(ctx, "finding_escalations.accept"), `
UPDATE easycode.finding_escalations SET decision = 'accept_risk', authorized_by = `+userOfPrincipal+`, decided_at = now(), justification = $5
 WHERE finding_id = `+findingByCode+` AND decision IS NULL`, projectID, code, p.OrganizationID, p.Subject, justification)
	if err != nil {
		return fmt.Errorf("escalation decision: %w", classify(err))
	}
	return nil
}

// DecideEscalation implements appfinding.DecisionStore (new_rework or scope_change on the pending escalation).
func (s Findings) DecideEscalation(ctx context.Context, p identity.Principal, projectID, code, decision, justification, modifiedBy string,
	starting []string, status string) error {
	q := conn(ctx, s.pool)
	if starting == nil {
		starting = []string{}
	}
	tag, err := q.Exec(WithOperation(ctx, "finding_escalations.decide"), `
UPDATE easycode.finding_escalations SET decision = $5, authorized_by = `+userOfPrincipal+`, decided_at = now(), justification = $6,
       modified_by = nullif($7, ''), starting_point_artifacts = $8
 WHERE finding_id = `+findingByCode+` AND decision IS NULL`, projectID, code, p.OrganizationID, p.Subject, decision, justification, modifiedBy, starting)
	if err != nil {
		return fmt.Errorf("escalation decision: %w", classify(err))
	}
	if tag.RowsAffected() != 1 {
		return failureStateConflict()
	}
	_, err = q.Exec(WithOperation(ctx, "findings.after_escalation"),
		`UPDATE easycode.findings SET status = $3, updated_at = now() WHERE artifact_id = `+findingByCode, projectID, code, status)
	if err != nil {
		return fmt.Errorf("finding after escalation: %w", classify(err))
	}
	return nil
}

// Discard implements appfinding.DecisionStore.
func (s Findings) Discard(ctx context.Context, projectID, code, reason, duplicateOf string) error {
	_, err := conn(ctx, s.pool).Exec(WithOperation(ctx, "findings.discard"), `
UPDATE easycode.findings SET status = 'discarded', discard_reason = $3, updated_at = now(),
       duplicate_of = (SELECT f.artifact_id FROM easycode.findings f JOIN easycode.artifacts a ON a.id = f.artifact_id
                        JOIN easycode.projects pr ON pr.id = f.project_id WHERE pr.public_id = $1::uuid AND a.code = nullif($4, ''))
 WHERE artifact_id = `+findingByCode, projectID, code, reason, duplicateOf)
	if err != nil {
		return fmt.Errorf("discard finding: %w", classify(err))
	}
	return nil
}

// ArtifactTypes implements appfinding.DecisionStore.
func (s Findings) ArtifactTypes(ctx context.Context, projectID string, codes []string) (map[string]string, error) {
	rows, err := conn(ctx, s.pool).Query(WithOperation(ctx, "artifacts.types"), `
SELECT ar.code, ar.type FROM easycode.artifacts ar JOIN easycode.projects pr ON pr.id = ar.project_id
 WHERE pr.public_id = $1::uuid AND ar.code = ANY($2)`, projectID, codes)
	if err != nil {
		return nil, fmt.Errorf("artifact types: %w", classify(err))
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var code, t string
		if err := rows.Scan(&code, &t); err != nil {
			return nil, err
		}
		out[code] = t
	}
	return out, rows.Err()
}

func sortFindings(findings []domainfinding.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := domainfinding.SeverityRank(findings[i].Severity), domainfinding.SeverityRank(findings[j].Severity)
		if a != b {
			return a < b
		}
		return findings[i].OpenedAt.Before(findings[j].OpenedAt)
	})
}

func failureInvalidCursor() error {
	return failure.Invalid(failure.FieldError{Field: "cursor", Message: "invalid"})
}

func failureStateConflict() error {
	return failure.New(failure.KindConflict, failure.CodeStateConflict, nil)
}
