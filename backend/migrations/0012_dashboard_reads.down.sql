-- Reversão manual da 0012 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
-- events_sequence_no_gap fica na forma da 0012 (mesma regra; a da 0001 só era mais lenta).
SET search_path TO easycode, public;

ALTER TABLE project_counters DROP COLUMN IF EXISTS updated_at;
