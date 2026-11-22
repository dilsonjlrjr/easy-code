---
id: AMEACAS
tipo: AMEACAS
titulo: Modelo de ameaças — Reserva de salas
autor: security-agent
onda: SEG-01
versao: 1
estado: rascunho
vinculos: [ARQ]
historico:
  - {versao: 1, data: "2026-11-22", onda: SEG-01, descricao: criação}
ativos: [Reservas das salas, Identidade dos funcionários]
atores: [Funcionário autenticado, Atacante na rede interna]
fluxo: "flowchart LR; usuario --> api"
ameacas:
  - id: AM-1
    fronteira: usuário → api
    stride: [e]
    descricao: Um funcionário cancela a reserva de outro, pois a api não confere o dono da reserva.
    probabilidade: media
    impacto: alto
    severidade: alto
    onde: [ARQ]
    status: achado
    achado: ACH-01
---
# Modelo de ameaças
