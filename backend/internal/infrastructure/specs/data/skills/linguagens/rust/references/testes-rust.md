# Testes (Rust): unitários, `oneshot`, `#[sqlx::test]` e testcontainers

Estratégia (pirâmide, o que testar em cada nível, rastreio CT -> teste): `estrategia-testes`.
Aqui: o "como em Rust". Regra: regra de negócio em teste **sem banco**; constraint, SQL,
transação e HTTP contra **PostgreSQL real**. Sem SQLite nem mock do driver no lugar do banco.

## Níveis

| Nível | Onde | Ferramenta | Banco |
|---|---|---|---|
| Unitário (regra, tipos, mapeamento) | `#[cfg(test)] mod testes` no próprio arquivo | `cargo test` | Não |
| Integração do repositório/serviço | `tests/` ou módulo com `#[sqlx::test]` | `sqlx::test` | PostgreSQL real |
| Integração HTTP (rota completa) | `tests/api_*.rs` | `tower::ServiceExt::oneshot` no `Router` | PostgreSQL real |
| Concorrência (reservas simultâneas) | `tests/` | `JoinSet` + banco real | PostgreSQL real |
| Ponta a ponta | Suíte da QA (Compose) | Fora do código Rust | Sim |

## Unitários: regra pura, em tabela

```rust
// src/reserva/modelo.rs
#[cfg(test)]
mod testes {
    use super::*;
    use chrono::TimeZone;

    fn t(h: u32) -> DateTime<Utc> { Utc.with_ymd_and_hms(2025, 3, 10, h, 0, 0).unwrap() }

    #[test]
    fn periodo_exige_fim_depois_do_inicio() {
        let casos = [
            ("fim depois", t(9), t(10), true),
            ("fim igual",  t(9), t(9),  false),
            ("fim antes",  t(10), t(9), false),
        ];
        for (nome, ini, fim, valido) in casos {
            assert_eq!(Periodo::novo(ini, fim).is_some(), valido, "caso: {nome}");
        }
    }

    #[test]
    fn titulo_apara_espacos_e_limita_tamanho() {
        assert_eq!(Titulo::novo("  Daily  ").unwrap().as_str(), "Daily");
        assert!(Titulo::novo("   ").is_err());
        assert!(Titulo::novo(&"x".repeat(121)).is_err());
    }
}
```

- `unwrap()` é aceitável **em teste** (`clippy.toml`: `allow-unwrap-in-tests = true`,
  conferir; ou `#![allow(clippy::unwrap_used)]` no arquivo de teste).
- Tabela = array de tuplas + laço com mensagem do caso (`"caso: {nome}"`). Para casos
  muito numerosos, crates como `rstest` ou `test-case` (conferir) reduzem boilerplate; o
  array já basta na maioria.
- Dependência de tempo/aleatoriedade entra por parâmetro ou por `trait` (`Relogio` com
  implementação fixa), nunca `Utc::now()` dentro da regra.
- Teste de erro: afirmar a **variante** (`assert!(matches!(r, Err(ReservaErro::Conflito)))`),
  não o texto da mensagem.

## Integração com `#[sqlx::test]`

`#[sqlx::test]` cria um **banco novo por teste**, aplica as migrações de `./migrations`, passa
o `PgPool` e apaga o banco no fim (isolamento total, testes em paralelo). Requer
`DATABASE_URL` apontando para um servidor onde o usuário pode criar bancos (o PostgreSQL do
Compose; ver `config-observabilidade-docker.md`).

```rust
// tests/reserva_repo.rs
use reservas::reserva::repository;
use sqlx::PgPool;

#[sqlx::test(fixtures("usuarios", "salas"))]    // tests/fixtures/usuarios.sql e salas.sql
async fn insere_reserva(pool: PgPool) {
    let nova = nova_reserva(1, "2025-03-10T09:00:00Z", "2025-03-10T10:00:00Z");
    let r = repository::inserir(&pool, &nova, 1).await.unwrap();
    assert_eq!(r.sala_id, 1);
}

#[sqlx::test(fixtures("usuarios", "salas"))]
async fn sobreposicao_vira_conflito_pela_constraint(pool: PgPool) {
    let a = nova_reserva(1, "2025-03-10T09:00:00Z", "2025-03-10T10:00:00Z");
    let b = nova_reserva(1, "2025-03-10T09:30:00Z", "2025-03-10T10:30:00Z");
    repository::inserir(&pool, &a, 1).await.unwrap();
    let r = repository::inserir(&pool, &b, 1).await;
    assert!(matches!(r, Err(ReservaErro::Conflito)), "esperava Conflito, veio {r:?}");
}

#[sqlx::test(fixtures("usuarios", "salas"))]
async fn horarios_adjacentes_nao_conflitam(pool: PgPool) {
    // tstzrange é [inicio, fim): fim de uma = início da outra é permitido
    let a = nova_reserva(1, "2025-03-10T09:00:00Z", "2025-03-10T10:00:00Z");
    let b = nova_reserva(1, "2025-03-10T10:00:00Z", "2025-03-10T11:00:00Z");
    repository::inserir(&pool, &a, 1).await.unwrap();
    repository::inserir(&pool, &b, 1).await.unwrap();
}
```

- Migrações vêm do diretório padrão; outro caminho: `#[sqlx::test(migrations = "./db/migrations")]`.
  `fixtures("nome")` carrega `fixtures/nome.sql` ao lado do arquivo de teste (conferir
  regras de caminho na versão). Fixture = SQL simples com `INSERT`.
- Sem `DATABASE_URL`, o macro falha ao rodar: documentar em `README` e na T. O CI sobe o
  PostgreSQL do Compose antes de `cargo test`.
- Os testes **usam a mesma migração de produção**: a constraint `EXCLUDE` existe no teste
  porque está na migração. Teste que cria schema próprio esconde divergência.
- Funcionalidades que usam `query!` precisam do cache `.sqlx/` ou de banco no build; o banco
  do `sqlx::test` não substitui o do compilador.

## Integração HTTP com `oneshot`

O `Router` é um `tower::Service`: dá para chamá-lo **sem abrir porta**.

```rust
// tests/comum/mod.rs
use axum::{body::Body, http::{header, Request, StatusCode}, Router};
use http_body_util::BodyExt;
use tower::ServiceExt;                      // .oneshot (feature "util" do tower)

pub fn app_de_teste(pool: PgPool) -> Router {
    reservas::app(Estado::new(pool, Config::de_teste()))
}

pub async fn chamar(app: &Router, req: Request<Body>) -> (StatusCode, serde_json::Value) {
    let resp = app.clone().oneshot(req).await.unwrap();      // Router é Clone: um por chamada
    let status = resp.status();
    let bytes = resp.into_body().collect().await.unwrap().to_bytes();
    let json = serde_json::from_slice(&bytes).unwrap_or(serde_json::Value::Null);
    (status, json)
}

pub fn post_json(uri: &str, sessao: Option<&str>, corpo: serde_json::Value) -> Request<Body> {
    let mut b = Request::builder().method("POST").uri(uri).header(header::CONTENT_TYPE, "application/json");
    if let Some(t) = sessao { b = b.header(header::COOKIE, format!("sessao={t}")); }
    b.body(Body::from(corpo.to_string())).unwrap()
}

/// Cria usuário e sessão direto no banco (sem passar pelo login: argon2 é lento em teste).
pub async fn sessao_de(pool: &PgPool, usuario_id: i64) -> String {
    let (token, hash) = reservas::auth::token::novo_token();
    sqlx::query!(
        "INSERT INTO sessoes (token_hash, usuario_id, expira_em, expira_max)
         VALUES ($1, $2, now() + interval '30 minutes', now() + interval '12 hours')",
        hash, usuario_id
    ).execute(pool).await.unwrap();
    token
}
```

```rust
// tests/api_reservas.rs
mod comum;
use comum::*;
use serde_json::json;

#[sqlx::test(fixtures("usuarios", "salas"))]
async fn ct07_reserva_sobreposta_devolve_409_problem_details(pool: PgPool) {
    let t = sessao_de(&pool, 1).await;
    let app = app_de_teste(pool);
    let corpo = json!({"sala_id": 1, "inicio": "2025-03-10T09:00:00Z", "fim": "2025-03-10T10:00:00Z", "titulo": "A"});

    let (s1, _) = chamar(&app, post_json("/api/reservas", Some(&t), corpo.clone())).await;
    assert_eq!(s1, StatusCode::CREATED);

    let (s2, p) = chamar(&app, post_json("/api/reservas", Some(&t), corpo)).await;
    assert_eq!(s2, StatusCode::CONFLICT);
    assert_eq!(p["status"], 409);
    assert!(p["title"].is_string());
}

#[sqlx::test(fixtures("usuarios", "salas"))]
async fn sem_sessao_devolve_401(pool: PgPool) {
    let app = app_de_teste(pool);
    let (s, _) = chamar(&app, post_json("/api/reservas", None, json!({}))).await;
    assert_eq!(s, StatusCode::UNAUTHORIZED);
}

#[sqlx::test(fixtures("usuarios", "salas"))]
async fn fim_antes_do_inicio_devolve_400_com_campo(pool: PgPool) {
    let t = sessao_de(&pool, 1).await;
    let app = app_de_teste(pool);
    let corpo = json!({"sala_id": 1, "inicio": "2025-03-10T10:00:00Z", "fim": "2025-03-10T09:00:00Z", "titulo": "A"});
    let (s, p) = chamar(&app, post_json("/api/reservas", Some(&t), corpo)).await;
    assert_eq!(s, StatusCode::BAD_REQUEST);
    assert_eq!(p["errors"][0]["campo"], "fim");
}

#[sqlx::test(fixtures("usuarios", "salas"))]
async fn usuario_nao_cancela_reserva_de_outro(pool: PgPool) {
    let dono = sessao_de(&pool, 1).await;
    let outro = sessao_de(&pool, 2).await;
    let app = app_de_teste(pool);
    // cria como dono...
    let (_, r) = chamar(&app, post_json("/api/reservas", Some(&dono), json!({"sala_id":1,"inicio":"2025-03-10T09:00:00Z","fim":"2025-03-10T10:00:00Z","titulo":"A"}))).await;
    let id = r["id"].as_i64().unwrap();
    // ...e outro tenta cancelar
    let req = Request::builder().method("DELETE").uri(format!("/api/reservas/{id}"))
        .header(header::COOKIE, format!("sessao={outro}")).body(Body::empty()).unwrap();
    let (s, _) = chamar(&app, req).await;
    assert_eq!(s, StatusCode::NOT_FOUND);       // IDOR: 404 (ou 403, conforme o contrato)
}
```

- Cada teste é um caso de CT; **nomear com o ID do CT** (`ct07_...`) liga teste e cenário.
- Asserções sobre status **e** forma do corpo de erro (`status`, `title`, `errors`), pois o
  contrato de Problem Details faz parte do `openapi.yaml`.
- `Router::clone` é barato (estado em `Arc`); cada `oneshot` consome uma cópia.
- Sessão criada direto no banco evita o custo do `argon2`. Teste do próprio login faz o
  fluxo completo (1 ou 2 testes). Em `Cargo.toml`, `[profile.dev.package.argon2] opt-level = 3`
  acelera o hash em debug.
- Para ler `Set-Cookie`: `resp.headers().get_all(header::SET_COOKIE)`; afirmar
  `HttpOnly`, `SameSite=Lax` e `Path=/`.

## Concorrência: a constraint decide

```rust
#[sqlx::test(fixtures("usuarios", "salas"))]
async fn duas_reservas_simultaneas_so_uma_vence(pool: PgPool) {
    let t = sessao_de(&pool, 1).await;
    let app = app_de_teste(pool);
    let corpo = json!({"sala_id": 1, "inicio": "2025-03-10T09:00:00Z", "fim": "2025-03-10T10:00:00Z", "titulo": "A"});

    let mut set = tokio::task::JoinSet::new();
    for _ in 0..8 {
        let (app, t, corpo) = (app.clone(), t.clone(), corpo.clone());
        set.spawn(async move { chamar(&app, post_json("/api/reservas", Some(&t), corpo)).await.0 });
    }
    let mut criadas = 0;
    let mut conflitos = 0;
    while let Some(r) = set.join_next().await {
        match r.unwrap() {
            StatusCode::CREATED => criadas += 1,
            StatusCode::CONFLICT => conflitos += 1,
            outro => panic!("status inesperado: {outro}"),
        }
    }
    assert_eq!((criadas, conflitos), (1, 7));
}
```

- O runtime é o do macro `#[sqlx::test]`; as chamadas de banco já se intercalam nos `.await`.
  Se a corrida não aparecer, aumentar o número de tasks ou configurar runtime multi-thread
  (conferir as opções do macro na versão).
- Sem `sleep` para "dar tempo": a sincronização vem do `join_next`.

## testcontainers (quando não há PostgreSQL pronto)

Alternativa para CI/máquinas sem o Compose: o teste sobe o PostgreSQL em contêiner
(`testcontainers` / `testcontainers-modules`, conferir API da versão).

```rust
use testcontainers_modules::{postgres::Postgres, testcontainers::{runners::AsyncRunner, ContainerAsync}};
use tokio::sync::OnceCell;

static PG: OnceCell<(ContainerAsync<Postgres>, String)> = OnceCell::const_new();

async fn url_base() -> &'static str {
    let (_, url) = PG.get_or_init(|| async {
        let c = Postgres::default().start().await.expect("subir PostgreSQL de teste");
        let porta = c.get_host_port_ipv4(5432).await.expect("porta");
        (c, format!("postgres://postgres:postgres@127.0.0.1:{porta}"))
    }).await;
    url
}
// cada teste: CREATE DATABASE test_<uuid>; PgPool; sqlx::migrate!().run(&pool); DROP no fim
```

- **Um** contêiner por binário de teste (`OnceCell`), **um banco por teste** (`CREATE
  DATABASE`) para isolamento e paralelismo. Subir contêiner por teste é lento.
- Exige Docker no ambiente de teste. Em aplicação pequena, preferir o PostgreSQL do Compose + `sqlx::test`
  (mais simples); testcontainers entra quando a suíte precisa ser autônoma.
- Usar a **mesma imagem/versão** do PostgreSQL de produção (`postgres:<major>`).

## Fakes e dublês

- Fronteira externa (e-mail, relógio, HTTP de terceiros): `trait` pequena + implementação
  falsa no teste. Evite crates de mock para o que um `struct` com `Vec` resolve.
- Repositório **não** é mockado; usa-se o banco. Serviço com regra pura recebe os dados como
  argumento e é testado sem banco.

## Execução e cobertura

| Ação | Comando |
|---|---|
| Tudo | `cargo test` |
| Só unitários (sem banco) | `cargo test --lib` |
| Um arquivo de integração | `cargo test --test api_reservas` |
| Um teste | `cargo test sobreposicao -- --nocapture` |
| Cobertura | `cargo llvm-cov --summary-only` (open source, conferir) |
| Dependências | `cargo audit` |

- `cargo test` roda testes em paralelo por padrão; com `sqlx::test` cada um tem seu banco,
  então não há interferência. Estado global (variáveis de ambiente, `static`) quebra o
  paralelismo: evitar.
- Teste que depende de ordem ou de sleep é teste quebrado: corrigir a causa.
