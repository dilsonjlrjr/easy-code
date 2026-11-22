-- Reversão manual da 0003 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;
REVOKE UPDATE (display_name, tool_use, context_tokens, prompt_cache, image_input, active) ON models FROM easycode_api;
ALTER TABLE models DROP COLUMN display_name;
DELETE FROM schema_migrations WHERE version = 3;
