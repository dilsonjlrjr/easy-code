# Template — Decisão de arquitetura (ADR) — v1

- **Esquema:** specs/dominio/artefatos/adr.schema.json
- **Quem preenche:** agente de Arquitetura.
- **Quem lê:** Dev, Segurança, e o próprio Arquitetura em retrabalho.
- **Gate confere:** toda ADR tem ao menos 2 opções comparadas e a consequência da escolha; a ADR do tipo `imposta` é a exceção (ver Regras).
- **Arquivo:** `docs/arquitetura/adr/ADR-NN.md`.

## Molde

````markdown
---
id: ADR-NN
tipo: ADR
titulo: <decisão em uma frase>
autor: architect-agent
onda: ARQ-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: ARQ-<nn>, descricao: criação}
contexto: <problema, restrições e requisitos que pedem esta decisão>
opcoes:                       # ao menos 2
  - nome: A — <nome>
    pros: [<prós>]
    contras: [<contras>]
  - nome: B — <nome>
    pros: [<prós>]
    contras: [<contras>]
decisao: <opção escolhida e por quê, em poucas linhas>
consequencias:                # ao menos 1
  - <o que fica mais fácil / mais difícil / obrigatório>
skills: []                    # skills que orientaram a decisão
motivada_por: [US-NN]         # ao menos 1 US ou RNF
# substitui: ADR-NN           # opcional; a ADR antiga passa a `substituido`
# natureza: imposta           # escolhida (padrão) | imposta: decisão já tomada (US-53), ver Regras
# restricoes: [RESTR-NN]      # obrigatório se natureza = imposta: ids das restrições do projeto (campo `restricoes`, US-54)
---

# ADR-NN — <decisão em uma frase>

## Contexto
## Opções consideradas
<tabela comparativa legível>

## Decisão
## Consequências
````

## Regras

- Decisão que muda uma ADR aprovada gera **nova** ADR (`substitui`) e marca a antiga como `substituido` (com `substituido_por`).
- Escolha de linguagem, framework, banco e padrão de autenticação sempre vira ADR.
- Projeto com mais de uma linguagem de backend tem uma ADR de stack por linguagem.
- **ADR `imposta`** (decisão já tomada, imposta por norma de infraestrutura ou stack corporativa): vale sem
  comparar opções (`opcoes` traz só a opção imposta; `motivada_por` não é exigido) e **cita a restrição do projeto**
  (ids `RESTR-NN` do campo `restricoes`, US-54) em `restricoes` e no `contexto`. `decisao` registra o que foi imposto e `consequencias` o que isso obriga.
  O agente de Arquitetura a cria a partir da restrição; uma pessoa também pode registrá-la, e a validação e formatação
  do agente (US-53) vale como em qualquer edição humana. Os nomes dos campos acima seguem `adr.schema.json`.

## Exemplo

````markdown
---
id: ADR-03
tipo: ADR
titulo: Conflito de horário garantido no banco
autor: architect-agent
onda: ARQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: ARQ-01, descricao: criação}
contexto: US-03/CA-2 exige que duas reservas da mesma sala nunca se sobreponham, mesmo com requisições simultâneas.
opcoes:
  - nome: A — Checagem só na aplicação
    pros: [Simples]
    contras: [Duas requisições ao mesmo tempo passam (condição de corrida)]
  - nome: B — Exclusion constraint no PostgreSQL (tstzrange + btree_gist)
    pros: [Garantia no banco, à prova de concorrência]
    contras: [Exige extensão, Erro do banco precisa ser traduzido]
decisao: Opção B. A regra é crítica e a concorrência é real.
consequencias:
  - Dev traduz a violação da constraint em erro 409 de conflito.
  - QA inclui teste de reservas simultâneas.
skills: [postgres-ddl]
motivada_por: [US-03]
---

# ADR-03 — Conflito de horário garantido no banco

## Contexto
Ver YAML. Reservas simultâneas são o cenário que a checagem na aplicação não cobre.
````
