# Persistência (Rust): sqlx, transações, modo offline e migrações

Modelagem, índices e constraints: `modelagem-postgresql`. Aqui: acesso a dados com `sqlx`
(0.8 na redação; conferir assinaturas na versão fixada). `sqlx` não é ORM: SQL escrito à mão,
tipos Rust nos parâmetros e nas colunas.

## Pool

```rust
// src/plataforma/db.rs
use sqlx::postgres::{PgConnectOptions, PgPoolOptions};
use std::{str::FromStr, time::Duration};

pub async fn abrir_pool(config: &Config) -> anyhow::Result<sqlx::PgPool> {
    let opcoes = PgConnectOptions::from_str(&config.database_url)
        .context("DATABASE_URL inválida")?
        .options([("statement_timeout", "10000")]);        // 10 s por comando (ms)
    let pool = PgPoolOptions::new()
        .max_connections(10)
        .min_connections(1)
        .acquire_timeout(Duration::from_secs(5))           // espera por conexão livre
        .idle_timeout(Duration::from_secs(300))
        .max_lifetime(Duration::from_secs(1800))
        .connect_with(opcoes)                              // falha cedo se o banco não responde
        .await
        .context("conectar ao PostgreSQL")?;
    Ok(pool)
}
```

- **Um** `PgPool` por processo, criado na `main`, clonado para o `Estado` (clone é barato);
  `pool.close().await` no fim.
- `max_connections` = `max_connections` do PostgreSQL dividido pelo número de instâncias,
  com folga; maior não é melhor.
- URL do ambiente (`DATABASE_URL`), `sslmode` explícito; nunca fixa no código.
- `acquire_timeout` evita requests pendurados quando o pool esgota (vira 500/503, não espera
  infinita); `statement_timeout` evita consulta presa. Confirmar o nome do método de opções
  na versão (`options(...)` do `PgConnectOptions`).

## Macros verificadas em compilação x `query_as` em runtime

| Forma | Verificação | Use quando |
|---|---|---|
| `query!` / `query_as!` / `query_scalar!` | Em compilação, contra o schema real | **Padrão**: SQL fixo |
| `query_as::<_, T>(sql).bind(..)` | Só em runtime | SQL dinâmico (filtros opcionais, `QueryBuilder`) |
| `QueryBuilder` | Runtime, com binds seguros | Listagem com filtros combináveis |

```rust
// src/reserva/repository.rs
pub struct ReservaLinha {
    pub id: i64,
    pub sala_id: i64,
    pub usuario_id: i64,
    pub inicio: DateTime<Utc>,
    pub fim: DateTime<Utc>,
    pub titulo: String,
}

pub async fn buscar(pool: &PgPool, id: i64) -> Result<Option<ReservaLinha>, sqlx::Error> {
    sqlx::query_as!(
        ReservaLinha,
        r#"SELECT id, sala_id, usuario_id, inicio, fim, titulo FROM reservas WHERE id = $1"#,
        id
    )
    .fetch_optional(pool)       // Ok(None) se não houver linha; o serviço decide o 404
    .await
}

pub async fn inserir(pool: &PgPool, n: &NovaReserva, usuario_id: i64) -> Result<ReservaLinha, ReservaErro> {
    sqlx::query_as!(
        ReservaLinha,
        r#"INSERT INTO reservas (sala_id, usuario_id, inicio, fim, titulo)
           VALUES ($1, $2, $3, $4, $5)
           RETURNING id, sala_id, usuario_id, inicio, fim, titulo"#,
        n.sala.0, usuario_id, n.periodo.inicio(), n.periodo.fim(), n.titulo.as_str()
    )
    .fetch_one(pool)
    .await
    .map_err(traduzir)          // 23P01 -> Conflito; 23503 -> Invalida
}
```

- A macro conecta ao banco **em tempo de compilação** (via `DATABASE_URL`) ou usa o cache
  offline. Erro de coluna, tipo ou nome vira **erro de compilação**: a maior vantagem do `sqlx`.
- Nulabilidade vem do schema: coluna `NOT NULL` -> tipo sem `Option`. Em expressões o `sqlx`
  não infere: force com `AS "campo!"` (não nulo) ou `AS "campo?"` (anulável). Colunas de
  `LEFT JOIN` costumam virar `Option`.
- `fetch_one` -> erro se não houver linha (`RowNotFound`); `fetch_optional` -> `Option`;
  `fetch_all` -> `Vec` (limitar com `LIMIT`); `fetch` -> stream para volumes grandes.
- Parâmetros sempre `$1, $2...`; nunca `format!` com entrada. Nome de coluna/tabela dinâmico
  vem de **lista fixa** no código, nunca do cliente.
- Newtype com `#[sqlx(transparent)]` pode ir direto no bind; nas macros, a conversão exige
  anotação de tipo (`AS "sala_id: SalaId"`) (conferir sintaxe de overrides).
- Tipos de data: feature `chrono` (`DateTime<Utc>` <-> `timestamptz`). Alternativa: feature
  `time` com `OffsetDateTime`. Usar uma só no projeto; `timestamptz` sempre.

## Modo offline (`.sqlx`)

Sem banco disponível no build (CI, Docker), as macros leem o cache versionado:

```bash
# com o banco migrado e DATABASE_URL apontando para ele
cargo sqlx prepare                    # gera/atualiza a pasta .sqlx/ (commitar)
cargo sqlx prepare --check            # CI: falha se o cache estiver desatualizado
SQLX_OFFLINE=true cargo build         # build sem banco (Docker/CI)
```

- `sqlx-cli` é instalado à parte (`cargo install sqlx-cli --no-default-features --features rustls,postgres`; conferir flags).
- Rodar `prepare` **toda vez** que mudar consulta ou migração; `--check` no CI pega o
  esquecimento. Sem `.sqlx/` e sem banco, o build falha com erro da macro.
- O `.env` com `DATABASE_URL` serve só ao desenvolvimento local (não vai para a imagem).
- Alternativa se o time rejeitar o cache: usar apenas `query_as` em runtime (perde a
  verificação em compilação; compensa-se com testes de integração mais completos).

## Transações

```rust
pub async fn transferir(pool: &PgPool, reserva_id: i64, nova_sala: SalaId) -> Result<ReservaLinha, ReservaErro> {
    let mut tx = pool.begin().await?;                         // rollback automático no Drop

    let atual = sqlx::query_as!(
        ReservaLinha,
        r#"SELECT id, sala_id, usuario_id, inicio, fim, titulo
             FROM reservas WHERE id = $1 FOR UPDATE"#,         // trava a linha
        reserva_id
    )
    .fetch_optional(&mut *tx)
    .await?
    .ok_or(ReservaErro::NaoEncontrada)?;

    let atualizada = sqlx::query_as!(
        ReservaLinha,
        r#"UPDATE reservas SET sala_id = $2 WHERE id = $1
           RETURNING id, sala_id, usuario_id, inicio, fim, titulo"#,
        atual.id, nova_sala.0
    )
    .fetch_one(&mut *tx)
    .await
    .map_err(traduzir)?;                                       // 23P01 -> Conflito (tx desfeita no Drop)

    sqlx::query!("INSERT INTO auditoria (reserva_id, acao) VALUES ($1, 'transferida')", atual.id)
        .execute(&mut *tx)
        .await?;

    tx.commit().await?;
    Ok(atualizada)
}
```

- `pool.begin()` devolve `Transaction`; **sem `commit()` explícito, o `Drop` faz rollback**.
  O `?` em qualquer ponto desfaz tudo: não precisa de `defer`.
- Executar na transação com `&mut *tx` (reborrow do `Transaction` como conexão). Misturar
  `pool` e `tx` dentro da mesma operação quebra a atomicidade.
- Transação **curta**: sem chamada de rede, sem `sleep`, sem hash de senha lá dentro.
- Funções do repositório que participam de transação recebem
  `&mut PgConnection` (ou `impl sqlx::Executor<'_, Database = Postgres>`) em vez de `&PgPool`,
  para serem reutilizáveis nos dois contextos. Comece com `&PgPool` e generalize quando uma
  transação real exigir.
- Isolamento padrão (`READ COMMITTED`) basta quando a invariante é constraint. Para regra
  "ler-decidir-escrever" que a constraint não cobre: `SELECT ... FOR UPDATE` na linha pai,
  ou `SERIALIZABLE` com **retentativa** em `40001` (serialization_failure) e `40P01`
  (deadlock). Preferir sempre a constraint.
- Concorrência de reserva: a `EXCLUDE USING gist (sala_id WITH =, tstzrange(inicio, fim) WITH &&)`
  garante; o `INSERT` concorrente que perde recebe `23P01`; a aplicação devolve 409.

## Tradução de erro no repositório

```rust
fn traduzir(e: sqlx::Error) -> ReservaErro {
    match pg::violacao(&e) {
        Some((Violacao::Exclusao, _)) => ReservaErro::Conflito,
        Some((Violacao::ChaveEstrangeira, _)) => ReservaErro::Invalida("sala inexistente".into()),
        Some((Violacao::Unicidade, _)) => ReservaErro::Conflito,
        None => ReservaErro::Banco(e),
    }
}
```

`pg::violacao` está em `api-axum-erros.md`. `SQLSTATE`: exclusion `23P01`, unique `23505`,
FK `23503`.

## Evitar N+1

- Em vez de buscar a sala dentro de um laço, junte (`JOIN`) ou busque em lote com
  `WHERE id = ANY($1)`:

```rust
let salas = sqlx::query_as!(Sala, "SELECT id, nome FROM salas WHERE id = ANY($1)", &ids[..])
    .fetch_all(pool).await?;
```

- `&ids[..]` (slice de `i64`) mapeia para `bigint[]`. Montar `HashMap<i64, Sala>` para
  consultar em memória.
- Consulta dentro de `for` com `.await` é o sintoma. Medir com `EXPLAIN (ANALYZE)` quando
  houver dúvida (índices: `modelagem-postgresql`).

## Migrações

```
migrations/
  20250101120000_criar_salas.sql
  20250101120100_criar_reservas.sql          -- EXCLUDE (precisa de btree_gist)
```

```sql
-- 20250101120100_criar_reservas.sql
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE TABLE reservas (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sala_id    bigint      NOT NULL REFERENCES salas(id),
  usuario_id bigint      NOT NULL REFERENCES usuarios(id),
  inicio     timestamptz NOT NULL,
  fim        timestamptz NOT NULL,
  titulo     text        NOT NULL,
  CONSTRAINT reservas_periodo_valido CHECK (fim > inicio),
  CONSTRAINT reservas_sem_sobreposicao
    EXCLUDE USING gist (sala_id WITH =, tstzrange(inicio, fim) WITH &&)
);
CREATE INDEX reservas_usuario_id_idx ON reservas (usuario_id);
```

- `sqlx migrate add -r nome` cria o par reversível (`.up.sql`/`.down.sql`); sem `-r`, só
  avanço. Nunca editar migração já aplicada (o `sqlx` confere o checksum e recusa): criar
  outra.
- `sqlx::migrate!()` **embute** os arquivos no binário em compilação (caminho padrão
  `./migrations`); `.run(&pool)` aplica as pendentes na subida. Com várias instâncias, o
  `sqlx` usa advisory lock para não rodar em paralelo. Alternativa: rodar `sqlx migrate run`
  como etapa do deploy (serviço `migrar` no Compose).
- Mudou a pasta `migrations/` e a macro não percebeu: forçar recompilação (`build.rs` com
  `println!("cargo:rerun-if-changed=migrations");`).
- Mudança destrutiva em duas fases (expandir e contrair): ver `modelagem-postgresql`.

## Alternativas (quando trocar)

| Alternativa | Observação |
|---|---|
| `diesel` | DSL tipada (`reservas::table.filter(...)`), schema gerado em `schema.rs`; síncrono, async por `diesel-async`. Boa se o time quer DSL; constraints e SQL ficam menos visíveis |
| `sea-orm` | Entidades e relacionamentos gerados; construído sobre `sqlx`. Reduz boilerplate de CRUD; esconde o SQL; só com ADR |
| `tokio-postgres` / `deadpool-postgres` | Driver baixo nível; mais código manual, sem macros verificadas |

Em qualquer escolha, o contrato não muda: constraints no banco, tradução de `SQLSTATE` em 409,
transação curta, migrações versionadas.
