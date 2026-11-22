# Configuração, observabilidade e Docker (Python)

Ambiente e Compose (serviços, healthcheck, volumes, rede): `ambiente-docker`. Aqui: o lado
Python — config por ambiente, logging, servidor, healthcheck e imagem.

## Configuração com `pydantic-settings`

Variáveis de ambiente lidas **uma vez**, validadas e tipadas, em um objeto imutável.

```python
# src/reservas/config.py
from datetime import timedelta
from functools import lru_cache

from pydantic import Field, SecretStr, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class SessaoCfg(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="SESSAO_", frozen=True)
    ttl_idle: timedelta = timedelta(minutes=30)
    ttl_max: timedelta = timedelta(hours=12)


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore", frozen=True)

    database_url: SecretStr                              # obrigatória: sem padrão
    origem_permitida: str                                # obrigatória
    cookie_seguro: bool = True                           # seguro por padrão
    producao: bool = True
    log_json: bool = True
    log_nivel: str = "INFO"
    corpo_max_bytes: int = Field(default=1_048_576, gt=0)
    sessao: SessaoCfg = SessaoCfg()

    @field_validator("database_url")
    @classmethod
    def so_psycopg3(cls, v: SecretStr) -> SecretStr:
        if not v.get_secret_value().startswith("postgresql+psycopg://"):
            raise ValueError("use postgresql+psycopg:// (psycopg 3)")
        return v

    @classmethod
    def de_teste(cls) -> "Settings":
        return cls(database_url=SecretStr("postgresql+psycopg://t:t@localhost/t"),  # type: ignore[call-arg]
                   origem_permitida="http://localhost", cookie_seguro=False, producao=False, log_json=False)


@lru_cache
def get_settings() -> Settings:
    return Settings()      # type: ignore[call-arg]  # campos obrigatórios vêm do ambiente
```

- Falha **cedo e clara** na subida (variável faltando ou inválida) com `ValidationError`
  em vez de falha tardia em runtime.
- `SecretStr` evita vazar segredo em `repr`/log; use `.get_secret_value()` só onde precisa
  (criar o engine). `frozen=True` impede mutação acidental.
- Padrão seguro: `cookie_seguro=True` por padrão; desligar só no `.env` de desenvolvimento.
- `.env` só em desenvolvimento (no Compose via `env_file`/`environment`); produção injeta
  variáveis. `.env` no `.gitignore`; só `.env.example` versionado.
- Variável por ambiente tem prefixo ou nome direto (`DATABASE_URL`); aninhadas com
  `env_nested_delimiter="__"` (conferir). `lru_cache` em `get_settings` cria um único
  objeto por processo; os testes constroem `Settings.de_teste()` e passam a `create_app`.
- Os `type: ignore[call-arg]` existem porque o `mypy` não enxerga que os campos
  obrigatórios vêm do ambiente; comentar o motivo.
- Segredo nunca em código, imagem ou log. `repr(settings)` mascara `SecretStr`.

## Logging estruturado

```python
# src/reservas/plataforma/log.py
import json
import logging
import sys
from datetime import UTC, datetime

from reservas.plataforma.middlewares import request_id_var

_RESERVADOS = set(logging.LogRecord("", 0, "", 0, "", (), None).__dict__) | {"message", "asctime"}


class FormatadorJson(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        evento: dict[str, object] = {
            "ts": datetime.fromtimestamp(record.created, UTC).isoformat(),
            "nivel": record.levelname,
            "logger": record.name,
            "msg": record.getMessage(),
            "request_id": request_id_var.get(),
        }
        evento |= {k: v for k, v in record.__dict__.items() if k not in _RESERVADOS}   # campos de `extra=`
        if record.exc_info:
            evento["erro"] = self.formatException(record.exc_info)
        return json.dumps(evento, ensure_ascii=False, default=str)


def configurar(settings: Settings) -> None:
    h = logging.StreamHandler(sys.stdout)                       # stdout: o Docker coleta
    h.setFormatter(FormatadorJson() if settings.log_json else logging.Formatter("%(asctime)s %(levelname)s %(name)s %(message)s"))
    raiz = logging.getLogger()
    raiz.handlers[:] = [h]
    raiz.setLevel(settings.log_nivel)
    logging.getLogger("sqlalchemy.engine").setLevel(logging.WARNING)   # sem SQL no log (INFO mostra tudo)
```

- JSON no **stdout** (um evento por linha); nível pelo ambiente (`INFO` em produção).
- `request_id` do `ContextVar` (`dependencias-paginacao.md`) entra em **toda** linha: liga o
  erro 500 à requisição.
- Campos estruturados via `extra`: `log.info("reserva criada", extra={"reserva_id": r.id})`.
  Evite montar texto com f-string e perder o campo consultável. Alternativa: `structlog`
  (conferir) para contexto e processadores prontos; a stdlib basta no começo.
- Níveis: `ERROR` falha inesperada (500, com `log.exception`), `WARNING` condição anormal
  tratada, `INFO` marcos de negócio, `DEBUG` detalhe. Erro de cliente (400/409) não é `ERROR`.
- Nunca logar senha, token de sessão, `senha_hash`, `DATABASE_URL`, corpo inteiro do login.
  Cuidado com `echo=True` do engine e com `repr` de objetos com segredo.
- Logs do uvicorn: `--log-config`/`--no-access-log` se preferir o log de acesso próprio
  (middleware) para não duplicar; configurar para o mesmo formatador (conferir).
- Métricas e traces (OpenTelemetry, `prometheus-fastapi-instrumentator`) entram quando a
  Arquitetura pedir (conferir). Em aplicação pequena, logs + healthcheck bastam.

## Healthcheck

```python
# rotas públicas, fora de /api, sem autenticação
@app.get("/saude/vivo", include_in_schema=False)
def vivo() -> dict[str, str]:                       # o processo responde
    return {"status": "ok"}

@app.get("/saude/pronto", include_in_schema=False)
def pronto(session: SessionDep) -> Response:        # o banco responde
    try:
        session.execute(text("SELECT 1"))
    except Exception:                               # noqa: BLE001 - qualquer falha = não pronto
        return Response(status_code=503)
    return Response(status_code=200)
```

- `vivo` não toca o banco (reiniciar por causa do banco fora não resolve); `pronto` confere
  dependências. O `healthcheck` do Compose usa `pronto`. Imagem slim não tem `curl`:
  `python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8080/saude/pronto').status==200 else 1)"`.

## Servidor: uvicorn

| Cenário | Recomendação |
|---|---|
| Contêiner (padrão) | **Um** `uvicorn` por contêiner; escalar com réplicas (orquestrador) ou `--workers N` pequeno |
| Vários núcleos numa máquina sem orquestrador | `uvicorn --workers N`, ou `gunicorn` com worker uvicorn (conferir: o módulo `uvicorn.workers` está em transição para o pacote `uvicorn-worker`) |
| Desenvolvimento | `uvicorn ... --reload` (nunca em produção) |

```
uvicorn reservas.main:create_app --factory --host 0.0.0.0 --port 8080 --proxy-headers --timeout-graceful-shutdown 20
```

- `--factory` chama `create_app()`; `--proxy-headers` (+ `--forwarded-allow-ips` confiável)
  quando atrás de proxy reverso; `--timeout-graceful-shutdown` (conferir o nome na versão)
  limita a espera ao receber `SIGTERM`. O uvicorn já trata `SIGTERM`: para de aceitar
  conexões, conclui as em voo e roda o `lifespan` de encerramento (dispose do engine).
- `N` workers = N processos = N engines: `N x pool_size` conexões no PostgreSQL.
- Alinhar `stop_grace_period` do Compose ao tempo de graceful shutdown.
- TLS e limites (corpo, timeout) ficam no **proxy reverso** (nginx/Caddy/Traefik); o uvicorn
  fica na rede interna.

## Docker: imagem slim com `uv`

```dockerfile
# syntax=docker/dockerfile:1
FROM python:3.13-slim AS build                    # fixar a versão em agentes/ambiente.md
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/    # fixar a tag do uv (não "latest") em produção
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never
WORKDIR /app

COPY pyproject.toml uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev --no-install-project      # só dependências (camada cacheável)
COPY src ./src
COPY migrations ./migrations
COPY alembic.ini ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev                           # instala o projeto

FROM python:3.13-slim AS final
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 PATH="/app/.venv/bin:$PATH"
RUN groupadd --system app && useradd --system --gid app --home /app --no-create-home app
WORKDIR /app
COPY --from=build --chown=app:app /app /app
USER app
EXPOSE 8080
CMD ["uvicorn", "reservas.main:create_app", "--factory", "--host", "0.0.0.0", "--port", "8080", "--proxy-headers"]
```

- **`uv sync --frozen`**: usa o `uv.lock` sem recalcular (falha se divergir). Duas etapas
  (`--no-install-project` e depois o projeto) fazem a camada de dependências ser reaproveitada
  quando só o código muda.
- **Multi-stage**: a imagem final não leva `uv`, cache nem compiladores; só o `.venv` e o
  código. Mesma versão do Python nos dois estágios (o `.venv` aponta para o interpretador).
- `psycopg[binary]` traz a `libpq` embutida: funciona em `slim` sem pacotes de sistema. A
  alternativa `psycopg[c]` exige `libpq-dev` e compilador no build e `libpq5` no final
  (conferir a recomendação atual para produção).
- **Usuário não root**, `read_only: true` no Compose quando possível (nesse caso, `/tmp` via
  `tmpfs`); sem `--privileged`.
- Migração: serviço separado no Compose com a **mesma imagem** e comando
  `alembic upgrade head` (a imagem leva `migrations/` e `alembic.ini`); a API só sobe após
  o serviço de migração concluir (`depends_on: condition: service_completed_successfully`).
- `.dockerignore`: `.venv/`, `.git/`, `.env*`, `__pycache__/`, `.mypy_cache/`, `.pytest_cache/`,
  `tests/` se não forem usados no build.
- Tags **fixadas** (não `latest`); varredura de imagem (`trivy`, open source) no CI.
- Alternativa: imagens `distroless`/`chainguard` para Python reduzem superfície, com custo
  de depuração (sem shell); começar por `slim`.

## Compose (trecho)

```yaml
services:
  migrar:
    build: ./backend
    command: ["alembic", "upgrade", "head"]
    environment: &env
      DATABASE_URL: postgresql+psycopg://reservas:${DB_SENHA}@db:5432/reservas
    depends_on: { db: { condition: service_healthy } }
  api:
    build: ./backend
    environment:
      <<: *env
      ORIGEM_PERMITIDA: http://localhost:5173
      COOKIE_SEGURO: "false"           # só desenvolvimento local (HTTP)
      PRODUCAO: "false"
    depends_on:
      migrar: { condition: service_completed_successfully }
    stop_grace_period: 25s
```

## Lint e CI (resumo)

| Etapa | Comando |
|---|---|
| Formato | `uv run ruff format --check .` |
| Lint | `uv run ruff check .` |
| Tipos | `uv run mypy` |
| Testes | `uv run pytest` (com Docker/PostgreSQL) |
| Lock em dia | `uv lock --check` |
| Dependências | `uv run pip-audit` |
| Imagem | `docker build`; `trivy image` (conferir) |
