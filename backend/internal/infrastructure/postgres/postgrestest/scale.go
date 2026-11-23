package postgrestest

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Scale is the size of a synthetic base (RNF-09: 100 projects, 20,000 waves, 50 people; assumption S-4).
type Scale struct {
	Users    int // people with the approver role in every project (load-01, load-02…)
	Projects int // one in ten created, paused, completed and canceled; the rest in progress
	Waves    int // spread over the started projects, two model calls each
	// EventsPerWave and AuditPerProject size the event history and the trail.
	EventsPerWave   int
	AuditPerProject int
}

// ScaleS4 is the base of RNF-09.
var ScaleS4 = Scale{Users: 50, Projects: 100, Waves: 20_000, EventsPerWave: 3, AuditPerProject: 60}

// ScaleProjectPrefix names the synthetic projects ("Carga 001"…).
const ScaleProjectPrefix = "Carga "

// The base is written by the superuser with the triggers off (session_replication_role = replica), so the
// rows are consistent by construction: the state machines are not replayed one transition at a time.
var scaleStatements = []string{
	`INSERT INTO easycode.users (organization_id, sub, display_name)
SELECT $1, 'load-' || lpad(g::text, 2, '0'), 'Pessoa ' || g FROM generate_series(1, $2::int) g
ON CONFLICT (organization_id, sub) DO NOTHING`,
	`INSERT INTO easycode.models (provider, name, tool_use, context_tokens, prompt_cache, image_input)
VALUES ('anthropic', 'load-reasoning', true, 200000, true, false), ('openai', 'load-execution', true, 200000, true, false)`,
	`INSERT INTO easycode.model_prices (organization_id, model_id, effective_since, currency, input_price, output_price, read_cache_price,
  write_cache_price, source, recorded_by)
SELECT $1, m.id, now() - interval '120 days', 'BRL', 20, 100, 2, 25, 'base sintética',
       (SELECT id FROM easycode.users WHERE organization_id = $1 AND sub = 'load-01')
  FROM easycode.models m WHERE m.name IN ('load-reasoning', 'load-execution')`,
	`INSERT INTO easycode.projects (organization_id, name, business_need, state, pause_reason, repository, budget_stop_limit,
  provider_acknowledgment_by, provider_acknowledgment_at, created_by, created_at, started_at, finished_at)
SELECT $1, '` + ScaleProjectPrefix + `' || lpad(g::text, 3, '0'),
       'Necessidade sintética ' || g || ': reservar salas de reunião sem conflito de horário, com aprovação e auditoria.',
       s.state, CASE s.state WHEN 'paused' THEN 'manual' END, CASE WHEN s.state <> 'created' THEN 'carga-' || g END,
       CASE WHEN s.state <> 'created' THEN 500 END, u.id, now() - interval '100 days', u.id,
       now() - interval '95 days' + make_interval(hours => g),
       CASE WHEN s.state <> 'created' THEN now() - interval '90 days' END,
       CASE WHEN s.state IN ('completed', 'canceled') THEN now() - interval '1 day' END
  FROM generate_series(1, $2::int) g
  CROSS JOIN LATERAL (SELECT CASE g % 10 WHEN 0 THEN 'created' WHEN 1 THEN 'paused' WHEN 2 THEN 'completed' WHEN 3 THEN 'canceled'
                                         ELSE 'in_progress' END AS state) s
  JOIN easycode.users u ON u.organization_id = $1 AND u.sub = 'load-01'`,
	`INSERT INTO easycode.project_members (project_id, user_id, role, assigned_by)
SELECT p.id, u.id, r.role, lead.id
  FROM easycode.projects p JOIN easycode.users lead ON lead.organization_id = $1 AND lead.sub = 'load-01'
  JOIN easycode.users u ON u.organization_id = $1 AND u.sub LIKE 'load-%'
  CROSS JOIN (VALUES ('approver'), ('lead')) r(role)
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND (r.role = 'approver' OR u.id = lead.id)`,
	`INSERT INTO easycode.stages (project_id, stage, type, state)
SELECT p.id, st.stage, 'stage', CASE WHEN p.state = 'completed' OR st.i < 3 THEN 'approved' WHEN st.i = 3 THEN 'running' ELSE 'blocked' END
  FROM easycode.projects p
  CROSS JOIN (VALUES (0, 'requirements'), (1, 'architecture'), (2, 'data'), (3, 'design'), (4, 'validation'), (5, 'implementation'),
                     (6, 'qa'), (7, 'security'), (8, 'release')) st(i, stage)
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND p.state <> 'created'`,
	`INSERT INTO easycode.agent_configurations (project_id, agent, version, instruction_version, instruction_hash, tools, limits)
SELECT p.id, a.agent, 1, 'v1', repeat('a', 64), '[]', '{}'
  FROM easycode.projects p
  CROSS JOIN (VALUES ('requirements-agent'), ('architect-agent'), ('dba-agent'), ('designer-agent'), ('developer-agent'), ('qa-agent'),
                     ('security-agent')) a(agent)
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND p.state <> 'created'`,
	`INSERT INTO easycode.model_pairs (project_id, reasoning_model_id, execution_model_id, defined_by)
SELECT p.id, (SELECT id FROM easycode.models WHERE name = 'load-reasoning'), (SELECT id FROM easycode.models WHERE name = 'load-execution'), p.created_by
  FROM easycode.projects p WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%'`,
	`INSERT INTO easycode.artifacts (project_id, code, type, title, state, current_version)
SELECT p.id, a.code, a.type, a.title, 'approved', a.version
  FROM easycode.projects p
  CROSS JOIN (VALUES ('ACH-01', 'ACH', 'Achado sintético de defeito', 1), ('ACH-02', 'ACH', 'Achado sintético escalado', 1),
                     ('VISAO', 'VISAO', 'Visão da reserva de salas', 2)) a(code, type, title, version)
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND p.state <> 'created'`,
	// $2 is the number of waves of each started project.
	`INSERT INTO easycode.waves (project_id, stage, stage_id, code, agent, purpose, finding_id, state, agent_configuration_id, max_retries,
  execution_timeout, call_timeout, failure_cause, input_tokens, output_tokens, read_cache_tokens, write_cache_tokens, useful_cost, lost_cost,
  estimated_lost_cost, currency, cost_estimate, estimate_basis, execution_duration_ms, created_at, started_at, finished_at)
SELECT p.id, st.stage, s.id, st.prefix || '-' || lpad(((g - 1) / 8 + 1)::text, 3, '0'), st.agent,
       CASE WHEN g % 10 = 0 THEN 'rework' ELSE 'execution' END,
       CASE WHEN g % 10 = 0 THEN (SELECT a.id FROM easycode.artifacts a WHERE a.project_id = p.id AND a.code = 'ACH-01') END,
       w.state, ac.id, 2, interval '1 hour', interval '5 minutes', CASE w.state WHEN 'failed' THEN 'retries_exhausted' END,
       1000 + g % 500, 200 + g % 100, 0, 0, w.useful, w.lost, g % 26 = 0, 'BRL', 0.01,
       jsonb_build_object('input_size_tokens', 900 + g % 300, 'price_table_version', '1', 'input_price', '20', 'no_history', true,
                          'method', 'input_times_price', 'considered_waves', 0),
       45000 + (g % 60) * 1000, w.created, w.created + interval '1 minute',
       CASE WHEN w.state = 'completed' THEN w.created + interval '1 minute' + make_interval(mins => 10 + g % 50) END
  FROM easycode.projects p
  CROSS JOIN generate_series(1, $2::int) g
  CROSS JOIN LATERAL (SELECT CASE WHEN g = $2::int AND p.state = 'in_progress' THEN 'running' WHEN g % 37 = 0 THEN 'failed' ELSE 'completed' END AS state,
                             ((g % 7) + 1) * 0.0123 AS useful, CASE WHEN g % 13 = 0 THEN 0.005 ELSE 0 END AS lost,
                             p.started_at + make_interval(mins => g * 5) AS created) w
  JOIN (VALUES (0, 'requirements', 'REQ', 'requirements-agent'), (1, 'architecture', 'ARQ', 'architect-agent'), (2, 'data', 'DBA', 'dba-agent'),
               (3, 'design', 'DSG', 'designer-agent'), (4, 'validation', 'VAL', 'architect-agent'), (5, 'implementation', 'DEV', 'developer-agent'),
               (6, 'qa', 'QA', 'qa-agent'), (7, 'security', 'SEG', 'security-agent')) st(i, stage, prefix, agent) ON st.i = (g - 1) % 8
  JOIN easycode.stages s ON s.project_id = p.id AND s.stage = st.stage
  JOIN easycode.agent_configurations ac ON ac.project_id = p.id AND ac.agent = st.agent
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND p.state <> 'created'`,
	`INSERT INTO easycode.wave_models (wave_id, role, model_id, mode, reason)
SELECT w.id, r.role, m.id, 'manual', 'project_configuration'
  FROM easycode.waves w JOIN easycode.projects p ON p.id = w.project_id
  CROSS JOIN (VALUES ('reasoning', 'load-reasoning'), ('execution', 'load-execution')) r(role, model)
  JOIN easycode.models m ON m.name = r.model
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%'`,
	// Two calls per wave share its useful cost (60/40); a wave with lost cost has a third, aborted call.
	`INSERT INTO easycode.model_calls (wave_id, attempt, step, model_id, role, price_id, input_tokens, output_tokens, cost, currency, lost,
  estimated_cost, result, input_hash, started_at, finished_at)
SELECT w.id, 1, c.step, m.id, c.role, mp.id, w.input_tokens / 2, w.output_tokens / 2,
       CASE c.step WHEN 1 THEN w.useful_cost * 0.6 WHEN 2 THEN w.useful_cost - w.useful_cost * 0.6 ELSE w.lost_cost END,
       'BRL', c.step = 3, c.step = 3 AND w.estimated_lost_cost, CASE WHEN c.step = 3 THEN 'aborted' ELSE 'success' END,
       encode(sha256(convert_to(w.id::text || ':' || c.step, 'UTF8')), 'hex'),
       w.started_at + make_interval(secs => c.step * 20), w.started_at + make_interval(secs => c.step * 20 + 15)
  FROM easycode.waves w JOIN easycode.projects p ON p.id = w.project_id
  CROSS JOIN (VALUES (1, 'reasoning', 'load-reasoning'), (2, 'execution', 'load-execution'), (3, 'reasoning', 'load-reasoning')) c(step, role, model)
  JOIN easycode.models m ON m.name = c.model
  JOIN easycode.model_prices mp ON mp.model_id = m.id AND mp.organization_id = $1
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND (c.step < 3 OR w.lost_cost > 0)`,
	`INSERT INTO easycode.findings (artifact_id, project_id, origin_wave_id, origin_stage, destination_stage, severity, category, status)
SELECT a.id, p.id, (SELECT min(w.id) FROM easycode.waves w WHERE w.project_id = p.id), 'qa', 'requirements',
       CASE a.code WHEN 'ACH-01' THEN 'high' ELSE 'medium' END, 'defect', CASE a.code WHEN 'ACH-01' THEN 'open' ELSE 'escalated' END
  FROM easycode.artifacts a JOIN easycode.projects p ON p.id = a.project_id
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND a.type = 'ACH'`,
	// Two versions of the VISAO with a long body; version 2 was driven by ACH-01 (US-27).
	`INSERT INTO easycode.artifact_versions (artifact_id, version, wave_id, author, wave_code, description, finding_id, path, header, body, content_hash)
SELECT a.id, v.version, rw.id, 'requirements-agent', rw.code, CASE v.version WHEN 1 THEN 'criação' ELSE 'ajuste pelo achado' END,
       CASE v.version WHEN 2 THEN (SELECT f.id FROM easycode.artifacts f WHERE f.project_id = p.id AND f.code = 'ACH-01') END,
       'docs/requisitos/VISAO.md',
       jsonb_build_object('id', 'VISAO', 'tipo', 'VISAO', 'versao', v.version, 'titulo', 'Visão da reserva de salas', 'onda', rw.code,
                          'objetivo', CASE v.version WHEN 1 THEN 'Reservar salas.' ELSE 'Reservar salas sem conflito de horário.' END),
       (SELECT string_agg(CASE WHEN v.version = 2 AND l % 9 = 0 THEN 'Linha revista ' || l ELSE 'Linha ' || l || ' da visão sintética.' END, E'\n' ORDER BY l)
          FROM generate_series(1, 300) l),
       repeat(v.version::text, 64)
  FROM easycode.artifacts a JOIN easycode.projects p ON p.id = a.project_id
  CROSS JOIN (VALUES (1), (2)) v(version)
  CROSS JOIN LATERAL (SELECT w.id, w.code FROM easycode.waves w WHERE w.project_id = p.id AND w.stage = 'requirements' ORDER BY w.id LIMIT 1) rw
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND a.code = 'VISAO'`,
	`INSERT INTO easycode.gate_rounds (project_id, stage, stage_id, number, state, approval_mode)
SELECT p.id, s.stage, s.id, 1, 'awaiting_human', 'human'
  FROM easycode.projects p JOIN easycode.stages s ON s.project_id = p.id AND s.stage = 'design'
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND p.state = 'in_progress'`,
	// $2 is the number of events per wave.
	`INSERT INTO easycode.events (public_id, organization_id, project_id, sequence, type, subject, waveid, correlationid, occurred_at, envelope)
SELECT x.uid, $1, x.project_id, x.seq, 'br.easycode.wave.started.v1', 'waves/' || x.code, x.code, x.correlation, x.at,
       jsonb_build_object('id', x.uid::text, 'sequence', x.seq, 'projectid', x.project_public, 'type', 'br.easycode.wave.started.v1',
                          'source', '/easycode', 'specversion', '1.0', 'time', x.at, 'data', '{}'::jsonb)
  FROM (SELECT gen_random_uuid() AS uid, gen_random_uuid() AS correlation, w.project_id, p.public_id::text AS project_public, w.code,
               w.created_at + make_interval(secs => k) AS at,
               row_number() OVER (PARTITION BY w.project_id ORDER BY w.id, k) AS seq
          FROM easycode.waves w JOIN easycode.projects p ON p.id = w.project_id CROSS JOIN generate_series(1, $2::int) k
         WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%') x`,
	`INSERT INTO easycode.project_counters (project_id, name, value)
SELECT e.project_id, 'event', max(e.sequence) FROM easycode.events e JOIN easycode.projects p ON p.id = e.project_id
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' GROUP BY e.project_id`,
	// $2 is the number of records per project; a tenth of them goes to the trail of the organization.
	`INSERT INTO easycode.audit_records (organization_id, project_id, recorded_at, actor_type, actor_id, origin, action, target, justification, correlationid)
SELECT $1, CASE WHEN k % 10 = 0 THEN NULL ELSE p.id END, p.started_at + make_interval(hours => k), 'human', 'load-' || lpad((k % 50 + 1)::text, 2, '0'),
       'web', a.action, CASE WHEN k % 10 = 0 THEN 'models/load-reasoning' ELSE 'waves/REQ-001' END,
       CASE a.action WHEN 'gate.reject' THEN 'Motivo sintético da devolução' END, gen_random_uuid()
  FROM easycode.projects p CROSS JOIN generate_series(1, $2::int) k
  CROSS JOIN LATERAL (SELECT CASE WHEN k % 10 = 0 THEN 'price.record'
                                  ELSE (ARRAY['gate.approve', 'gate.reject', 'wave.pause', 'wave.resume', 'project.pause', 'configuration.modify'])[k % 6 + 1]
                             END AS action) a
 WHERE p.organization_id = $1 AND p.name LIKE '` + ScaleProjectPrefix + `%' AND p.state <> 'created'`,
}

// SeedScale writes the synthetic base of s into the (only) organization of the database and returns the
// public ids of its projects. It takes some seconds at the scale of RNF-09.
func (d *Database) SeedScale(s Scale) []string {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var organization int64
	if err := d.Admin.QueryRow(ctx, "SELECT min(id) FROM easycode.organizations").Scan(&organization); err != nil {
		d.t.Fatal(err)
	}
	started := s.Projects - s.Projects/10
	if started < 1 {
		started = 1
	}
	perProject := (s.Waves + started - 1) / started
	err := pgx.BeginFunc(ctx, d.Admin, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
			return err
		}
		for i, sql := range scaleStatements {
			arg := 0
			switch i {
			case 0:
				arg = s.Users
			case 3:
				arg = s.Projects
			case 9:
				arg = perProject
			case 15:
				arg = s.EventsPerWave
			case 17:
				arg = s.AuditPerProject
			}
			var args []any
			if strings.Contains(sql, "$1") {
				args = append(args, organization)
			}
			if strings.Contains(sql, "$2") {
				args = append(args, arg)
			}
			if _, err := tx.Exec(ctx, sql, args...); err != nil {
				d.t.Fatalf("scale statement %d: %v", i, err)
			}
		}
		return nil
	})
	if err != nil {
		d.t.Fatal(err)
	}
	// What autovacuum does in production after a bulk of writes: statistics and the visibility map.
	if _, err := d.Admin.Exec(ctx, "VACUUM ANALYZE"); err != nil {
		d.t.Fatal(err)
	}
	rows, err := d.Admin.Query(ctx, "SELECT public_id::text FROM easycode.projects WHERE organization_id = $1 AND name LIKE $2 ORDER BY name",
		organization, ScaleProjectPrefix+"%")
	if err != nil {
		d.t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		d.t.Fatal(err)
	}
	return ids
}
