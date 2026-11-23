-- Reversão manual da 0010 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
-- A função artifact_versions_immutable volta à versão da 0001 reaplicando-a de lá (sem RELEASE).
SET search_path TO easycode, public;

REVOKE UPDATE (last_alert_percentage) ON projects FROM easycode_api;
