---
id: US-01
tipo: US
titulo: Reservar sala disponível
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
- versao: 1
  data: '2026-11-22'
  onda: REQ-01
  descricao: criação
ator: Funcionário
quero: reservar uma sala disponível numa data e num horário
para: usar a sala numa reunião sem conflito
prioridade: must
origem: fazem reservas e cancelam as próprias. Não pode haver duas reservas da mesma sala no mesmo horário.
tem_interface: true
criterios_aceite:
- {id: CA-1, dado: sala livre no horário, quando: reservo, entao: a reserva aparece na minha lista de reservas futuras}
- {id: CA-2, dado: sala já reservada no horário, quando: tento reservar, entao: a reserva é recusada com o aviso de conflito}
regras_negocio:
- {id: RN-1, descricao: Não pode haver duas reservas da mesma sala no mesmo horário.}
perguntas_abertas: []
rnf: [RNF-01, RNF-02, RNF-03, RNF-04]
---
# US-01 — Reservar sala disponível

Como funcionário, quero reservar uma sala disponível.
