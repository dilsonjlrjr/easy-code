# Segurança (Rust): sessão em cookie com tabela no PostgreSQL

Decisão e conceitos: `arquitetura-web` (autenticação e autorização) e `revisao-seguranca`
(autenticação/sessão, controle de acesso, cabeçalhos/CORS). Aqui: a implementação padrão em
Rust com `axum`, `axum-extra` (cookies), `argon2`, `sha2` e `sqlx` — login próprio
(usuário/senha), sessão em cookie `HttpOnly` cujo estado fica numa tabela do PostgreSQL. O
mecanismo é pequeno e fica visível; `tower-sessions` com store `sqlx` é alternativa pronta
(conferir compatibilidade com a versão do axum), com o mesmo desenho (cookie opaco + tabela).
Versões e assinaturas de `argon2`, `rand`, `cookie` mudam: conferir na versão fixada.

## Modelo

```
POST /api/auth/login  -> confere senha -> cria sessão (token aleatório) -> Set-Cookie: sessao=<token>; HttpOnly; Secure; SameSite=Lax
GET  /api/...         -> cookie -> hash do token -> UPDATE/SELECT na tabela -> UsuarioAtual
POST /api/auth/logout -> DELETE da sessão -> cookie expirado
```

- O cookie guarda um **token opaco** (32 bytes aleatórios); o banco guarda só o **SHA-256**.
  Vazar o banco não vaza sessões utilizáveis.
- Revogação imediata: apagar a linha. Sem estado no cliente além do cookie.

## Tabela

```sql
CREATE TABLE sessoes (
  token_hash  bytea       PRIMARY KEY,                  -- sha256(token)
  usuario_id  bigint      NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  criada_em   timestamptz NOT NULL DEFAULT now(),
  expira_em   timestamptz NOT NULL,                     -- expiração por inatividade
  expira_max  timestamptz NOT NULL                      -- teto absoluto
);
CREATE INDEX sessoes_usuario_id_idx ON sessoes (usuario_id);   -- toda FK com índice
CREATE INDEX sessoes_expira_em_idx  ON sessoes (expira_em);    -- limpeza
```

Limpeza periódica por task com dono (`limpar_sessoes`, ver `async-concorrencia.md`).

## Hash de senha (argon2) em `spawn_blocking`

```rust
// src/auth/senha.rs
use argon2::{
    password_hash::{rand_core::OsRng, PasswordHash, PasswordHasher, PasswordVerifier, SaltString},
    Argon2,
};
use std::sync::LazyLock;

pub fn hash_senha(senha: &str) -> Result<String, argon2::password_hash::Error> {
    let sal = SaltString::generate(&mut OsRng);
    Ok(Argon2::default().hash_password(senha.as_bytes(), &sal)?.to_string())
}

pub fn senha_confere(hash: &str, senha: &str) -> bool {
    match PasswordHash::new(hash) {
        Ok(parsed) => Argon2::default().verify_password(senha.as_bytes(), &parsed).is_ok(),
        Err(_) => false,
    }
}

/// Hash válido de uma senha qualquer, para igualar o tempo quando o usuário não existe.
static HASH_FALSO: LazyLock<String> = LazyLock::new(|| {
    hash_senha("senha-inexistente").unwrap_or_default()   // calculado uma vez, no primeiro uso
});

/// Versões async: argon2 é CPU pesada e NUNCA roda direto no runtime.
pub async fn hash_senha_async(senha: String) -> Result<String, AuthErro> {
    tokio::task::spawn_blocking(move || hash_senha(&senha))
        .await.map_err(|e| AuthErro::Interno(e.into()))?
        .map_err(|e| AuthErro::Interno(e.to_string().into()))
}

pub async fn senha_confere_async(hash: String, senha: String) -> bool {
    tokio::task::spawn_blocking(move || senha_confere(&hash, &senha)).await.unwrap_or(false)
}
```

- `argon2` (RustCrypto) com `Argon2::default()` usa **Argon2id** e parâmetros padrão da
  biblioteca; conferir contra a recomendação atual da OWASP e ajustar com
  `Argon2::new(Algorithm::Argon2id, Version::V0x13, Params::new(m, t, p, None)?)`. O hash em
  formato PHC (`$argon2id$v=19$m=...`) guarda sal e parâmetros: permite migrar parâmetros no
  futuro (rehash no login quando os parâmetros antigos).
- `bcrypt` (crate `bcrypt`) é alternativa simples (limite de 72 bytes na senha). Não use
  SHA-256/MD5 para senha.
- **`spawn_blocking`** obrigatório: o hash leva dezenas/centenas de ms de CPU.
- Limite de tamanho da senha (ex.: 10 a 128 caracteres) **antes** de hashear (evita DoS por
  senha gigante).
- Guarde só o hash (`senha_hash`); nunca logue senha (`#[instrument(skip(senha))]`).
- `unwrap_or_default` / `unwrap_or(false)` acima não entram em pânico; o `unwrap_or(false)`
  trata `JoinError` como "senha não confere" (aceitável) e deve ser logado em código real.

## Token de sessão

```rust
use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
use rand::RngCore;                       // rand 0.8 (em 0.9 a API mudou: conferir)
use sha2::{Digest, Sha256};

pub fn novo_token() -> (String, Vec<u8>) {
    let mut bytes = [0u8; 32];
    rand::rngs::OsRng.fill_bytes(&mut bytes);       // CSPRNG do sistema operacional
    let token = URL_SAFE_NO_PAD.encode(bytes);
    let hash = hash_do_token(&token);
    (token, hash)
}

pub fn hash_do_token(token: &str) -> Vec<u8> {
    Sha256::digest(token.as_bytes()).to_vec()
}
```

- CSPRNG do sistema (`OsRng`), nunca gerador não criptográfico. 256 bits: não adivinhável.
- O token só existe em claro no cookie; no banco, o hash (a busca é por igualdade de hash:
  sem comparação de strings sujeita a temporização).

## Login

```rust
// src/auth/service.rs
pub struct SessaoNova { pub token: String, pub expira: DateTime<Utc> }

pub async fn login(pool: &PgPool, cfg: &SessaoCfg, email: &str, senha: String) -> Result<SessaoNova, ApiError> {
    let email = email.trim().to_lowercase();
    let usuario = sqlx::query!(
        "SELECT id, senha_hash, ativo FROM usuarios WHERE email = $1", email
    ).fetch_optional(pool).await.map_err(|e| ApiError::Interno(Box::new(e)))?;

    // mesmo custo de tempo exista ou não o usuário
    let (id, hash, ativo) = match usuario {
        Some(u) => (Some(u.id), u.senha_hash, u.ativo),
        None => (None, HASH_FALSO.clone(), false),
    };
    let confere = senha_confere_async(hash, senha).await;
    let Some(id) = id.filter(|_| confere && ativo) else {
        return Err(ApiError::NaoAutenticado);   // mesma resposta para qualquer causa
    };

    let (token, token_hash) = novo_token();     // sessão NOVA a cada login: sem fixação
    let agora = Utc::now();
    let expira = agora + cfg.ttl_idle;
    sqlx::query!(
        "INSERT INTO sessoes (token_hash, usuario_id, expira_em, expira_max) VALUES ($1, $2, $3, $4)",
        token_hash, id, expira, agora + cfg.ttl_max
    ).execute(pool).await.map_err(|e| ApiError::Interno(Box::new(e)))?;
    Ok(SessaoNova { token, expira })
}
```

Handler:

```rust
pub async fn login_handler(
    State(estado): State<Estado>,
    jar: CookieJar,
    ApiJson(req): ApiJson<LoginRequest>,
) -> Result<(CookieJar, StatusCode), ApiError> {
    let s = auth::service::login(&estado.pool, &estado.config.sessao, &req.email, req.senha).await?;
    let cookie = Cookie::build(("sessao", s.token))
        .path("/")
        .http_only(true)                                  // JS não lê
        .secure(estado.config.cookie_seguro)              // true em produção (HTTPS); false só em dev local
        .same_site(SameSite::Lax)                         // não enviado em POST cross-site
        .max_age(time::Duration::seconds(estado.config.sessao.ttl_idle.num_seconds()))
        .build();
    Ok((jar.add(cookie), StatusCode::NO_CONTENT))
}
```

- `CookieJar` vem de `axum-extra` (feature `cookie`); `Cookie::build((nome, valor))` é a API
  do crate `cookie` 0.18 (conferir). `jar.add` devolve o jar para compor a resposta.
- Mesmo status/mensagem para "usuário inexistente", "senha errada" e "conta inativa".
- Força bruta: contador por e-mail/IP no PostgreSQL com bloqueio temporário (429); se
  ausente, registrar como achado.
- Cookie com `Path=/`, `HttpOnly`, `Secure` (produção), `SameSite=Lax`, sem `Domain`. O
  prefixo `__Host-sessao` é endurecimento extra opcional.
- Logout: `DELETE FROM sessoes WHERE token_hash = $1` e `jar.remove(Cookie::build("sessao").path("/"))`;
  método `POST`.

## Resolver a sessão (extractor)

O extractor `UsuarioAtual` (esqueleto em `middlewares-paginacao.md`) chama:

```rust
pub async fn usuario_da_sessao(pool: &PgPool, cfg: &SessaoCfg, token: &str) -> Result<UsuarioAtual, ApiError> {
    let linha = sqlx::query!(
        r#"UPDATE sessoes s
              SET expira_em = LEAST(now() + make_interval(secs => $2), s.expira_max)
             FROM usuarios u
            WHERE s.token_hash = $1
              AND s.expira_em  > now()
              AND s.expira_max > now()
              AND u.id = s.usuario_id AND u.ativo
        RETURNING u.id AS "id!", u.papel AS "papel!""#,
        hash_do_token(token), cfg.ttl_idle.num_seconds() as f64
    )
    .fetch_optional(pool).await
    .map_err(|e| ApiError::Interno(Box::new(e)))?;      // banco fora = 5xx, nunca 401

    let l = linha.ok_or(ApiError::NaoAutenticado)?;
    Ok(UsuarioAtual { id: l.id, papel: Papel::try_from(l.papel.as_str()).map_err(|_| ApiError::NaoAutenticado)? })
}
```

- Uma consulta valida e renova (expiração deslizante limitada pelo teto `expira_max`).
  Escrever a cada request é simples; se pesar, renovar só quando faltar menos da metade do TTL.
- Sem linha -> 401. Falha de **infraestrutura** é 5xx, nunca 401.
- O `Papel` é `enum` (`Usuario`, `Admin`) com `TryFrom<&str>`; coluna `papel` com `CHECK`.

## CSRF

Cookie é enviado automaticamente pelo navegador: defesa em camadas.

1. **`SameSite=Lax`** no cookie: bloqueia o envio em POST cross-site.
2. **Verificação de origem** em métodos que mudam estado: rejeitar quando `Origin` (ou
   `Sec-Fetch-Site`) indica outro site.
3. CORS restritivo (só a origem do front; `cabecalhos-cors` em `revisao-seguranca`).

```rust
pub async fn proteger_csrf(
    State(estado): State<Estado>,
    req: Request,
    next: Next,
) -> Result<Response, ApiError> {
    if matches!(*req.method(), Method::GET | Method::HEAD | Method::OPTIONS) {
        return Ok(next.run(req).await);
    }
    let h = req.headers();
    let site_ok = h.get("sec-fetch-site").and_then(|v| v.to_str().ok())
        .map_or(true, |s| s == "same-origin" || s == "none");
    let origem_ok = h.get(header::ORIGIN).and_then(|v| v.to_str().ok())
        .map_or(true, |o| o == estado.config.origem_permitida);
    if !(site_ok && origem_ok) {
        return Err(ApiError::Proibido);
    }
    Ok(next.run(req).await)
}
// .layer(axum::middleware::from_fn_with_state(estado.clone(), proteger_csrf))
```

- Reforço: **token CSRF** (valor aleatório por sessão em `X-CSRF-Token`, comparado em tempo
  constante com `subtle::ConstantTimeEq`) se houver clientes sem `Origin`/`Sec-Fetch-Site`;
  para navegador moderno com `SameSite=Lax`, a verificação de origem costuma bastar (ADR).
- `GET` **nunca** muda estado. Requisição sem `Origin` (curl, testes) passa: o cookie
  `SameSite` continua protegendo o navegador.

## Autorização: papel e dono do recurso

Papel por rota não basta: A não pode cancelar a reserva de B (IDOR). Checar **no serviço**,
perto do dado:

```rust
pub async fn cancelar(pool: &PgPool, u: &UsuarioAtual, id: i64) -> Result<(), ReservaErro> {
    let r = repository::buscar(pool, id).await?.ok_or(ReservaErro::NaoEncontrada)?;
    if r.usuario_id != u.id && u.papel != Papel::Admin {
        return Err(ReservaErro::NaoEncontrada);   // 404: não revela que existe
    }
    repository::remover(pool, id).await?;
    Ok(())
}
```

- Decida e documente (ADR/openapi) 403 ou 404 para recurso de outro usuário; 404 evita
  enumerar IDs.
- O dono vem **sempre** de `UsuarioAtual` (a sessão), nunca do corpo ou da URL (mass
  assignment): o `CriarReservaRequest` não tem `usuario_id`.
- Listagens filtram por dono no SQL (`WHERE usuario_id = $1`); rotas administrativas conferem
  o papel e o serviço também confere.
- Tipo como guarda: função que exige permissão pode receber um tipo `Admin(UsuarioAtual)`
  produzido por um extractor `ExigeAdmin`, tornando o esquecimento um erro de compilação.

## Cabeçalhos e outras proteções

- `X-Content-Type-Options: nosniff`, `Referrer-Policy`, CSP e HSTS no proxy reverso ou em
  `from_fn` (ver `middlewares-paginacao.md`).
- CORS só com origens diferentes; nunca `*` com credenciais.
- Segredos só por variável de ambiente; comparar segredos com `subtle`; `cargo audit` e
  `cargo deny` no CI.
- Erros sem detalhe interno para o cliente (ver `ApiError::Interno`).

## Erros comuns

| Erro | Correção |
|---|---|
| Token de sessão em claro no banco | Guardar SHA-256 do token |
| Gerador não criptográfico para token | `OsRng`, 32 bytes |
| Reaproveitar a sessão anterior no login | Criar sessão **nova** a cada login |
| `argon2` direto no handler async | `spawn_blocking` |
| Mensagens/tempos diferentes para usuário inexistente e senha errada | Mesma resposta; hash falso para igualar tempo |
| Cookie sem `HttpOnly`/`Secure`/`SameSite` | Os três (Secure em produção) |
| Sem proteção CSRF por usar cookie | `SameSite=Lax` + verificação de origem/token |
| Só papel na rota | Checar dono do recurso no serviço |
| Banco fora vira 401 | Erro de infraestrutura é 5xx |
| Senha com SHA-256 | `argon2` (Argon2id) ou `bcrypt` |
| `derive(Serialize)` em struct com `senha_hash` | DTO de resposta separado |
