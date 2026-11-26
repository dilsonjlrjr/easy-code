# Template — História de usuário (US) — v1

- **Esquema:** specs/dominio/artefatos/us.schema.json
- **Quem preenche:** agente de Requisitos.
- **Quem lê:** QA (escreve os CT), Arquitetura (escreve as T), Design (desenha as telas).
- **Gate confere:** toda US tem ao menos 1 critério de aceite no formato Dado/Quando/Então;
  nenhuma pergunta aberta bloqueante.
- **Arquivo:** `docs/requisitos/US-NN-<slug>.md`.

## Molde

````markdown
---
id: US-NN
tipo: US
titulo: <título curto>
autor: requirements-agent
onda: REQ-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido
vinculos: []                  # só relação genérica; RNF vai em `rnf`
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: REQ-<nn>, descricao: criação}
ator: <quem>
quero: <o quê>
para: <benefício>
prioridade: must              # must | should | could | wont
origem: "<trecho exato da entrada ou resposta do usuário>"
tem_interface: true           # define se o Design precisa de tela
criterios_aceite:             # ao menos 1
  - id: CA-1
    dado: <contexto>
    quando: <ação>
    entao: <resultado verificável>
regras_negocio:               # [] se não houver
  - id: RN-1
    descricao: <regra>
perguntas_abertas:            # [] se não houver
  - pergunta: <pergunta>
    bloqueante: false
rnf: []                       # ids de RNF relacionados
---

# US-NN — <título curto>

## Contexto
<texto livre: situação do ator, observações; não contradiz o YAML>

## Notas para Design e QA
<texto livre>
````

## Regras

- Critério de aceite tem que ser **testável**: resultado observável, sem "rápido", "fácil", "adequado".
- Uma história = um objetivo do ator. Se tem "e" no `quero`, provavelmente são duas.
- `origem` nunca inventada: se não veio da entrada, vira pergunta aberta ao usuário.
- Vínculo tipado no campo próprio (`rnf`); `vinculos` só para relação genérica.

## Exemplo

````markdown
---
id: US-03
tipo: US
titulo: Reservar sala
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: REQ-01, descricao: criação}
ator: Funcionário
quero: reservar uma sala para uma data e horário
para: garantir o espaço da minha reunião
prioridade: must
origem: '"fazem reservas" / "Não pode haver duas reservas da mesma sala no mesmo horário"'
tem_interface: true
criterios_aceite:
  - id: CA-1
    dado: sala livre das 10h às 11h
    quando: reservo esse horário
    entao: a reserva é criada e aparece em "minhas reservas"
  - id: CA-2
    dado: sala reservada das 10h às 11h
    quando: tento reservar das 10h30 às 11h30
    entao: recebo erro de conflito e nada é criado
  - id: CA-3
    dado: usuário sem login
    quando: tento reservar
    entao: sou levado ao login
regras_negocio:
  - id: RN-1
    descricao: Reservas encostadas (10h–11h e 11h–12h) não são conflito.
perguntas_abertas:
  - pergunta: Há duração mínima ou máxima de reserva?
    bloqueante: false
rnf: [RNF-02]
---

# US-03 — Reservar sala

## Contexto
O funcionário reserva salas de reunião e precisa ter certeza de que ninguém ocupa o mesmo horário.
````
