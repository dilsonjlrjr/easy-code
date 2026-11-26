# Template — Backlog priorizado (BACKLOG) — v1

- **Esquema:** specs/dominio/artefatos/backlog.schema.json
- **Quem preenche:** agente de Requisitos, último artefato da etapa.
- **Quem lê:** Arquitetura (ordem das tarefas), QA, aprovador humano.
- **Gate confere:** todas as US listadas, com prioridade MoSCoW; suposições registradas.
- **Arquivo:** `docs/requisitos/BACKLOG.md` (um por projeto).

## Molde

````markdown
---
id: BACKLOG
tipo: BACKLOG
titulo: Backlog — <nome>
autor: requirements-agent
onda: REQ-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: REQ-<nn>, descricao: criação}
historias:                    # em ordem de execução; ao menos 1
  - {ordem: 1, us: US-01, prioridade: must, valor: alto, esforco: p, observacao: ""}   # valor: alto|medio|baixo; esforco: p|m|g; opcionais: epico, fase, marco, depende_de (ids de US)
rnf:
  - {rnf: RNF-01, prioridade: must}
suposicoes: []                # obrigatório, mesmo vazio
#  - {id: S-1, descricao: <o que foi decidido sem o cliente>, motivo: <por quê>}
adiadas: []                   # histórias Won't registradas e adiadas
#  - {us: US-09, motivo: <por quê>, prioridade: wont}   # prioridade opcional, só wont
epicos: []                    # opcional: [{id: EP-01, nome: <nome>}]
marcos: []                    # opcional: [{id: MARCO-1, nome: <nome>, criterio: <como saber que fechou>}]
---

# Backlog — <nome>

## Histórias em ordem de prioridade
<tabela legível: Ordem, US, Título, Prioridade, Valor, Esforço, Observação>

## RNF
## Suposições
## Fora deste backlog (Won't)
````

## Regras

- No máximo cerca de 60% do esforço em Must; se passar, propor cortes ao cliente.
- Ordem dentro de cada prioridade segue dependência e valor.
- Toda US do projeto aparece em `historias` ou em `adiadas`.
