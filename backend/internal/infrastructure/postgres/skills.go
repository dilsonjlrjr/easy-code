package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appproject "easycode/internal/application/project"
	appskill "easycode/internal/application/skill"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	domainskill "easycode/internal/domain/skill"
)

// Skills is the skill library (native catalog and organization skills), the skills of the agents of a
// project and their agent configurations (US-43).
type Skills struct{ pool *pgxpool.Pool }

// NewSkills builds the adapter.
func NewSkills(pool *pgxpool.Pool) Skills { return Skills{pool: pool} }

var (
	_ appskill.Library        = Skills{}
	_ appskill.Configurations = Skills{}
	_ appskill.Publisher      = Skills{}
	_ appskill.Pairs          = Skills{}
)

// visible restricts the skills to the native catalog and the organization ($1 = organization public id).
const visibleSkills = `(s.organization_id IS NULL OR s.organization_id = (SELECT o.id FROM easycode.organizations o WHERE o.public_id = $1::uuid))`

// List implements appskill.Library: the current version of each skill (the last one), by name.
func (k Skills) List(ctx context.Context, organizationID string, filter appskill.Filter) ([]appskill.Summary, error) {
	rows, err := conn(ctx, k.pool).Query(WithOperation(ctx, "skills.list"), `
SELECT s.name FROM easycode.skills s
  JOIN LATERAL (SELECT v.agents, v.stages FROM easycode.skill_versions v WHERE v.skill_id = s.id ORDER BY v.version DESC LIMIT 1) cur ON true
 WHERE `+visibleSkills+`
   AND ($2 = '' OR s.origin = $2) AND ($3 = '' OR $3 = ANY(cur.agents)) AND ($4 = '' OR $4 = ANY(cur.stages)) AND s.name > $5
 ORDER BY s.name LIMIT $6`, organizationID, filter.Origin, filter.Agent, filter.Stage, filter.After, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", classify(err))
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make([]appskill.Summary, 0, len(names))
	for _, name := range names {
		summary, err := k.Get(ctx, organizationID, name)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
}

// Get implements appskill.Library.
func (k Skills) Get(ctx context.Context, organizationID, name string) (appskill.Summary, error) {
	rows, err := conn(ctx, k.pool).Query(WithOperation(ctx, "skills.get"), `
SELECT s.name, s.origin, v.version, v.description, v.stages, v.agents, v.size_tokens, v.status, v.submitted_at, coalesce(v.submission_origin, ''), v.content_hash
  FROM easycode.skills s JOIN easycode.skill_versions v ON v.skill_id = s.id
 WHERE `+visibleSkills+` AND s.name = $2
 ORDER BY v.version DESC`, organizationID, name)
	if err != nil {
		return appskill.Summary{}, fmt.Errorf("get skill: %w", classify(err))
	}
	defer rows.Close()
	var out appskill.Summary
	for rows.Next() {
		var v appskill.VersionInfo
		var description, origin string
		var stages, agents []string
		var size int
		if err := rows.Scan(&out.Name, &origin, &v.Version, &description, &stages, &agents, &size, &v.Status, &v.PublishedAt, &v.Origin, &v.SHA256); err != nil {
			return out, err
		}
		if len(out.Versions) == 0 {
			out.Origin, out.Description, out.Stages, out.Agents, out.CurrentVersion, out.EstimatedTokens, out.Status =
				origin, description, stages, agents, v.Version, size, v.Status
		}
		out.Versions = append(out.Versions, v)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Versions) == 0 {
		return out, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return out, nil
}

// PublishNative implements appskill.Publisher (worker role): the skill row once, and a new unblocked
// version whenever the content hash differs from the last one (a native version is never blocked).
func (k Skills) PublishNative(ctx context.Context, pkg domainskill.Package) (bool, error) {
	q := conn(ctx, k.pool)
	var skillID int64
	err := q.QueryRow(WithOperation(ctx, "skills.native_id"), "SELECT id FROM easycode.skills WHERE name = $1 AND organization_id IS NULL", pkg.Name).Scan(&skillID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = q.QueryRow(WithOperation(ctx, "skills.native"), "INSERT INTO easycode.skills (name, origin) VALUES ($1, 'native') RETURNING id", pkg.Name).Scan(&skillID)
	}
	if err != nil {
		return false, fmt.Errorf("native skill: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "skills.lock"), "SELECT pg_advisory_xact_lock(hashtext('skill:' || $1::text))", pkg.Name); err != nil {
		return false, err
	}
	var last int
	var hash string
	err = q.QueryRow(WithOperation(ctx, "skill_versions.last"),
		"SELECT version, content_hash FROM easycode.skill_versions WHERE skill_id = $1 ORDER BY version DESC LIMIT 1", skillID).Scan(&last, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("last skill version: %w", classify(err))
	}
	if hash == pkg.Hash() {
		return false, nil
	}
	_, err = q.Exec(WithOperation(ctx, "skill_versions.native"), `
INSERT INTO easycode.skill_versions (skill_id, version, description, stages, agents, size_tokens, content, content_hash, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'unblocked')`,
		skillID, last+1, pkg.Description, pkg.Stages, pkg.Agents, pkg.SizeTokens(), pkg.Content(), pkg.Hash())
	if err != nil {
		return false, fmt.Errorf("publish native skill version: %w", classify(err))
	}
	return true, nil
}

// Latest implements appskill.Configurations.
func (k Skills) Latest(ctx context.Context, projectID, agent string) (*appskill.Configuration, error) {
	versions, err := k.versions(ctx, projectID, agent, 1)
	if err != nil || len(versions) == 0 {
		return nil, err
	}
	return &versions[0], nil
}

// Versions implements appskill.Configurations.
func (k Skills) Versions(ctx context.Context, projectID, agent string) ([]appskill.Configuration, error) {
	return k.versions(ctx, projectID, agent, 1000)
}

func (k Skills) versions(ctx context.Context, projectID, agent string, limit int) ([]appskill.Configuration, error) {
	rows, err := conn(ctx, k.pool).Query(WithOperation(ctx, "agent_configurations.versions"), `
SELECT ac.version, ac.instruction_version, coalesce(ac.adjustment_instructions, ''), ac.tools, ac.created_at, u.sub, u.display_name
  FROM easycode.agent_configurations ac JOIN easycode.projects pr ON pr.id = ac.project_id
  LEFT JOIN easycode.users u ON u.id = ac.created_by
 WHERE pr.public_id = $1::uuid AND ac.agent = $2 ORDER BY ac.version DESC LIMIT $3`, projectID, agent, limit)
	if err != nil {
		return nil, fmt.Errorf("agent configurations: %w", classify(err))
	}
	defer rows.Close()
	var out []appskill.Configuration
	for rows.Next() {
		var c appskill.Configuration
		var sub, name *string
		if err := rows.Scan(&c.Version, &c.InstructionVersion, &c.Adjustment, &c.Tools, &c.CreatedAt, &sub, &name); err != nil {
			return nil, err
		}
		if sub != nil {
			c.CreatedBy = &appproject.UserRef{Subject: *sub, Name: deref(name)}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Pinned implements appskill.Configurations: the pinned version of each skill of the agent and the newest
// unblocked version above it, the notice of US-43 CA-3.
func (k Skills) Pinned(ctx context.Context, projectID, agent string) ([]domainskill.Pinned, error) {
	rows, err := conn(ctx, k.pool).Query(WithOperation(ctx, "project_skills.pinned"), `
SELECT s.name, v.version, coalesce((SELECT max(n.version) FROM easycode.skill_versions n WHERE n.skill_id = s.id AND n.status = 'unblocked' AND n.version > v.version), 0)
  FROM easycode.project_skills ps JOIN easycode.projects pr ON pr.id = ps.project_id
  JOIN easycode.skills s ON s.id = ps.skill_id JOIN easycode.skill_versions v ON v.id = ps.skill_version_id
 WHERE pr.public_id = $1::uuid AND $2 = ANY(ps.agents) ORDER BY s.name`, projectID, agent)
	if err != nil {
		return nil, fmt.Errorf("pinned skills: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domainskill.Pinned, error) {
		var p domainskill.Pinned
		return p, row.Scan(&p.Name, &p.Version, &p.Available)
	})
}

// VersionStatus implements appskill.Configurations.
func (k Skills) VersionStatus(ctx context.Context, organizationID, name string, version int) (string, error) {
	var status string
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "skill_versions.status"), `
SELECT v.status FROM easycode.skills s JOIN easycode.skill_versions v ON v.skill_id = s.id
 WHERE `+visibleSkills+` AND s.name = $2 AND v.version = $3`, organizationID, name, version).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("skill version status: %w", classify(err))
	}
	return status, nil
}

// Save implements appskill.Configurations (api role): the default version 1 when the project has none,
// the next version with the tools, and the skills of the agent (project_skills: the agent joins the row
// of each skill asked, in the asked version; it leaves the others, and a row without agents goes away).
func (k Skills) Save(ctx context.Context, p identity.Principal, projectID string, profile appskill.Profile, tools json.RawMessage, skills []appskill.Ref,
	adjustment *string) (appskill.Configuration, error) {
	q := conn(ctx, k.pool)
	var project int64
	if err := q.QueryRow(WithOperation(ctx, "projects.id"), "SELECT id FROM easycode.projects WHERE public_id = $1::uuid", projectID).Scan(&project); err != nil {
		return appskill.Configuration{}, fmt.Errorf("project: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "agent_configurations.default"), `
INSERT INTO easycode.agent_configurations (project_id, agent, version, instruction_version, instruction_hash, tools, limits)
SELECT $1, $2, 1, $3, $4, $5::jsonb, $6::jsonb
 WHERE NOT EXISTS (SELECT 1 FROM easycode.agent_configurations WHERE project_id = $1 AND agent = $2)`,
		project, profile.Agent, profile.InstructionVersion, profile.InstructionHash, string(profile.Tools), string(profile.Limits)); err != nil {
		return appskill.Configuration{}, fmt.Errorf("default agent configuration: %w", classify(err))
	}
	var saved appskill.Configuration
	err := q.QueryRow(WithOperation(ctx, "agent_configurations.next"), `
INSERT INTO easycode.agent_configurations (project_id, agent, version, instruction_version, instruction_hash, tools, limits, output_criteria,
                                          adjustment_instructions, created_by)
SELECT ac.project_id, ac.agent, ac.version + 1, ac.instruction_version, ac.instruction_hash, $3::jsonb, ac.limits, ac.output_criteria,
       CASE WHEN $6::boolean THEN nullif($7::text, '') ELSE ac.adjustment_instructions END, `+principalUserIDSQL+`
  FROM easycode.agent_configurations ac WHERE ac.project_id = $4 AND ac.agent = $5 ORDER BY ac.version DESC LIMIT 1
RETURNING version, instruction_version, coalesce(adjustment_instructions, ''), tools, created_at`, p.OrganizationID, p.Subject, string(tools), project, profile.Agent,
		adjustment != nil, deref(adjustment)).
		Scan(&saved.Version, &saved.InstructionVersion, &saved.Adjustment, &saved.Tools, &saved.CreatedAt)
	if err != nil {
		return saved, fmt.Errorf("agent configuration: %w", classify(err))
	}
	names := make([]string, 0, len(skills))
	for _, ref := range skills {
		names = append(names, ref.Name)
		_, err := q.Exec(WithOperation(ctx, "project_skills.associate"), `
INSERT INTO easycode.project_skills (project_id, skill_id, skill_version_id, agents, associated_by)
SELECT $3, s.id, v.id, ARRAY[$4::text], `+principalUserIDSQL+`
  FROM easycode.skills s JOIN easycode.skill_versions v ON v.skill_id = s.id
 WHERE s.name = $5 AND v.version = $6 AND (s.organization_id IS NULL OR s.organization_id = (SELECT o.id FROM easycode.organizations o WHERE o.public_id = $1::uuid))
ON CONFLICT (project_id, skill_id) DO UPDATE SET skill_version_id = EXCLUDED.skill_version_id, pending_version_id = NULL,
       agents = (SELECT array_agg(DISTINCT a ORDER BY a) FROM unnest(project_skills.agents || EXCLUDED.agents) a),
       associated_by = EXCLUDED.associated_by, associated_at = now()`,
			p.OrganizationID, p.Subject, project, profile.Agent, ref.Name, ref.Version)
		if err != nil {
			return saved, fmt.Errorf("associate skill: %w", classify(err))
		}
	}
	if _, err := q.Exec(WithOperation(ctx, "project_skills.leave"), `
UPDATE easycode.project_skills ps SET agents = array_remove(ps.agents, $2)
  FROM easycode.skills s WHERE s.id = ps.skill_id AND ps.project_id = $1 AND $2 = ANY(ps.agents) AND NOT (s.name = ANY($3::text[]))
   AND cardinality(ps.agents) > 1`, project, profile.Agent, names); err != nil {
		return saved, fmt.Errorf("leave skills: %w", classify(err))
	}
	if _, err := q.Exec(WithOperation(ctx, "project_skills.remove"), `
DELETE FROM easycode.project_skills ps USING easycode.skills s
 WHERE s.id = ps.skill_id AND ps.project_id = $1 AND ps.agents = ARRAY[$2::text] AND NOT (s.name = ANY($3::text[]))`, project, profile.Agent, names); err != nil {
		return saved, fmt.Errorf("remove skills: %w", classify(err))
	}
	return saved, nil
}

// Pair implements appskill.Pairs: the pair in force of a stage, if configured.
func (k Skills) Pair(ctx context.Context, projectID, stage string) (appproject.Pair, bool, error) {
	var pair appproject.Pair
	err := conn(ctx, k.pool).QueryRow(WithOperation(ctx, "model_pairs.of_stage"), `
SELECT rm.provider, rm.name, em.provider, em.name
  FROM easycode.model_pairs mp JOIN easycode.projects pr ON pr.id = mp.project_id
  JOIN easycode.models rm ON rm.id = mp.reasoning_model_id JOIN easycode.models em ON em.id = mp.execution_model_id
 WHERE pr.public_id = $1::uuid AND (mp.stage = $2 OR mp.stage IS NULL)
 ORDER BY mp.stage IS NULL LIMIT 1`, projectID, stage).Scan(&pair.Reasoning.Provider, &pair.Reasoning.Model, &pair.Execution.Provider, &pair.Execution.Model)
	if errors.Is(err, pgx.ErrNoRows) {
		return pair, false, nil
	}
	if err != nil {
		return pair, false, fmt.Errorf("pair of stage: %w", classify(err))
	}
	return pair, true, nil
}

// waveSkills records the skills of the agent pinned in the project as loaded by description in a new
// wave (US-43 CA-2, RNF-15).
func waveSkills(ctx context.Context, q querier, projectID, waveID int64, agent string) error {
	_, err := q.Exec(WithOperation(ctx, "wave_skills.insert"), `
INSERT INTO easycode.wave_skills (wave_id, skill_version_id, load)
SELECT $2, ps.skill_version_id, 'description' FROM easycode.project_skills ps WHERE ps.project_id = $1 AND $3 = ANY(ps.agents)`, projectID, waveID, agent)
	if err != nil {
		return fmt.Errorf("wave skills: %w", classify(err))
	}
	return nil
}

// loadedSkills reads the skills of a wave with their content.
func loadedSkills(ctx context.Context, q querier, waveID int64) ([]domainskill.Loaded, error) {
	rows, err := q.Query(WithOperation(ctx, "wave_skills.loaded"), `
SELECT v.id, s.name, v.version, v.description, v.content FROM easycode.wave_skills ws
  JOIN easycode.skill_versions v ON v.id = ws.skill_version_id JOIN easycode.skills s ON s.id = v.skill_id
 WHERE ws.wave_id = $1 ORDER BY s.name`, waveID)
	if err != nil {
		return nil, fmt.Errorf("wave skills: %w", classify(err))
	}
	defer rows.Close()
	var out []domainskill.Loaded
	for rows.Next() {
		var l domainskill.Loaded
		var content []byte
		if err := rows.Scan(&l.VersionID, &l.Name, &l.Version, &l.Description, &content); err != nil {
			return nil, err
		}
		if l.Files, err = domainskill.Files(content); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// waveSkillRefs are the skill versions a wave used (Wave.versions.skills).
func waveSkillRefs(ctx context.Context, q querier, waveID int64) ([]domainskill.Pinned, error) {
	rows, err := q.Query(WithOperation(ctx, "wave_skills.refs"), `
SELECT s.name, v.version FROM easycode.wave_skills ws JOIN easycode.skill_versions v ON v.id = ws.skill_version_id
  JOIN easycode.skills s ON s.id = v.skill_id WHERE ws.wave_id = $1 ORDER BY s.name`, waveID)
	if err != nil {
		return nil, fmt.Errorf("wave skill refs: %w", classify(err))
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domainskill.Pinned, error) {
		var p domainskill.Pinned
		return p, row.Scan(&p.Name, &p.Version)
	})
}
