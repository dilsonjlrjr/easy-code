-- 0019 — Claude Code do servidor como executor do agente (MARCO-11, US-58, ADR-23). O provedor claude-code
-- guarda a credencial como as outras chaves (US-15), com o tipo derivado do prefixo; a detecção do binário
-- feita pelo easycode-sandbox fica numa linha única, gravada pelo worker e lida pela API. Nenhum segredo aqui.
SET search_path TO easycode, public;

ALTER TABLE provider_keys ADD COLUMN credential_kind text NULL;
ALTER TABLE provider_keys ADD CONSTRAINT provider_keys_credential_kind_ck
  CHECK (credential_kind IS NULL OR credential_kind IN ('api_key','subscription_token'));
COMMENT ON COLUMN provider_keys.credential_kind IS 'Tipo da credencial do provedor claude-code, derivado do prefixo antes de selar (sk-ant-oat = subscription_token; senão api_key); nulo nos demais provedores (US-58 CA-1, CA-4). Nunca guarda o valor.';
COMMENT ON CONSTRAINT provider_keys_credential_kind_ck ON provider_keys IS 'regra: US-58/CA-4 — chave de API ou token da assinatura (uso individual).';

GRANT SELECT (credential_kind) ON provider_keys TO easycode_api;

CREATE TABLE claude_code_runtime (
  id          smallint    NOT NULL DEFAULT 1,
  detected    boolean     NOT NULL,
  version     text        NULL,
  checked_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT claude_code_runtime_pkey PRIMARY KEY (id),
  CONSTRAINT claude_code_runtime_single_ck CHECK (id = 1),
  CONSTRAINT claude_code_runtime_version_ck CHECK (version IS NULL OR char_length(version) BETWEEN 1 AND 128)
);
COMMENT ON TABLE claude_code_runtime IS 'Detecção do Claude Code no servidor (US-58 CA-1, CA-2; ADR-23): uma linha, gravada pelo worker a cada verificação do easycode-sandbox; sem linha = ainda não verificado (não detectado).';
COMMENT ON COLUMN claude_code_runtime.id IS 'Sempre 1: uma detecção por instalação.';
COMMENT ON COLUMN claude_code_runtime.detected IS 'O easycode-sandbox rodou claude --version com o binário montado (sessões de agente possíveis).';
COMMENT ON COLUMN claude_code_runtime.version IS 'Versão informada pelo binário; nula quando não detectado.';
COMMENT ON COLUMN claude_code_runtime.checked_at IS 'Quando o worker gravou a última verificação.';
COMMENT ON CONSTRAINT claude_code_runtime_single_ck ON claude_code_runtime IS 'Uma linha só.';

GRANT SELECT ON claude_code_runtime TO easycode_api, easycode_worker, easycode_backup;
GRANT INSERT, UPDATE ON claude_code_runtime TO easycode_worker;

-- A execução do Claude Code (categoria agent_session do runner) é evidência da onda como os comandos do sandbox:
-- a saída stream-json (transcrição) e a versão no resumo (RNF-13, RNF-15).
ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_category_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_category_ck CHECK (category IS NULL OR (permission = 'allowed' AND category IN
  ('build','tests','lint','migrations','package_manager','disposable_database','validate_ddl','validate_openapi','validate_schema',
   'local_static_server','scanner','local_http_client','install_dependencies','agent_session')));
COMMENT ON CONSTRAINT tool_executions_category_ck ON tool_executions IS 'Categorias de command_categories (permissoes.yaml) e agent_session (execução do Claude Code, ADR-23); só em chamada allowed.';
