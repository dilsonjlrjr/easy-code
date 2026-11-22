# Testes (Python): pytest, fakes, banco real com rollback e `TestClient`

Estratégia (pirâmide, o que testar em cada nível, rastreio CT -> teste): `estrategia-testes`.
Aqui: o "como em Python". Regra: regra de negócio em teste **sem banco**; constraint, SQL,
transação e HTTP contra **PostgreSQL real**. Sem SQLite nem mock do driver no lugar do banco.

## Níveis

| Nível | Onde | Ferramenta | Banco |
|---|---|---|---|
| Unitário (regra, tipos, schemas) | `tests/unit/` | `pytest` | Não (fakes) |
| Integração do repositório/serviço | `tests/integracao/` | `pytest` + `Session` real | PostgreSQL (testcontainers) |
| Integração HTTP (rota completa) | `tests/integracao/` | `TestClient` | PostgreSQL |
| Concorrência (reservas simultâneas) | `tests/integracao/` | threads + banco real | PostgreSQL |
| Ponta a ponta | Suíte da QA (Compose) | Fora do código Python | Sim |

Marcador `integracao` (em `pyproject.toml`) separa o que precisa de banco:
`pytest -m "not integracao"` roda só os rápidos.

## Unitários: regra pura, em tabela

```python
# tests/unit/test_periodo.py
from datetime import UTC, datetime

import pytest

from reservas.reserva.dominio import Periodo


def t(h: int) -> datetime:
    return datetime(2025, 3, 10, h, tzinfo=UTC)


@pytest.mark.parametrize(
    ("inicio", "fim", "valido"),
    [(t(9), t(10), True), (t(9), t(9), False), (t(10), t(9), False)],
    ids=["fim depois", "fim igual", "fim antes"],
)
def test_periodo_exige_fim_depois_do_inicio(inicio: datetime, fim: datetime, valido: bool) -> None:
    if valido:
        assert Periodo(inicio, fim).fim == fim
    else:
        with pytest.raises(ValueError, match="fim"):
            Periodo(inicio, fim)


def test_schema_recusa_data_sem_fuso_e_campo_desconhecido() -> None:
    from pydantic import ValidationError
    from reservas.reserva.schemas import CriarReserva

    base = {"sala_id": 1, "inicio": "2025-03-10T09:00:00Z", "fim": "2025-03-10T10:00:00Z", "titulo": "A"}
    CriarReserva.model_validate(base)
    with pytest.raises(ValidationError):
        CriarReserva.model_validate({**base, "inicio": "2025-03-10T09:00:00"})   # sem fuso
    with pytest.raises(ValidationError):
        CriarReserva.model_validate({**base, "usuario_id": 99})                  # extra="forbid"
```

- `@pytest.mark.parametrize` com `ids` legíveis = teste em tabela. Um comportamento por teste;
  nome descreve o comportamento (`test_<comportamento>`).
- Relógio e aleatoriedade entram por **parâmetro** (ou `Protocol` `Relogio` com versão fixa),
  nunca `datetime.now()` dentro da regra. Para código legado, `time-machine`/`freezegun`
  (conferir) são o remédio; o ideal é injetar.
- Erro de domínio: `with pytest.raises(Conflito)`; afirmar a **classe**, não o texto.
- `assert` simples do pytest (reescrita de asserção dá a mensagem); sem `self.assertEqual`.
- Teste anotado (`-> None`) para o `mypy --strict`.

## Fakes em vez de mocks

Fronteira externa (e-mail, relógio, HTTP de terceiros): `Protocol` pequena + implementação
falsa que **guarda o que recebeu**.

```python
class RelogioFixo:
    def __init__(self, instante: datetime) -> None:
        self.instante = instante
    def agora(self) -> datetime:
        return self.instante

class EmailFalso:
    def __init__(self) -> None:
        self.enviados: list[tuple[str, str]] = []
    def enviar(self, para: str, assunto: str) -> None:
        self.enviados.append((para, assunto))

def test_cancelamento_notifica_o_dono() -> None:
    email = EmailFalso()
    servico = ServicoCancelamento(email=email, relogio=RelogioFixo(t(8)))
    servico.cancelar(reserva_de(usuario="ana@x.com"))
    assert email.enviados == [("ana@x.com", "Reserva cancelada")]
```

- Fake verifica **estado/resultado**, não "foi chamado 1 vez com X" (isso acopla o teste à
  implementação). `unittest.mock`/`pytest-mock` ficam para o que um fake não resolve.
- Repositório **não** é mockado: usa-se o banco. Serviço com regra pura recebe os dados
  como argumento e é testado sem banco.

## Banco real: testcontainers + Alembic + rollback por teste

```python
# tests/conftest.py
from collections.abc import Iterator

import pytest
from alembic import command
from alembic.config import Config
from sqlalchemy import Engine, create_engine
from sqlalchemy.orm import Session
from testcontainers.postgres import PostgresContainer


@pytest.fixture(scope="session")
def engine() -> Iterator[Engine]:
    with PostgresContainer("postgres:17", driver="psycopg") as pg:       # mesma major da produção
        url = pg.get_connection_url()                                    # postgresql+psycopg://...
        cfg = Config("alembic.ini")
        cfg.set_main_option("sqlalchemy.url", url)
        command.upgrade(cfg, "head")                                     # schema = migrações reais
        eng = create_engine(url)
        yield eng
        eng.dispose()


@pytest.fixture
def session(engine: Engine) -> Iterator[Session]:
    """Cada teste roda numa transação externa que é desfeita no fim."""
    conn = engine.connect()
    trans = conn.begin()
    s = Session(bind=conn, join_transaction_mode="create_savepoint", expire_on_commit=False)
    try:
        yield s
    finally:
        s.close()
        trans.rollback()
        conn.close()
```

- **Um contêiner por execução** (`scope="session"`), schema criado pelas **migrações reais**
  (`alembic upgrade head`): a constraint `EXCLUDE` existe no teste porque está na migração.
  Teste que usa `Base.metadata.create_all` esconde divergência entre modelo e migração.
- **Rollback por teste**: transação externa + `join_transaction_mode="create_savepoint"`
  (SQLAlchemy 2.0): o `session.commit()` do serviço só libera um *savepoint*; no fim, o
  `rollback()` externo apaga tudo. Rápido e isolado, sem recriar banco.
- Em `PostgresContainer`, o parâmetro `driver` e `get_connection_url()` mudaram entre
  versões do `testcontainers` (conferir); o objetivo é uma URL `postgresql+psycopg://`.
- Exige Docker na máquina de teste. Também pode apontar para o PostgreSQL do Compose
  via variável `TEST_DATABASE_URL` (mesmo `session`), útil quando Docker-in-Docker é ruim.
- **Dado de teste**: fábricas simples (`def sala(session, **kw) -> Sala`) com padrões
  válidos; evite fixtures SQL enormes. Cada teste cria o que precisa.
- Testes que **precisam** de commit real (concorrência, visibilidade entre conexões) não
  usam a fixture com rollback: usam o `engine` e limpam com `TRUNCATE ... RESTART IDENTITY
  CASCADE` em fixture própria.

```python
# tests/integracao/test_reserva_repo.py
pytestmark = pytest.mark.integracao

def test_sobreposicao_vira_conflito_pela_constraint(session: Session) -> None:
    sala = criar_sala(session)
    usuario = criar_usuario(session)
    repository.inserir(session, nova_reserva(sala, usuario, t(9), t(10)))
    with pytest.raises(Conflito):
        repository.inserir(session, nova_reserva(sala, usuario, t(9), t(10)))   # constraint decide

def test_horarios_adjacentes_nao_conflitam(session: Session) -> None:
    sala, usuario = criar_sala(session), criar_usuario(session)
    repository.inserir(session, nova_reserva(sala, usuario, t(9), t(10)))
    repository.inserir(session, nova_reserva(sala, usuario, t(10), t(11)))      # [9,10) e [10,11)
```

## Integração HTTP: `TestClient` + `dependency_overrides`

```python
# tests/integracao/conftest.py
from fastapi.testclient import TestClient

@pytest.fixture
def app(session: Session) -> FastAPI:
    app = create_app(Settings.de_teste())
    app.dependency_overrides[get_session] = lambda: session      # mesma sessão (e transação) do teste
    return app

@pytest.fixture
def client(app: FastAPI) -> Iterator[TestClient]:
    with TestClient(app, raise_server_exceptions=False) as c:    # vê o 500 em Problem Details
        yield c

def cookie_de(session: Session, usuario: Usuario) -> dict[str, str]:
    """Sessão criada direto no banco (argon2 é lento; o login tem seus próprios testes)."""
    token, h = novo_token()
    agora = datetime.now(UTC)
    session.add(Sessao(token_hash=h, usuario_id=usuario.id,
                       expira_em=agora + timedelta(minutes=30), expira_max=agora + timedelta(hours=12)))
    session.flush()
    return {"sessao": token}
```

```python
# tests/integracao/test_api_reservas.py
pytestmark = pytest.mark.integracao

def corpo(sala_id: int, h1: int = 9, h2: int = 10) -> dict[str, object]:
    return {"sala_id": sala_id, "inicio": f"2025-03-10T{h1:02d}:00:00Z",
            "fim": f"2025-03-10T{h2:02d}:00:00Z", "titulo": "Daily"}

def test_ct07_reserva_sobreposta_devolve_409_problem_details(client: TestClient, session: Session) -> None:
    sala, ana = criar_sala(session), criar_usuario(session)
    cookies = cookie_de(session, ana)

    r1 = client.post("/api/reservas", json=corpo(sala.id), cookies=cookies)
    assert r1.status_code == 201

    r2 = client.post("/api/reservas", json=corpo(sala.id), cookies=cookies)
    assert r2.status_code == 409
    assert r2.headers["content-type"].startswith("application/problem+json")
    assert r2.json()["status"] == 409

def test_sem_sessao_devolve_401(client: TestClient) -> None:
    assert client.post("/api/reservas", json={}).status_code == 401

def test_fim_antes_do_inicio_devolve_400_com_campo(client: TestClient, session: Session) -> None:
    sala, ana = criar_sala(session), criar_usuario(session)
    r = client.post("/api/reservas", json=corpo(sala.id, 10, 9), cookies=cookie_de(session, ana))
    assert r.status_code == 400
    assert any(e["campo"] == "fim" for e in r.json()["errors"])

def test_usuario_nao_cancela_reserva_de_outro(client: TestClient, session: Session) -> None:
    sala, ana, beto = criar_sala(session), criar_usuario(session), criar_usuario(session)
    rid = client.post("/api/reservas", json=corpo(sala.id), cookies=cookie_de(session, ana)).json()["id"]
    r = client.delete(f"/api/reservas/{rid}", cookies=cookie_de(session, beto))
    assert r.status_code == 404                           # IDOR: 404 (ou 403, conforme o contrato)
```

- Cada teste é um caso de CT; **nomear com o ID do CT** (`test_ct07_...`) liga teste e
  cenário.
- Asserções sobre status **e** forma do erro (`status`, `title`, `errors`, content-type
  `application/problem+json`): o contrato de Problem Details faz parte do `openapi.yaml`.
- `dependency_overrides[get_session]` entrega a **mesma** sessão do teste, então a API enxerga
  os dados criados na fixture e o rollback final limpa tudo. Limpe o override se o `app` for
  compartilhado (`app.dependency_overrides.clear()`).
- O `TestClient` (baseado em `httpx`) executa a app em memória, sem abrir porta; com `with`
  roda o `lifespan`. Para ler cabeçalhos de cookie: `r.headers.get_list("set-cookie")` e
  afirmar `HttpOnly`, `SameSite=lax`, `Path=/`.
- Teste do login faz o fluxo completo (1 ou 2 testes). Acelerar o hash em teste:
  `PasswordHasher(time_cost=1, memory_cost=8, parallelism=1)` via `Settings.de_teste`.

## Concorrência: a constraint decide

```python
# usa engine (commits reais), não a fixture com rollback
def test_duas_reservas_simultaneas_so_uma_vence(engine: Engine, banco_limpo: None) -> None:
    sala_id, usuario_id = semear(engine)

    def tentar(_: int) -> str:
        with Session(engine) as s:
            try:
                repository.inserir(s, Reserva(sala_id=sala_id, usuario_id=usuario_id,
                                              inicio=t(9), fim=t(10), titulo="A"))
                s.commit()
                return "ok"
            except Conflito:
                return "conflito"

    with ThreadPoolExecutor(max_workers=8) as pool:
        resultados = list(pool.map(tentar, range(8)))

    assert sorted(resultados) == ["conflito"] * 7 + ["ok"]
```

- Sem `sleep` para "dar tempo": a sincronização vem do `pool.map`.
- `banco_limpo` é uma fixture com `TRUNCATE ... RESTART IDENTITY CASCADE` antes do teste.

## Execução e cobertura

| Ação | Comando |
|---|---|
| Tudo | `uv run pytest` |
| Só rápidos (sem banco) | `uv run pytest -m "not integracao"` |
| Um teste | `uv run pytest -k sobreposicao -s` |
| Cobertura | `uv run pytest --cov=reservas --cov-report=term-missing` |
| Tipos dos testes | `uv run mypy` (`files = ["src", "tests"]`) |

- `pytest-xdist` (`-n auto`) paraleliza testes unitários; com o banco compartilhado e rollback
  por teste, paralelizar exige um schema/banco por worker (conferir) — comece sem.
- Teste que depende de ordem, de relógio real ou de `sleep` está quebrado: corrigir a causa.
- Cobertura é sinal, não meta: priorize regra e constraints (CT), não linhas.
