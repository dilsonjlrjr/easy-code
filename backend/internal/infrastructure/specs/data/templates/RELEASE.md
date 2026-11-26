# Template — Release (RELEASE) — v1

- **Esquema:** specs/dominio/artefatos/release.schema.json
- **Quem preenche:** orquestrador (consolidação automática), com aprovação humana.
- **Quem lê:** aprovador humano, equipe que publica, auditoria.
- **Gate confere:** todos os gates anteriores aprovados; zero achado Crítico ou Alto aberto
  (ou aceito com registro); rastreabilidade completa; aprovação humana registrada.
- **Arquivo:** `docs/release/RELEASE-NN.md` (um por entrega).

## Molde

````markdown
---
id: RELEASE-NN
tipo: RELEASE
titulo: Release NN — <projeto>
autor: orquestrador
onda: REL-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido (ciclo do artefato)
vinculos: []                  # VALIDACAO e demais (relação genérica)
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: REL-<nn>, descricao: criação}
gates:                        # exatamente as 8 etapas
  - {etapa: requisitos, aprovado_por: <nome>, quando: "<AAAA-MM-DD>"}
  - {etapa: arquitetura, aprovado_por: <nome>, quando: "<AAAA-MM-DD>"}
  - {etapa: dados, aprovado_por: <nome>, quando: "<AAAA-MM-DD>"}
  - {etapa: design, aprovado_por: <nome>, quando: "<AAAA-MM-DD>"}
  - {etapa: validacao, aprovado_por: <nome>, quando: "<AAAA-MM-DD>"}
  - {etapa: implementacao, aprovado_por: <nome ou automatico>, quando: "<AAAA-MM-DD>"}
  - {etapa: qa, aprovado_por: <nome>, quando: "<AAAA-MM-DD>"}
  - {etapa: seguranca, aprovado_por: <nome>, quando: "<AAAA-MM-DD>"}
qualidade: {rel_qa: REL-QA-NN, rel_seg: REL-SEG-NN}
riscos_aceitos: []            # ids de ACH aceitos (quem e por quê ficam no ACH)
achados_abertos: []           # ids de ACH ainda abertos na liberação (Médio e Baixo)
validacao: VALIDACAO-NN       # id da validação técnica que liberou o desenho
historias: [US-NN]            # US entregues nesta release
rastreabilidade: {us_must_total: <y>, us_must_completas: <x>}
entrega:
  repositorio: <url ou caminho>
  commit: <hash>
  tag: <tag>
  pipeline: <link ou nome>
  ambiente: teste             # teste | homologacao | producao
custos:                       # um item por etapa
  - {etapa: requisitos, ondas: <n>, retrabalho: <n>, tokens: <n>, custo: <valor>, tempo_execucao_min: <n>, tempo_ciclo_min: <n>}
situacao: rascunho            # rascunho | aprovado | publicado | recusado
# aprovacao:                  # obrigatória a partir de aprovado / recusado
#   por: <nome>
#   quando: "<AAAA-MM-DD>"
#   decisao: aprovado         # aprovado | recusado
#   motivo: <obrigatório se recusado>
---

# Release NN — <projeto>

## Gates
## Qualidade e segurança
- QA: <REL-QA-NN — CA Must cobertos x de y, veredito>
- Segurança: <REL-SEG-NN — achados por severidade, veredito>
- Riscos aceitos: <ACH-NN, quem aceitou, justificativa>

## Rastreabilidade
## Entrega
## Custo e tempo
## Aprovação humana
````

## Regras

- Publicação em produção nunca sem aprovação humana registrada em `aprovacao`.
- O orquestrador preenche a partir dos artefatos; ninguém edita números à mão.
- `situacao` aprovado ou publicado exige `aprovacao.decisao: aprovado`; recusado exige
  `aprovacao.decisao: recusado` com `motivo`.
- `estado` é o ciclo do artefato; `situacao` é o da entrega. Não confundir.
- Custo vem da tabela de preços versionada (PROJETO.md, seção 10); o template não fixa valores.
- Zero ACH Crítico ou Alto aberto e `us_must_completas` = `us_must_total` são checagens de
  governança (o esquema não consulta outros artefatos).
