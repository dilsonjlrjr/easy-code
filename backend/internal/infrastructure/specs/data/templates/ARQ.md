# Template — Documento de arquitetura da solução (ARQ) — v1

- **Esquema:** specs/dominio/artefatos/arq.schema.json
- **Quem preenche:** agente Arquiteto.
- **Quem lê:** DBA, Designer, Desenvolvedor, QA, Segurança; aprovador humano (gate de Arquitetura).
- **Gate confere:** toda US Must coberta por componente e tarefa; RNF técnicos endereçados;
  decisões relevantes em ADR; riscos técnicos listados com mitigação.
- **Arquivo:** `docs/arquitetura/ARQ.md` (um por projeto, versionado).

## Molde

````markdown
---
id: ARQ
tipo: ARQ
titulo: Arquitetura da solução — <projeto>
autor: architect-agent
onda: ARQ-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido
vinculos: []
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: ARQ-<nn>, descricao: criação}
contexto:
  resumo: <o que o sistema resolve, atores, restrições>
  restricoes: []
  rnf_criticos: []            # ids de RNF que mais pesam
c4: docs/arquitetura/ARQ.md
componentes:                  # ao menos 1; nenhum sem US/RNF que o justifique
  - nome: <componente>
    responsabilidade: <responsabilidade>
    tecnologia: <tecnologia>
    atende: [US-01]
contratos:
  openapi: docs/arquitetura/api/openapi.yaml
  integracoes: []             # vazio se não houver
#  - {sistema: <sistema>, protocolo: <protocolo>, autenticacao: <como>, dono: <quem>}
decisoes: []                  # ids de ADR
diretrizes:                   # ao menos 1
  - <organização do código, convenções, erros, autenticação, logs>
persistencia: <resumo; o modelo é do DBA em docs/dados/>
riscos: []                    # obrigatório, mesmo vazio
#  - {id: RK-1, descricao: <risco>, impacto: alto, mitigacao: <mitigação>}   # impacto: baixo|medio|alto|critico
---

# Arquitetura da solução — <projeto>

## 1. Contexto e objetivos
## 2. Visão de contexto e contêineres
<C4 em Mermaid resumido, com protocolo em cada seta>

## 3. Componentes e responsabilidades
## 4. Contratos e integrações
## 5. Decisões
## 6. Diretrizes de implementação
## 7. Persistência
## 8. Riscos técnicos
````

## Regras

- O documento resume e aponta; o detalhe vive nas ADR, no OpenAPI e no C4.
- Nada de componente sem US ou RNF que o justifique (`atende`).
- Mudança que afeta modelo de dados vai ao DBA por achado; não decidir persistência sozinho.
