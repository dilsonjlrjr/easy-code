# Persistência (Python): SQLAlchemy 2, Unit of Work e Alembic

Modelagem, índices e constraints: `modelagem-postgresql`. Aqui: acesso a dados com
SQLAlchemy 2 (estilo 2.0) + `psycopg` 3, e migrações com Alembic. Conferir assinaturas na
versão fixada. Estilo 2.0 = `Mapped`/`mapped_column`, `select()` e `session.scalars()`; **não**
use o estilo legado `session.query(...)`.

## Engine e `Session`

```python
# src/reservas/plataforma/db.py
from sqlalchemy import Engine, create_engine, event
from sqlalchemy.orm import DeclarativeBase, Session, sessionmaker

from reservas.config import Settings


class Base(DeclarativeBase):
    pass


_engine: Engine | None = None
SessionLocal: sessionmaker[Session]


def iniciar(settings: Settings) -> None:
    global _engine, SessionLocal                               # uma vez, no lifespan
    _engine = create_engine(
        settings.database_url,                                 # postgresql+psycopg://...
        pool_size=10, max_overflow=0,
        pool_pre_ping=True, pool_recycle=1800,
        connect_args={"options": "-c statement_timeout=10000"},   # 10 s por comando
    )
    SessionLocal = sessionmaker(_engine, expire_on_commit=False)


def encerrar() -> None:
    if _engine is not None:
        _engine.dispose()
```

- **Um** `Engine` por processo (ele já é o pool de conexões); nunca crie engine por request.
- `pool_size` x número de workers <= `max_connections` do PostgreSQL, com folga.
  `pool_pre_ping` descarta conexão morta; `pool_recycle` evita conexão velha demais.
- URL `postgresql+psycopg://...` (psycopg 3). `postgresql://` sozinho escolhe o driver
  antigo (`psycopg2`): erro comum.
- `statement_timeout` evita consulta presa; `connect_args` repassa ao `psycopg` (conferir).
- **`expire_on_commit=False`**: depois do `commit` os objetos continuam legíveis (sem
  consulta extra, e obrigatório em async). Contrapartida: o objeto pode ficar defasado; como
  a `Session` vive só um request, é aceitável.
- A `Session` **não é thread-safe**: uma por request/tarefa, entregue por `Depends`.

## Modelos

```python
# src/reservas/reserva/modelos.py
from datetime import datetime

from sqlalchemy import BigInteger, CheckConstraint, DateTime, ForeignKey, Identity, Text
from sqlalchemy.orm import Mapped, mapped_column, relationship

from reservas.plataforma.db import Base


class Reserva(Base):
    __tablename__ = "reservas"
    __table_args__ = (
        CheckConstraint("fim > inicio", name="reservas_periodo_valido"),
        # EXCLUDE reservas_sem_sobreposicao: criada na migração (SQL manual); ver abaixo
    )

    id: Mapped[int] = mapped_column(BigInteger, Identity(always=True), primary_key=True)
    sala_id: Mapped[int] = mapped_column(ForeignKey("salas.id"), index=False)   # índice: migração
    usuario_id: Mapped[int] = mapped_column(ForeignKey("usuarios.id"))
    inicio: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    fim: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    titulo: Mapped[str] = mapped_column(Text)

    sala: Mapped["Sala"] = relationship(lazy="raise")          # acesso preguiçoso acidental = erro
```

- `Mapped[...]` dá tipos ao `mypy`; `Mapped[str | None]` vira coluna anulável. `DateTime(timezone=True)`
  = `timestamptz`, sempre. `Identity` gera `GENERATED ... AS IDENTITY`.
- **`lazy="raise"`** em todo relacionamento: acessar sem carregar explicitamente levanta
  erro em vez de disparar consulta escondida (N+1). Carregue com `selectinload`/`joinedload`.
- Constraints que o ORM entende (`CHECK`, `UNIQUE`, FK) ficam no modelo **e** na migração;
  `EXCLUDE` e índices parciais ficam **na migração** (abaixo), porque o autogenerate não os
  entende bem (conferir). O modelo documenta com comentário.
- Modelos **não** são schemas da API (`api-fastapi-erros.md`): nada de `response_model` com
  objeto ORM.
- Objeto do domínio puro (`Periodo`) não precisa herdar de `Base`; converta na fronteira do
  repositório quando a regra for complexa. Para CRUD simples, o modelo ORM serve ao serviço.

## Consultas (repositório)

```python
# src/reservas/reserva/repository.py
from collections.abc import Sequence

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload


def buscar(session: Session, reserva_id: int) -> Reserva | None:
    return session.get(Reserva, reserva_id)                  # None se não existe; serviço decide o 404


def listar_por_sala(session: Session, sala_id: int, de: datetime, ate: datetime) -> Sequence[Reserva]:
    stmt = (
        select(Reserva)
        .where(Reserva.sala_id == sala_id, Reserva.inicio < ate, Reserva.fim > de)
        .options(selectinload(Reserva.sala))                 # evita N+1 se a resposta usar a sala
        .order_by(Reserva.inicio, Reserva.id)
    )
    return session.scalars(stmt).all()
```

- `select()` + `session.scalars(stmt).all()/.first()/.one()`. `session.get(Modelo, pk)` para
  busca por chave. `.one()` levanta se não houver exatamente uma linha; `.first()` devolve
  `None`.
- SQL **sempre parametrizado**: o SQLAlchemy faz isso com `where(col == valor)`. SQL cru só
  via `text("... :p")` + `bindparams`/dicionário; **nunca** f-string com entrada.
  Nome de coluna/ordenação dinâmicos vêm de **lista fixa** (`{"inicio": Reserva.inicio}[campo]`).
- `Sequence`/`list`: limitar com `.limit()`; nunca `.all()` em tabela sem teto.
- Repositório não faz `commit`; faz `add`/`flush` e consultas. O serviço decide a transação.

## Transações: Unit of Work com a `Session`

A `Session` **é** a Unit of Work: acumula mudanças e as grava em **uma** transação no
`commit()`.

```python
# src/reservas/reserva/service.py
def criar(session: Session, usuario: UsuarioAtual, dados: CriarReserva) -> Reserva:
    sala = session.get(Sala, dados.sala_id)
    if sala is None or not sala.ativa:
        raise EntradaInvalida("Sala inexistente ou inativa.")
    reserva = Reserva(
        sala_id=sala.id, usuario_id=usuario.id,
        inicio=dados.inicio, fim=dados.fim, titulo=dados.titulo,
    )
    repository.inserir(session, reserva)            # flush: EXCLUDE dispara aqui -> Conflito
    session.add(Auditoria(reserva_id=reserva.id, acao="criada"))
    session.commit()                                # uma transação: reserva + auditoria
    return reserva


def transferir(session: Session, usuario: UsuarioAtual, reserva_id: int, nova_sala: int) -> Reserva:
    reserva = session.scalars(
        select(Reserva).where(Reserva.id == reserva_id).with_for_update()   # trava a linha
    ).first()
    if reserva is None:
        raise NaoEncontrado(f"Reserva {reserva_id} não existe.")
    reserva.sala_id = nova_sala                       # dirty tracking: UPDATE no flush
    repository.confirmar(session)                     # flush + traduz IntegrityError (EXCLUDE) -> Conflito
    session.commit()
    return reserva
```

- **Uma `Session` por request, um `commit` no serviço.** Se qualquer coisa levantar
  exceção antes do `commit`, o `close()` da dependência desfaz a transação (nada
  parcial). Não chame `commit` em cada repositório.
- Autobegin: a transação começa na primeira operação; `session.begin()` explícito é opcional.
  `with session.begin():` serve quando quiser commit/rollback automáticos no bloco.
- Transação **curta**: sem chamada de rede, sem `sleep`, sem hash de senha dentro.
- Após `IntegrityError`, **`rollback()`** antes de qualquer outra operação (ver tradução em
  `api-fastapi-erros.md`).
- Isolamento padrão (`READ COMMITTED`) basta quando a invariante é constraint. Para "ler-
  decidir-escrever" que a constraint não cobre: `with_for_update()` na linha pai, ou
  `SERIALIZABLE` com **retentativa** em `40001`/`40P01`. Preferir sempre a constraint.
- Concorrência de reserva: a `EXCLUDE` garante; o `INSERT` concorrente que perde recebe
  `23P01` (traduzido em 409).
- Atualização em massa sem carregar objetos: `session.execute(update(Reserva).where(...).values(...))`
  (SQL direto, ignora eventos do ORM; usar quando for deliberado).

## N+1 e carregamento

| Estratégia | Quando |
|---|---|
| `selectinload(Rel)` | Coleções e muitos-para-um em listas: uma consulta extra com `IN (...)` (padrão) |
| `joinedload(Rel)` | Muitos-para-um simples (`sala` de uma `reserva`): um `JOIN` na mesma consulta |
| `lazy="raise"` no modelo | Sempre: torna o N+1 um erro visível em teste |
| Consulta por colunas (`select(Reserva.id, Sala.nome)`) | Listagem leve, sem hidratar objetos |

- Sintoma de N+1: `for r in reservas: r.sala.nome` com consulta por iteração. Com
  `lazy="raise"` o teste falha na hora; corrija com `selectinload`.
- Para detectar em teste: `event.listen(engine, "before_cursor_execute", contador)` e afirmar
  o número de consultas de um endpoint de lista.
- Paginação com `joinedload` em coleção duplica linhas; use `selectinload` com `limit`.

## Alembic

```
migrations/
  env.py
  versions/
    20250101_1200_criar_salas.py
    20250101_1210_criar_reservas.py
alembic.ini
```

```python
# migrations/env.py (trechos)
from reservas.config import get_settings
from reservas.plataforma.db import Base
import reservas.modelos  # noqa: F401  # importa todos os modelos para registrar em Base.metadata

config.set_main_option("sqlalchemy.url", get_settings().database_url)   # URL do ambiente, nunca no .ini
target_metadata = Base.metadata
```

- A URL vem da `Settings` (ambiente); `alembic.ini` não guarda segredo.
- `reservas/modelos.py` importa todos os módulos de modelo; sem isso o autogenerate "não vê"
  tabela alguma e propõe apagar tudo.
- Nome de arquivo com data/hora (`file_template` no `alembic.ini`) facilita ordenar.

### Autogenerate: gerar e **revisar à mão**

```bash
uv run alembic revision --autogenerate -m "criar reservas"
# LEIA o arquivo gerado antes de aplicar
uv run alembic upgrade head
uv run alembic downgrade -1     # confirma que o downgrade funciona
```

O autogenerate **não** detecta ou erra em: `EXCLUDE`, índices parciais/de expressão,
renomeação (vira drop+add: perda de dado), alteração de `CHECK`/`server_default`,
`CREATE EXTENSION`, tipos enum, triggers. Sempre revisar; nunca confiar às cegas.

```python
# versions/20250101_1210_criar_reservas.py
from alembic import op
import sqlalchemy as sa

def upgrade() -> None:
    op.execute("CREATE EXTENSION IF NOT EXISTS btree_gist")
    op.create_table(
        "reservas",
        sa.Column("id", sa.BigInteger, sa.Identity(always=True), primary_key=True),
        sa.Column("sala_id", sa.BigInteger, sa.ForeignKey("salas.id"), nullable=False),
        sa.Column("usuario_id", sa.BigInteger, sa.ForeignKey("usuarios.id"), nullable=False),
        sa.Column("inicio", sa.DateTime(timezone=True), nullable=False),
        sa.Column("fim", sa.DateTime(timezone=True), nullable=False),
        sa.Column("titulo", sa.Text, nullable=False),
        sa.CheckConstraint("fim > inicio", name="reservas_periodo_valido"),
    )
    op.create_index("reservas_usuario_id_idx", "reservas", ["usuario_id"])
    op.create_index("reservas_sala_id_idx", "reservas", ["sala_id"])         # toda FK com índice
    op.execute("""
        ALTER TABLE reservas ADD CONSTRAINT reservas_sem_sobreposicao
        EXCLUDE USING gist (sala_id WITH =, tstzrange(inicio, fim) WITH &&)
    """)

def downgrade() -> None:
    op.drop_table("reservas")          # a EXCLUDE e os índices caem junto
```

- **Constraints como `EXCLUDE` são escritas à mão** (`op.execute`), com nome explícito
  (o nome aparece no erro e no `constraint_name` da tradução). A mesma regra vale para
  índice parcial, extensão e `CHECK` complexo.
- `tstzrange(inicio, fim)` é `[inicio, fim)`: reservas adjacentes não conflitam.
- Nunca editar migração já aplicada em outro ambiente: criar outra. Migração que altera
  dado e schema juntos: separar, ou usar `op.execute` com cuidado e testar com dados.
- Mudança destrutiva em duas fases (expandir e contrair): ver `modelagem-postgresql`.
- O teste de integração roda `alembic upgrade head` contra um banco vazio: valida que a
  cadeia de migrações produz o schema que o código espera (`testes-python.md`).
- Migração aplicada por etapa **separada** do deploy da API (serviço `migrar` no Compose:
  `alembic upgrade head`), nunca por cada réplica ao subir (corrida entre instâncias).
- Alembic é síncrono; com app async use a URL síncrona no `env.py` (ou o template async,
  conferir).
