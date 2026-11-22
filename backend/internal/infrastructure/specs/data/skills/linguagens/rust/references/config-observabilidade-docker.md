# Configuração, observabilidade e Docker (Rust)

Ambiente e Compose (serviços, healthcheck, volumes, rede): `ambiente-docker`. Aqui: o lado
Rust — config por ambiente, `tracing`, healthcheck e imagem.

## Configuração por ambiente

Variáveis de ambiente lidas **uma vez** na `main`, validadas, em uma `struct` imutável.
Nada de `std::env::var` espalhado pelo código.

```rust
// src/config.rs
use std::{net::SocketAddr, time::Duration};

#[derive(Debug, Clone)]
pub struct SessaoCfg { pub ttl_idle: chrono::Duration, pub ttl_max: chrono::Duration }

#[derive(Debug, Clone)]
pub struct Config {
    pub endereco: SocketAddr,
    pub database_url: String,
    pub cookie_seguro: bool,
    pub origem_permitida: String,
    pub log_json: bool,
    pub sessao: SessaoCfg,
}

#[derive(Debug, thiserror::Error)]
pub enum ConfigErro {
    #[error("variável {0} ausente")]
    Ausente(&'static str),
    #[error("variável {0} inválida: {1}")]
    Invalida(&'static str, String),
}

fn obrigatoria(nome: &'static str) -> Result<String, ConfigErro> {
    std::env::var(nome).ok().filter(|v| !v.is_empty()).ok_or(ConfigErro::Ausente(nome))
}
fn opcional<T: std::str::FromStr>(nome: &'static str, padrao: T) -> Result<T, ConfigErro>
where T::Err: std::fmt::Display {
    match std::env::var(nome) {
        Ok(v) if !v.is_empty() => v.parse().map_err(|e: T::Err| ConfigErro::Invalida(nome, e.to_string())),
        _ => Ok(padrao),
    }
}

impl Config {
    pub fn do_ambiente() -> Result<Self, ConfigErro> {
        Ok(Self {
            endereco: opcional("LISTEN_ADDR", "0.0.0.0:8080".parse().expect("literal válido"))?,
            database_url: obrigatoria("DATABASE_URL")?,
            cookie_seguro: opcional("COOKIE_SEGURO", true)?,      // seguro por padrão
            origem_permitida: obrigatoria("ORIGEM_PERMITIDA")?,
            log_json: opcional("LOG_JSON", true)?,
            sessao: SessaoCfg {
                ttl_idle: chrono::Duration::minutes(opcional("SESSAO_IDLE_MIN", 30)?),
                ttl_max: chrono::Duration::hours(opcional("SESSAO_MAX_H", 12)?),
            },
        })
    }
    pub fn de_teste() -> Self {
        Self {
            endereco: SocketAddr::from(([127, 0, 0, 1], 0)),
            database_url: String::new(),               // o pool de teste vem do sqlx::test
            cookie_seguro: false,
            origem_permitida: "http://localhost".into(),
            log_json: false,
            sessao: SessaoCfg { ttl_idle: chrono::Duration::minutes(30), ttl_max: chrono::Duration::hours(12) },
        }
    }
}
```

- Falha **cedo e clara** na subida (variável faltando) em vez de falha tardia em runtime.
- Padrão seguro: `COOKIE_SEGURO=true` por padrão; desligar só no `.env` de desenvolvimento local.
- `.env` só em desenvolvimento (carregado pelo Compose ou por `dotenvy` **apenas** em
  `debug`); produção injeta variáveis. `.env` no `.gitignore`; só `.env.example` versionado.
- Segredo (`DATABASE_URL`, chave) nunca em código, imagem ou log. `Debug` da `Config` **não**
  deve imprimir segredo: implementar `Debug` à mão ou envolver em tipo `Segredo(String)` com
  `Debug` mascarado.
- Crates para config (`config`, `figment`, `envy`, `clap` com `env`) são opcionais
  (conferir); a `struct` acima cobre o caso comum sem dependência.
- O `expect` do literal de `LISTEN_ADDR` é invariante de constante válida; qualquer valor
  vindo do ambiente propaga `Result`.

## Observabilidade com `tracing`

```rust
// src/plataforma/log.rs
use tracing_subscriber::{fmt, layer::SubscriberExt, util::SubscriberInitExt, EnvFilter};

pub fn iniciar_tracing(config: &Config) {
    let filtro = EnvFilter::try_from_default_env()               // RUST_LOG
        .unwrap_or_else(|_| EnvFilter::new("info,sqlx=warn,tower_http=info"));
    let registro = tracing_subscriber::registry().with(filtro);
    if config.log_json {
        registro.with(fmt::layer().json().with_current_span(true).with_target(false)).init();
    } else {
        registro.with(fmt::layer().pretty()).init();            // legível em desenvolvimento
    }
}
```

- JSON no **stdout** (o Docker coleta); um evento por linha. Nível por `RUST_LOG`
  (`info` em produção; `debug` pontual).
- O span do request (em `TraceLayer`, ver `middlewares-paginacao.md`) carrega `request_id`,
  método e caminho; **todo** evento dentro dele herda esses campos. Isso liga erro 500 ao
  request.
- Campos estruturados, não texto montado: `tracing::info!(reserva_id = r.id, sala_id = r.sala_id, "reserva criada")`.
- `#[tracing::instrument(skip(pool, senha), fields(usuario_id = %u.id), err)]` abre um span
  por função e registra o erro retornado; **`skip` obrigatório** para pool, senha, token e
  qualquer dado sensível (por padrão `instrument` registra os argumentos com `Debug`).
- Níveis: `error` falha inesperada (500), `warn` condição anormal tratada (login com
  falha repetida), `info` marcos de negócio, `debug` detalhe. Erro de cliente (400/409) não é
  `error`.
- Nunca logar senha, token de sessão, `senha_hash`, `DATABASE_URL`, corpo inteiro de login.
- Métricas e traces distribuídos (OpenTelemetry: `tracing-opentelemetry`,
  `axum-prometheus`/`metrics`) entram quando a Arquitetura pedir (conferir). Em aplicação pequena, logs
  + healthcheck bastam.

## Healthcheck

```rust
// rotas públicas, fora de /api e sem autenticação
async fn vivo() -> StatusCode { StatusCode::OK }                    // /saude/vivo: o processo responde

async fn pronto(State(e): State<Estado>) -> StatusCode {            // /saude/pronto: banco responde
    match tokio::time::timeout(Duration::from_secs(2), sqlx::query("SELECT 1").execute(&e.pool)).await {
        Ok(Ok(_)) => StatusCode::OK,
        _ => StatusCode::SERVICE_UNAVAILABLE,
    }
}
```

- `vivo` não toca o banco (reiniciar o processo por causa do banco fora não resolve);
  `pronto` confere dependências. O `healthcheck` do Compose usa `pronto` (ou `vivo` +
  `depends_on` com `condition: service_healthy` no banco).
- Imagem mínima sem `curl`: faça o binário aceitar um subcomando `--healthcheck` (faz a
  requisição a `127.0.0.1` e sai com 0/1) e use-o no `HEALTHCHECK` / `healthcheck.test`.

## Docker: multi-stage com `cargo-chef`

```dockerfile
# syntax=docker/dockerfile:1
FROM rust:slim AS base                       # fixar a versão em agentes/ambiente.md (ex.: rust:<versão>-slim)
RUN cargo install cargo-chef --locked
WORKDIR /app

FROM base AS planner
COPY . .
RUN cargo chef prepare --recipe-path recipe.json        # lista só as dependências

FROM base AS builder
COPY --from=planner /app/recipe.json recipe.json
ENV SQLX_OFFLINE=true
RUN cargo chef cook --release --recipe-path recipe.json # compila dependências (camada cacheável)
COPY . .                                                # inclui .sqlx/ e migrations/
RUN cargo build --release --locked --bin reservas

FROM gcr.io/distroless/cc-debian12:nonroot AS final     # glibc, sem shell; usuário não root (conferir tag atual)
COPY --from=builder /app/target/release/reservas /usr/local/bin/reservas
ENV LISTEN_ADDR=0.0.0.0:8080 RUST_LOG=info
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/usr/local/bin/reservas"]
```

- **`cargo-chef`** separa a compilação das dependências da do código: mudar uma linha do
  app não recompila o mundo. `planner` -> `cook` (cacheado) -> `build`.
- **`SQLX_OFFLINE=true`** + `.sqlx/` versionado: o build não precisa de banco. Sem o cache,
  as macros `query!` falham. Conferir `cargo sqlx prepare --check` no CI antes do `docker build`.
- Migrações vão **embutidas** no binário (`sqlx::migrate!()`): a imagem final tem só o executável.
- Imagem final: `distroless/cc` (glibc, ~20 MB, sem shell nem gerenciador de pacotes) ou
  `debian:<versão>-slim` se precisar de ferramentas. `scratch` exige alvo `musl` estático
  (`x86_64-unknown-linux-musl`), cabível porque `sqlx` usa `rustls` (sem OpenSSL), mas
  cuidado com alocador e DNS em `musl`; distroless é o ponto de partida mais seguro.
- Com `sqlx` e `tls-rustls`, não é preciso `libssl`. Se alguma dependência exigir OpenSSL,
  instale o runtime na imagem final.
- **Usuário não root** (`nonroot`), sem `--privileged`, sistema de arquivos de leitura
  quando possível (`read_only: true` no Compose).
- `.dockerignore`: `target/`, `.git/`, `.env*`, `tests/` se não forem usados no build —
  mas **não** ignorar `.sqlx/` nem `migrations/`.
- Perfil de release: `[profile.release] lto = "thin"`, `codegen-units = 1`, `strip = true`
  reduzem o binário (conferir ganho x tempo de build). `panic = "abort"` desativa o
  `CatchPanicLayer`: não usar junto.
- Tags **fixadas** (não `latest`); varredura de imagem (`trivy`, open source) no CI.

## Compose (trecho)

```yaml
services:
  api:
    build: ./backend
    environment:
      DATABASE_URL: postgres://reservas:${DB_SENHA}@db:5432/reservas
      ORIGEM_PERMITIDA: http://localhost:5173
      COOKIE_SEGURO: "false"           # só desenvolvimento local (HTTP)
    depends_on:
      db: { condition: service_healthy }
    stop_grace_period: 20s             # >= tempo de graceful shutdown
    read_only: true
```

## Lint e CI (resumo)

| Etapa | Comando |
|---|---|
| Formato | `cargo fmt --check` |
| Lint | `cargo clippy --all-targets -- -D warnings` |
| Cache SQL | `cargo sqlx prepare --check` |
| Testes | `cargo test` (com PostgreSQL) |
| Dependências | `cargo audit`; `cargo deny check` |
| Imagem | `docker build`; `trivy image` (conferir) |
