# Ambiente efêmero para testes e comandos de projeto

> **Onde isto roda.** `docker-compose.yml` e Dockerfiles do projeto gerado continuam sendo artefatos entregues, para CI e para humanos. Os comandos `docker compose ...` e `docker run ...` deste arquivo são **para CI/humano**. No sandbox da plataforma o agente não tem Docker nem rede: testes de integração/E2E rodam contra os **serviços de apoio declarados no perfil** (ex.: PostgreSQL efêmero) que o runner sobe, e só com os comandos da allowlist da sua categoria (`testes`, `migracoes`, `banco_descartavel`).

## Princípios

1. **Um comando sobe, um comando testa.** `make up`, `make test`. Quem executa não precisa
   saber a ordem das peças.
2. **Efêmero:** o ambiente de teste nasce limpo e é destruído no final; nenhum teste
   depende do que sobrou da execução anterior.
3. **Isolado:** projeto Compose próprio para testes (nome e portas diferentes), para
   rodar junto do ambiente de desenvolvimento.
4. **Determinístico:** seed fixo, relógio e dados controlados; sem esperas arbitrárias.
5. **Código de saída honesto:** `make test` retorna não zero se qualquer parte falhar
   (CI e agentes dependem disso).

## Camadas de teste e onde cada uma roda

| Camada | O que sobe | Onde roda | Banco |
|---|---|---|---|
| Unidade (backend, front) | nada | no host/contêiner de build | nenhum |
| Integração (backend + PostgreSQL) | só `db` (efêmero) | testes do backend | PostgreSQL real (contêiner) |
| API/contrato | `db` + `api` | cliente HTTP de teste | PostgreSQL real, seed |
| e2e (navegador) | `db` + `api` + `web` + `e2e` | contêiner do e2e (perfil `test`) | PostgreSQL real, seed |

Preferir **PostgreSQL real em contêiner** a banco em memória/emulado: as regras críticas
(EXCLUDE, constraints, tipos) só são comprovadas no motor real. Testes de concorrência
(`../../dados/modelagem-postgresql/references/concorrencia-transacoes.md`) também.

## Banco efêmero para integração

```yaml
# docker-compose.test.yml (sobreposição) ou serviço com perfil
services:
  db-test:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: reservas_test
      POSTGRES_USER: test
      POSTGRES_PASSWORD: test
    tmpfs: [/var/lib/postgresql/data]      # tudo em memória, some ao parar
    ports: ["55432:5432"]                  # porta diferente da de dev
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U test -d reservas_test"]
      interval: 2s
      timeout: 2s
      retries: 20
```

Uso: `docker compose -p reservas-test -f docker-compose.test.yml up -d --wait db-test`
(`--wait` espera os serviços ficarem saudáveis; **conferir** a versão do Compose), roda os
testes de integração apontando para `localhost:55432`, depois `docker compose -p reservas-test down -v`.

Alternativa em código: bibliotecas open source que sobem o contêiner do PostgreSQL a partir
do teste (família "Testcontainers", disponível para várias linguagens). Vantagem: o teste
é autossuficiente; custo: exige acesso ao Docker no ambiente do teste. Decida em ADR/`estrutura.md`.

Isolamento entre testes: cada teste cria seus próprios dados e limpa (transação revertida,
`TRUNCATE ... RESTART IDENTITY CASCADE`, ou esquema/banco por suíte). Não depender de ordem.

## e2e (Playwright) com Compose

Serviço `e2e` no perfil `test` (ver `compose.md`) usando a imagem oficial do Playwright
(conferir tag compatível com a versão do pacote de teste):

```yaml
  e2e:
    image: mcr.microsoft.com/playwright:<tag-compativel>
    profiles: ["test"]
    working_dir: /e2e
    volumes: ["./e2e:/e2e"]
    environment:
      BASE_URL: http://web:8080
      API_URL: http://api:8080
      CI: "true"
    command: ["sh", "-c", "npm ci && npx playwright test"]
    depends_on:
      web: { condition: service_started }
      api: { condition: service_healthy }
```

Execução e saída de código:

```bash
docker compose -p reservas-test --profile test up --build \
  --abort-on-container-exit --exit-code-from e2e
```

`--exit-code-from e2e` faz o comando devolver o código do serviço e2e (implica
`--abort-on-container-exit`). Depois, `down -v` sempre (mesmo em falha: `trap` no script).
Relatório/vídeos/traces do Playwright em volume ou pasta montada (`e2e/test-results`), para
o QA anexar como evidência.

Boas práticas de e2e estável: esperar por **condições** (elemento visível, resposta de
rede), não por `sleep`; um seed determinístico por execução; seletores acessíveis (papel,
rótulo) em vez de CSS frágil; usuário de teste por papel vindo do seed.

## Makefile de referência

```makefile
.PHONY: up down reset logs ps migrate seed test test-unit test-int test-e2e lint

COMPOSE      = docker compose
COMPOSE_TEST = docker compose -p reservas-test -f docker-compose.yml -f docker-compose.test.yml

up:                     ## sobe front, API e banco, esperando ficarem saudáveis
	$(COMPOSE) up --build -d --wait

down:                   ## derruba (mantém dados)
	$(COMPOSE) down

reset:                  ## recria do zero (apaga volumes)
	$(COMPOSE) down -v
	$(MAKE) up
	$(MAKE) seed

migrate:                ## aplica migrações (ajustar ao comando do projeto)
	$(COMPOSE) run --rm api <comando-de-migracao>

seed:                   ## dados e usuários de teste (idempotente; não roda em produção)
	$(COMPOSE) exec -T db sh -c 'psql -U $$POSTGRES_USER -d $$POSTGRES_DB' < db/seed.sql

test: test-unit test-int test-e2e   ## tudo; falha se qualquer parte falhar

test-unit:              ## unidade backend + front (comandos da linguagem; ver estrutura.md)
	cd backend  && <comando-de-teste-unitario>
	cd frontend && npm test -- --run

test-int:               ## integração com PostgreSQL efêmero
	$(COMPOSE_TEST) up -d --wait db-test
	cd backend && <comando-de-teste-de-integracao>; rc=$$?; \
	  $(COMPOSE_TEST) down -v; exit $$rc

test-e2e:               ## e2e no ambiente efêmero completo
	$(COMPOSE_TEST) --profile test up --build --abort-on-container-exit --exit-code-from e2e; rc=$$?; \
	  $(COMPOSE_TEST) down -v; exit $$rc

lint:                   ## lint e formatação de backend e front
	cd backend  && <comando-de-lint>
	cd frontend && npm run lint

logs:
	$(COMPOSE) logs -f --tail=100

ps:
	$(COMPOSE) ps
```

Detalhes:
- `rc=$$?; ...; exit $$rc` garante a limpeza **e** preserva o código de saída.
- `--wait` em `up` espera healthchecks (conferir versão do Compose); alternativa: `docker compose up -d` e laço que consulta `ps`.
- Make exige tabulação nas receitas.
- Em ambiente sem `make`: scripts equivalentes em `scripts/` (`up.sh`, `test.sh`); o README documenta o nome real.
- O arquivo de sobreposição `docker-compose.test.yml` troca portas, remove volumes persistentes e adiciona `db-test`/`e2e`.

## Script de teste (alternativa sem Make)

```bash
#!/usr/bin/env bash
set -euo pipefail
cleanup() { docker compose -p reservas-test down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker compose -p reservas-test -f docker-compose.yml -f docker-compose.test.yml \
  --profile test up --build --abort-on-container-exit --exit-code-from e2e
```

## CI

O mesmo `make test` roda no CI (qualquer provedor): requer Docker e Compose V2 disponíveis.
Cache de camadas do BuildKit para acelerar. Falha do teste = falha do pipeline. Artefatos
(relatórios, traces) enviados como evidência ao QA.

## Dados e usuários de teste

- Seed determinístico: mesmos ids/e-mails/senhas de teste sempre (vêm do `.env.example`).
- Um usuário por papel + dados para cada CT (sala ativa, sala inativa, reserva existente
  para forçar conflito).
- Senhas de teste só valem no ambiente efêmero/dev; seed bloqueado em produção.
- e2e autentica uma vez e reaproveita o estado (armazenamento de sessão do teste) para ganhar tempo.

## Checklist

- [ ] `make up` sobe tudo saudável; `make test` roda unidade, integração e e2e.
- [ ] Projeto Compose de teste isolado (nome, portas), sem depender do ambiente de dev.
- [ ] PostgreSQL real e efêmero (`tmpfs` ou `down -v`) nos testes de integração e e2e.
- [ ] Código de saída reflete falha; limpeza garantida (`trap`/`rc`).
- [ ] e2e espera condições, não `sleep`; evidências salvas.
- [ ] Seed determinístico com usuários por papel.
- [ ] Mesmo comando no CI e local.

## Erros comuns

| Erro | Correção |
|---|---|
| Testes de integração com banco em memória/emulado | PostgreSQL real em contêiner |
| Testes que reaproveitam o banco de dev | Projeto e banco de teste separados |
| `down` não executado quando o teste falha | `trap`/`rc` |
| `make test` sempre retorna 0 | Propagar o código de saída |
| `sleep 30` para esperar o app | `--wait`/healthcheck |
| e2e acoplado a seletores CSS frágeis | Seletores por papel/rótulo |
| Portas de teste colidindo com dev | Portas e nome de projeto próprios |
| Testes dependentes entre si | Dados próprios por teste |
| Imagem do Playwright com versão diferente do pacote | Alinhar a tag à versão do pacote |
