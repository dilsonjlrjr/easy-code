# API HTTP em Python: schemas, rotas, validação e erros

Contrato de erros e regra 400/409 vêm de `arquitetura-web` (Problem Details, RFC 9457). Aqui:
a implementação com FastAPI e Pydantic v2 (conferir assinaturas na versão fixada).

## Quem responde o quê

| Situação | Status | Onde nasce |
|---|---|---|
| JSON malformado, tipo errado, campo desconhecido | 400 (FastAPI usa 422: reescrever) | `RequestValidationError` |
| Campo inválido (vazio, fora da faixa, fim antes do início) | 400 | Validador do schema |
| Parâmetro de caminho/consulta inválido | 400 | `RequestValidationError` |
| Sem sessão válida | 401 | Dependência `usuario_atual` |
| Autenticado sem permissão no recurso | 403 (ou 404 para não revelar existência) | Serviço |
| Não existe | 404 | `NaoEncontrado` |
| **Conflito com o estado** (sala já reservada, e-mail em uso) | **409** | Constraint traduzida |
| Falha inesperada | 500 | Handler de `Exception` |

Regra: **400 = pedido inválido por si**; **409 = pedido válido que conflita com o estado**. O
FastAPI responde 422 por padrão para validação; o contrato do produto usa 400 (handler abaixo).

## Schemas Pydantic: entrada e saída próprios

```python
# src/reservas/reserva/schemas.py
from typing import Annotated, Self

from pydantic import AwareDatetime, BaseModel, ConfigDict, Field, StringConstraints, model_validator

Titulo = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=120)]


class CriarReserva(BaseModel):
    model_config = ConfigDict(extra="forbid")          # campo desconhecido = 400

    sala_id: Annotated[int, Field(gt=0)]
    inicio: AwareDatetime                              # exige fuso; "2025-03-10T09:00:00" sem fuso = 400
    fim: AwareDatetime
    titulo: Titulo

    @model_validator(mode="after")
    def fim_depois_do_inicio(self) -> Self:
        if self.fim <= self.inicio:
            raise ValueError("fim deve ser depois do início")
        return self


class ReservaResposta(BaseModel):
    model_config = ConfigDict(from_attributes=True)    # lê do objeto ORM/dataclass

    id: int
    sala_id: int
    usuario_id: int
    inicio: AwareDatetime
    fim: AwareDatetime
    titulo: str
```

- **Schema de entrada e de saída são classes à parte** do modelo SQLAlchemy. Campo que o
  cliente não pode definir (`id`, `usuario_id`) **não existe** na entrada (evita mass
  assignment); `usuario_id` vem da sessão.
- `extra="forbid"` torna o contrato estrito. `AwareDatetime` recusa data sem fuso; datas
  serializam em RFC 3339.
- Restrições declarativas (`Field`, `StringConstraints`) para campo; `model_validator` para
  regra entre campos. Mensagens em pt-BR no `ValueError`.
- `ReservaResposta.model_validate(obj)` converte o ORM em saída (`from_attributes=True`);
  nunca devolva o objeto ORM direto (vazaria `senha_hash`, colunas internas, relacionamentos).
- Regra de **negócio** que depende do banco (sala existe, horário livre) não vai em
  validador: é constraint ou consulta no serviço.
- Opcional x nulo: `campo: str | None = None` aceita ausente ou `null`; para PATCH que
  distingue "não enviado" de `null`, use `model_fields_set` ou `exclude_unset=True`.

## Rotas finas

```python
# src/reservas/reserva/router.py
from typing import Annotated

from fastapi import APIRouter, Depends, Response, status

from reservas.deps import SessionDep, UsuarioDep
from reservas.plataforma.paginacao import Pagina, PaginaDep
from reservas.reserva import service
from reservas.reserva.schemas import CriarReserva, ReservaResposta

router = APIRouter()


@router.post("", status_code=status.HTTP_201_CREATED, response_model=ReservaResposta)
def criar(corpo: CriarReserva, session: SessionDep, usuario: UsuarioDep, response: Response) -> ReservaResposta:
    reserva = service.criar(session, usuario, corpo)
    response.headers["Location"] = f"/api/reservas/{reserva.id}"
    return ReservaResposta.model_validate(reserva)


@router.get("/{reserva_id}", response_model=ReservaResposta)
def buscar(reserva_id: int, session: SessionDep, usuario: UsuarioDep) -> ReservaResposta:
    return ReservaResposta.model_validate(service.buscar(session, usuario, reserva_id))


@router.delete("/{reserva_id}", status_code=status.HTTP_204_NO_CONTENT)
def cancelar(reserva_id: int, session: SessionDep, usuario: UsuarioDep) -> None:
    service.cancelar(session, usuario, reserva_id)
```

- Rotas são `def` (não `async def`) com `Session` síncrona: o FastAPI as executa em thread
  pool, sem bloquear o loop (`sync-async.md`).
- `SessionDep` e `UsuarioDep` são `Annotated[..., Depends(...)]` (`dependencias-paginacao.md`).
  `UsuarioDep` -> 401 se não houver sessão.
- `response_model` filtra o que sai; com tipo de retorno anotado e `response_model`
  consistente, o `mypy` e o OpenAPI concordam.
- `{reserva_id}` tipado `int`: valor não numérico vira `RequestValidationError` -> 400.
- A rota passa o **schema** ou valores primitivos ao serviço; o serviço devolve objeto do
  domínio/ORM. Sem SQL nem regra aqui; rota com mais de ~10 linhas pede revisão.
- `status_code` de sucesso declarado na rota (201, 204); `204` não devolve corpo.

## Exception handlers: Problem Details

```python
# src/reservas/erro.py (continuação)
import logging

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException as StarletteHTTPException

log = logging.getLogger(__name__)
PROBLEM_JSON = "application/problem+json"


def _problema(status: int, tipo: str, titulo: str, detalhe: str, erros: list[dict[str, str]] | None = None) -> JSONResponse:
    corpo: dict[str, object] = {
        "type": f"https://exemplo.invalid/problemas/{tipo}",
        "title": titulo,
        "status": status,
        "detail": detalhe,
    }
    if erros:
        corpo["errors"] = erros
    return JSONResponse(corpo, status_code=status, media_type=PROBLEM_JSON)


def registrar_handlers(app: FastAPI) -> None:
    @app.exception_handler(ErroDominio)
    async def _dominio(_: Request, exc: ErroDominio) -> JSONResponse:
        return _problema(exc.status, exc.tipo, exc.titulo, exc.detalhe)

    @app.exception_handler(RequestValidationError)
    async def _validacao(_: Request, exc: RequestValidationError) -> JSONResponse:
        erros = [
            {"campo": ".".join(str(p) for p in e["loc"][1:]) or str(e["loc"][0]), "mensagem": e["msg"]}
            for e in exc.errors()
        ]
        return _problema(400, "requisicao-invalida", "Requisição inválida", "Corpo ou parâmetros inválidos.", erros)

    @app.exception_handler(StarletteHTTPException)
    async def _http(_: Request, exc: StarletteHTTPException) -> JSONResponse:
        titulos = {401: "Não autenticado", 403: "Sem permissão", 404: "Não encontrado", 405: "Método não permitido"}
        return _problema(exc.status_code, f"http-{exc.status_code}", titulos.get(exc.status_code, "Erro"), str(exc.detail))

    @app.exception_handler(Exception)
    async def _inesperado(request: Request, exc: Exception) -> JSONResponse:
        log.exception("erro inesperado", extra={"caminho": request.url.path})
        return _problema(500, "erro-interno", "Erro interno", "Erro inesperado. Tente novamente.")
```

- `loc` do Pydantic é `("body", "fim")` ou `("query", "limite")`; o primeiro item é a origem.
  Montar `campo` a partir do resto. `e["msg"]` vem em inglês para erros padrão
  (`"Field required"`): mapear para pt-BR se o contrato exigir (dicionário por `e["type"]`).
- Não devolver `exc.errors()` cru: pode trazer `ctx` com a exceção (não serializável) e
  `input` (eco do dado enviado, possível dado sensível).
- O handler de `Exception` loga a pilha (`log.exception`) e responde genérico: nada de SQL,
  caminho ou `repr` para o cliente. Em Starlette, esse handler roda no
  `ServerErrorMiddleware`, que **também re-levanta** a exceção após responder; no
  `TestClient`, use `raise_server_exceptions=False` para ver o 500.
- Handlers `async def` leves são o padrão do FastAPI mesmo com rotas síncronas.
- `StarletteHTTPException` cobre 404/405 do roteador e `HTTPException` das dependências.

## Tradução de `IntegrityError` por SQLSTATE (409)

```python
# src/reservas/plataforma/pg.py
from enum import Enum

from psycopg import errors as pgerr
from sqlalchemy.exc import IntegrityError


class Violacao(Enum):
    EXCLUSAO = "23P01"          # exclusion_violation (EXCLUDE)
    UNICIDADE = "23505"         # unique_violation
    CHAVE_ESTRANGEIRA = "23503" # foreign_key_violation


def violacao(exc: IntegrityError) -> tuple[Violacao, str | None] | None:
    """Classifica um IntegrityError do psycopg 3; None se não for uma violação conhecida."""
    orig = exc.orig
    if not isinstance(orig, pgerr.Error):      # outro driver / erro fora do psycopg
        return None
    try:
        tipo = Violacao(orig.sqlstate)
    except ValueError:
        return None
    return tipo, orig.diag.constraint_name
```

```python
# src/reservas/reserva/repository.py
def inserir(session: Session, r: Reserva) -> Reserva:
    session.add(r)
    try:
        session.flush()                        # envia o INSERT agora; a constraint dispara aqui
    except IntegrityError as exc:
        session.rollback()                     # obrigatório: a transação ficou inválida
        v = violacao(exc)
        if v and v[0] is Violacao.EXCLUSAO:
            raise Conflito("A sala já está reservada nesse período.") from exc
        if v and v[0] is Violacao.CHAVE_ESTRANGEIRA:
            raise EntradaInvalida("Sala inexistente.") from exc
        raise
    return r
```

- Em `psycopg` 3 o `SQLSTATE` está em `orig.sqlstate` e o nome da constraint em
  `orig.diag.constraint_name`; as classes específicas (`psycopg.errors.ExclusionViolation`,
  `UniqueViolation`, `ForeignKeyViolation`) também funcionam com `isinstance`. Conferir os
  atributos na versão fixada; com outro driver (`asyncpg`) o acesso muda.
- **Sempre `rollback()`** após `IntegrityError`; senão a sessão fica em estado inválido
  (`PendingRollbackError`). O serviço, ao traduzir, já está fora da transação inválida.
- `flush()` antes do `commit()` faz a violação aparecer **onde o código a trata**, não no
  `commit` distante. `SQLSTATE`: exclusion `23P01`, unique `23505`, FK `23503`.
- `nome da constraint` (do `docs/dados/esquema.sql`) permite mensagens distintas quando houver várias
  `UNIQUE` (ex.: `usuarios_email_key` -> "E-mail já em uso").
- Mesma tradução em `UPDATE` e em qualquer escrita que possa violar a constraint.

## Paginação e contrato de lista

Schema de lista com `itens`, `total`, `pagina`, `limite` (seguir o `openapi.yaml`); parâmetros
com teto via `Query(ge=1, le=100)` — detalhe em `dependencias-paginacao.md`. Lista vazia
serializa `[]`, nunca `null`.

## OpenAPI

- O FastAPI gera o OpenAPI do código; o **contrato** é o `openapi.yaml` da Arquitetura.
  Comparar a saída (`/openapi.json`) com o contrato em teste (ou no CI) para não divergir.
- Declare `responses={409: {...}, 401: {...}}` nas rotas com `media_type="application/problem+json"`
  para o contrato de erro aparecer na documentação.
- Desligue `/docs` e `/redoc` em produção se a Arquitetura assim definir (`docs_url=None`).
