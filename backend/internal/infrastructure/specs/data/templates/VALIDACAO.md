# Template — Validação técnica (VALIDACAO) — v1

- **Esquema:** specs/dominio/artefatos/validacao.schema.json
- **Quem preenche:** Arquiteto, com parecer do DBA e do Designer (uma onda por participante,
  consolidada pelo orquestrador).
- **Quem lê:** aprovador humano (gate antes da Implementação), Desenvolvedor.
- **Gate confere:** zero inconsistência bloqueante aberta entre arquitetura, modelo de dados,
  contrato da API, telas e modelo de ameaças; parecer de cada participante registrado.
- **Arquivo:** `docs/validacao/VALIDACAO-NN.md` (uma por execução da validação).

## Molde

````markdown
---
id: VALIDACAO-NN
tipo: VALIDACAO
titulo: Validação técnica NN — <projeto>
autor: architect-agent
onda: VAL-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []                  # ARQ, ADR, DIC, PERSIST, openapi, TELAS, AMEACAS (relação genérica)
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: VAL-<nn>, descricao: criação}
conferencias:                 # exatamente os 5 pares, um item cada
  - {par: arquitetura_dados, resultado: ok}        # ok | divergente
  - {par: api_dados, resultado: ok}
  - {par: api_telas, resultado: ok}
  - {par: ameacas_arquitetura, resultado: ok}
  - {par: cobertura_us_t_ct, resultado: ok}
  # se divergente: acrescentar inconsistencia: <descrição> e achado: ACH-NN (obrigatórios)
pareceres:                    # exatamente arquiteto, dba, designer
  - {participante: arquiteto, parecer: aprovado, ressalvas: []}   # aprovado | reprovado
  - {participante: dba, parecer: aprovado, ressalvas: []}         # reprovado exige ressalvas
  - {participante: designer, parecer: aprovado, ressalvas: []}
artefatos_conferidos:         # ids e versões lidos na conferência (ARQ, ADR, DIC, PERSIST, TELAS, AMEACAS...)
  - {id: ARQ, versao: 1}
veredito: liberado            # liberado | devolvido; qualquer parecer reprovado força devolvido
---

# Validação técnica NN — <projeto>

## Conferências
<pares: arquitetura × dados; API × dados; API × telas (campos, ações, erros); ameaças × arquitetura; US Must × T × CT>

## Pareceres
## Veredito
````

## Regras

- Qualquer parecer reprovado (arquiteto, dba ou designer) bloqueia a liberação: o esquema recusa `veredito: liberado`. O autor é o architect-agent, que lidera; cada participante emite o próprio parecer.
  Pelo mesmo motivo, qualquer conferência `divergente` exige `veredito: devolvido`.
- Divergência vira ACH para a etapa dona do artefato; a validação é refeita depois (nova NN).
