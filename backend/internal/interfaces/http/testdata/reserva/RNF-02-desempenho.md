---
id: RNF-02
tipo: RNF
titulo: Requisito de desempenho
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
categoria: desempenho
requisito: A lista de salas disponíveis responde rápido.
metrica: p95 abaixo de 1 s com 100 usuários
verificacao: {tipo: medicao, detalhe: conferido no CI do projeto}
prioridade: must
origem: {tipo: boa_pratica, detalhe: requisito mínimo da instrução do agente}
us_afetadas: [US-01]
---
# RNF-02

A lista de salas disponíveis responde rápido.
