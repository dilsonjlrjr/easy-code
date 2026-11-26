# Template — Modelo de ameaças (AMEACAS) — v1

- **Esquema:** specs/dominio/artefatos/ameacas.schema.json
- **Quem preenche:** agente de Segurança, modo AMEAÇAS (logo após a Arquitetura); `conferencia` no modo AUDITORIA.
- **Quem lê:** Arquitetura (mitigações no desenho), Dev, QA, Segurança na auditoria.
- **Gate confere:** toda ameaça Alta ou Crítica com mitigação no desenho ou ACH aberto.
- **Arquivo:** `docs/seguranca/ameacas.md` (um por projeto; atualizado na auditoria).

## Molde

````markdown
---
id: AMEACAS
tipo: AMEACAS
titulo: Modelo de ameaças — <projeto>
autor: security-agent
onda: SEG-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []                  # ADR, RNF de segurança (relação genérica)
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: SEG-<nn>, descricao: criação}
ativos:                       # ao menos 1
  - <o que proteger: dados pessoais, credenciais, disponibilidade>
atores:                       # ao menos 1
  - <usuário legítimo por papel ou atacante considerado>
fluxo: |                      # diagrama Mermaid ou caminho do arquivo do diagrama
  flowchart LR
    navegador -->|1| api
    api -->|2| banco
ameacas:                      # ao menos 1
  - id: AM-1
    fronteira: <fronteira de confiança ou fluxo>
    stride: [e]               # s | t | r | i | d | e (ao menos 1)
    descricao: <ameaça concreta, não categoria>
    probabilidade: media      # baixa | media | alta
    impacto: alto             # baixo | medio | alto
    severidade: alto          # critico | alto | medio | baixo
    mitigacao: <mitigação no desenho>   # obrigatória se status = mitigada
    onde: [ADR-NN, RNF-NN, T-NN]        # ao menos 1 se mitigada; [] caso contrário
    status: mitigada          # mitigada | achado | aceita
    # achado: ACH-NN          # obrigatório se status = achado ou aceita (aceita: id do ACH com o aceite humano)
# conferencia:                # preenchida na auditoria
#   - {ameaca: AM-1, implementada: true, testada_por: [CT-NN], resultado: <resultado>}
---

# Modelo de ameaças — <projeto>

## 1. Escopo, ativos e atores
## 2. Fluxo de dados e fronteiras de confiança
## 3. Ameaças
| # | Fronteira | STRIDE | Ameaça | Severidade | Mitigação | Status |
|---|---|---|---|---|---|---|

## 4. Ameaças sem mitigação
(as de `status: achado`, com o ACH)

## 5. Conferência na auditoria
````

## Regras

- Campo de acompanhamento (`conferencia`): a Segurança o preenche na auditoria, sem nova versão nem reabertura.
  O resto do modelo aprovado é imutável.
- Ameaça concreta, não categoria: "funcionário cancela reserva de outro trocando o id na URL",
  não "elevação de privilégio".
- Severidade na mesma escala do ACH (`agentes/templates/ACH.md`).
- Ameaça sem mitigação no desenho vira ACH (destino Arquitetura ou Requisitos): `status: achado`.
- Detalhes do método: skill `agentes/skills/seguranca/revisao-seguranca/references/modelagem-ameacas-stride.md`.
