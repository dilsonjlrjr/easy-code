-- 0014 — exclusão de projeto (MARCO-8, US-04; policy.data_retention, RNF-20).
-- A api grava o pedido (deletion_requested_by/at); o job de retenção do worker, com o papel easycode_retention,
-- destrói o repositório do projeto e anonimiza o dado pessoal do projeto (textos livres humanos, conteúdo de
-- chamadas e checkpoints), grava data_anonymized_at e emite project.data_anonymized. A 0009 criou
-- finding_escalations.justification sem dar UPDATE ao papel de retenção; esta migração corrige.
SET search_path TO easycode, public;

GRANT UPDATE (justification) ON finding_escalations TO easycode_retention;
GRANT INSERT ON events TO easycode_retention;
GRANT INSERT, UPDATE ON project_counters TO easycode_retention;
GRANT USAGE ON SEQUENCE events_global_position_seq TO easycode_retention;

CREATE INDEX projects_pending_anonymization_idx ON projects (deletion_requested_at)
  WHERE deletion_requested_at IS NOT NULL AND data_anonymized_at IS NULL;
COMMENT ON INDEX projects_pending_anonymization_idx IS 'Projetos com exclusão pedida e ainda não anonimizados (job de retenção, US-04/RN-1).';
