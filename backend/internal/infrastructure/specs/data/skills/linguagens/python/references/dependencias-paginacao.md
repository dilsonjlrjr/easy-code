# Dependências (`Depends`), middlewares e paginação (Python)

Em FastAPI, **dependência** (`Depends`) é o mecanismo de injeção: sessão de banco, usuário
autenticado, configurações, paginação. **Middleware** é para o que vale para toda requisição
(request id, cabeçalhos, CSRF, CORS). Conferir assinaturas na versão fixada.

## Dependências comuns

```python
# src/reservas/deps.py
from collections.abc import Iterator
from typing import Annotated

from fastapi import Depends, Request
from sqlalchemy.orm import Session

from reservas.auth.modelos import UsuarioAtual
from reservas.auth.service import usuario_da_sessao
from reservas.config import Settings
from reservas.plataforma import db


def get_settings_dep(request: Request) -> Settings:
    settings: Settings = request.app.state.settings
    return settings


def get_session() -> Iterator[Session]:
    with db.SessionLocal() as session:        # fecha (e desfaz o que não houve commit) ao fim
        yield session


SettingsDep = Annotated[Settings, Depends(get_settings_dep)]
SessionDep = Annotated[Session, Depends(get_session)]


def usuario_atual(request: Request, session: SessionDep, settings: SettingsDep) -> UsuarioAtual:
    token = request.cookies.get("sessao")
    return usuario_da_sessao(session, settings.sessao, token)    # levanta 401 (NaoAutenticado)


UsuarioDep = Annotated[UsuarioAtual, Depends(usuario_atual)]
```

- **`Annotated[T, Depends(...)]`** como alias (`SessionDep`) mantém a assinatura da rota
  curta e tipada; evita o padrão antigo `x: T = Depends(...)`.
- `get_session` com `yield`: uma `Session` **por request**, compartilhada por todas as
  dependências do mesmo request (o FastAPI faz cache de dependência por request). O `commit`
  **não** fica aqui: é do serviço (a transação é decisão do caso de uso). O `with` garante
  `close()` e rollback do que não foi confirmado.
- O código após o `yield` roda depois da rota; o momento exato relativo ao envio da resposta
  mudou entre versões do FastAPI (conferir). Por isso o `commit` não depende dele.
- `UsuarioDep` falha com 401 antes de a rota rodar; rota sem `UsuarioDep` é pública por
  construção. Rotas protegidas em um `APIRouter(dependencies=[Depends(usuario_atual)])` evitam
  esquecimento, mas o serviço continua checando o **dono do recurso**.
- Dependência de papel:

```python
def exige_admin(usuario: UsuarioDep) -> UsuarioAtual:
    if usuario.papel is not Papel.ADMIN:
        raise SemPermissao("Apenas administradores.")
    return usuario

AdminDep = Annotated[UsuarioAtual, Depends(exige_admin)]
```

- `dependency_overrides` (testes) troca qualquer dependência: por isso sessão, usuário e
  configurações entram **por `Depends`**, nunca por import direto de objeto global.
- Singleton barato por processo (engine, `Settings`): `functools.lru_cache` em
  `get_settings()` ou objeto em `app.state` criado no `lifespan`.
- Dependências **síncronas** (`def`) rodam em thread pool; `async def` rodam no loop. Não
  misturar `Session` síncrona em dependência `async` (ver `sync-async.md`).

## Middlewares

```python
# src/reservas/plataforma/middlewares.py
import contextvars
import time
import uuid

from fastapi import FastAPI, Request, Response
from fastapi.middleware.cors import CORSMiddleware
import logging

request_id_var: contextvars.ContextVar[str] = contextvars.ContextVar("request_id", default="-")
log = logging.getLogger("reservas.http")


def registrar(app: FastAPI, settings: Settings) -> None:
    # Em Starlette/FastAPI o ÚLTIMO middleware adicionado fica mais EXTERNO.
    app.add_middleware(
        CORSMiddleware,
        allow_origins=[settings.origem_permitida],         # origem exata, nunca "*" com credenciais
        allow_credentials=True,
        allow_methods=["GET", "POST", "PUT", "DELETE"],
        allow_headers=["Content-Type", "X-CSRF-Token"],
        max_age=600,
    )

    @app.middleware("http")
    async def limitar_corpo(request: Request, call_next):  # type: ignore[no-untyped-def]  # conferir tipagem
        tamanho = request.headers.get("content-length")
        if tamanho and tamanho.isdigit() and int(tamanho) > settings.corpo_max_bytes:
            return problema_413()
        return await call_next(request)

    @app.middleware("http")
    async def cabecalhos_e_log(request: Request, call_next):  # type: ignore[no-untyped-def]
        rid = request.headers.get("x-request-id") or uuid.uuid4().hex
        token = request_id_var.set(rid)
        inicio = time.perf_counter()
        try:
            resposta: Response = await call_next(request)
        finally:
            request_id_var.reset(token)
        resposta.headers["X-Request-ID"] = rid
        resposta.headers["X-Content-Type-Options"] = "nosniff"
        resposta.headers["Cache-Control"] = "no-store"
        log.info("request", extra={
            "metodo": request.method, "caminho": request.url.path, "status": resposta.status_code,
            "duracao_ms": round((time.perf_counter() - inicio) * 1000, 1), "request_id": rid,
        })
        return resposta
```

- `@app.middleware("http")` (baseado em `BaseHTTPMiddleware`) é simples e suficiente aqui.
  Para custo mínimo ou streaming pesado, escreva um middleware **ASGI puro** (classe com
  `__call__(scope, receive, send)`); conferir limitações do `BaseHTTPMiddleware` na versão.
- **Ordem:** o último adicionado é o mais externo. CORS fica **externo** para responder o
  preflight `OPTIONS` e também os erros.
- O `request_id` em `ContextVar` é lido pelo formatador de log (`config-observabilidade-docker.md`),
  ligando cada linha de log e cada 500 à requisição. Exceção não tratada que escapa do
  `call_next` deixa de passar pelo `log.info` acima; o handler de `Exception` loga com o
  mesmo `request_id` (ainda dentro do contexto).
- **Limite de corpo:** o FastAPI não limita por padrão. Defina o limite no **proxy reverso**
  (`client_max_body_size` do nginx) e, na aplicação, recuse por `Content-Length`; corpo
  `chunked` sem tamanho exige leitura limitada (middleware ASGI puro). Registrar como
  decisão da Arquitetura.
- **Timeout:** não há timeout de request no FastAPI; use o do proxy/servidor
  (`--timeout-keep-alive`, `timeout` do gunicorn, proxy) e `statement_timeout` no PostgreSQL
  (`persistencia-sqlalchemy-alembic.md`). `asyncio.timeout` só vale em `async`.
- CORS e CSRF: `seguranca-sessao.md`. Cabeçalhos mais pesados (CSP, HSTS) ficam no proxy.
- `TrustedHostMiddleware` e `--proxy-headers` do uvicorn quando atrás de proxy (conferir
  `--forwarded-allow-ips`), para esquema/host corretos.

## Paginação

Por padrão, `pagina` + `limite` com teto, ordenação **determinística** (desempate por `id`).
Cursor (keyset) quando a lista é grande ou muda com frequência.

```python
# src/reservas/plataforma/paginacao.py
from dataclasses import dataclass
from typing import Annotated, Generic, TypeVar

from fastapi import Depends, Query
from pydantic import BaseModel

T = TypeVar("T")


@dataclass(frozen=True, slots=True)
class Pagina:
    pagina: int
    limite: int

    @property
    def deslocamento(self) -> int:
        return (self.pagina - 1) * self.limite


def pagina_query(
    pagina: Annotated[int, Query(ge=1)] = 1,
    limite: Annotated[int, Query(ge=1, le=100)] = 20,      # teto: 100
) -> Pagina:
    return Pagina(pagina, limite)


PaginaDep = Annotated[Pagina, Depends(pagina_query)]


class Paginado(BaseModel, Generic[T]):
    itens: list[T]
    total: int
    pagina: int
    limite: int
```

```python
# repository.py
def listar_do_usuario(session: Session, usuario_id: int, p: Pagina) -> tuple[Sequence[Reserva], int]:
    base = select(Reserva).where(Reserva.usuario_id == usuario_id)
    itens = session.scalars(
        base.order_by(Reserva.inicio, Reserva.id).limit(p.limite).offset(p.deslocamento)
    ).all()
    total = session.scalar(select(func.count()).select_from(base.subquery())) or 0
    return itens, total
```

```python
# router.py
@router.get("", response_model=Paginado[ReservaResposta])
def listar(session: SessionDep, usuario: UsuarioDep, p: PaginaDep) -> Paginado[ReservaResposta]:
    itens, total = service.listar(session, usuario, p)
    return Paginado(itens=[ReservaResposta.model_validate(i) for i in itens],
                    total=total, pagina=p.pagina, limite=p.limite)
```

- `Query(ge=1, le=100)` impede `?limite=1000000`; valor fora vira 400 pelo handler de
  validação. O `ORDER BY` com `id` no fim evita linhas repetidas/perdidas entre páginas.
- `Paginado[ReservaResposta]` usa generics do Pydantic v2 (`Generic[T]`); em Python 3.12+ pode
  usar a sintaxe `class Paginado[T](BaseModel)` (conferir suporte da versão do Pydantic).
- `OFFSET` grande é lento; para listas longas, keyset: `WHERE (inicio, id) > (:i, :id)
  ORDER BY inicio, id LIMIT :n` e devolver o cursor da última linha (SQLAlchemy:
  `tuple_(Reserva.inicio, Reserva.id) > tuple_(i, id_)`).
- O contrato (`itens`, `total`, nomes) vem do `openapi.yaml`; o código segue o contrato.
- Filtros opcionais: construa o `select` incrementalmente (`if filtro: stmt = stmt.where(...)`),
  sempre com parâmetros do SQLAlchemy (nunca f-string com entrada).
