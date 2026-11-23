-- Reversão manual da 0014 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

DROP INDEX IF EXISTS projects_pending_anonymization_idx;
REVOKE USAGE ON SEQUENCE events_global_position_seq FROM easycode_retention;
REVOKE INSERT ON events FROM easycode_retention;
REVOKE INSERT, UPDATE ON project_counters FROM easycode_retention;
REVOKE UPDATE (justification) ON finding_escalations FROM easycode_retention;
