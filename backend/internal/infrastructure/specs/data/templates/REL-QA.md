# Template — Relatório de QA (REL-QA) — v1

- **Esquema:** specs/dominio/artefatos/rel-qa.schema.json
- **Quem preenche:** agente de QA, ao fim da execução dos testes.
- **Quem lê:** aprovador humano (gate de QA), Segurança, Release.
- **Gate confere:** todo critério de aceite de US Must com CT passando.
- **Arquivo:** `docs/qa/REL-QA-NN.md` (um por execução).

## Molde

````markdown
---
id: REL-QA-NN
tipo: REL-QA
titulo: Relatório de QA — execução NN
autor: qa-agent
onda: QA-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []                  # relação genérica; CT e ACH vão nos campos próprios
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: QA-<nn>, descricao: criação}
execucao: 1
resumo:
  ct_executados: <n>
  passaram: <n>
  falharam: <n>
  bloqueados: <n>
  ca_must_cobertos: <x>       # CA de US Must com CT passando
  ca_must_total: <y>
  # cobertura_codigo: <0-100> # se medida
matriz:
  - {us: US-NN, criterio: CA-N, ct: CT-NN, nivel: integracao, resultado: passou}   # achado: ACH-NN se falhou
sem_cobertura: []             # {us, criterio, motivo}; [] se nenhum
comandos:                     # ao menos 1; rodam do zero em ambiente limpo
  - <comando exato>
achados_abertos: []           # ids de ACH abertos
veredito: reprovado           # aprovado | reprovado
---

# Relatório de QA — execução NN

## Resumo
## Matriz de rastreabilidade
## Critérios sem cobertura
## Como rodar os testes
## Observações
<instabilidades, testes intermitentes, limitações do ambiente>
````

## Regras

- Veredito `aprovado` só se `ca_must_cobertos` = `ca_must_total` e não há ACH Crítico ou Alto
  aberto. O esquema não compara números: é checagem de governança do gate.
- Teste intermitente conta como falhou até ser estabilizado.
- Comandos de execução precisam rodar do zero num ambiente limpo (a revalidação e o Release vão usar).
