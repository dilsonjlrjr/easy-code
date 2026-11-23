-- Reversão manual da 0013 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

DROP TABLE IF EXISTS local_client_sessions;
