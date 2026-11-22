# Plano de tarefas (T)

Template: `agentes/templates/T.md`; esquema: `specs/dominio/artefatos/t.schema.json`. O plano é a ponte entre a Arquitetura e o Dev: cada T é um
pedido executável por um agente que **não pode perguntar**. Entrega: `T-NN.md` por tarefa.

## Princípios

1. **Fatia vertical.** Uma T entrega comportamento observável de ponta a ponta (rota + regra
   + dado + teste), não "camada de banco" separada de "camada de API".
2. **Pequena.** P ou M. G exige justificativa e, se toca mais de uma camada, deve ser quebrada.
3. **Cobertura rastreável.** Toda US Must tem T; toda T funcional cita US/CA e ADR (`operacoes` com os operationId do contrato, quando houver); CT entram depois, quando o QA os escrever.
4. **Escopo disjunto.** O `escopo_arquivos` evita que duas T paralelas editem o mesmo arquivo.
5. **Critério de pronto verificável.** Teste, comando ou CT; nunca "funciona bem".
6. **Ordem por risco e dependência.** O que destrava o resto e o que é mais arriscado vêm cedo.

## T-01: o esqueleto

Sempre a primeira, sem US de negócio (`natureza: esqueleto`, `cobre` pode ficar vazio). Entrega o ambiente em que todas as outras rodam.

| Item | Critério de pronto |
|---|---|
| Estrutura de pastas | Igual a `estrutura.md` |
| Docker Compose | `make up` (ou equivalente) sobe front, API, banco saudáveis (verificado em CI/humano; no sandbox, testes contra serviços de apoio do perfil) |
| Migração inicial | Banco criado e migração base aplicada |
| Teste vazio por parte | Um teste passando no backend, no front e no e2e |
| Lint e formatação | Configurados e limpos |
| Endpoint de saúde | `GET /saude` devolve 200 com verificação do banco |
| `.env.example`, README | Comandos de subir, testar e lintar documentados |

Detalhes e checklist em `../../ambiente-docker/SKILL.md`. Escopo de arquivos: toda a raiz
`app/`. Como toca tudo, é a única T que roda sozinha.

## Ordem típica depois do esqueleto

1. **Modelo base e migrações** necessárias a quase tudo (usuários, tabelas centrais), só
   o necessário para a próxima T.
2. **Autenticação** (login, sessão, papéis), pois quase toda rota protegida depende.
3. **Entidades de apoio** (ex.: salas) com CRUD mínimo exigido por US.
4. **Regra crítica** (ex.: reserva com conflito) cedo: é o maior risco.
5. **Listagens e filtros**.
6. **Telas**, cada uma ligada a endpoints já prontos.
7. **Acabamento:** mensagens de erro, acessibilidade, e2e dos fluxos principais.

Uma tela e o endpoint que ela usa podem ser a mesma T (fatia vertical) ou duas T quando o
front e o backend são feitos por Devs diferentes e o contrato OpenAPI já está fixado.

## Como fatiar uma US

| Situação | Fatiamento |
|---|---|
| US pequena (1 endpoint + 1 tela) | Uma T vertical |
| US com regra crítica | T da regra (backend + teste), T da tela depois |
| US com vários CA independentes | Uma T por grupo de CA, na ordem do caminho feliz para os limites |
| US que exige tabela nova | A migração entra na mesma T que a usa, salvo se várias T dependem dela |
| Cross-cutting (erros, logging) | T própria cedo (T-02 ou na autenticação) |

Evite fatias horizontais ("todas as tabelas", "todos os endpoints", "todas as telas"): atrasam
o primeiro valor observável e criam conflito de arquivos.

## Tamanho

| Tamanho | Referência | Exemplo |
|---|---|---|
| P | até ~meio dia de agente, 1 recurso, poucos arquivos | `GET /salas` com teste |
| M | um dia, 1 regra ou 1 tela + endpoint | Criar reserva com conflito |
| G | só com justificativa; nunca multi-camada | Esqueleto (T-01) |

Sinal para quebrar: mais de ~6 CA cobertos, mais de uma regra de negócio, mais de ~15
arquivos tocados, ou o "O que fazer" não cabe em dez linhas.

## Dependências e paralelismo

- Declare `depende_de` explícito; o grafo não pode ter ciclo.
- Duas T são paralelizáveis se **não dependem uma da outra e os escopos são disjuntos**.
- Arquivos compartilhados (`openapi.yaml`, rotas centrais, `package.json`, migrações) são
  pontos de conflito: dê a uma T o papel de editá-los, ou combine uma regra ("cada T adiciona
  sua migração com número único", "rotas registradas por arquivo da fatia").
- Migrações: nomeação que evite colisão (`V003__...`, ou timestamp). Duas T paralelas não
  escolhem o mesmo número: o plano atribui a faixa.

Exemplo de grafo:

```
T-01 esqueleto
 ├─ T-02 autenticação ──┬─ T-04 reservas (regra de conflito)
 └─ T-03 salas ─────────┘        └─ T-06 tela de reservas
                      T-05 tela de salas (depende de T-03)
```

T-04 e T-05 podem rodar em paralelo se escopos forem disjuntos.

## Escopo de arquivos

Lista `escopo_arquivos`: padrões globais, caminho relativo a `app/`, nomeando a fatia:

```
backend/src/**/reserva/**          (código da fatia)
backend/src/main/resources/db/migration/V004__*.sql
frontend/src/features/reservas/**
e2e/tests/reservas.spec.*
```

Se a T precisa tocar arquivo fora do escopo, é desvio e vai em `registro_dev.desvios`. Se
precisar sempre, o escopo foi mal definido: corrigir o plano.

## Critério de pronto: padrão

```yaml
criterios_pronto:
  - <teste específico da regra> passa
  - build verde e lint limpo
  - CT-NN e CT-MM passando (quando o QA os tiver escrito)
  - endpoint conforme `openapi.yaml` (status e corpo de erro)
```

Para rotas, inclua pelo menos um teste de sucesso e um de falha (401/403/409 conforme caso).

## Exemplo completo

Cabeçalho YAML exigido pelo esquema `t.schema.json` (vale o esquema; o Dev só atualiza os campos
de acompanhamento `situacao`, `justificativa` e `registro_dev`, sem nova versão):

```yaml
---
id: T-04
tipo: T
titulo: Criar reserva com checagem de conflito
autor: architect-agent
onda: ARQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: ARQ-01, descricao: criação}
natureza: funcional           # funcional | esqueleto | refatoracao
cobre:
  - {us: US-03, criterios: [CA-1, CA-2, CA-3]}
segue: [ADR-03, ADR-05]
depende_de: [T-02, T-03]
escopo_arquivos:
  - backend/src/**/reserva/**
  - backend/src/main/resources/db/migration/V004__reservas.sql
tamanho: m
cts: [CT-07, CT-08]
descricao: POST /reservas (sala, início, fim) cria a reserva do usuário da sessão; violação da exclusion constraint vira 409 conflito-de-horario.
criterios_pronto:
  - teste de serviço cria, rejeita fim <= início e traduz conflito em 409
  - teste de integração com PostgreSQL, duas requisições simultâneas, uma 201 e uma 409
  - build verde e lint limpo
  - CT-07 e CT-08 passando
situacao: planejada
---
```

O corpo Markdown (`## O que fazer`, `## Critério de pronto`) detalha em prosa e espelha
`criterios_pronto`; não contradiz o YAML.

## Cobertura: matriz US x T

Ao final, gere (em `estrutura.md` ou no próprio backlog) a tabela:

| US | Prioridade | T | CT |
|---|---|---|---|
| US-03 | Must | T-04, T-06 | CT-07, CT-08, CT-09 |

Toda linha Must com ao menos uma T. US sem T é lacuna; T sem US (fora T-01 e acabamento
técnico, `natureza: esqueleto` ou `refatoracao`) é componente sem justificativa.

## Checklist

- [ ] T-01 esqueleto primeiro, com critério de pronto completo.
- [ ] Toda US Must coberta; CT citados.
- [ ] Todas as T com os campos obrigatórios do esquema `t.schema.json`.
- [ ] Tamanho P/M; G justificado e não multi-camada.
- [ ] Sem ciclo de dependência; paralelas com escopos disjuntos.
- [ ] Migrações e arquivos compartilhados com regra anti-colisão.
- [ ] Critério de pronto verificável por comando ou teste.

## Erros comuns

| Erro | Correção |
|---|---|
| T "implementar backend" | Fatiar por comportamento |
| Critério "funciona" | Teste ou CT nomeado |
| Duas T editando `openapi.yaml` ao mesmo tempo | Contrato fechado antes; T não o altera |
| Esqueleto sem teste vazio | Sem ele o CI do Dev não prova nada |
| Dependência circular | Extrair o comum numa T anterior |
| T sem ADR citada quando há ADR aplicável | Preencher `segue` |
| Numeração de migração duplicada | Atribuir faixas no plano |
| Plano não cobre RNF (acessibilidade, desempenho) | T de acabamento ou critério dentro das T |
