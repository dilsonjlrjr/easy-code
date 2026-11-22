-- 0008 — MARCO-4 (US-43): skills associadas ao projeto.
-- * O responsável retira uma skill de um agente; a linha de project_skills sem nenhum agente é apagada
--   (project_skills_agents_ck exige ao menos um), então a api precisa de DELETE.
-- * A onda carrega a skill pela descrição e marca o conteúdo completo quando o agente o lê (US-43/RN-1):
--   o worker atualiza wave_skills.load de description para complete.
SET search_path TO easycode, public;

GRANT DELETE ON project_skills TO easycode_api;
GRANT UPDATE (load) ON wave_skills TO easycode_worker;
