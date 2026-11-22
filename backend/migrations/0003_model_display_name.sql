-- 0003 — nome de exibição do modelo e atualização da matriz pela api (US-24 CA-4, US-16, US-37).
-- O contrato (NewModel.display_name, recordModel "cadastra ou atualiza … Idempotente") pede um nome para
-- a interface e a troca das capacidades de um modelo já cadastrado; a tabela models não tinha o nome e a
-- api só tinha INSERT. Modelos já gravados recebem o próprio name como nome de exibição.
SET search_path TO easycode, public;

ALTER TABLE models ADD COLUMN display_name text NOT NULL DEFAULT '';
UPDATE models SET display_name = name WHERE display_name = '';
ALTER TABLE models ADD CONSTRAINT models_display_name_ck CHECK (char_length(display_name) <= 120);
COMMENT ON COLUMN models.display_name IS 'Nome do modelo para a interface (NewModel.display_name); vazio = usar name.';
COMMENT ON CONSTRAINT models_display_name_ck ON models IS 'Até 120 caracteres (NewModel.display_name).';

GRANT UPDATE (display_name, tool_use, context_tokens, prompt_cache, image_input, active) ON models TO easycode_api;
