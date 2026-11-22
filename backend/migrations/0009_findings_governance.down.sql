-- Reversão manual da 0009 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

ALTER TABLE gate_rounds DROP CONSTRAINT gate_rounds_reevaluated_ck;
ALTER TABLE gate_rounds DROP CONSTRAINT gate_rounds_reevaluated_finding_fk;
ALTER TABLE gate_rounds DROP COLUMN reevaluated_by_finding_id;
REVOKE UPDATE (justification) ON finding_escalations FROM easycode_api;
ALTER TABLE finding_escalations DROP CONSTRAINT finding_escalations_justification_ck;
ALTER TABLE finding_escalations DROP COLUMN justification;
