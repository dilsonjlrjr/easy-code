package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appskill "easycode/internal/application/skill"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	domainskill "easycode/internal/domain/skill"
)

// SkillImports implements appskill.Imports (api role) and appskill.Processor (worker role) over skill_imports
// and skill_versions (US-44).
type SkillImports struct {
	pool *pgxpool.Pool
}

var (
	_ appskill.Imports   = SkillImports{}
	_ appskill.Processor = SkillImports{}
)

// NewSkillImports builds the adapter.
func NewSkillImports(pool *pgxpool.Pool) SkillImports { return SkillImports{pool: pool} }

const importColumns = `i.public_id::text, i.state, i.origin, i.received_at, coalesce(s.name, ''), coalesce(v.version, 0), coalesce(v.status, ''),
       coalesce(v.block_reasons, '{}'), coalesce(i.refusal->>'reason', '')
  FROM easycode.skill_imports i
  LEFT JOIN easycode.skill_versions v ON v.id = i.skill_version_id
  LEFT JOIN easycode.skills s ON s.id = v.skill_id`

func scanImport(row pgx.Row) (appskill.Import, error) {
	var out appskill.Import
	err := row.Scan(&out.ID, &out.State, &out.Origin, &out.ReceivedAt, &out.Skill, &out.Version, &out.VersionStatus, &out.BlockReasons, &out.RefusalReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return out, fmt.Errorf("skill import: %w", classify(err))
	}
	return out, nil
}

// Receive implements appskill.Imports.
func (k SkillImports) Receive(ctx context.Context, p identity.Principal, pkg []byte, key string) (appskill.Import, error) {
	var id string
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "skill_imports.receive"), `
INSERT INTO easycode.skill_imports (organization_id, origin, package, idempotency_key, requested_by)
VALUES ((SELECT id FROM easycode.organizations WHERE public_id = $1::uuid), 'upload', $3, nullif($4, ''), `+principalUserIDSQL+`)
RETURNING public_id::text`, p.OrganizationID, p.Subject, pkg, key).Scan(&id)
	if err != nil {
		return appskill.Import{}, fmt.Errorf("receive skill import: %w", classify(err))
	}
	return k.Get(ctx, p, id)
}

// Get implements appskill.Imports.
func (k SkillImports) Get(ctx context.Context, p identity.Principal, id string) (appskill.Import, error) {
	return scanImport(conn(ctx, k.pool).QueryRow(WithOperation(ctx, "skill_imports.get"), "SELECT "+importColumns+`
 WHERE i.public_id = $2::uuid AND i.organization_id = (SELECT id FROM easycode.organizations WHERE public_id = $1::uuid)`, p.OrganizationID, id))
}

// NativeName implements appskill.Imports.
func (k SkillImports) NativeName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "skills.native_name"),
		"SELECT EXISTS (SELECT 1 FROM easycode.skills WHERE organization_id IS NULL AND name = $1)", name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("native skill name: %w", classify(err))
	}
	return exists, nil
}

const organizationVersionSQL = `(SELECT v.id FROM easycode.skill_versions v JOIN easycode.skills s ON s.id = v.skill_id
 WHERE s.organization_id = (SELECT id FROM easycode.organizations WHERE public_id = $1::uuid) AND s.name = $3 AND v.version = $4)`

// VersionState implements appskill.Imports.
func (k SkillImports) VersionState(ctx context.Context, p identity.Principal, name string, version int) (string, []string, error) {
	var status string
	var reasons []string
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "skill_versions.state"), `
SELECT v.status, coalesce(v.block_reasons, '{}') FROM easycode.skill_versions v WHERE v.id = `+organizationVersionSQL+` AND $2::text IS NOT NULL FOR UPDATE`,
		p.OrganizationID, p.Subject, name, version).Scan(&status, &reasons)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	if err != nil {
		return "", nil, fmt.Errorf("skill version state: %w", classify(err))
	}
	return status, reasons, nil
}

// Unblock implements appskill.Imports.
func (k SkillImports) Unblock(ctx context.Context, p identity.Principal, name string, version int, justification string) error {
	_, err := conn(ctx, k.pool).Exec(WithOperation(ctx, "skill_versions.unblock"), `
UPDATE easycode.skill_versions SET status = 'unblocked', unblocked_by = `+principalUserIDSQL+`, unblocked_at = now(), unblock_justification = $5
 WHERE id = `+organizationVersionSQL, p.OrganizationID, p.Subject, name, version, justification)
	if err != nil {
		return fmt.Errorf("unblock skill version: %w", classify(err))
	}
	return nil
}

// Deactivate implements appskill.Imports.
func (k SkillImports) Deactivate(ctx context.Context, p identity.Principal, name string, version int, justification string) error {
	_, err := conn(ctx, k.pool).Exec(WithOperation(ctx, "skill_versions.deactivate"), `
UPDATE easycode.skill_versions SET status = 'deactivated', deactivated_by = `+principalUserIDSQL+`, deactivated_at = now(), deactivation_justification = $5
 WHERE id = `+organizationVersionSQL, p.OrganizationID, p.Subject, name, version, justification)
	if err != nil {
		return fmt.Errorf("deactivate skill version: %w", classify(err))
	}
	return nil
}

// Pending implements appskill.Processor.
func (k SkillImports) Pending(ctx context.Context, limit int) ([]appskill.Received, error) {
	rows, err := conn(ctx, k.pool).Query(WithOperation(ctx, "skill_imports.pending"), `
SELECT id, organization_id, requested_by, package FROM easycode.skill_imports
 WHERE state = 'received' AND package IS NOT NULL ORDER BY received_at LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending skill imports: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appskill.Received, error) {
		var r appskill.Received
		return r, row.Scan(&r.ID, &r.OrganizationID, &r.RequestedBy, &r.Package)
	})
}

// Refuse implements appskill.Processor.
func (k SkillImports) Refuse(ctx context.Context, r appskill.Received, reason string) error {
	refusal, _ := json.Marshal(map[string]string{"type": "urn:easycode:problem:" + domainskill.CodeInvalidPackage, "reason": reason})
	_, err := conn(ctx, k.pool).Exec(WithOperation(ctx, "skill_imports.refuse"), `
UPDATE easycode.skill_imports SET state = 'refused', refusal = $2::jsonb, package = NULL, completed_at = now()
 WHERE id = $1 AND state = 'received'`, r.ID, refusal)
	if err != nil {
		return fmt.Errorf("refuse skill import: %w", classify(err))
	}
	return nil
}

// CreateVersion implements appskill.Processor: imported → scanning → unblocked or blocked (machine skill, D-26).
func (k SkillImports) CreateVersion(ctx context.Context, r appskill.Received, pkg domainskill.Package, scan domainskill.Scan, stages []string) (int, error) {
	q := conn(ctx, k.pool)
	if _, err := q.Exec(WithOperation(ctx, "skills.lock"), "SELECT pg_advisory_xact_lock(hashtext('skill:' || $1::bigint::text || ':' || $2::text))", r.OrganizationID, pkg.Name); err != nil {
		return 0, err
	}
	var skillID int64
	err := q.QueryRow(WithOperation(ctx, "skills.organization_id"), "SELECT id FROM easycode.skills WHERE organization_id = $1 AND name = $2", r.OrganizationID, pkg.Name).Scan(&skillID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = q.QueryRow(WithOperation(ctx, "skills.organization"),
			"INSERT INTO easycode.skills (organization_id, name, origin, created_by) VALUES ($1, $2, 'organization', $3) RETURNING id",
			r.OrganizationID, pkg.Name, r.RequestedBy).Scan(&skillID)
	}
	if err != nil {
		return 0, fmt.Errorf("organization skill: %w", classify(err))
	}
	var version int
	var versionID int64
	err = q.QueryRow(WithOperation(ctx, "skill_versions.import"), `
INSERT INTO easycode.skill_versions (skill_id, version, submission_origin, description, stages, agents, size_tokens, content, content_hash, submitted_by)
SELECT $1, coalesce(max(version), 0) + 1, 'upload', $2, $3, $4, $5, $6, $7, $8 FROM easycode.skill_versions WHERE skill_id = $1
RETURNING version, id`, skillID, pkg.Description, stages, pkg.Agents, min(pkg.SizeTokens(), 200000), pkg.Content(), pkg.Hash(), r.RequestedBy).Scan(&version, &versionID)
	if err != nil {
		return 0, fmt.Errorf("import skill version: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "skill_versions.scanning"), "UPDATE easycode.skill_versions SET status = 'scanning' WHERE id = $1", versionID); err != nil {
		return 0, fmt.Errorf("scan skill version: %w", classify(err))
	}
	report, _ := json.Marshal(scan.Report)
	var reasons any
	if len(scan.Reasons) > 0 {
		reasons = scan.Reasons
	}
	if _, err := q.Exec(WithOperation(ctx, "skill_versions.scanned"),
		"UPDATE easycode.skill_versions SET status = $2, block_reasons = $3, scan_report = $4::jsonb WHERE id = $1", versionID, scan.Status, reasons, report); err != nil {
		return 0, fmt.Errorf("scanned skill version: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "skill_imports.version_created"), `
UPDATE easycode.skill_imports SET state = 'version_created', skill_version_id = $2, package = NULL, completed_at = $3 WHERE id = $1`,
		r.ID, versionID, time.Now()); err != nil {
		return 0, fmt.Errorf("link skill import: %w", classify(err))
	}
	return version, nil
}
