-- Reversão manual da 0015 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

ALTER TABLE agent_configurations DROP COLUMN IF EXISTS adjustment_instructions;
