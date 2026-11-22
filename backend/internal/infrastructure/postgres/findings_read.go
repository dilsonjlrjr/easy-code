package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	domainfinding "easycode/internal/domain/finding"
)

// findingColumns reads a finding with its ACH artifact, origin wave and acceptance (findings, DIC).
const findingColumns = `
SELECT ar.code, ar.title, f.severity, f.category, f.status, f.origin_stage, f.destination_stage, ow.agent, ow.code,
       coalesce(f.resolution, ''), f.modified_artifacts, coalesce(au.sub, ''), coalesce(au.display_name, ''), f.accepted_at,
       coalesce(f.acceptance_justification, ''), f.acceptance_valid_until, coalesce(f.discard_reason, ''), coalesce(dup.code, ''),
       f.opened_at, ar.current_version, v.header, v.body, f.artifact_id,
       coalesce((SELECT array_agg(fa.reference ORDER BY fa.id) FROM easycode.finding_affected_artifacts fa WHERE fa.finding_id = f.artifact_id), '{}')
  FROM easycode.findings f JOIN easycode.artifacts ar ON ar.id = f.artifact_id
  JOIN easycode.projects pr ON pr.id = f.project_id
  JOIN easycode.artifact_versions v ON v.artifact_id = ar.id AND v.version = ar.current_version
  JOIN easycode.waves ow ON ow.id = f.origin_wave_id
  LEFT JOIN easycode.users au ON au.id = f.accepted_by
  LEFT JOIN easycode.artifacts dup ON dup.id = f.duplicate_of`

// loadFindings returns the findings of a project matching the extra condition (on f, ar), with their
// cycles and escalations.
func loadFindings(ctx context.Context, q querier, operation, projectID, condition string, args ...any) ([]domainfinding.Finding, error) {
	sql := findingColumns + " WHERE pr.public_id = $1::uuid"
	if condition != "" {
		sql += " AND " + condition
	}
	sql += " ORDER BY f.opened_at, ar.code"
	rows, err := q.Query(WithOperation(ctx, operation), sql, append([]any{projectID}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("findings: %w", classify(err))
	}
	var findings []domainfinding.Finding
	var ids []int64
	for rows.Next() {
		f := domainfinding.Finding{ProjectID: projectID}
		var header []byte
		var id int64
		var validUntil *time.Time
		if err := rows.Scan(&f.Code, &f.Title, &f.Severity, &f.Category, &f.Status, &f.OriginStage, &f.DestinationStage, &f.Author, &f.OriginWave,
			&f.Resolution, &f.ModifiedArtifacts, &f.AcceptedBy, &f.AcceptedByName, &f.AcceptedAt, &f.Justification, &validUntil,
			&f.DiscardReason, &f.DuplicateOf, &f.OpenedAt, &f.Version, &header, &f.Body, &id, &f.Affected); err != nil {
			rows.Close()
			return nil, err
		}
		f.ValidUntil = validUntil
		_ = json.Unmarshal(header, &f.Header)
		findings = append(findings, f)
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return findings, nil
	}
	index := map[int64]int{}
	for i, id := range ids {
		index[id] = i
	}
	cycles, err := q.Query(WithOperation(ctx, operation+"_cycles"), `
SELECT c.finding_id, c.number, rw.code, rw.agent, coalesce(c.commit_or_version, ''), coalesce(vw.code, ''), coalesce(vw.agent, ''), coalesce(c.result, '')
  FROM easycode.finding_cycles c JOIN easycode.waves rw ON rw.id = c.rework_wave_id
  LEFT JOIN easycode.waves vw ON vw.id = c.revalidation_wave_id
 WHERE c.finding_id = ANY($1) ORDER BY c.finding_id, c.number`, ids)
	if err != nil {
		return nil, fmt.Errorf("finding cycles: %w", classify(err))
	}
	for cycles.Next() {
		var id int64
		var c domainfinding.Cycle
		if err := cycles.Scan(&id, &c.Number, &c.ReworkWave, &c.ReworkAgent, &c.CommitOrVersion, &c.RevalidationWave, &c.RevalidationAgent, &c.Result); err != nil {
			cycles.Close()
			return nil, err
		}
		findings[index[id]].Cycles = append(findings[index[id]].Cycles, c)
	}
	cycles.Close()
	if err := cycles.Err(); err != nil {
		return nil, err
	}
	escalations, err := q.Query(WithOperation(ctx, operation+"_escalations"), `
SELECT e.finding_id, e.number, e.escalated_at, e.reason, coalesce(e.decision, ''), coalesce(u.sub, ''), coalesce(u.display_name, ''), e.decided_at,
       coalesce(e.modified_by, ''), e.starting_point_artifacts, coalesce(e.justification, '')
  FROM easycode.finding_escalations e LEFT JOIN easycode.users u ON u.id = e.authorized_by
 WHERE e.finding_id = ANY($1) ORDER BY e.finding_id, e.number`, ids)
	if err != nil {
		return nil, fmt.Errorf("finding escalations: %w", classify(err))
	}
	defer escalations.Close()
	for escalations.Next() {
		var id int64
		var e domainfinding.Escalation
		if err := escalations.Scan(&id, &e.Number, &e.At, &e.Reason, &e.Decision, &e.AuthorizedBy, &e.AuthorizedByName, &e.DecidedAt,
			&e.ModifiedBy, &e.StartingPoint, &e.Justification); err != nil {
			return nil, err
		}
		findings[index[id]].Escalations = append(findings[index[id]].Escalations, e)
	}
	return findings, escalations.Err()
}
