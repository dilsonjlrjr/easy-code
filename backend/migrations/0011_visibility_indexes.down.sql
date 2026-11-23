-- Reversão manual da 0011 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

DROP INDEX IF EXISTS waves_estimate_history_idx;
DROP INDEX IF EXISTS waves_project_cost_idx;
DROP INDEX IF EXISTS events_project_paused_idx;
