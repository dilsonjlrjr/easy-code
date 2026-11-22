-- 0004 — a api remove o ajuste de par de modelos por etapa (US-16 CA-3, modifyProjectConfiguration).
-- O contrato aceita `pair_by_stage.<etapa>: null` (JSON Merge Patch) para voltar ao par do projeto. A linha de
-- model_pairs não é histórico: cada onda guarda o par com que começou em wave_models (US-16 CA-4, RNF-15).
SET search_path TO easycode, public;

GRANT DELETE ON model_pairs TO easycode_api;
