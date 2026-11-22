-- Reversão manual da 0004 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;
REVOKE DELETE ON model_pairs FROM easycode_api;
DELETE FROM schema_migrations WHERE version = 4;
