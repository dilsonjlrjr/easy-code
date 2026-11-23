-- 0011 — MARCO-7 (US-40, US-50, RNF-09): índices das leituras de visibilidade.
-- * waves_estimate_history_idx: a estimativa de uma etapa (getStageEstimate e o despacho de cada onda) lê as 20
--   ondas `execution` concluídas mais recentes da mesma etapa na organização; sem o índice a leitura varre todas as
--   ondas (20.000 na base do RNF-09, suposição S-4).
-- * waves_project_cost_idx: o gasto acumulado do projeto (dashboard, orçamento, saldo do despacho) soma
--   useful_cost + lost_cost das ondas do projeto; com as colunas no índice a soma é index-only (as ondas de um projeto
--   ficam espalhadas pelas páginas da tabela).
-- * events_project_paused_idx: quem pausou o projeto, quando e por quê (Project.pause, ESP-06 seção 6) vêm do último
--   project.paused e do último project.budget_exhausted; sem o índice a leitura percorre o histórico do projeto.
SET search_path TO easycode, public;

CREATE INDEX waves_estimate_history_idx ON waves (stage, finished_at) WHERE state = 'completed' AND purpose = 'execution';
COMMENT ON INDEX waves_estimate_history_idx IS 'Média histórica da estimativa (US-40/CA-1): ondas execution concluídas da etapa, mais recentes primeiro.';

CREATE INDEX waves_project_cost_idx ON waves (project_id) INCLUDE (useful_cost, lost_cost);
COMMENT ON INDEX waves_project_cost_idx IS 'Gasto acumulado do projeto (US-50/CA-1, US-39, RNF-09): soma index-only de useful_cost + lost_cost.';

CREATE INDEX events_project_paused_idx ON events (project_id, type, sequence) WHERE type IN ('br.easycode.project.paused.v1', 'br.easycode.project.budget_exhausted.v1');
COMMENT ON INDEX events_project_paused_idx IS 'Último project.paused e budget_exhausted do projeto: quem pausou, quando e por quê (ESP-06 seção 6, RNF-09).';
