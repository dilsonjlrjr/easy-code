-- Reversão manual da 0018 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

DROP TABLE IF EXISTS gate_round_annotations;
