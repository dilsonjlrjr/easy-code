-- 0007 — MARCO-3 (US-41, US-19, US-42): evidência do sandbox e commit da onda.
-- * tool_executions.category passa a usar as categorias de permissoes.yaml (command_categories), as mesmas
--   de SandboxExecution.category na API; a lista antiga (test, dependencies, other) não tinha linha gravada.
-- * end_reason ganha disk_limit (cota do workspace, S-13), igual ao EndReason do executor e da API.
-- * tests_skipped: testes ignorados (SandboxExecution.tests.ignored).
-- * waves.git_commit: commit do repositório do projeto que fechou a onda (D-17, US-19 CA-1, Repository.last_commit).
SET search_path TO easycode, public;

ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_category_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_category_ck CHECK (category IS NULL OR (permission = 'allowed' AND category IN
  ('build','tests','lint','migrations','package_manager','disposable_database','validate_ddl','validate_openapi','validate_schema',
   'local_static_server','scanner','local_http_client','install_dependencies')));
COMMENT ON CONSTRAINT tool_executions_category_ck ON tool_executions IS 'Categorias de command_categories (permissoes.yaml); só em chamada allowed.';

ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_end_reason_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_end_reason_ck CHECK ((category IS NOT NULL AND status <> 'ongoing') = (end_reason IS NOT NULL)
  AND (end_reason IS NULL OR end_reason IN ('completed','time_limit','memory_limit','process_limit','disk_limit','output_limit','aborted','executor_error')));
COMMENT ON CONSTRAINT tool_executions_end_reason_ck ON tool_executions IS 'Execução no sandbox terminada registra o motivo do fim (enumeração EndReason única com o runner e a API).';

ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_limit_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_limit_ck CHECK (end_reason IS NULL
  OR ((status = 'limit_exceeded') = (end_reason IN ('time_limit','memory_limit','process_limit','disk_limit','output_limit'))));
COMMENT ON CONSTRAINT tool_executions_limit_ck ON tool_executions IS 'regra: US-41/CA-3 — limite excedido registra o recurso (end_reason *_limit).';

ALTER TABLE tool_executions ADD COLUMN tests_skipped integer NULL;
COMMENT ON COLUMN tool_executions.tests_skipped IS 'Testes ignorados (SandboxExecution.tests.ignored).';
ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_tests_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_tests_ck CHECK (tests_total IS NULL
  OR (tests_passed + tests_failed + coalesce(tests_skipped, 0) <= tests_total AND tests_passed >= 0 AND tests_failed >= 0 AND coalesce(tests_skipped, 0) >= 0));
COMMENT ON CONSTRAINT tool_executions_tests_ck ON tool_executions IS 'Contagem de testes coerente.';

ALTER TABLE waves ADD COLUMN git_commit text NULL;
ALTER TABLE waves ADD CONSTRAINT waves_git_commit_ck CHECK (git_commit IS NULL OR git_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$');
COMMENT ON COLUMN waves.git_commit IS 'Commit do repositório do projeto gravado ao concluir a onda (D-17; US-19 CA-1; Repository.last_commit).';
COMMENT ON CONSTRAINT waves_git_commit_ck ON waves IS 'Hash de commit git (SHA-1 ou SHA-256).';
