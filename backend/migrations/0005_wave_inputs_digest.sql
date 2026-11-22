-- 0005 — resumo SHA-256 das entradas da onda (RNF-15; Wave.versions.inputs_summary_sha256 da API).
-- O runtime calcula o resumo no início da tentativa (instruções na versão fixada, necessidade de negócio,
-- restrições e skills); a onda o guarda para conferir a reprodutibilidade.
SET search_path TO easycode, public;

ALTER TABLE waves ADD COLUMN inputs_digest text NULL;
ALTER TABLE waves ADD CONSTRAINT waves_inputs_digest_ck CHECK (inputs_digest IS NULL OR inputs_digest ~ '^[0-9a-f]{64}$');
COMMENT ON COLUMN waves.inputs_digest IS 'SHA-256 das entradas da onda (instruções, necessidade, restrições, skills), RNF-15.';
COMMENT ON CONSTRAINT waves_inputs_digest_ck ON waves IS 'SHA-256 em hexadecimal.';
