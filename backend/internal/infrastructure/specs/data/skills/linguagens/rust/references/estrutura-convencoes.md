# Estrutura e convenções (Rust)

Princípios de arquitetura (camadas, quando separar): `padroes-arquitetura-codigo`. Qualidade
geral: `qualidade-codigo`. Aqui: como isso fica em um crate Rust.

## Crate: lib + bin

Um único crate com `src/lib.rs` (tudo que importa) e `src/main.rs` (só composição). Os testes
de integração em `tests/` usam a lib como um cliente externo. Workspace com vários crates só
quando houver motivo real (tempo de compilação, binários diferentes); começar com um.

```toml
# Cargo.toml
[package]
name = "reservas"
version = "0.1.0"
edition = "2024"            # edição atual estável (conferir)
rust-version = "1.85"       # mínimo suportado (conferir na toolchain fixada)

[dependencies]
axum = { version = "0.8", features = ["macros"] }          # conferir versão
tokio = { version = "1", features = ["rt-multi-thread", "macros", "signal", "net", "time"] }
tower = "0.5"
tower-http = { version = "0.6", features = ["trace", "timeout", "cors", "limit", "request-id", "catch-panic"] }
sqlx = { version = "0.8", default-features = false, features = ["runtime-tokio", "tls-rustls", "postgres", "macros", "migrate", "chrono"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
chrono = { version = "0.4", features = ["serde"] }
thiserror = "2"
anyhow = "1"
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter", "json"] }

[dev-dependencies]
http-body-util = "0.1"
tower = { version = "0.5", features = ["util"] }

[lints.clippy]
unwrap_used = "deny"
expect_used = "warn"
dbg_macro = "deny"
todo = "deny"
```

- Versões acima são ilustrativas: fixar as do projeto em `Cargo.lock` e conferir.
- `unwrap_used = "deny"` no `[lints]` faz o clippy barrar `unwrap()` em produção. Nos testes,
  liberar com `#![allow(clippy::unwrap_used)]` no topo do arquivo de teste (ou
  `clippy.toml` com `allow-unwrap-in-tests = true`, conferir).
- `rust-toolchain.toml` fixa o canal (`stable`) e os componentes (`rustfmt`, `clippy`) para
  todos os ambientes.

## Módulos por funcionalidade

```
src/
  main.rs
  lib.rs
  config.rs        Config::do_ambiente()
  erro.rs          ApiError + IntoResponse (Problem Details)
  estado.rs        Estado { pool, config, relogio }
  reserva/
    mod.rs         pub use ...; pub fn rotas() -> Router<Estado>
    handler.rs     funções async de rota
    service.rs     regra de negócio
    repository.rs  SQL
    modelo.rs      tipos de domínio + DTOs de entrada/saída
  sala/  auth/  plataforma/ (paginacao.rs, extractors.rs)
```

```rust
// src/lib.rs
pub mod auth;
pub mod config;
pub mod erro;
pub mod estado;
pub mod plataforma;
pub mod reserva;
pub mod sala;

use axum::Router;
use estado::Estado;

/// Monta toda a aplicação. A `main` e os testes de integração chamam esta função.
pub fn app(estado: Estado) -> Router {
    let rotas = Router::new()
        .nest("/api/reservas", reserva::rotas())
        .nest("/api/salas", sala::rotas())
        .nest("/api/auth", auth::rotas())
        .with_state(estado);
    plataforma::com_camadas(rotas)
}
```

```rust
// src/main.rs
use anyhow::Context;
use reservas::{app, config::Config, estado::Estado};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let config = Config::do_ambiente().context("configuração inválida")?;
    reservas::plataforma::iniciar_tracing(&config);
    let pool = reservas::plataforma::abrir_pool(&config).await?;
    sqlx::migrate!().run(&pool).await.context("migrações")?;
    reservas::plataforma::servir(app(Estado::new(pool, config.clone())), &config).await
}
```

- `anyhow` aparece só na `main` (e em ferramentas); a biblioteca devolve erros tipados.
- `pub` só o que outro módulo/teste precisa; o resto fica privado (`pub(crate)` por padrão
  em itens internos). Superfície pequena facilita refatorar.
- Módulo por funcionalidade: tudo de "reserva" fica junto. Evitar `models/`, `handlers/`,
  `utils/` globais.

## Tipos ricos: parse, não valide

```rust
// src/reserva/modelo.rs
use chrono::{DateTime, Utc};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Titulo(String);

#[derive(Debug, thiserror::Error)]
#[error("título deve ter de 1 a 120 caracteres")]
pub struct TituloInvalido;

impl Titulo {
    pub fn novo(bruto: &str) -> Result<Self, TituloInvalido> {
        let t = bruto.trim();
        let n = t.chars().count();
        if n == 0 || n > 120 {
            return Err(TituloInvalido);
        }
        Ok(Self(t.to_owned()))
    }
    pub fn as_str(&self) -> &str { &self.0 }
}

/// Período válido por construção: fim sempre depois do início.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Periodo { inicio: DateTime<Utc>, fim: DateTime<Utc> }

impl Periodo {
    pub fn novo(inicio: DateTime<Utc>, fim: DateTime<Utc>) -> Option<Self> {
        (fim > inicio).then_some(Self { inicio, fim })
    }
    pub fn inicio(&self) -> DateTime<Utc> { self.inicio }
    pub fn fim(&self) -> DateTime<Utc> { self.fim }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, sqlx::Type)]
#[sqlx(transparent)]
pub struct SalaId(pub i64);   // newtype: não troca SalaId por UsuarioId sem querer
```

- **Newtype** impede misturar `SalaId` com `UsuarioId` (ambos `i64`); `#[sqlx(transparent)]`
  deixa o `sqlx` ler/gravar como `bigint`.
- Construtor devolve `Result`/`Option`; o campo é privado. Depois de construído, o valor é
  válido: o serviço não revalida.
- Não criar newtype para tudo: só para valor com regra ou identidade confundível.
- `Option<T>` para ausência real (campo opcional, busca sem resultado no repositório).
  Regra de negócio "tem que existir" vira `Err(Erro::NaoEncontrado)` no serviço.
- Enumerar estados com `enum` (`StatusReserva::{Ativa, Cancelada}`), nunca `String` ou `bool`
  quando houver mais de dois estados ou significado.

## Erros de domínio

```rust
// src/reserva/service.rs
#[derive(Debug, thiserror::Error)]
pub enum ReservaErro {
    #[error("reserva não encontrada")]
    NaoEncontrada,
    #[error("sala já reservada nesse período")]
    Conflito,
    #[error("sem permissão sobre a reserva")]
    SemPermissao,
    #[error("entrada inválida: {0}")]
    Invalida(String),
    #[error(transparent)]
    Banco(#[from] sqlx::Error),   // infra: vira 500 na API
}
```

- Um `enum` por funcionalidade, com variantes que o chamador decide (404, 409, 403) e uma
  variante de infraestrutura com `#[from]` + `?`. A API só mapeia variantes, não texto.
- `#[error(transparent)]` repassa `Display` e `source` do erro interno.
- O repositório traduz o erro do `sqlx` (constraint -> `Conflito`) antes de subir; ver
  `persistencia-sqlx.md`. Quem não é constraint continua `Banco`.
- Nunca `Box<dyn Error>` em retorno de função pública da biblioteca: perde o tipo.

## Traits: só quando houver motivo

Funções e `struct` concretas bastam. Crie `trait` quando precisar de **duas implementações
reais** (ex.: relógio fixo no teste, envio de e-mail falso) ou para inverter dependência em
fronteira de I/O.

```rust
pub trait Relogio: Send + Sync {
    fn agora(&self) -> chrono::DateTime<chrono::Utc>;
}
pub struct RelogioSistema;
impl Relogio for RelogioSistema {
    fn agora(&self) -> chrono::DateTime<chrono::Utc> { chrono::Utc::now() }
}
```

- Defina a `trait` onde ela é **usada** (pacote do serviço), com 1-3 métodos.
- `async fn` em `trait` é suportado nas versões estáveis recentes; para uso dinâmico
  (`dyn Trait`) ainda é preciso cuidado (conferir; `async-trait` é o atalho conhecido).
  Preferir genérico ou `enum` a `Box<dyn Trait>` quando as implementações são conhecidas.
- Para o repositório, **não** criar `trait` só para mockar: o teste usa PostgreSQL real
  (`testes-rust.md`). Serviço com regra pura recebe dados como argumento e é testado sem banco.

## Nomes e estilo

| Item | Convenção |
|---|---|
| Crate, módulo, função, variável | `snake_case` |
| Tipo, `trait`, variante de `enum` | `PascalCase` |
| Constante | `SCREAMING_SNAKE_CASE` |
| Construtor | `novo`/`new`, `com_...`, `de_...`; fallível devolve `Result` |
| Conversão | `as_` (barata, empresta), `to_` (cara, copia), `into_` (consome) |
| Getter | `inicio()`, sem prefixo `get_` |

- Identificadores do domínio podem ficar em pt-BR (`Reserva`, `sala_id`), como no restante do
  produto; termos de biblioteca (`Router`, `State`) ficam em inglês. Não misturar na mesma
  palavra.
- Documentar o contrato público com `///`; comentar o **porquê**, não o quê.
- `cargo fmt` sem configuração extra; `rustfmt.toml` só se o time exigir.
- `#[derive]` o mínimo: `Debug` quase sempre; `Clone`/`PartialEq` quando usados; `Serialize`
  só em DTO de resposta; **nunca** `Serialize` em tipo com campo sensível (`senha_hash`).

## Propriedade e empréstimo (regra prática)

- Parâmetro: `&str`/`&[T]` para ler; `String`/`Vec<T>` só quando a função fica com o valor.
- Retorno: devolva valor próprio nas bordas; evite lifetimes explícitos em struct de DTO.
- `clone()` para calar o compilador é sinal de desenho errado; resolver na propriedade.
- Compartilhar entre tasks: `Arc<T>` (imutável) e, se precisar mutar, estado no banco ou
  `tokio::sync::Mutex`/`RwLock`; ver `async-concorrencia.md`.
- Indexação `v[i]` entra em pânico: use `.get(i)` e trate o `None`.

## Clippy e lints

- CI: `cargo clippy --all-targets -- -D warnings` e `cargo fmt --check`.
- Lints úteis além do padrão: `clippy::pedantic` seletivo (ex.: `must_use_candidate` é ruído;
  ligar só o que o time aceitar). Preferir poucas regras fixas a muitas ignoradas.
- `#[allow(...)]` só com comentário do motivo e no menor escopo.
- `unsafe` proibido no projeto: `#![forbid(unsafe_code)]` no topo de `lib.rs`.
