---
id: ACH-01
tipo: ACH
titulo: Arquitetura sem controle de acesso por dono da reserva
autor: security-agent
onda: SEG-01
versao: 1
estado: rascunho
vinculos: [ARQ, AMEACAS]
historico:
  - {versao: 1, data: "2026-11-22", onda: SEG-01, descricao: criação}
severidade: alto
categoria: desvio_arquitetura
etapa_destino: arquitetura
artefatos_afetados: [ARQ]
evidencia: "AMEACAS/AM-1: o componente api não confere quem é o dono da reserva antes de cancelar."
impacto: Funcionário cancela reserva de outro (acesso indevido).
correcao_sugerida: Incluir no ARQ a autorização por dono da reserva no componente api.
situacao: aberto
ciclos: []
---
# ACH-01 — Controle de acesso por dono da reserva

## Evidência
Ver AMEACAS/AM-1.
## Impacto
## Correção sugerida
## Justificativa do destino
Desvio de arquitetura: destino padrão da categoria.
