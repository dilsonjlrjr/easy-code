-- 0015 — ajuste das instruções do agente no projeto (MARCO-8, US-25).
-- O responsável grava um texto adicional às instruções padrão do agente; cada gravação é uma nova versão
-- imutável da configuração (US-25 CA-2) e a onda aponta a versão usada (agent_configuration_id, CA-4, RNF-15).
SET search_path TO easycode, public;

ALTER TABLE agent_configurations ADD COLUMN adjustment_instructions text NULL;
ALTER TABLE agent_configurations ADD CONSTRAINT agent_configurations_adjustment_ck
  CHECK (adjustment_instructions IS NULL OR char_length(btrim(adjustment_instructions)) BETWEEN 1 AND 20000);
COMMENT ON COLUMN agent_configurations.adjustment_instructions IS 'Texto adicional do projeto às instruções padrão do agente (US-25 CA-2); nulo = só as instruções padrão. Entra no prompt depois das instruções, nunca as substitui.';
COMMENT ON CONSTRAINT agent_configurations_adjustment_ck ON agent_configurations IS 'Ajuste não vazio e de até 20.000 caracteres (openapi AgentConfiguration).';
