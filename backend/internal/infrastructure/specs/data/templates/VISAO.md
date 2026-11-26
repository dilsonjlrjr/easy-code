# Template — Visão do produto (VISAO) — v1

- **Esquema:** specs/dominio/artefatos/visao.schema.json
- **Quem preenche:** agente de Requisitos, primeiro artefato da etapa.
- **Quem lê:** todas as etapas (contexto) e o aprovador humano (gate de Requisitos).
- **Gate confere:** problema, público, objetivo e fora de escopo preenchidos; todo termo de
  domínio usado nas US está no glossário.
- **Arquivo:** `docs/requisitos/VISAO.md` (um por projeto).

## Molde

````markdown
---
id: VISAO
tipo: VISAO
titulo: Visão do produto — <nome>
autor: requirements-agent
onda: REQ-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: REQ-<nn>, descricao: criação}
problema: <o que dói hoje, para quem>
atores:                       # público (ao menos 1)
  - nome: <ator>
    quem_e: <quem é>
    necessidade: <o que precisa>
objetivo: <o que o sistema resolve, em uma ou duas frases>
metricas_sucesso:             # opcional
  - <como saber que funcionou>
fora_de_escopo:               # ao menos 1
  - <o que não será feito>
glossario:                    # ao menos 1
  - termo: <termo>
    definicao: <definição>
---

# Visão do produto — <nome>

## Problema
<texto livre; não contradiz o YAML>

## Público e atores
<tabela ou texto>

## Objetivo
## Métricas de sucesso
## Fora de escopo
## Glossário do domínio
````

## Regras

- O YAML é a fonte; o corpo explica e nunca o contradiz. Em conflito, vale o YAML.
- O histórico fica no campo `historico` do cabeçalho (sem seção própria no corpo).
