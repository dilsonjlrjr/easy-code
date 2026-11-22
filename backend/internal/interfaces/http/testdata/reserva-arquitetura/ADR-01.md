---
id: ADR-01
tipo: ADR
titulo: Backend em Go
autor: architect-agent
onda: ARQ-01
versao: 1
estado: rascunho
vinculos: [ARQ]
historico:
- versao: 1
  data: '2026-11-22'
  onda: ARQ-01
  descricao: criação
natureza: imposta
restricoes: [RESTR-01]
contexto: A organização impõe Go no backend (RESTR-01).
opcoes:
- {nome: Go, pros: [Imposto pela organização], contras: [Nenhum relevante]}
decisao: Usar Go com a biblioteca padrão.
consequencias: [Build e testes com go build e go test]
skills: [go]
---
# ADR-01 — Backend em Go
