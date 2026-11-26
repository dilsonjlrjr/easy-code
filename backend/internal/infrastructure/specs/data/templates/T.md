# Template — Tarefa de desenvolvimento (T) — v1

- **Esquema:** specs/dominio/artefatos/t.schema.json
- **Quem preenche:** agente de Arquitetura (plano de implementação); o Dev preenche `situacao` e `registro_dev`.
- **Quem lê:** Dev (executa), QA (confere cobertura).
- **Gate confere:** toda US com prioridade Must está coberta por ao menos uma T; toda T tem
  critério de pronto verificável.
- **Arquivo:** `docs/arquitetura/tarefas/T-NN.md`.

## Molde

````markdown
---
id: T-NN
tipo: T
titulo: <o que entregar>
autor: architect-agent
onda: ARQ-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: ARQ-<nn>, descricao: criação}
natureza: funcional           # funcional | esqueleto | refatoracao (padrão funcional)
cobre:                        # ao menos 1 se natureza = funcional; opcional (pode ser []) em esqueleto e refatoracao
  - {us: US-NN, criterios: [CA-1, CA-2]}
segue: []                     # ids de ADR; [] se nenhuma aplicável
depende_de: []                # ids de T; [] = nenhuma
escopo_arquivos:              # ao menos 1
  - <pasta ou glob>
tamanho: m                    # p | m | g
operacoes: []                 # opcional: operationId do contrato (openapi.yaml) que a T implementa
cts: []                       # CT que devem passar (quando o QA já tiver escrito)
descricao: <endpoints, entidades, telas, regras>
criterios_pronto:             # ao menos 1
  - <condição verificável>
  - build verde e lint limpo
situacao: planejada           # planejada | em_andamento | concluida | justificada
# justificativa: <motivo>     # obrigatória se situacao = justificada
# registro_dev:               # preenchido pelo Dev ao concluir
#   commits: [<hash>]
#   testes: [<arquivo>]
#   desvios: nenhum           # ou o que mudou e por quê
---

# T-NN — <o que entregar>

## O que fazer
<texto livre com detalhe de implementação>

## Critério de pronto
- [ ] <espelha `criterios_pronto`>
````

## Regras

- Campos de acompanhamento (`situacao`, `justificativa`, `registro_dev`): o Dev os atualiza depois da aprovação,
  sem nova versão nem reabertura. O resto da T aprovada é imutável.
- Tarefa grande (G) que toca mais de 1 camada deve ser quebrada, salvo justificativa.
- `escopo_arquivos` limita o Dev e prepara o paralelismo da fase futura (escopos disjuntos).
- Tarefa não feita fica `situacao: justificada`, com `justificativa`, nunca some.
- `estado` é o ciclo do artefato (plano); `situacao` é a execução da tarefa. Não confundir.

## Exemplo

````markdown
---
id: T-05
tipo: T
titulo: Endpoint de criar reserva com checagem de conflito
autor: architect-agent
onda: ARQ-02
versao: 1
estado: aprovado
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: ARQ-02, descricao: criação}
cobre:
  - {us: US-03, criterios: [CA-1, CA-2, CA-3]}
segue: [ADR-03]
depende_de: [T-02, T-03]
escopo_arquivos:
  - backend/src/**/reserva/**
  - backend/src/main/resources/db/migration/
tamanho: m
cts: [CT-07, CT-08]
descricao: POST /reservas com sala, início e fim. Cria a reserva do usuário logado. Violação da constraint de sobreposição vira 409 Conflict com mensagem clara.
criterios_pronto:
  - testes unitários do serviço de reserva passam
  - build verde e lint limpo
  - CT-07 e CT-08 passando
situacao: planejada
---

# T-05 — Endpoint de criar reserva com checagem de conflito

## O que fazer
Ver YAML.
````
