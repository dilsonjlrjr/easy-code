---
id: ACH-02
tipo: ACH
titulo: Reserva que começa antes de outra e termina durante ela é aceita
autor: qa-agent
onda: QA-02
versao: 1
estado: rascunho
vinculos: [US-01, CT-02]
historico:
  - {versao: 1, data: "2026-11-22", onda: QA-02, descricao: criação}
severidade: alto
categoria: defeito
etapa_destino: implementacao
artefatos_afetados: [US-01/CA-2, app/reserva.go]
evidencia: "go test ./... em app/: --- FAIL: TestConflictStartingBefore (want conflict, got <nil>)"
impacto: Duas reservas da mesma sala no mesmo horário (US-01 RN-1).
correcao_sugerida: Conferir a sobreposição pelos dois extremos do intervalo.
situacao: aberto
ciclos: []
---
# ACH-02 — Conflito não detectado

## Evidência
CT-02 falhou.
## Impacto
## Correção sugerida
## Justificativa do destino
Defeito de código: destino padrão da categoria.
