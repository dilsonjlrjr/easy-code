---
name: backend-python
description: Convenções e boas práticas para backend Python de API REST com PostgreSQL - stack padrão (Python estável atual, uv, FastAPI com Pydantic v2, SQLAlchemy 2 estilo 2.0 com psycopg 3 síncrono por padrão e async por critério, Alembic, pytest com TestClient e testcontainers, ruff, mypy strict), estrutura src/ por funcionalidade com router, service e repository, schemas Pydantic separados dos modelos, Depends para sessão e usuário, exception handlers em Problem Details, tradução de IntegrityError por SQLSTATE do psycopg para 409, paginação, sync x async e armadilhas de bloquear o event loop, Unit of Work com Session, N+1 e selectinload, Alembic com autogenerate revisado e constraints EXCLUDE manuais, autenticação com sessão em cookie e tabela no PostgreSQL com argon2, checagem por recurso e CSRF, tipagem estrita e erros de domínio, testes com fakes e banco real com rollback por teste, pydantic-settings, logging estruturado, uvicorn e Docker (slim, uv sync --frozen, não root). Use em Arquitetura, Dev, QA e Segurança em projetos com backend Python.
---

# Backend Python

API REST em Python simples, tipada e testável: código síncrono e direto por padrão, tipos
checados (`mypy --strict`), SQL com SQLAlchemy 2 explícito, constraints do PostgreSQL
protegendo as invariantes. O backend **é a fonte da regra e da autorização**; o front só
apresenta. Stack padrão abaixo; a Arquitetura pode mudar com ADR. Skills vizinhas (não
duplicadas aqui; esta skill dá só o "como em Python"): `estrategia-testes` e
`qualidade-codigo` (Dev), `padroes-arquitetura-codigo`, `modelagem-postgresql`,
`ambiente-docker` e `arquitetura-web` (Arquitetura), `revisao-seguranca` (Segurança).

## Princípios

1. **Simplicidade primeiro.** Função e classe simples antes de abstração; sem `ABC`/`Protocol`
   "por precaução"; sem camada que só repassa chamada; sem metaprogramação nem magia.
2. **Handler fino, regra no serviço.** A rota recebe schema validado, chama o serviço e
   devolve schema; a regra vive em módulo sem dependência de FastAPI.
3. **O banco protege a invariante.** Unicidade, sobreposição e FK são constraints
   (`UNIQUE`, `EXCLUDE`, `FOREIGN KEY`); a aplicação traduz `IntegrityError` (SQLSTATE) em 409.
   Checar "antes" sozinho é condição de corrida.
4. **Tipos estritos em todo lugar.** Anotações completas, `mypy --strict` verde, `X | None`
   explícito, Pydantic na fronteira; `Any` e `# type: ignore` só com comentário.
5. **Erros de domínio explícitos.** Exceções próprias (`NaoEncontrado`, `Conflito`) levantadas
   no serviço e traduzidas **uma vez** em exception handlers; nunca `except Exception: pass`.
6. **Síncrono por padrão; async por critério.** `def` nas rotas e `Session` síncrona; `async`
   só com motivo medido, e então tudo async no caminho (nunca bloquear o event loop).
7. **Transação com dono.** Uma `Session` por request; o serviço faz o `commit`; falha
   desfaz tudo; sem escritas soltas.
8. **Segurança por padrão.** Sessão em cookie `HttpOnly`, hash adaptativo (argon2), checagem
   por recurso em todo endpoint protegido, segredos só por ambiente.

## Stack padrão

| Item | Padrão |
|---|---|
| Linguagem | Python estável atual (suportada); versão fixada em `.python-version` / `requires-python` |
| Projeto e dependências | `uv` (`pyproject.toml` + `uv.lock`), layout `src/` |
| HTTP | `FastAPI` + `Pydantic` v2 (schemas), `uvicorn` |
| Banco | `SQLAlchemy` 2 (estilo 2.0: `Mapped`, `select()`) com `psycopg` 3, `Session` síncrona |
| Migração | `Alembic` (autogenerate **revisado à mão**) |
| Teste | `pytest` + `TestClient` (httpx); integração com `testcontainers` (PostgreSQL real) |
| Lint/formato | `ruff check` + `ruff format` |
| Tipos | `mypy --strict` |
| Logs | `logging` da stdlib com formatador JSON no stdout |
| Config | `pydantic-settings` (variáveis de ambiente, lida uma vez) |
| Sessão | Cookie `HttpOnly` + tabela `sessoes` no PostgreSQL (hash do token); `argon2-cffi` |

Versões: "estável atual"; as efetivas ficam em `agentes/ambiente.md` e no `uv.lock`. FastAPI,
Pydantic e SQLAlchemy mudam de API entre versões menores: conferir a documentação da versão
fixada antes de copiar código.

## Alternativas e quando trocar

| Padrão | Alternativa | Trocar quando |
|---|---|---|
| FastAPI | `Django` + `DRF` | Precisa de admin pronto, ORM e auth embutidos e o domínio é CRUD pesado; custo: framework opinativo grande, constraints/SQL menos visíveis |
| FastAPI | `Litestar` | Quer DI, DTOs e OpenAPI integrados com outra filosofia; custo: comunidade menor |
| FastAPI | `Flask` | Quase nunca no padrão: sem tipos/validação integrados |
| SQLAlchemy ORM | SQL puro com `psycopg` | Poucas consultas e time prefere SQL direto; custo: mapeamento manual, sem `Session`/UoW |
| `Session` síncrona | `AsyncSession` (async) | Muitas esperas de E/S concorrentes (chamadas externas, SSE/WebSocket) comprovadas por medição |
| `psycopg` 3 | `asyncpg` | Só em stack async com ganho medido; troca o dialeto e as exceções (SQLSTATE vem em outro atributo) |
| `argon2-cffi` | `pwdlib`, `bcrypt` | Preferência do time; `pwdlib` é o que a documentação do FastAPI usa (conferir) |
| `pydantic-settings` | variáveis lidas à mão | Config mínima (poucas variáveis) |
| uvicorn direto | `gunicorn` + worker uvicorn | Quer gerenciador de processos maduro na mesma máquina (conferir recomendação atual) |

Troca exige ADR; o catálogo do produto traz um padrão por linguagem.

## Regras de decisão

| Situação | Faça | Evite | Referência |
|---|---|---|---|
| Organizar código | `src/<pacote>/<funcionalidade>/` com router, service, repository | Pastas por camada global (`models/`, `routers/`) | estrutura-convencoes |
| Entrada/saída da API | Schemas Pydantic próprios (`extra="forbid"`) | Expor o modelo SQLAlchemy | api-fastapi-erros |
| Erro de domínio | Exceção própria + um exception handler | `HTTPException` dentro do serviço | estrutura-convencoes |
| Violação de `EXCLUDE`/`UNIQUE` | `IntegrityError` -> SQLSTATE `23P01`/`23505` -> 409 | `select` "existe?" antes | api-fastapi-erros |
| Sessão do banco e usuário | `Depends` com `Annotated` | Sessão global / import direto | dependencias-paginacao |
| Rota | `def` (thread pool) com `Session` síncrona | `async def` chamando `Session` síncrona | sync-async |
| Trabalho de CPU em rota `async` | `run_in_threadpool` / `anyio.to_thread` | Hash de senha direto no loop | sync-async |
| Várias escritas atômicas | Uma `Session`, um `commit` no serviço | `commit` em cada repositório | persistencia-sqlalchemy-alembic |
| Relacionamento em lista | `selectinload`/`joinedload` | Acesso preguiçoso em laço (N+1) | persistencia-sqlalchemy-alembic |
| Migração | Autogenerate + revisão; `EXCLUDE` à mão | Confiar no autogenerate às cegas | persistencia-sqlalchemy-alembic |
| Login | Sessão em cookie + tabela no banco | Token em `localStorage` | seguranca-sessao |
| Autorização | Papel **e** dono do recurso | Só `Depends` por rota | seguranca-sessao |
| Teste da regra | Função pura / fake, sem banco | Banco para testar `if` | testes-python |
| Teste de constraint/SQL | `testcontainers` com PostgreSQL | SQLite/mock do driver | testes-python |

## Estrutura (resumo)

```
backend/
  pyproject.toml  uv.lock  alembic.ini  .python-version
  src/reservas/
    main.py              create_app(): rotas, handlers, middlewares, lifespan
    config.py  erro.py  deps.py
    reserva/   router.py service.py repository.py schemas.py modelos.py erros.py
    sala/  auth/
    plataforma/  db.py (engine, Session, Base)  log.py  paginacao.py
  migrations/  env.py  versions/
  tests/
```

Detalhe em `references/estrutura-convencoes.md`.

## Fluxo de trabalho (Dev, por T de backend)

1. Ler a T, o `openapi.yaml`, as constraints do `docs/dados/esquema.sql` e os CT.
2. Migração primeiro (se a T muda o schema): `alembic revision --autogenerate`, **revisar**;
   nunca editar migração já aplicada.
3. Teste primeiro (vermelho): regra pura; depois integração do endpoint (`TestClient`).
4. Implementar: schemas -> modelos -> repositório -> serviço -> rota fina.
5. Mapear erros: validação/JSON -> 400, regra violada -> 409/404, sem sessão -> 401, sem
   permissão -> 403, tudo em Problem Details.
6. Rodar `ruff check`, `ruff format --check`, `mypy`, `pytest`.
7. Conferir `openapi.yaml`, a checklist abaixo e registrar o que for na T.

## Comandos

| Ação | Comando |
|---|---|
| Instalar dependências | `uv sync` (CI/Docker: `uv sync --frozen`) |
| Adicionar dependência | `uv add <pacote>` / `uv add --dev <pacote>` |
| Formatar | `uv run ruff format .` |
| Lint | `uv run ruff check .` (`--fix` com revisão) |
| Tipos | `uv run mypy` |
| Testes | `uv run pytest` |
| Só unitários | `uv run pytest -m "not integracao"` |
| Cobertura | `uv run pytest --cov=reservas` (`pytest-cov`) |
| Migração nova | `uv run alembic revision --autogenerate -m "descricao"` |
| Aplicar migrações | `uv run alembic upgrade head` |
| Rodar | `uv run uvicorn reservas.main:create_app --factory --reload` |
| Auditoria de dependências | `uv run pip-audit` (open source, conferir) |

## Checklist de revisão Python

- [ ] Rota só valida formato (schema), delega ao serviço e devolve schema; sem SQL nem regra nela.
- [ ] Schemas Pydantic de entrada/saída próprios (`extra="forbid"` na entrada); modelo SQLAlchemy nunca na resposta.
- [ ] Anotações completas; `mypy --strict` verde; sem `Any`/`type: ignore` sem comentário.
- [ ] Exceções de domínio levantadas no serviço e traduzidas em handlers; sem `except Exception` engolindo erro.
- [ ] Rota `def` com `Session` síncrona (ou tudo `async` com `AsyncSession`); nada bloqueante em `async def`.
- [ ] Invariantes como constraint; `IntegrityError` traduzido por SQLSTATE (`23P01`/`23505`/`23503`) em 409, com `rollback`.
- [ ] Uma `Session` por request via `Depends`; `commit` no serviço; sem `Session` global.
- [ ] Sem N+1: relacionamentos carregados com `selectinload`/`joinedload` (`lazy="raise"` no modelo).
- [ ] Migração revisada à mão; `EXCLUDE`/índices parciais escritos manualmente; `upgrade` e `downgrade` testados.
- [ ] SQL parametrizado (nunca f-string com entrada); `text()` só com `bindparams`.
- [ ] Sessão: cookie `HttpOnly`/`Secure`/`SameSite`, token aleatório (`secrets`), só o hash no banco; CSRF tratado.
- [ ] Hash de senha com argon2; resposta de login igual para usuário inexistente e senha errada.
- [ ] Todo endpoint protegido checa o dono do recurso; 401 x 403/404 corretos.
- [ ] Paginação com teto e ordenação determinística (desempate por `id`).
- [ ] Config por `pydantic-settings`, lida uma vez; sem segredo em código; logs sem senha/token.
- [ ] `ruff` e `pytest` verdes; imagem Docker slim, `uv sync --frozen`, usuário não root.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `select` de conflito e depois `insert` | Duas requisições passam; dado inválido | Constraint + traduzir `IntegrityError` em 409 |
| `async def` com `Session` síncrona/`requests`/`time.sleep` | Event loop travado; latência de todos | `def` (thread pool) ou driver/cliente async |
| Esquecer `rollback` após `IntegrityError` | `PendingRollbackError` nas próximas operações | `session.rollback()` ao capturar |
| Acesso preguiçoso a relacionamento em laço | N+1 consultas | `selectinload`/`joinedload`; `lazy="raise"` |
| `expire_on_commit=True` + ler atributos após `commit` | Consulta extra ou erro (sessão fechada/async) | `expire_on_commit=False`, devolver schema |
| Modelo SQLAlchemy como `response_model` | Vaza `senha_hash`, acopla API ao banco | Schema de saída próprio |
| `except Exception: pass` | Erro some, estado inconsistente | Capturar o específico; deixar o resto subir ao handler 500 |
| Default mutável / `datetime.now()` sem fuso | Estado compartilhado, datas erradas | `default_factory`; `datetime` com `tzinfo=UTC` |
| Autogenerate aplicado sem ler | Drop de coluna, `EXCLUDE` ausente | Revisar o arquivo; escrever constraint à mão |
| Token de sessão em texto no banco | Vazamento do banco = sessões roubadas | Guardar SHA-256 do token |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/estrutura-convencoes.md` | Ao criar o projeto: `pyproject.toml`, `src/`, pacotes, tipagem estrita, erros de domínio, ruff/mypy |
| `references/api-fastapi-erros.md` | Para schemas Pydantic, rotas, validação, Problem Details e 409 por constraint |
| `references/dependencias-paginacao.md` | Para `Depends` (sessão, usuário), middlewares, request id e paginação |
| `references/sync-async.md` | Para escolher sync x async, thread pool, `AsyncSession` e armadilhas do event loop |
| `references/persistencia-sqlalchemy-alembic.md` | Para modelos, `Session`, transações, N+1, Alembic e `EXCLUDE` |
| `references/seguranca-sessao.md` | Para login, sessão em cookie, `argon2-cffi`, autorização por recurso e CSRF |
| `references/testes-python.md` | Para testes unitários com fakes, banco real com rollback por teste e `dependency_overrides` |
| `references/config-observabilidade-docker.md` | Para `pydantic-settings`, logging JSON, uvicorn, healthcheck e Docker |
