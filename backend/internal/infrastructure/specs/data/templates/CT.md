# Template — Cenário de teste (CT) — v1

- **Esquema:** specs/dominio/artefatos/ct.schema.json
- **Quem preenche:** agente de QA, logo após Requisitos (antes do código); `execucao` na fase de QA.
- **Quem lê:** Dev (implementa para passar), QA na execução (automatiza e roda).
- **Gate confere:** todo critério de aceite de US Must tem ao menos um CT.
- **Arquivo:** `docs/qa/cenarios/CT-NN.md`.

## Molde

````markdown
---
id: CT-NN
tipo: CT
titulo: <comportamento testado>
autor: qa-agent
onda: QA-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: QA-<nn>, descricao: criação}
cobre: {us: US-NN, criterio: CA-N}   # ou, para teste não funcional: {rnf: RNF-NN}
nivel: integracao             # unitario | integracao | e2e | nao_funcional
tipo_caso: positivo           # positivo | negativo | limite | seguranca
precondicoes:                 # ao menos 1
  - <estado inicial: dados, usuário logado etc.>
passos:                       # ao menos 1
  - <ação>
resultado_esperado:           # ao menos 1
  - <resultado observável e verificável>
# execucao:                   # preenchida pelo QA na execução
#   teste: <arquivo::nome do teste>
#   resultado: passou         # passou | falhou | bloqueado | nao_executado
#   evidencia: <saída, print, log>     # obrigatória se falhou
#   achado: ACH-NN                     # obrigatório se falhou
#   onda: QA-<nn>
---

# CT-NN — <comportamento testado>

## Pré-condições
## Passos
## Resultado esperado
````

## Regras

- Campo de acompanhamento (`execucao`): o QA o atualiza depois da aprovação, sem nova versão nem reabertura.
  O resto do CT aprovado é imutável.
- Para cada CA, ao menos um caso positivo; CA com regra de negócio também pede negativo e limite.
- O CT descreve comportamento, não implementação: não citar classe ou função.
- Falhou na execução → QA abre um ACH com destino Dev (defeito) ou Requisitos (CA ambíguo) e
  registra em `execucao.achado`.
- `cobre` aponta para US/CA ou RNF; não repetir esse vínculo em `vinculos`.

## Exemplo

````markdown
---
id: CT-08
tipo: CT
titulo: Reserva sobreposta é recusada
autor: qa-agent
onda: QA-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: QA-01, descricao: criação}
cobre: {us: US-03, criterio: CA-2}
nivel: integracao
tipo_caso: negativo
precondicoes:
  - Sala "Azul" com reserva das 10h às 11h de amanhã.
  - Funcionário logado.
passos:
  - Pedir reserva da sala "Azul" das 10h30 às 11h30 de amanhã.
resultado_esperado:
  - Resposta de conflito (409) com mensagem indicando horário ocupado.
  - Nenhuma reserva nova no banco.
---

# CT-08 — Reserva sobreposta é recusada
````
