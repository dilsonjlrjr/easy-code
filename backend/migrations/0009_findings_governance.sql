-- 0009 — MARCO-5 (US-31, US-32, US-33, US-34): motor de governança e máquina do achado.
-- * finding_escalations.justification: a decisão humana sobre achado escalado (rodada extra, mudança de escopo,
--   aceite de risco) registra a justificativa pedida pelo contrato (EscalationDecision, ScopeChangeDecision); a
--   api grava junto com a decisão.
-- * A api confirma as checagens humanas da rodada no aprovar (check_executions) e passa a gravar a
--   justificativa no escalonamento.
SET search_path TO easycode, public;

ALTER TABLE finding_escalations ADD COLUMN justification text NULL;
ALTER TABLE finding_escalations ADD CONSTRAINT finding_escalations_justification_ck
  CHECK (justification IS NULL OR (decision IS NOT NULL AND btrim(justification) <> ''));
COMMENT ON COLUMN finding_escalations.justification IS 'Justificativa da decisão humana (rodada extra, mudança de escopo, aceite de risco); nula sem decisão.';

GRANT UPDATE (justification) ON finding_escalations TO easycode_api;

-- * gate_rounds.reevaluated_by_finding_id: o ACH Crítico/Alto aberto durante a espera humana que levou a rodada de
--   volta a evaluating (US-31 CA-3); a tela do gate mostra a causa da reavaliação.
ALTER TABLE gate_rounds ADD COLUMN reevaluated_by_finding_id bigint NULL;
ALTER TABLE gate_rounds ADD CONSTRAINT gate_rounds_reevaluated_finding_fk
  FOREIGN KEY (reevaluated_by_finding_id) REFERENCES findings (artifact_id) ON DELETE RESTRICT;
ALTER TABLE gate_rounds ADD CONSTRAINT gate_rounds_reevaluated_ck
  CHECK (reevaluated_by_finding_id IS NULL OR current_evaluation > 1);
COMMENT ON COLUMN gate_rounds.reevaluated_by_finding_id IS 'ACH cuja abertura reexecutou as checagens da rodada (reevaluation_started); nulo sem reavaliação.';
