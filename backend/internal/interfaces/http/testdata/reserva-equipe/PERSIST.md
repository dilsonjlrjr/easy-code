---
id: PERSIST
tipo: PERSIST
titulo: Estratégia de persistência — Reserva de salas
autor: dba-agent
onda: DBA-01
versao: 1
estado: rascunho
vinculos: [DIC, RNF-02]
historico:
  - {versao: 1, data: "2026-11-22", onda: DBA-01, descricao: criação}
banco: {sgbd: PostgreSQL, versao: "18"}
acesso: SQL explícito com pgx, conforme ADR-01
concorrencia:
  - {situacao: Duas reservas da mesma sala ao mesmo tempo, garantia: Constraint EXCLUDE reservas_sem_conflito}
indices:
  - {nome: reservas_sala_periodo_idx, tabela: reservas, colunas: [sala, periodo], tipo: gist, justificativa: Conferência de conflito e reservas futuras da sala}
migracoes: {ferramenta: migrações SQL numeradas, convencao: migração aplicada nunca é editada}
desempenho: []
backup: {sem_requisito: true, justificativa: Projeto de referência sem requisito de recuperação}
permissoes:
  - {papel: reserva_app, privilegios: [SELECT, INSERT, DELETE], ddl_em_producao: false}
protecao_dados_pessoais: O autor não vai para os logs
---
# Estratégia de persistência — Reserva de salas
