-- Reversão manual da 0006 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
-- O search_path fixo nas funções fica: tirá-lo só devolveria a dependência da conexão.
SET search_path TO easycode, public;
REVOKE UPDATE (display_name, active, anonymized_at) ON users FROM easycode_worker;
DROP TABLE web_sessions;
DELETE FROM schema_migrations WHERE version = 6;
