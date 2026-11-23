-- 0010 — MARCO-6 (US-39, US-08, US-51, US-52, US-12, US-13): orçamento, Release e controle.
-- * projects.last_alert_percentage: quando o responsável muda o limite de parada (modifyProjectConfiguration),
--   a api zera o último alerta emitido; o alerta sai de Pendências e o orquestrador reavalia os alertas pelo novo
--   limite (US-39 RN-4). O estado do projeto continua só do worker.
-- * artifact_versions_immutable: RELEASE ganha campos de acompanhamento (situacao, aprovacao): a decisão humana do
--   gate.release vai para RELEASE.aprovacao sem nova versão (gates.yaml decisions_always_human release_approval;
--   estados.yaml tracking_fields).
SET search_path TO easycode, public;

GRANT UPDATE (last_alert_percentage) ON projects TO easycode_api;

CREATE OR REPLACE FUNCTION artifact_versions_immutable() RETURNS trigger LANGUAGE plpgsql
  SET search_path = easycode, pg_catalog AS $$
DECLARE
  v_type   text;
  v_free_keys text[] := ARRAY['estado','substituido_por','revisado_por'];
BEGIN
  IF pg_has_role(current_user, 'easycode_retention', 'MEMBER') OR OLD.git_commit IS NULL THEN
    RETURN NEW;
  END IF;
  SELECT type INTO v_type FROM artifacts WHERE id = OLD.artifact_id;
  v_free_keys := v_free_keys || CASE v_type
      WHEN 'T'       THEN ARRAY['situacao','justificativa','registro_dev']
      WHEN 'CT'      THEN ARRAY['execucao']
      WHEN 'ACH'     THEN ARRAY['situacao','ciclos','aceite','escalonamentos','resolucao','artefatos_alterados','motivo_descarte']
      WHEN 'AMEACAS' THEN ARRAY['conferencia']
      WHEN 'RELEASE' THEN ARRAY['situacao','aprovacao']
      ELSE ARRAY[]::text[] END;
  IF NEW.artifact_id <> OLD.artifact_id OR NEW.version <> OLD.version OR NEW.body <> OLD.body
     OR NEW.git_commit IS DISTINCT FROM OLD.git_commit OR NEW.path <> OLD.path
     OR (NEW.header - v_free_keys) <> (OLD.header - v_free_keys) THEN
    RAISE EXCEPTION 'versão commitada de artefato é imutável (fora dos campos de acompanhamento)'
      USING ERRCODE = '23514', CONSTRAINT = 'artifact_versions_immutable';
  END IF;
  RETURN NEW;
END $$;
