---
id: ACH-01
tipo: ACH
titulo: Conflito de horário sem garantia no banco na decisão de persistência
autor: dba-agent
onda: DBA-01
versao: 1
estado: rascunho
vinculos: [ADR-01, DIC]
historico:
  - {versao: 1, data: "2026-11-22", onda: DBA-01, descricao: criação}
severidade: alto
categoria: desvio_arquitetura
etapa_origem: dados
etapa_destino: arquitetura
artefatos_afetados: [ADR-01, US-01/RN-1]
evidencia: |
  ARQ, RK-1: "Conferência de conflito na mesma operação" deixa a regra US-01 RN-1 na aplicação;
  sob concorrência duas reservas passam. O modelo garante com EXCLUDE reservas_sem_conflito (DIC).
impacto: Duas reservas da mesma sala no mesmo horário sob concorrência
correcao_sugerida: A mitigação de RK-1 passa a citar a constraint do banco
situacao: aberto
ciclos: []
---
# ACH-01 — Conflito de horário sem garantia no banco

A decisão de persistência da arquitetura é reprovada pelo DBA (US-20 RN-1).
