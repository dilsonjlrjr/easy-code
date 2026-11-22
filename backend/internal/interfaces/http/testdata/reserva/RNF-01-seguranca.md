---
id: RNF-01
tipo: RNF
titulo: Requisito de seguranca
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
categoria: seguranca
requisito: Somente usuários autenticados acessam as reservas.
metrica: 100% das rotas exigem login
verificacao: {tipo: teste_automatizado, detalhe: conferido no CI do projeto}
prioridade: must
origem: {tipo: boa_pratica, detalhe: requisito mínimo da instrução do agente}
us_afetadas: [US-01]
---
# RNF-01

Somente usuários autenticados acessam as reservas.
