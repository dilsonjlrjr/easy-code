-- Reversão manual da 0005 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;
ALTER TABLE waves DROP COLUMN inputs_digest;
DELETE FROM schema_migrations WHERE version = 5;
