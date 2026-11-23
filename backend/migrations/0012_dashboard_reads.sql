-- 0012 — MARCO-7 (US-50, RNF-09): leituras do dashboard e escrita de evento sem varrer o histórico.
-- * project_counters.updated_at: instante do último evento do projeto, mantido pelo outbox junto com a sequence
--   (linha 'event'); é o updated_at do projeto no dashboard, sem ler as partições de events por projeto listado.
-- * events_sequence_no_gap: a mesma regra com uma consulta por fluxo (projeto ou organização), que usa o índice da
--   sequence; o CASE no WHERE não usava índice e lia todos os eventos da organização a cada INSERT.
SET search_path TO easycode, public;

ALTER TABLE project_counters ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
COMMENT ON COLUMN project_counters.updated_at IS 'Última mudança do contador; na linha event, instante (occurred_at) do último evento do projeto, o updated_at do dashboard (US-50, RNF-09).';
UPDATE project_counters c SET updated_at = coalesce((SELECT max(e.occurred_at) FROM events e WHERE e.project_id = c.project_id), c.updated_at)
 WHERE c.name = 'event';

CREATE OR REPLACE FUNCTION events_sequence_no_gap() RETURNS trigger LANGUAGE plpgsql
  SET search_path = easycode, pg_catalog AS $$
BEGIN
  -- One branch per stream so each lookup uses events_sequence_idx or events_sequence_organization_idx: a CASE in
  -- the WHERE was not sargable and read every event of the organization at each insert (RNF-09).
  IF NEW.project_id IS NULL THEN
    IF EXISTS (SELECT 1 FROM events e WHERE e.project_id IS NULL AND e.organization_id = NEW.organization_id AND e.sequence = NEW.sequence) THEN
      RAISE EXCEPTION 'sequencia repetida no fluxo de eventos' USING ERRCODE = '23505', CONSTRAINT = 'events_sequence_no_gap';
    END IF;
    IF NEW.sequence > 1 AND NOT EXISTS (SELECT 1 FROM events e WHERE e.project_id IS NULL AND e.organization_id = NEW.organization_id
                                           AND e.sequence = NEW.sequence - 1) THEN
      RAISE EXCEPTION 'lacuna na sequencia de eventos do fluxo' USING ERRCODE = '23514', CONSTRAINT = 'events_sequence_no_gap';
    END IF;
  ELSE
    IF EXISTS (SELECT 1 FROM events e WHERE e.project_id = NEW.project_id AND e.sequence = NEW.sequence) THEN
      RAISE EXCEPTION 'sequencia repetida no fluxo de eventos' USING ERRCODE = '23505', CONSTRAINT = 'events_sequence_no_gap';
    END IF;
    IF NEW.sequence > 1 AND NOT EXISTS (SELECT 1 FROM events e WHERE e.project_id = NEW.project_id AND e.sequence = NEW.sequence - 1) THEN
      RAISE EXCEPTION 'lacuna na sequencia de eventos do fluxo' USING ERRCODE = '23514', CONSTRAINT = 'events_sequence_no_gap';
    END IF;
  END IF;
  IF NEW.project_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = NEW.project_id AND p.organization_id = NEW.organization_id) THEN
    RAISE EXCEPTION 'projeto de outra organização' USING ERRCODE = '23514', CONSTRAINT = 'events_sequence_no_gap';
  END IF;
  IF NEW.project_id IS NOT NULL AND (NEW.envelope ->> 'projectid') IS DISTINCT FROM (SELECT p.public_id::text FROM projects p WHERE p.id = NEW.project_id) THEN
    RAISE EXCEPTION 'projetoid do envelope não é o id público do projeto' USING ERRCODE = '23514', CONSTRAINT = 'events_envelope_ck';
  END IF;
  IF NEW.published_at IS NOT NULL OR NEW.global_position IS NOT NULL THEN
    RAISE EXCEPTION 'evento nasce pendente: posição global só na publicação' USING ERRCODE = '23514', CONSTRAINT = 'events_sequence_no_gap';
  END IF;
  RETURN NEW;
END $$;
