---
name: arquitetura-web
description: Decisões e artefatos de arquitetura para aplicação web front + API REST + PostgreSQL com Docker Compose - ADR, C4 em Mermaid, contrato OpenAPI 3, padrão de erros (Problem Details), autenticação/autorização no desenho, padrão de acesso a dados, documento de arquitetura (ARQ), plano de tarefas, validação técnica conjunta (modo VALIDAÇÃO) e checklist do gate. Use na etapa de Arquitetura, ao decidir a estrutura técnica, desenhar contratos ou quebrar o trabalho em tarefas (T) para o Dev.
---

# Arquitetura de aplicação web

Skill da etapa de **Arquitetura**. Serve a qualquer aplicação web no formato front + API + banco;
os exemplos usam reserva de salas só como ilustração. Skills irmãs, para decisões específicas:
`padroes-arquitetura-codigo` (como organizar o código), `dados/modelagem-postgresql` (banco, usada pelo DBA) e
`ambiente-docker` (T-01 e ambiente). Segurança de verdade (ameaças, revisão) é da skill
`seguranca/revisao-seguranca`; aqui só se desenha. **O modelo de dados é do DBA** (skill
`dados/modelagem-postgresql`): o Arquiteto define só o padrão de acesso a dados.

## Para que serve a Arquitetura neste processo

A Arquitetura converte US e RNF em decisões, contratos e Tarefas (T) que o Dev executa sem
perguntar nada. O que fica ambíguo aqui vira Achado e Onda de retrabalho depois. Cada Artefato
tem um leitor: **ADR** (Dev, Segurança), **C4** (todos, orientação), **ARQ** (documento de arquitetura: visão geral, riscos técnicos; todos),
**OpenAPI** (Dev backend, Dev front, QA), **T** (Dev, QA).

## Princípios

1. **Simplicidade primeiro.** O mais simples que atende às US e aos RNF. Todo componente
   (fila, cache, microsserviço, camada, biblioteca) precisa de uma US ou RNF que o justifique.
   Para aplicação pequena: um front, uma API, um banco.
2. **Custo conta.** Cada camada e abstração a mais é código que o agente escreve, testa e
   revisa. Custo conta: orçamento do projeto e custo por etapa são indicadores da
   plataforma; arquitetura inchada consome orçamento sem entregar valor.
3. **Garantir no banco o que não pode falhar.** Integridade sob concorrência é constraint, não `if`.
4. **Contrato antes de código.** O OpenAPI é a fonte do contrato da API; o DBML (do DBA) é a
   fonte do modelo. DDL e stubs derivam deles.
5. **Decisão registrada ou não existe.** Escolha de stack, autenticação, persistência e erros
   sempre vira ADR, com alternativa real comparada.
6. **Rastreável.** Toda decisão cita a US/RNF que a motivou; toda T cita a US que cobre.
7. **Não inventar requisito.** Sem US ou RNF, não há motivo para o componente.

## Estrutura padrão do repositório

```
app/
  backend/        API na linguagem do projeto
  frontend/       Vue
  e2e/            Playwright
  docker-compose.yml
  .env.example
  Makefile        (ou scripts/): up, test, lint
  README.md
```

## Fluxo de trabalho

1. **Ler US, RNF e CT.** Listar o que força decisão: concorrência, autenticação, volume,
   integrações, prazos. O resto segue o padrão simples.
2. **Listar decisões** e separar: as que sempre viram ADR (abaixo) e as que seguem o padrão.
3. **Escrever ADR** com opções reais (`references/escrever-adr.md`).
4. **Desenhar C4** contexto e contêineres (`references/c4-mermaid.md`).
5. **Definir o padrão de acesso a dados** na ADR (ORM, query builder ou SQL; ferramenta de
   migração). O modelo é do DBA (skill `dados/modelagem-postgresql`): não modelar tabelas aqui.
6. **Escrever OpenAPI** (`references/api-rest-openapi.md`).
7. **Desenhar autenticação e autorização** (`references/autenticacao-autorizacao.md`).
8. **Escolher organização do código** (skill `padroes-arquitetura-codigo`) e registrar em `estrutura.md`.
9. **Quebrar em T** (`references/plano-de-tarefas.md`), T-01 sendo o esqueleto
   (skill `ambiente-docker`).
10. **Escrever o documento de arquitetura** (artefato ARQ, `ARQ.md`; template
    `agentes/templates/ARQ.md`, esquema `arq.schema.json`): contexto, componentes, contratos,
    decisões, diretrizes, riscos.
11. **Revisar** com `references/checklist-gate-ARQ.md` antes de entregar.

### Modo VALIDAÇÃO (validação técnica conjunta)

Depois de DBA e Design, o Arquiteto confere a consistência do desenho todo e registra o seu
parecer em `docs/validacao/VALIDACAO-NN.md` (artefato VALIDACAO; template
`agentes/templates/VALIDACAO.md`, esquema `validacao.schema.json`; DBA e Designer registram os
deles em `pareceres`). Conferir: arquitetura x modelo de dados (`docs/dados/`); contrato da API x modelo e x
telas; ameaças x mitigações; US Must x T x CT. Inconsistência vira ACH para a etapa dona do
artefato; reprovação do DBA em persistência bloqueia a liberação. Ver a seção 11 do
`references/checklist-gate-ARQ.md`.

## Formato dos artefatos (ARQ, ADR, T, VALIDACAO)

Cada artefato é um arquivo Markdown com **cabeçalho YAML validado por JSON Schema** (decisão D-01,
`specs/dominio/README.md`) e corpo livre que nunca contradiz o YAML. Esquemas em
`specs/dominio/artefatos/`, moldes em `agentes/templates/`. **Vale o esquema**; o cabeçalho comum
(`id`, `tipo`, `titulo`, `autor`, `onda`, `versao`, `estado`, `vinculos`, `historico`) vale para todos.
Exemplo mínimo do ARQ (`arq.schema.json`; `ARQ` é artefato único, id fixo):

```yaml
---
id: ARQ
tipo: ARQ
titulo: Arquitetura da solução — Reserva de salas
autor: architect-agent
onda: ARQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: ARQ-01, descricao: criação}
contexto:
  resumo: Aplicação web interna para reservar salas sem conflito de horário (front Vue, API REST, PostgreSQL).
  restricoes: [Ambiente com Docker Compose]
  rnf_criticos: [RNF-02]
c4: docs/arquitetura/ARQ.md
componentes:
  - nome: API de reservas
    responsabilidade: Regras de reserva, autenticação e autorização por recurso
    tecnologia: backend na linguagem do projeto, API REST
    atende: [US-03, RNF-02]
contratos:
  openapi: docs/arquitetura/api/openapi.yaml
  integracoes: []
decisoes: [ADR-01, ADR-02]
diretrizes:
  - Erros da API em Problem Details (RFC 9457); violação de constraint vira 409
persistencia: PostgreSQL; padrão de acesso na ADR-02; o modelo é do DBA em docs/dados/
riscos:
  - {id: RK-1, descricao: Corrida entre reservas simultâneas, impacto: alto, mitigacao: Constraint no banco e teste de concorrência}
---
```

- `ADR-NN` (`adr.schema.json`): `contexto`, `opcoes` (ao menos 2, cada uma com `pros` e `contras`),
  `decisao`, `consequencias`, `skills`, `motivada_por` (ids de US/RNF); `substitui` na ADR nova.
  Exemplo em `references/escrever-adr.md`.
- `T-NN` (`t.schema.json`): `cobre`, `segue`, `depende_de`, `escopo_arquivos`, `tamanho`, `descricao`,
  `criterios_pronto`, `situacao`. Exemplo em `references/plano-de-tarefas.md`.
- `VALIDACAO-NN` (`validacao.schema.json`): `conferencias` (os 5 pares), `pareceres` (arquiteto, dba,
  designer), `veredito`. Divergência exige `inconsistencia` e `achado`; ver o exemplo na seção 11 de
  `references/checklist-gate-ARQ.md`.
- Id de artefato numerado tem prefixo + 2 ou mais dígitos; item interno (`CA-1`, `RK-1`) é local ao
  artefato. Vínculo tipado vai no campo próprio (`cobre`, `segue`, `motivada_por`, `decisoes`);
  `vinculos` só para relação genérica.

## Decisões que sempre viram ADR

| # | Decisão | Pergunta que a ADR responde |
|---|---|---|
| 1 | Framework e bibliotecas do backend | Por que este e não a alternativa mais simples da linguagem? |
| 2 | Autenticação e autorização | Sessão ou token? Onde a senha fica, com qual hash? Como o papel é checado? |
| 3 | Persistência e migração | Acesso a dados (ORM, query builder, SQL) e ferramenta de migração versionada |
| 4 | Regra de integridade crítica | Como a aplicação trata a violação? A constraint em si (ex.: exclusion constraint contra sobreposição) é do DBA; a ADR registra a necessidade e o mapeamento do erro |
| 5 | Padrão de erros da API | Problem Details? Como violação de constraint vira 409? |
| 6 | Organização do código | Camadas, fatias ou outro? (ver `padroes-arquitetura-codigo`) |

Decisão pequena que o padrão da skill já cobre não precisa de ADR; vale uma linha em
`estrutura.md`. Desviar do padrão, sim.

## Regras de decisão

| Situação | Decisão |
|---|---|
| Nenhuma US/RNF pede o componente (fila, cache, busca) | Não incluir; mencionar em "fora de escopo" se alguém sugerir |
| Regra de negócio quebra sob requisições simultâneas | Constraint no banco + tradução do erro para 409 |
| Dois estilos viáveis, diferença pequena | Escolher o padrão da skill, 3 linhas na ADR, seguir |
| Escolha cara de reverter (banco, autenticação, linguagem) | ADR completa, com consequências |
| Endpoint não corresponde a recurso (ex.: "cancelar") | Subrecurso ou ação explícita, documentado (ver references) |
| ADR aprovada precisa mudar | Nova ADR com `substitui: ADR-NN`; antiga vira `estado: substituido` com `substituido_por` |
| Requisito de qualidade sem número (rápido, seguro) | Devolver à Requisitos como RNF mensurável, não decidir no escuro |
| Skill de linguagem recomenda X e você quer Y | Pode, mas a ADR diz por quê |
| Mais de uma T toca o mesmo arquivo | Reordenar com dependência, ou dividir diferente |

## Autenticação, modelo de dados e contrato: o essencial

- **Autenticação (aplicação pequena):** login próprio com usuário/senha e sessão em cookie `HttpOnly` +
  `SameSite`, ou token curto. Hash adaptativo (argon2id ou bcrypt). Detalhes em
  `references/autenticacao-autorizacao.md`.
- **Dados:** a ADR define o padrão de acesso (ORM, query builder ou SQL) e a migração versionada.
  O modelo (PK, `timestamptz`, FK com índice, `snake_case`, constraints), o DDL, o dicionário e a
  estratégia de persistência são do DBA (`dados/modelagem-postgresql`).
- **API:** recursos no plural sem verbo; status corretos; erros em Problem Details (RFC 9457);
  datas ISO 8601 com fuso; todo `operationId` com exemplos e esquemas de erro.

## Checklist rápido (versão completa em `references/checklist-gate-ARQ.md`)

- [ ] Toda US Must coberta por ao menos uma T, com os CT que ela deve fazer passar.
- [ ] Toda ADR com 2 ou mais opções reais e consequências.
- [ ] `openapi.yaml` válido; `ARQ.md` (artefato ARQ, cabeçalho válido no esquema) escrito; padrão de acesso a dados em ADR.
- [ ] Nenhum componente sem US/RNF que o justifique.
- [ ] Papéis e checagem por recurso definidos para cada endpoint protegido.
- [ ] T-01 sobe os três serviços com um comando e roda um teste vazio por parte.
- [ ] Escopos de arquivos das T paralelizáveis são disjuntos.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Arquitetura "de livro" para app de 8 US (hexagonal completo, CQRS, eventos) | Custo e código a mais sem benefício | `padroes-arquitetura-codigo`: padrão simples |
| ADR com opção única ou opção de palha | Decisão sem trade-off real | Opção que alguém razoável escolheria |
| Regra crítica só na aplicação | Condição de corrida | Constraint no banco |
| OpenAPI sem erros nem exemplos | Dev e QA adivinham | Todo endpoint com 4xx relevantes e exemplo |
| Verbo na URL (`/criarReserva`) | API inconsistente | Recurso + método HTTP |
| T grande (G) cobrindo front, API e banco | Revisão e retrabalho caros | Fatia vertical pequena |
| T sem critério de pronto verificável | Dev e QA discordam do "pronto" | Condição objetiva (teste, CT, build) |
| Checagem de papel só no front | Qualquer cliente HTTP burla | Checar na API, por recurso |
| Fixar versão de lib "de cabeça" | Versão inexistente ou insegura | "estável atual", conferir no lockfile |
| Decidir o que o aprovador humano reservou para si | Invade a competência do aprovador | Propor com trade-offs e recomendação |
| Modelar tabelas e escrever DDL na etapa de Arquitetura | Invade a competência do DBA | Registrar o padrão de acesso na ADR e deixar o modelo ao DBA |

## Índice de references

| Arquivo | Quando ler |
|---|---|
| `references/escrever-adr.md` | Ao escrever ou revisar qualquer ADR |
| `references/c4-mermaid.md` | Ao desenhar contexto, contêineres ou componentes |
| `references/api-rest-openapi.md` | Ao escrever `openapi.yaml`, definir status, paginação, erros, versionamento |
| `references/autenticacao-autorizacao.md` | Ao decidir login, sessão/token, papéis e checagem por recurso |
| `references/plano-de-tarefas.md` | Ao quebrar o trabalho em T |
| `references/checklist-gate-ARQ.md` | Antes de entregar a onda de Arquitetura |

Skills irmãs: `../padroes-arquitetura-codigo/`, `../../dados/modelagem-postgresql/`,
`../ambiente-docker/`, `../../seguranca/revisao-seguranca/`.
