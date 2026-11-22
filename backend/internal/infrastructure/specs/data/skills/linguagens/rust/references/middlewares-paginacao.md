# Middlewares (tower), usuário autenticado e paginação (Rust)

Em axum, middleware é `tower::Layer`; `tower-http` já traz as camadas comuns. Escrever à mão
só o que não existe pronto. Nomes de tipos conferir na versão fixada (`tower-http` muda).

## Pilha de camadas

```rust
// src/plataforma/mod.rs
use std::time::Duration;
use axum::http::{HeaderName, StatusCode};
use tower::ServiceBuilder;
use tower_http::{
    catch_panic::CatchPanicLayer,
    limit::RequestBodyLimitLayer,
    request_id::{MakeRequestUuid, PropagateRequestIdLayer, SetRequestIdLayer},
    timeout::TimeoutLayer,
    trace::TraceLayer,
};

static X_REQUEST_ID: HeaderName = HeaderName::from_static("x-request-id");

pub fn com_camadas(router: axum::Router) -> axum::Router {
    // ServiceBuilder: a primeira camada listada é a mais externa
    router.layer(
        ServiceBuilder::new()
        .layer(SetRequestIdLayer::new(X_REQUEST_ID.clone(), MakeRequestUuid))
        .layer(
            TraceLayer::new_for_http().make_span_with(|req: &axum::http::Request<_>| {
                let id = req.headers().get("x-request-id").and_then(|v| v.to_str().ok()).unwrap_or("-");
                tracing::info_span!("http", metodo = %req.method(), caminho = %req.uri().path(), request_id = %id)
            }),
        )
        .layer(PropagateRequestIdLayer::new(X_REQUEST_ID.clone()))
        .layer(CatchPanicLayer::new())                       // panic vira 500 em vez de derrubar a conexão
        .layer(TimeoutLayer::new(Duration::from_secs(15)))   // 408 ao estourar (conferir assinatura atual)
        .layer(RequestBodyLimitLayer::new(1024 * 1024))      // 1 MiB; 413 acima disso
    )
}
```

- A função recebe e devolve `Router`: evita o tipo de retorno complexo de uma `Layer`; `app()`
  chama `plataforma::com_camadas(router)`.
- **Ordem:** request id primeiro (todo log o carrega), trace, depois panic, timeout e limite
  de corpo. Em `Router::layer`, a última chamada fica mais externa; em `ServiceBuilder`, a
  primeira. Conferir com um teste que o `x-request-id` aparece na resposta de um erro.
- `TimeoutLayer::new(d)` está sendo substituída por `with_status_code(status, d)` nas
  versões recentes (conferir). O timeout só cancela o `Future` do handler: consultas no
  banco precisam do próprio timeout (ver `persistencia-sqlx.md`).
- Limite de corpo: `RequestBodyLimitLayer` (tower-http) ou `DefaultBodyLimit` do axum (2 MiB
  por padrão nos extractors de corpo). Escolher **um** limite explícito e testar o 413.
- O 500 de `CatchPanicLayer` sai em texto simples por padrão; use
  `CatchPanicLayer::custom(...)` com um handler que devolva Problem Details (reaproveite
  `ApiError::Interno`).

### CORS

```rust
use tower_http::cors::{AllowOrigin, CorsLayer};
use axum::http::{header, Method};

pub fn cors(origem: &str) -> CorsLayer {
    CorsLayer::new()
        .allow_origin(AllowOrigin::exact(origem.parse().expect("CORS_ORIGEM validada na Config")))
        .allow_methods([Method::GET, Method::POST, Method::PUT, Method::DELETE])
        .allow_headers([header::CONTENT_TYPE, HeaderName::from_static("x-csrf-token")])
        .allow_credentials(true)       // cookie de sessão; exige origem exata (nunca "*")
        .max_age(Duration::from_secs(600))
}
```

- Com cookie de sessão, `allow_credentials(true)` exige origem **exata**, nunca `Any`. Se o
  front é servido pela mesma origem (proxy reverso do Compose), nem precisa de CORS.
- A origem vem da `Config`, validada na subida; o `expect` acima é aceitável porque roda uma
  vez na inicialização (comentar a invariante) — ou propagar o erro com `?` em `Config`.

## Camada própria quando preciso: `from_fn`

```rust
use axum::{extract::Request, middleware::Next, response::Response, http::{header, HeaderValue}};

pub async fn cabecalhos_seguranca(req: Request, next: Next) -> Response {
    let mut resp = next.run(req).await;
    let h = resp.headers_mut();
    h.insert(header::X_CONTENT_TYPE_OPTIONS, HeaderValue::from_static("nosniff"));
    h.insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    resp
}
// Router::new()....layer(axum::middleware::from_fn(cabecalhos_seguranca))
```

- `middleware::from_fn` é a forma simples; `from_fn_with_state` quando precisa do pool.
- Autenticação **não** é middleware global: é extractor (abaixo), que o handler pede e o
  compilador cobra. Rota sem `UsuarioAtual` na assinatura é pública por construção.

## Usuário autenticado como extractor

```rust
// src/auth/extractor.rs
use axum::{extract::FromRequestParts, http::request::Parts};
use axum_extra::extract::CookieJar;           // feature "cookie" (conferir)

#[derive(Debug, Clone)]
pub struct UsuarioAtual { pub id: i64, pub papel: Papel }

impl FromRequestParts<Estado> for UsuarioAtual {
    type Rejection = ApiError;

    async fn from_request_parts(parts: &mut Parts, estado: &Estado) -> Result<Self, ApiError> {
        let jar = CookieJar::from_headers(&parts.headers);
        let token = jar.get("sessao").map(|c| c.value().to_owned()).ok_or(ApiError::NaoAutenticado)?;
        auth::service::usuario_da_sessao(&estado.pool, &estado.config.sessao, &token).await
    }
}
```

- `FromRequestParts<S>` com `async fn` direto na `impl` é suportado nas versões recentes do
  axum; em versões antigas era `#[async_trait]` (conferir a fixada).
- O handler que declara `usuario: UsuarioAtual` só roda com sessão válida; senão 401 em
  Problem Details. Resolução da sessão no banco: `seguranca-sessao.md`.
- Papel e **dono do recurso** são checados no serviço, não no extractor.
- Resultado por requisição não é cacheado entre requisições; dentro da requisição, o
  extractor roda uma vez por handler.

## Paginação

Por padrão, `limite` + `pagina` (ou `deslocamento`) com teto, ordenação **determinística**
(sempre desempate por `id`). Cursor (keyset) quando a lista é grande ou muda com frequência.

```rust
// src/plataforma/paginacao.rs
use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PaginaQuery {
    #[serde(default = "pagina_padrao")] pub pagina: u32,
    #[serde(default = "limite_padrao")] pub limite: u32,
}
fn pagina_padrao() -> u32 { 1 }
fn limite_padrao() -> u32 { 20 }

pub struct Pagina { pub limite: i64, pub deslocamento: i64 }

impl PaginaQuery {
    pub fn validar(&self) -> Result<Pagina, ApiError> {
        if self.pagina == 0 || !(1..=100).contains(&self.limite) {
            return Err(ApiError::Invalido {
                detalhe: "Paginação inválida.".into(),
                campos: vec![CampoInvalido::novo("limite", "entre 1 e 100; pagina a partir de 1")],
            });
        }
        Ok(Pagina {
            limite: i64::from(self.limite),
            deslocamento: i64::from(self.pagina - 1) * i64::from(self.limite),
        })
    }
}

#[derive(Serialize)]
pub struct Paginado<T: Serialize> {
    pub itens: Vec<T>,
    pub total: i64,
    pub pagina: u32,
    pub limite: u32,
}
```

```rust
// repository.rs
let itens = sqlx::query_as!(
    ReservaLinha,
    "SELECT id, sala_id, usuario_id, inicio, fim, titulo
       FROM reservas WHERE usuario_id = $1
      ORDER BY inicio, id LIMIT $2 OFFSET $3",
    usuario_id, p.limite, p.deslocamento
).fetch_all(pool).await?;
let total: i64 = sqlx::query_scalar!(
    r#"SELECT count(*) AS "total!" FROM reservas WHERE usuario_id = $1"#, usuario_id
).fetch_one(pool).await?;
```

- Teto de `limite` impede `?limite=1000000`. `ORDER BY` com `id` no fim evita linhas
  repetidas/perdidas entre páginas.
- `count(*)` retorna `Option<i64>` na macro; a anotação `"total!"` força não nulo.
- `OFFSET` grande é lento; para listas longas, keyset: `WHERE (inicio, id) > ($1, $2)
  ORDER BY inicio, id LIMIT $3` e devolver o cursor da última linha.
- O contrato (`itens`, `total`, nomes) vem do `openapi.yaml`; este código segue o contrato,
  não o contrário.
- Filtros opcionais com `query_as!` fixo exigem truques (`$1::tipo IS NULL OR col = $1`);
  se ficar ilegível, use `sqlx::QueryBuilder` (runtime, com binds) e cubra com teste de
  integração.
