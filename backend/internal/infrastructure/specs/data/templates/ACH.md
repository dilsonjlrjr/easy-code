# Template — Achado (ACH) — v1

- **Esquema:** specs/dominio/artefatos/ach.schema.json
- **Contrato:** specs/dominio/achado.md (quem emite, roteamento, ciclo, aceite, descarte, efeito no gate).
- **Quem preenche:** qualquer agente que encontre um problema (mais comum: QA e Segurança).
- **Quem lê:** o orquestrador, que cria a onda de retrabalho para a etapa destino.
- **Gate confere:** achado sem evidência é descartado (PROJETO.md, seção 6.3); Crítico ou Alto
  aberto bloqueia o gate.
- **Arquivo:** `docs/achados/ACH-NN.md`.

## Molde

````markdown
---
id: ACH-NN
tipo: ACH
titulo: <problema em uma frase>
autor: <emissor>-agent       # quem emite é quem revalida
onda: <onda em que foi encontrado>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: <onda>, descricao: criação}
severidade: alto              # critico | alto | medio | baixo
categoria: defeito            # defeito | requisito_ambiguo | vulnerabilidade | desvio_arquitetura | inconsistencia_dados | inconsistencia_design | lacuna_contrato | divida_tecnica
etapa_origem: qa             # etapa em que o achado foi encontrado; preenchida pelo orquestrador
etapa_destino: implementacao  # requisitos | arquitetura | dados | design | validacao | implementacao (lacuna_contrato: padrão arquitetura)
artefatos_afetados:           # ao menos 1: id de artefato, US-03/CA-2 ou caminho (com ou sem :linha)
  - <T-NN | US-NN/CA-N | caminho/arquivo:linha>
evidencia: |                  # obrigatória, não vazia
  <saída de teste, trecho de código, resultado de varredura, passos que reproduzem>
impacto: <o que acontece se não corrigir>
correcao_sugerida: <o que mudar, sem impor a implementação>
situacao: aberto              # aberto | em_retrabalho | revalidando | fechado | escalado | aguardando_escopo | aceito | descartado
ciclos: []                    # máximo 3; mais de 3 só com escalonamentos[].decisao: novo_retrabalho autorizado por humano
#  - {numero: 1, onda_retrabalho: DEV-<nn>, commit_ou_versao: <hash ou versão>, onda_revalidacao: QA-<nn>, resultado: corrigido}   # corrigido | nao_corrigido
# aceite:                     # obrigatório se situacao = aceito (só humano)
#   por: <nome>
#   quando: "<AAAA-MM-DD>"
#   justificativa: <por que o risco é aceito>
# escalonamentos:             # lista, histórico preservado; obrigatório se situacao = escalado
#   - quando: "<AAAA-MM-DD>"
#     motivo: <3 ciclos sem corrigir | outro>
#     decisao: novo_retrabalho  # novo_retrabalho | aceitar_risco | mudanca_escopo (quando houver)
#     autorizado_por: <nome>    # obrigatório se houver decisao (só humano)
#     decidido_em: "<AAAA-MM-DD>"   # obrigatório se houver decisao
# resolucao: corrigido          # obrigatório se situacao = fechado: corrigido | mudanca_escopo
# artefatos_alterados: [<ids>]  # obrigatório se resolucao = mudanca_escopo (exige decisao: mudanca_escopo em escalonamentos)
# motivo_descarte: <sem evidência | duplicado de ACH-NN>   # obrigatório se situacao = descartado
---

# ACH-NN — <problema em uma frase>

## Evidência
<detalhe e passos de reprodução; o YAML guarda o resumo>

## Impacto
## Correção sugerida
## Justificativa do destino
<por que esta `etapa_destino` é a dona do artefato afetado>
````

## Regras de severidade

| Severidade | Quando |
|---|---|
| `critico` | Perda ou vazamento de dados, acesso indevido, sistema não sobe |
| `alto` | Requisito Must não atendido, falha de segurança explorável com esforço |
| `medio` | Requisito Should não atendido, comportamento errado com contorno |
| `baixo` | Cosmético, melhoria, dívida técnica sem efeito visível |

## Regras

- Campos de acompanhamento (`situacao`, `ciclos`, `aceite`, `escalonamentos`, `resolucao`, `artefatos_alterados`,
  `motivo_descarte`) mudam sem nova versão, por quem a máquina do achado permitir; o resto do ACH é imutável.
- Quem emitiu é quem **revalida** depois do retrabalho. Limite de 3 ciclos; estourou, `escalado`. Rodada extra só com decisão humana registrada em `escalonamentos`.
- `aguardando_escopo`: entre `escalado` e `fechado`, depois da decisão `mudanca_escopo`. A alteração dos requisitos (Onda de
  Requisitos ou edição humana validada pelo agente, US-53) passa pelo Gate de Requisitos; o bloqueio se mantém até lá.
- `fechado` exige `resolucao`: `corrigido` (revalidado pelo emissor) ou `mudanca_escopo` (decisão humana + ids em `artefatos_alterados`).
- Retrabalho de `implementacao` é ciclo interno: não reabre o gate; a correção passa em build, testes e lint e o emissor revalida.
- Achado que muda artefato já aprovado (ex.: ADR) reabre o gate da etapa dona; exceção: `implementacao` (ciclo interno).
- Aceite de risco só por humano, com `aceite` completo. Descarte só sem evidência ou duplicado.
- `estado` é o ciclo do artefato; `situacao` é o ciclo do problema. Não confundir.

## Exemplo

````markdown
---
id: ACH-04
tipo: ACH
titulo: Funcionário consegue cancelar reserva de outro
autor: security-agent
onda: SEG-01
versao: 1
estado: aprovado
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: SEG-01, descricao: criação}
severidade: alto
categoria: vulnerabilidade
etapa_origem: seguranca
etapa_destino: implementacao
artefatos_afetados:
  - US-05
  - T-07
  - backend/src/main/java/app/reserva/ReservaService.java:58   # com ou sem :linha
evidencia: |
  `DELETE /reservas/42` com token do usuário B, reserva 42 é do usuário A → resposta 204 e
  reserva apagada. O método `cancelar` não compara o dono da reserva com o usuário logado.
impacto: Qualquer funcionário apaga reservas de terceiros.
correcao_sugerida: Permitir o cancelamento só ao dono da reserva ou a admin; responder 403 aos demais.
situacao: aberto
ciclos: []
---

# ACH-04 — Funcionário consegue cancelar reserva de outro

## Justificativa do destino
O defeito está no código do serviço; a correção é do Dev.
````
