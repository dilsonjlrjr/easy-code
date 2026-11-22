---
id: ARQ
tipo: ARQ
titulo: Arquitetura da reserva de salas
autor: architect-agent
onda: ARQ-01
versao: 1
estado: rascunho
vinculos: [US-01, ADR-01, T-01]
historico:
- versao: 1
  data: '2026-11-22'
  onda: ARQ-01
  descricao: criação
contexto:
  resumo: Aplicação web pequena para reservar salas sem conflito de horário.
  restricoes: ['RESTR-01: Backend em Go']
  rnf_criticos: [RNF-01]
componentes:
- {nome: api, responsabilidade: Regras de reserva e conflito de horário, tecnologia: Go, atende: [US-01, RNF-01]}
contratos:
  openapi: docs/arquitetura/api/openapi.yaml
  integracoes: []
decisoes: [ADR-01]
diretrizes: [Regra de conflito coberta por teste unitário]
riscos:
- {id: RK-1, descricao: Duas reservas simultâneas da mesma sala, impacto: medio, mitigacao: Conferência de conflito na mesma operação}
---
# Arquitetura

```mermaid
flowchart LR
  usuario --> api
```
