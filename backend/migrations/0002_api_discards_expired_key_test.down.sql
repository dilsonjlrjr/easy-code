-- Reversão manual da 0002 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;
REVOKE UPDATE (test_result, tested_at) ON provider_keys FROM easycode_api;
DELETE FROM schema_migrations WHERE version = 2;
