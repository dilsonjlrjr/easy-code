-- Reversão manual da 0007 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
-- Só reverte com tool_executions sem categoria nova e sem disk_limit gravados.
SET search_path TO easycode, public;
ALTER TABLE waves DROP COLUMN git_commit;
ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_tests_ck;
ALTER TABLE tool_executions DROP COLUMN tests_skipped;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_tests_ck CHECK (tests_total IS NULL OR (tests_passed + tests_failed <= tests_total AND tests_passed >= 0 AND tests_failed >= 0));
ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_limit_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_limit_ck CHECK (end_reason IS NULL OR ((status = 'limit_exceeded') = (end_reason IN ('time_limit','memory_limit','process_limit','output_limit'))));
ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_end_reason_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_end_reason_ck CHECK ((category IS NOT NULL AND status <> 'ongoing') = (end_reason IS NOT NULL)
  AND (end_reason IS NULL OR end_reason IN ('completed','time_limit','memory_limit','process_limit','output_limit','aborted','executor_error')));
ALTER TABLE tool_executions DROP CONSTRAINT tool_executions_category_ck;
ALTER TABLE tool_executions ADD CONSTRAINT tool_executions_category_ck CHECK (category IS NULL OR (permission = 'allowed' AND category IN ('build','test','lint','scanner','validate_openapi','dependencies','other')));
DELETE FROM schema_migrations WHERE version = 7;
