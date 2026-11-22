# Easy Code — project

Esqueleto da plataforma, iniciado a pedido do dono em 2026-11-21. As convenções estão na
[ADR-21](../docs/arquitetura/adr/ADR-21.md): o código é em inglês, segue Clean Architecture e usa i18n pt-BR (padrão)
e en. O backend registra tudo em log, com redação. A construção segue os marcos de
[docs/entrega/PLANO.md](../docs/entrega/PLANO.md), a começar pelo MARCO-0 (Fundação).

| Pasta | Conteúdo |
|---|---|
| [backend/](backend/) | Go com valyala/fasthttp e pgx v5. Binários `easycode-api`, `easycode-worker`, `easycode-sandbox` e `easycode-gateway` (ADR-02). Camadas `internal/{domain,application,infrastructure,interfaces/http}`. Migrações SQL em `migrations/` |
| [frontend/](frontend/) | Vue 3, Vite, TypeScript, Vue Router, Pinia, vue-i18n e Vuetify 3 (Material). Pastas `src/{app,features,shared}` |
| [compose.yaml](compose.yaml) | Stack de desenvolvimento: PostgreSQL, migrate, api, worker e frontend |

## Subir localmente

Só Docker é necessário no host.

```sh
docker compose up --build
# web:  http://localhost:8080
# api:  http://localhost:8081/api/v1/health   (liveness)
#       http://localhost:8081/api/v1/ready    (readiness, confere o banco)
```

O serviço `migrate` (`easycode-api migrate`) aplica as migrações em ordem, registra cada uma em `schema_migrations` e
termina. Depois disso sobem a api e o worker.

## CI

Ponto de entrada único, só com Docker e make no host (RNF-17). Não há arquivo de fornecedor de CI porque o
repositório ainda não tem remoto: qualquer executor (GitHub Actions, GitLab CI, Jenkins, cron no servidor) só chama

```sh
make -C project ci
```

| Alvo | O que roda | Container |
|---|---|---|
| `backend-ci` | `make -C backend ci` (lint, testes e build do backend) | `golang`, `golangci-lint` |
| `frontend-ci` | `npm ci`, depois `lint`, `type-check`, `test` (vitest) e `build-only` | `node:24.22.3-bookworm-slim` |
| `frontend-e2e` | Playwright contra o build de produção | `mcr.microsoft.com/playwright:v1.65.1-noble` |
| `specs` | `python3 specs/dominio/ferramentas/validar_tudo.py` (repositório montado só leitura) | `python:3.13.9-slim-bookworm` |

Cada alvo também roda sozinho (por exemplo `make -C project frontend-ci`). O cache do npm fica no volume
`easycode-npm-cache`. A Definition of Done está em [docs/entrega/DoD.md](../docs/entrega/DoD.md) e o plano dos marcos em
[docs/entrega/PLANO.md](../docs/entrega/PLANO.md).

## Backend

```sh
cd backend
make build      # compila os 4 binários e roda go vet (dentro de golang:1.26.10-bookworm)
make test       # testes; com EASYCODE_TEST_DATABASE_URL, também o teste de integração do migrador
make lint       # golangci-lint v2.15.0
make image      # imagem distroless non-root
```

Variáveis principais: `EASYCODE_DATABASE_URL`, `EASYCODE_MIGRATION_DATABASE_URL`, `EASYCODE_HTTP_ADDR`,
`EASYCODE_LOG_LEVEL` e `EASYCODE_OTEL_ENABLED`/`EASYCODE_OTEL_ENDPOINT`. Não há endereço fixo no código (RNF-17).

**Log:** sai em JSON na saída padrão. Cada requisição gera uma linha `http request` e cada consulta uma linha
`db query`, além de jobs, chamadas externas e erros com a cadeia de causa, tudo ligado ao trace id. O redator central
remove chaves, tokens, cookies, senhas, `Authorization` e conteúdo de prompt e de artefato (RNF-20).

## Frontend

```sh
cd frontend
npm ci
npm run dev         # servidor de desenvolvimento
npm run lint        # eslint (inclui a regra contra texto literal nos templates) e prettier
npm run test        # vitest
npm run build       # type-check e bundle
npm run e2e:docker  # playwright dentro da imagem oficial
```

A URL da API vem de `config.js`, gerado na subida do container a partir de `API_BASE_URL`. Toda requisição manda
`Accept-Language`.

## Pendências conhecidas

- O compose de desenvolvimento conecta como superusuário. Os papéis por processo (ADR-18), o Keycloak, o proxy TLS e
  o CORS entre a web e a api ficam para as tarefas da E7.
- Os diagramas de `docs/dados/er.md` passaram por conversão textual para inglês e ainda não mostram as PKs `id` novas
  nem as relações por `stage_id`. Precisam ser regenerados a partir do catálogo.
- Os nomes de variáveis e funções dos validadores Python em `specs/dominio/ferramentas/` continuam em português. As
  strings que eles conferem já estão em inglês.
