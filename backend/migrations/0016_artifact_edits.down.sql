-- Reversão manual da 0016 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

DROP TABLE IF EXISTS artifact_edits;
