-- Reversão manual da migração 0001_initial_schema (ambiente de desenvolvimento ou teste).
-- O executor de migrações não aplica este arquivo: ele só lê NNNN_nome.sql e só anda para a frente
-- (ADR-18 v2). Em produção não se reverte a 0001: restaura-se a cópia de segurança (RNF-11).
-- Apaga o esquema inteiro, com dados, partições e funções. Os papéis easycode_* são do cluster e ficam;
-- a linha da 0001 em schema_migrations sai para a próxima aplicação refazer o esquema.
DROP SCHEMA IF EXISTS easycode CASCADE;
DELETE FROM schema_migrations WHERE version = 1;
