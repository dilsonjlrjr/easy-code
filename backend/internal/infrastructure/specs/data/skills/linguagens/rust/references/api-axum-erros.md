# API HTTP em Rust: rotas, extractors, validação e erros

Contrato de erros e regra 400/409 vêm de `arquitetura-web` (Problem Details, RFC 9457). Aqui:
a implementação com `axum`. Exemplos para axum 0.8 (caminho `/{id}`); em versões anteriores o
parâmetro era `/:id` (conferir a versão fixada).

## Quem responde o quê

| Situação | Status | Onde nasce |
|---|---|---|
| JSON malformado, tipo errado, campo desconhecido | 400 | Extractor `ApiJson` |
| Content-Type diferente de JSON | 415 | Extractor `ApiJson` |
| Corpo grande demais | 413 | Camada de limite de corpo |
| Campo inválido (vazio, fora da faixa, fim antes do início) | 400 | Validação na borda |
| `{id}` não numérico, query inválida | 400 | Extractors `ApiPath`/`ApiQuery` |
| Sem sessão válida | 401 | Extractor `UsuarioAtual` |
| Autenticado sem permissão no recurso | 403 (ou 404 para não revelar existência) | Serviço |
| Não existe | 404 | `Err(NaoEncontrada)` |
| **Conflito com o estado** (sala já reservada, e-mail em uso) | **409** | Constraint traduzida |
| Falha inesperada | 500 | `ApiError::Interno` / `CatchPanicLayer` |

Regra: **400 = pedido inválido por si**; **409 = pedido válido que conflita com o estado**.

## Rotas

```rust
// src/reserva/mod.rs
use axum::{routing::{get, post}, Router};
use crate::estado::Estado;

pub fn rotas() -> Router<Estado> {
    Router::new()
        .route("/", post(handler::criar).get(handler::listar))
        .route("/{id}", get(handler::buscar).delete(handler::cancelar))
}
```

- `Router<Estado>` sem `with_state`: o estado é injetado uma vez em `lib.rs::app`. Cada
  funcionalidade expõe `rotas()`; `nest` aplica o prefixo.
- Método não previsto numa rota existente devolve **405** com `Allow`; rota inexistente 404.
  Para ambos em Problem Details, defina `fallback` (404) e, se desejar, o fallback de método
  (conferir o nome na versão): `Router::new()....fallback(|| async { ApiError::NaoEncontrado("rota") })`.

## Extractors próprios: rejeição em Problem Details

As rejeições padrão do axum são texto puro. Troque por extractors finos que devolvem
`ApiError` (padrão documentado do axum com `#[derive(FromRequest)]`; requer a feature `macros`):

```rust
// src/plataforma/extractors.rs
use axum::extract::{FromRequest, FromRequestParts, rejection::{JsonRejection, PathRejection, QueryRejection}};
use crate::erro::ApiError;

#[derive(FromRequest)]
#[from_request(via(axum::Json), rejection(ApiError))]
pub struct ApiJson<T>(pub T);

#[derive(FromRequestParts)]
#[from_request(via(axum::extract::Path), rejection(ApiError))]
pub struct ApiPath<T>(pub T);

#[derive(FromRequestParts)]
#[from_request(via(axum::extract::Query), rejection(ApiError))]
pub struct ApiQuery<T>(pub T);

impl From<JsonRejection> for ApiError {
    fn from(r: JsonRejection) -> Self {
        match r {
            JsonRejection::MissingJsonContentType(_) => ApiError::TipoNaoSuportado,
            JsonRejection::BytesRejection(_) => ApiError::invalido("Não foi possível ler o corpo."),
            // sintaxe e tipo: mensagem do serde ajuda o cliente (campo e posição)
            outro => ApiError::invalido(format!("JSON inválido: {}", outro.body_text())),
        }
    }
}
impl From<PathRejection> for ApiError {
    fn from(r: PathRejection) -> Self { ApiError::invalido(format!("Parâmetro de caminho inválido: {}", r.body_text())) }
}
impl From<QueryRejection> for ApiError {
    fn from(r: QueryRejection) -> Self { ApiError::invalido(format!("Parâmetro de consulta inválido: {}", r.body_text())) }
}
```

- `body_text()` expõe a mensagem do `serde` (campo e posição); aceitável para erro de formato.
- O `413` vem de `RequestBodyLimitLayer` (ver `middlewares-paginacao.md`).

## DTOs: request e response próprios

```rust
// src/reserva/modelo.rs
use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]            // contrato estrito: campo desconhecido = 400
pub struct CriarReservaRequest {
    pub sala_id: i64,
    pub inicio: DateTime<Utc>,
    pub fim: DateTime<Utc>,
    pub titulo: String,
}

#[derive(Debug, Serialize)]
pub struct ReservaResposta {
    pub id: i64,
    pub sala_id: i64,
    pub usuario_id: i64,
    pub inicio: DateTime<Utc>,           // serializa em RFC 3339
    pub fim: DateTime<Utc>,
    pub titulo: String,
}
```

- Campo que o cliente não pode definir (`id`, `usuario_id`) **não existe** no request (evita
  mass assignment); `usuario_id` vem da sessão.
- Response é tipo à parte da linha do banco: nunca serializar `Usuario` com `senha_hash`.
- `Vec` vazio serializa `[]`, nunca `null`.

## Validação

Por padrão, validação **manual** que converte o request em tipo de domínio e acumula erros
por campo:

```rust
pub struct NovaReserva { pub sala: SalaId, pub periodo: Periodo, pub titulo: Titulo }

impl CriarReservaRequest {
    pub fn validar(self) -> Result<NovaReserva, ApiError> {
        let mut campos = Vec::new();
        if self.sala_id <= 0 {
            campos.push(CampoInvalido::novo("sala_id", "obrigatório"));
        }
        let periodo = Periodo::novo(self.inicio, self.fim);
        if periodo.is_none() {
            campos.push(CampoInvalido::novo("fim", "deve ser depois do início"));
        }
        let titulo = Titulo::novo(&self.titulo);
        if titulo.is_err() {
            campos.push(CampoInvalido::novo("titulo", "obrigatório, até 120 caracteres"));
        }
        match (periodo, titulo) {
            (Some(periodo), Ok(titulo)) if campos.is_empty() => {
                Ok(NovaReserva { sala: SalaId(self.sala_id), periodo, titulo })
            }
            _ => Err(ApiError::Invalido { detalhe: "Corpo da requisição inválido.".into(), campos }),
        }
    }
}
```

- Retorna tipos ricos: depois de `validar`, o serviço recebe `Titulo`/`Periodo` já válidos.
- Alternativa: crate `validator` com `#[derive(Validate)]` e atributos (`length`, `range`,
  `email`); reduz repetição quando há muitos DTOs. API muda entre versões: conferir. Mesmo
  com ele, regras entre campos (fim > início) e conversão para newtype continuam manuais.
- Validação de **formato** na borda; regra de **negócio** (sala existe, horário comercial) no
  serviço. O que depende do estado do banco é constraint ou consulta no serviço, nunca na borda.

## Handlers finos

```rust
// src/reserva/handler.rs
pub async fn criar(
    State(estado): State<Estado>,
    usuario: UsuarioAtual,                       // 401 se não houver sessão
    ApiJson(req): ApiJson<CriarReservaRequest>,  // extractor de corpo SEMPRE por último
) -> Result<impl IntoResponse, ApiError> {
    let nova = req.validar()?;
    let r = service::criar(&estado.pool, usuario.id, nova).await?;   // ReservaErro -> ApiError via From
    let corpo = ReservaResposta::from(r);
    Ok((
        StatusCode::CREATED,
        [(header::LOCATION, format!("/api/reservas/{}", corpo.id))],
        Json(corpo),
    ))
}
```

- A ordem dos extractors importa: o que consome o corpo (`ApiJson`) é o **último**.
- Handler devolve `Result<T, ApiError>`; `?` converte `ReservaErro` em `ApiError` via `From`.
- Sem SQL, sem regra, sem `unwrap`. Handler com mais de ~15 linhas: a regra pertence ao serviço.

## Erro da API: enum + `IntoResponse` (Problem Details)

```rust
// src/erro.rs
use axum::{http::{header, StatusCode}, response::{IntoResponse, Response}};
use serde::Serialize;

#[derive(Debug, Serialize)]
pub struct CampoInvalido { pub campo: String, pub mensagem: String }
impl CampoInvalido {
    pub fn novo(campo: &str, mensagem: &str) -> Self {
        Self { campo: campo.into(), mensagem: mensagem.into() }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ApiError {
    #[error("requisição inválida: {detalhe}")]
    Invalido { detalhe: String, campos: Vec<CampoInvalido> },
    #[error("tipo de conteúdo não suportado")]
    TipoNaoSuportado,
    #[error("não autenticado")]
    NaoAutenticado,
    #[error("sem permissão")]
    Proibido,
    #[error("{0} não encontrado")]
    NaoEncontrado(&'static str),
    #[error("conflito: {0}")]
    Conflito(String),
    #[error("erro interno")]
    Interno(#[source] Box<dyn std::error::Error + Send + Sync>),
}

impl ApiError {
    pub fn invalido(detalhe: impl Into<String>) -> Self {
        Self::Invalido { detalhe: detalhe.into(), campos: vec![] }
    }
}

#[derive(Serialize)]
struct Problema<'a> {
    #[serde(rename = "type")]
    tipo: String,
    title: &'a str,
    status: u16,
    detail: String,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    errors: Vec<CampoInvalido>,
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        let (status, slug, titulo, detalhe, campos) = match self {
            Self::Invalido { detalhe, campos } => (StatusCode::BAD_REQUEST, "requisicao-invalida", "Requisição inválida", detalhe, campos),
            Self::TipoNaoSuportado => (StatusCode::UNSUPPORTED_MEDIA_TYPE, "tipo-nao-suportado", "Tipo de conteúdo não suportado", "Use application/json.".into(), vec![]),
            Self::NaoAutenticado => (StatusCode::UNAUTHORIZED, "nao-autenticado", "Não autenticado", "Sessão ausente ou expirada.".into(), vec![]),
            Self::Proibido => (StatusCode::FORBIDDEN, "proibido", "Sem permissão", "Você não pode executar esta ação.".into(), vec![]),
            Self::NaoEncontrado(o) => (StatusCode::NOT_FOUND, "nao-encontrado", "Não encontrado", format!("{o} não encontrado."), vec![]),
            Self::Conflito(d) => (StatusCode::CONFLICT, "conflito", "Conflito", d, vec![]),
            Self::Interno(causa) => {
                // loga a causa; o cliente recebe mensagem genérica (nada de SQL ou stack)
                tracing::error!(erro = %causa, "erro interno");
                (StatusCode::INTERNAL_SERVER_ERROR, "erro-interno", "Erro interno", "Erro inesperado. Tente novamente.".into(), vec![])
            }
        };
        let corpo = Problema {
            tipo: format!("https://exemplo.invalid/problemas/{slug}"),
            title: titulo, status: status.as_u16(), detail: detalhe, errors: campos,
        };
        (
            status,
            [(header::CONTENT_TYPE, "application/problem+json")],
            serde_json::to_vec(&corpo).unwrap_or_default(),
        ).into_response()
    }
}
```

- 500 com mensagem genérica; a causa vai para o log (com request id). Não vazar SQL nem `Debug`.
- `type` é URI estável por classe de problema; `errors` é a extensão por campo (seguir o `openapi.yaml`).
- `Interno` aceita qualquer erro: `ApiError::Interno(Box::new(e))`.

## Tradução de `sqlx::Error` por SQLSTATE (409)

```rust
// src/plataforma/pg.rs
#[derive(Debug, PartialEq, Eq)]
pub enum Violacao { Exclusao, Unicidade, ChaveEstrangeira }

pub fn violacao(err: &sqlx::Error) -> Option<(Violacao, Option<String>)> {
    let sqlx::Error::Database(db) = err else { return None };
    let tipo = match db.code().as_deref() {
        Some("23P01") => Violacao::Exclusao,          // exclusion_violation (EXCLUDE)
        Some("23505") => Violacao::Unicidade,         // unique_violation
        Some("23503") => Violacao::ChaveEstrangeira,  // foreign_key_violation
        _ => return None,
    };
    Some((tipo, db.constraint().map(str::to_owned)))
}
```

O repositório traduz perto do SQL (`traduzir`, em `persistencia-sqlx.md`); o serviço só vê o erro de domínio.

```rust
impl From<ReservaErro> for ApiError {
    fn from(e: ReservaErro) -> Self {
        match e {
            ReservaErro::NaoEncontrada => ApiError::NaoEncontrado("reserva"),
            ReservaErro::Conflito => ApiError::Conflito("A sala já está reservada nesse período.".into()),
            ReservaErro::SemPermissao => ApiError::Proibido,
            ReservaErro::Invalida(m) => ApiError::invalido(m),
            ReservaErro::Banco(e) => ApiError::Interno(Box::new(e)),
        }
    }
}
```

- `db.code()` devolve o SQLSTATE; `23P01` = exclusion, `23505` = unique, `23503` = FK.
  `db.constraint()` dá o nome da constraint (útil para mensagens distintas quando há várias
  `UNIQUE`; o nome vem do `docs/dados/esquema.sql`).
- `sqlx` também tem `db.is_unique_violation()`/`is_foreign_key_violation()`; **não** há
  atalho para exclusion: use o código.
- Mesma tradução em `UPDATE` e em qualquer escrita que possa violar a constraint.
- Códigos de retorno: `POST` cria 201 + `Location` + corpo; `DELETE` 204; datas em UTC com offset.
