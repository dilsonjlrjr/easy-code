# Estrutura e convenções (Python)

Princípios de arquitetura (camadas, quando separar): `padroes-arquitetura-codigo`. Qualidade
geral: `qualidade-codigo`. Aqui: como isso fica em um projeto Python com `uv`.

## Projeto: `pyproject.toml` com layout `src/`

```toml
[project]
name = "reservas"
version = "0.1.0"
requires-python = ">=3.12"          # mínimo suportado; versão efetiva em .python-version
dependencies = [
  "fastapi>=0.115",                 # versões ilustrativas: fixar as do projeto no uv.lock (conferir)
  "uvicorn[standard]>=0.30",
  "pydantic>=2",
  "pydantic-settings>=2",
  "sqlalchemy>=2",
  "psycopg[binary]>=3.2",
  "alembic>=1.13",
  "argon2-cffi>=23",
]

[dependency-groups]                 # PEP 735; uv add --dev grava aqui (conferir na versão do uv)
dev = ["pytest>=8", "pytest-cov", "httpx", "testcontainers[postgresql]", "ruff", "mypy"]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[tool.ruff]
line-length = 100
target-version = "py312"

[tool.ruff.lint]
select = ["E", "F", "I", "B", "UP", "SIM", "C4", "PT", "RUF", "S", "DTZ", "T20"]
# S = bandit (segurança), DTZ = datetime sem fuso, T20 = print esquecido, B = armadilhas
[tool.ruff.lint.per-file-ignores]
"tests/**" = ["S101", "S105", "S106"]     # assert e senhas fixas são normais em teste

[tool.mypy]
strict = true
plugins = ["pydantic.mypy"]
mypy_path = "src"
files = ["src", "tests"]

[tool.pytest.ini_options]
testpaths = ["tests"]
markers = ["integracao: usa PostgreSQL real"]
addopts = "-ra --strict-markers"
```

- **Layout `src/`**: o pacote só é importável depois de instalado (`uv sync` instala o
  projeto em modo editável), o que evita testar código que não seria empacotado.
- `uv.lock` é **versionado**; CI/Docker usam `uv sync --frozen` (falha se o lock divergir
  do `pyproject.toml`). Atualizar com `uv lock --upgrade-package <pacote>` e revisar o diff.
- Os intervalos mínimos acima são ilustrativos; o lock é a verdade das versões.
- Um único `pyproject.toml` configura ruff, mypy e pytest: sem `setup.cfg`, `tox.ini`,
  `.flake8`, `black`, `isort` (o ruff substitui flake8, isort e black).
- Estilo de regras do ruff: começar enxuto e crescer; `# noqa` só com código e motivo.

## Pacotes por funcionalidade

```
src/reservas/
  __init__.py
  main.py          create_app()
  config.py        Settings (pydantic-settings)
  erro.py          ErroDominio + handlers (Problem Details)
  deps.py          dependências comuns (sessão, usuário atual, configurações)
  plataforma/
    db.py          engine, SessionLocal, Base
    paginacao.py   log.py   seguranca.py
  reserva/
    __init__.py    (vazio: sem reexportar tudo)
    router.py      APIRouter: rotas finas
    service.py     regra de negócio, transação
    repository.py  consultas SQLAlchemy
    schemas.py     Pydantic: entrada e saída da API
    modelos.py     SQLAlchemy ORM (tabela)
    erros.py       exceções da funcionalidade (opcional)
  sala/   auth/
```

```python
# src/reservas/main.py
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from reservas import auth, reserva, sala
from reservas.config import Settings, get_settings
from reservas.erro import registrar_handlers
from reservas.plataforma import db, log, middlewares


def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or get_settings()
    log.configurar(settings)

    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        db.iniciar(settings)              # cria engine/sessionmaker uma vez
        yield
        db.encerrar()                     # engine.dispose()

    app = FastAPI(title="Reservas", lifespan=lifespan, docs_url=None if settings.producao else "/docs")
    app.state.settings = settings
    registrar_handlers(app)
    middlewares.registrar(app, settings)
    app.include_router(reserva.router, prefix="/api/reservas", tags=["reservas"])
    app.include_router(sala.router, prefix="/api/salas", tags=["salas"])
    app.include_router(auth.router, prefix="/api/auth", tags=["auth"])
    return app
```

- **Application factory** (`create_app`): os testes criam o app com `Settings` de teste e
  `dependency_overrides`; `uvicorn ... --factory` sobe em produção. Evite `app = FastAPI()`
  no escopo do módulo com efeitos colaterais no import.
- Módulo por funcionalidade: tudo de "reserva" fica junto. Evite `models/`, `routers/`,
  `utils/`, `helpers/` globais.
- `__init__.py` das funcionalidades expõe só o que outro pacote usa (`router`); evita
  importação circular. Se aparecer ciclo, a dependência está invertida: extraia o tipo
  comum ou injete por parâmetro.
- Camadas: `router` -> `service` -> `repository`. O `service` não importa `fastapi`; o
  `repository` não conhece HTTP. Módulo com poucas linhas não precisa dos três arquivos.

## Tipagem estrita

```python
from collections.abc import Sequence
from datetime import datetime

def conflitam(a: tuple[datetime, datetime], b: tuple[datetime, datetime]) -> bool:
    return a[0] < b[1] and b[0] < a[1]

def primeira(xs: Sequence[int]) -> int | None:
    return xs[0] if xs else None
```

- `mypy --strict` com `plugins = ["pydantic.mypy"]`; anotar **todas** as funções, inclusive
  testes (`-> None`). `Any` implícito é erro; explícito exige comentário.
- Sintaxe moderna: `list[int]`, `dict[str, int]`, `X | None` (nunca `Optional[X]` nem
  `typing.List`); `collections.abc` para parâmetros (`Sequence`, `Mapping`, `Iterable`).
- `NewType` para identificadores confundíveis (`SalaId = NewType("SalaId", int)`);
  `Literal`/`StrEnum` para conjuntos fechados (`PapelUsuario`, `StatusReserva`).
- `@dataclass(frozen=True, slots=True)` para valores de domínio imutáveis (`Periodo`);
  Pydantic fica na **fronteira** (HTTP/config), dataclass/`NamedTuple` no domínio puro.
- `Protocol` só quando houver duas implementações reais (ex.: `Relogio` real e fixo); sem
  `ABC` por precaução.
- `TypedDict` para dicionário com forma conhecida vinda de fora; nunca `dict[str, Any]`
  circulando pelo domínio.

```python
from dataclasses import dataclass
from datetime import datetime

@dataclass(frozen=True, slots=True)
class Periodo:
    inicio: datetime
    fim: datetime

    def __post_init__(self) -> None:
        if self.inicio.tzinfo is None or self.fim.tzinfo is None:
            raise ValueError("datas devem ter fuso")
        if self.fim <= self.inicio:
            raise ValueError("fim deve ser depois do início")
```

- Valor inválido não existe: o construtor valida (parse, não valide). Na borda HTTP, o
  schema Pydantic traduz o `ValueError` em 400.
- Datas **sempre com fuso** (`AwareDatetime`, `datetime.now(UTC)`); `datetime.utcnow()` e
  datas ingênuas são proibidas (regra `DTZ` do ruff pega).

## Erros de domínio

```python
# src/reservas/erro.py
class ErroDominio(Exception):
    """Base: o handler traduz para Problem Details."""
    status = 400
    tipo = "erro-dominio"
    titulo = "Erro"

    def __init__(self, detalhe: str) -> None:
        super().__init__(detalhe)
        self.detalhe = detalhe

class EntradaInvalida(ErroDominio):
    status, tipo, titulo = 400, "requisicao-invalida", "Requisição inválida"

class NaoEncontrado(ErroDominio):
    status, tipo, titulo = 404, "nao-encontrado", "Não encontrado"

class Conflito(ErroDominio):
    status, tipo, titulo = 409, "conflito", "Conflito"

class SemPermissao(ErroDominio):
    status, tipo, titulo = 403, "proibido", "Sem permissão"
```

- O serviço **levanta** `NaoEncontrado("Reserva 7 não existe")`; um único exception handler
  (`api-fastapi-erros.md`) converte em resposta. O serviço nunca levanta `HTTPException`
  (acoplaria o domínio ao HTTP).
- Hierarquia pequena e plana; uma classe por decisão que o chamador toma (404, 409, 403).
- Não usar exceção para fluxo normal frequente; "não achou" no repositório devolve `None`, e
  o **serviço** decide se vira `NaoEncontrado`.
- Capturar o **específico** (`except IntegrityError`), nunca `except Exception: pass`. Ao
  re-levantar, `raise Conflito(...) from exc` preserva a causa para o log.
- Retorno `X | None` para ausência; nunca valor sentinela (`-1`, `""`).

## Nomes e estilo

| Item | Convenção |
|---|---|
| Módulo, função, variável | `snake_case` |
| Classe, exceção, tipo | `PascalCase` |
| Constante | `MAIUSCULAS_COM_SUBLINHADO` |
| Privado ao módulo | prefixo `_` |
| Teste | `test_<comportamento>` (ex.: `test_sobreposicao_devolve_409`) |

- Identificadores do domínio podem ficar em pt-BR (`reserva`, `sala_id`), como no restante do
  produto; termos de biblioteca ficam em inglês. Não misturar na mesma palavra.
- Docstring curta no contrato público; comentário explica o **porquê**.
- `ruff format` decide o estilo; sem discussão de formatação em revisão.
- Imports absolutos (`from reservas.reserva import service`), ordenados pelo ruff (`I`).
- Sem `print`; sem `import *`; sem estado mutável no escopo do módulo (use `lru_cache` só
  para `get_settings`).
- Argumentos mutáveis como padrão (`def f(x=[])`) são proibidos (`B006`); use `None` ou
  `field(default_factory=list)`.
- Parâmetros booleanos posicionais confundem: forçar nomeados com `*` (`def f(a, *, ativo: bool)`).
- Funções curtas com um propósito; `if` aninhado em excesso vira retorno antecipado.
