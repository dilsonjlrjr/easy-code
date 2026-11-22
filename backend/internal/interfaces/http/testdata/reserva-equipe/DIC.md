---
id: DIC
tipo: DIC
titulo: Dicionário de dados — Reserva de salas
autor: dba-agent
onda: DBA-01
versao: 1
estado: rascunho
vinculos: [ADR-01]
historico:
  - {versao: 1, data: "2026-11-22", onda: DBA-01, descricao: criação}
tabelas:
  - nome: reservas
    descricao: Uma reserva de uma sala num intervalo de horário
    colunas:
      - {nome: id, tipo: bigint identity, nulo: false, padrao: null, descricao: Identificador, dado_pessoal: false}
      - {nome: sala, tipo: text, nulo: false, padrao: null, descricao: Sala reservada, dado_pessoal: false}
      - {nome: periodo, tipo: tstzrange, nulo: false, padrao: null, descricao: Início e fim da reserva, dado_pessoal: false}
      - {nome: autor, tipo: text, nulo: false, padrao: null, descricao: Quem reservou, dado_pessoal: true}
    constraints:
      - {nome: reservas_pkey, tipo: primary_key, definicao: PRIMARY KEY (id)}
      - {nome: reservas_sem_conflito, tipo: exclude, definicao: "EXCLUDE USING gist (sala WITH =, periodo WITH &&)", regra: US-01/RN-1}
    indices: [reservas_sala_periodo_idx]
dados_pessoais:
  - {campo: reservas.autor, finalidade: Mostrar as reservas de cada funcionário, base_legal: a_confirmar, retencao: Enquanto a reserva existir}
---
# Dicionário de dados — Reserva de salas

A regra de conflito (US-01 RN-1) é a constraint `reservas_sem_conflito`.
