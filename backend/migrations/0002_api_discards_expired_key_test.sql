-- 0002 — a api descarta o teste de chave vencido (US-15, interno.md seção 1, ADR-13 v2 A4).
-- "Passado o prazo de 15 s, a api descarta a linha se ainda estiver testing": o descarte grava o resultado
-- (test_result, obrigatório em discarded) e a data do teste (tested_at, obrigatória fora de testing), colunas
-- que só o worker podia alterar. Sem elas a api não cumpre o fluxo; nenhum outro privilégio muda.
SET search_path TO easycode, public;

GRANT UPDATE (test_result, tested_at) ON provider_keys TO easycode_api;
