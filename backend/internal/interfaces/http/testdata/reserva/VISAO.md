---
id: VISAO
tipo: VISAO
titulo: Visão da reserva de salas
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
problema: Funcionários reservam salas de reunião sem controle e há reservas da mesma sala no mesmo horário.
atores:
- {nome: Funcionário, quem_e: pessoa da empresa com login, necessidade: reservar e cancelar as próprias reservas}
- {nome: Administrador, quem_e: responsável pelas salas, necessidade: cadastrar salas e cancelar qualquer reserva}
objetivo: Reservar salas pelo navegador sem conflito de horário.
metricas_sucesso: [Nenhuma reserva sobreposta da mesma sala]
fora_de_escopo: [Integração com agenda corporativa]
glossario:
- {termo: Sala, definicao: "espaço de reunião com nome, capacidade e recursos"}
- {termo: Reserva, definicao: uso de uma sala por um funcionário numa data e num horário}
---
# Visão

Sistema web para reservar salas de reunião sem conflito de horário.
