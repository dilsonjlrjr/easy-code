-- Reversão manual da 0019 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

DELETE FROM tool_executions WHERE category = 'agent_session';
ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_category_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_category_ck CHECK (category IS NULL OR (permission = 'allowed' AND category IN
  ('build','tests','lint','migrations','package_manager','disposable_database','validate_ddl','validate_openapi','validate_schema',
   'local_static_server','scanner','local_http_client','install_dependencies')));
COMMENT ON CONSTRAINT tool_executions_category_ck ON tool_executions IS 'Categorias de command_categories (permissoes.yaml); só em chamada allowed.';

DROP TABLE IF EXISTS claude_code_runtime;
REVOKE SELECT (credential_kind) ON provider_keys FROM easycode_api;
ALTER TABLE provider_keys DROP CONSTRAINT IF EXISTS provider_keys_credential_kind_ck;
ALTER TABLE provider_keys DROP COLUMN IF EXISTS credential_kind;
