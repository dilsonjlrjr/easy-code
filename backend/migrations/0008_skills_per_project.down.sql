-- Reversão manual da 0008 (desenvolvimento ou teste; o executor só anda para a frente, ADR-18 v2).
SET search_path TO easycode, public;

REVOKE UPDATE (load) ON wave_skills FROM easycode_worker;
REVOKE DELETE ON project_skills FROM easycode_api;
