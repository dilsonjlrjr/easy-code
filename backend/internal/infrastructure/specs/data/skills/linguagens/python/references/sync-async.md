# Sync x async e o event loop (Python)

Decisão: **síncrono por padrão** (`def`, `Session`, `psycopg`); async por critério medido.
Aqui: o critério, como cada modo funciona no FastAPI e as armadilhas de bloquear o loop.

## Como o FastAPI executa

| Rota | Quem executa | Consequência |
|---|---|---|
| `def` | Thread pool (AnyIO; limite padrão de ~40 threads, conferir) | Pode bloquear (banco síncrono, CPU curta) sem travar as outras requisições |
| `async def` | **Event loop** (uma thread) | Qualquer chamada bloqueante **congela todas** as requisições |

- Dependências seguem a mesma regra (`def` -> thread pool; `async def` -> loop).
- Com `Session` síncrona, `def` é o modo certo: simples, idiomático e sem risco de travar o
  loop. O teto de concorrência é o thread pool (e o pool de conexões do banco).
- Um servidor com poucos workers e `def` atende centenas de requisições por segundo em CRUD
  com PostgreSQL; a latência dominante é o banco, não o Python.

## Critério para trocar para async

Use `async` (e `AsyncSession`) **só se** houver evidência de pelo menos um destes:

1. Muitas requisições concorrentes **esperando E/S externa lenta** (chamadas HTTP a terceiros,
   filas) que esgotam o thread pool.
2. Conexões longas: WebSocket, SSE, streaming.
3. Medição (carga) mostrando o thread pool como gargalo.

Sem isso, async traz custo (todo o caminho precisa ser async, bibliotecas bloqueantes viram
armadilha, depuração mais difícil) sem ganho. "Async é mais rápido" é falso para CRUD
dominado pelo banco. A troca exige ADR.

## Se for async: tudo async no caminho

```python
# src/reservas/plataforma/db_async.py
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

engine = create_async_engine(
    "postgresql+psycopg://usuario:senha@db/reservas",   # psycopg 3 serve aos dois modos
    pool_size=10, max_overflow=0, pool_pre_ping=True,
)
SessionLocal = async_sessionmaker(engine, expire_on_commit=False)


async def get_session() -> AsyncIterator[AsyncSession]:
    async with SessionLocal() as session:
        yield session
```

```python
@router.get("/{reserva_id}", response_model=ReservaResposta)
async def buscar(reserva_id: int, session: AsyncSessionDep, usuario: UsuarioDep) -> ReservaResposta:
    r = await session.scalar(select(Reserva).where(Reserva.id == reserva_id))
    if r is None:
        raise NaoEncontrado(f"Reserva {reserva_id} não existe.")
    return ReservaResposta.model_validate(r)
```

- `psycopg` 3 suporta async com a mesma URL `postgresql+psycopg://`; `asyncpg`
  (`postgresql+asyncpg://`) é alternativa só com ganho medido: muda exceções (o SQLSTATE
  vem de outro atributo) e o tratamento de tipos.
- **`expire_on_commit=False` é obrigatório em async**: depois do `commit`, ler atributo
  expirado dispararia E/S implícita, que falha (`MissingGreenlet`).
- **Sem carregamento preguiçoso** em async: relacionamento não carregado levanta erro. Use
  `selectinload`/`joinedload` explícitos (e `lazy="raise"` no modelo, que vale nos dois modos).
- Mesmos modelos, mesmos schemas, mesmo Alembic (Alembic roda síncrono mesmo com app async:
  use URL síncrona ou o template async do Alembic, conferir).
- Dois engines (sync e async) no mesmo app só se houver motivo; o padrão é um modo só.

## Armadilhas: o que trava o event loop

| Chamada em `async def` | Problema | Faça |
|---|---|---|
| `Session` síncrona, `psycopg.connect` | Bloqueia o loop durante a consulta | `AsyncSession` ou rota `def` |
| `requests.get(...)` | E/S síncrona | `httpx.AsyncClient` |
| `time.sleep(...)` | Para o loop | `await asyncio.sleep(...)` |
| `argon2`/`bcrypt` (hash de senha) | CPU por dezenas/centenas de ms | `await run_in_threadpool(...)` / `anyio.to_thread.run_sync` |
| Ler/gravar arquivo grande, `open()` | E/S síncrona | `anyio.open_file` ou thread |
| `json.dumps` de payload enorme, compressão | CPU | Thread/processo, ou reduzir o payload |
| Loop longo sem `await` | Nunca cede | Dividir, ou mover para thread/worker |

```python
from fastapi.concurrency import run_in_threadpool

async def login(...) -> ...:
    confere = await run_in_threadpool(senha_confere, hash_, senha)   # hash fora do loop
```

- Sintoma de loop bloqueado: latência de **todas** as rotas sobe junto, mesmo as triviais
  (`/saude`), enquanto a CPU de um núcleo fica em 100%. Medir: modo debug do asyncio
  (`PYTHONASYNCIODEBUG=1`) avisa corrotinas lentas; `py-spy` mostra onde a thread está.
- Rota `async def` que **não** faz `await` de nada é sinal de que devia ser `def`.
- Trabalho de CPU realmente pesado (relatórios, imagens): processo/worker separado (fila),
  não thread (o GIL impede paralelismo de CPU em threads Python).

## Concorrência dentro da requisição

- Em `async`: `asyncio.gather` / `asyncio.TaskGroup` (Python 3.11+) para consultas
  **independentes**; `TaskGroup` cancela as irmãs na falha e propaga erro (preferir a
  `gather` solto). Cada tarefa que usa banco precisa da **própria** `AsyncSession` (a sessão
  não é segura para uso concorrente).
- Em sync: normalmente não há concorrência dentro do request; se precisar,
  `concurrent.futures.ThreadPoolExecutor` com sessões separadas. Antes, pergunte se um `JOIN`
  resolve.
- **Sessão nunca é compartilhada entre threads/tarefas**: uma por request (ou por tarefa).
- Tarefa que deve sobreviver ao request (e-mail, relatório): fila em tabela do PostgreSQL com
  worker próprio ou `BackgroundTasks` apenas para trabalho leve e descartável (se o processo
  cair, o trabalho some; **não** use para o que não pode se perder).

## Tarefas em segundo plano e `lifespan`

```python
@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    db.iniciar(settings)
    limpeza = asyncio.create_task(limpar_sessoes_periodicamente())   # guardar a referência!
    try:
        yield
    finally:
        limpeza.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await limpeza
        db.encerrar()
```

- `asyncio.create_task` sem guardar a referência pode ser coletada pelo GC no meio da
  execução: **guarde** e **cancele** no encerramento. Toda tarefa tem dono e fim.
- Em app síncrono, a limpeza de sessões expiradas é melhor como **comando agendado**
  (`python -m reservas.manutencao`, serviço do Compose/cron) do que thread dentro do
  servidor; com vários workers, cada um rodaria a limpeza (idempotente, mas desperdício).
- Mesmo na versão síncrona, o `lifespan` é `async def`: cria/fecha o engine, não faz E/S longa.

## Workers e processos

- Um processo Python usa **um núcleo** para código Python (GIL). Escala com **processos**:
  `uvicorn --workers N` ou N contêineres/réplicas. Cada processo tem o próprio engine e o
  próprio pool: `N x pool_size` conexões no PostgreSQL (dimensionar `max_connections`).
- Estado em memória (cache, contadores) **não** é compartilhado entre workers: estado de
  negócio vive no banco (sessão em tabela, não em dicionário).
- Python com GIL desligado (free-threaded) é experimental/em evolução: não adotar no padrão
  (conferir estado atual antes de qualquer ADR).
