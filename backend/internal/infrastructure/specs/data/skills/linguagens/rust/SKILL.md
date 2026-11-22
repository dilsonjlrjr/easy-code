---
name: backend-rust
description: Convenções e boas práticas para backend Rust de API REST com PostgreSQL - stack padrão (axum sobre tokio, sqlx com sqlx migrate, serde, thiserror/anyhow, tracing, cargo fmt e clippy, testes com sqlx::test e testcontainers), estrutura lib mais bin com Router montado em lib.rs, extractors, validação, erro da API como enum com IntoResponse em Problem Details e tradução de sqlx::Error::Database por SQLSTATE para 409, tipos ricos (newtype, Option, Result, sem unwrap em produção), estado compartilhado, async correto (spawn_blocking para hash de senha), transações e macros verificadas em compilação com modo offline, autenticação com sessão em cookie e tabela no PostgreSQL com argon2, checagem por recurso e CSRF, middlewares tower e tower-http, graceful shutdown, testes com oneshot e banco real, configuração e observabilidade com tracing em JSON e Docker (multi-stage, cargo-chef, imagem mínima, não root). Use em Arquitetura, Dev, QA e Segurança em projetos com backend Rust.
---

# Backend Rust

API REST em Rust simples, idiomática e testável: tipos que tornam o estado inválido
impossível, SQL explícito verificado, erros como valores, constraints do PostgreSQL
protegendo as invariantes. O backend **é a fonte da regra e da autorização**; o front só
apresenta. Stack padrão abaixo; a Arquitetura pode mudar com ADR. Skills vizinhas (não
duplicadas aqui; esta skill dá só o "como em Rust"): `estrategia-testes` e
`qualidade-codigo` (Dev), `padroes-arquitetura-codigo`, `modelagem-postgresql`,
`ambiente-docker` e `arquitetura-web` (Arquitetura), `revisao-seguranca` (Segurança).

## Princípios

1. **Simplicidade primeiro.** Função, `struct` e `enum` antes de `trait`; sem generics nem
   `Box<dyn Trait>` "por precaução"; sem camada que só repassa chamada; sem macro própria.
2. **Handler fino, regra no serviço.** O handler extrai, valida formato, chama e devolve; a
   regra vive em função/tipo sem dependência de `axum`.
3. **O banco protege a invariante.** Unicidade, sobreposição e FK são constraints
   (`UNIQUE`, `EXCLUDE`, `FOREIGN KEY`); a aplicação traduz `sqlx::Error::Database` em 409.
   Checar "antes" sozinho é condição de corrida.
4. **Erros são valores.** `Result<T, E>` com `enum` de erro (`thiserror`), operador `?`,
   `#[from]` para converter. Sem `unwrap()`/`expect()` em produção, sem `panic!` em fluxo
   normal; `Option` para ausência, nunca valor sentinela.
5. **Tipos ricos na fronteira.** Parse, não valide: `struct` newtype (`Email`, `Titulo`,
   `Periodo`) só se constrói válida; o resto do código confia no tipo.
6. **Async sem bloquear.** Nada de CPU pesada ou E/S síncrona no runtime; hash de senha em
   `spawn_blocking`; toda tarefa em segundo plano tem dono e sinal de parada.
7. **Compilador e clippy como primeira revisão.** `cargo clippy -- -D warnings`, sem
   `#[allow]` sem comentário.
8. **Segurança por padrão.** Sessão em cookie `HttpOnly`, hash adaptativo, checagem por
   recurso em todo endpoint protegido, segredos só por ambiente.

## Stack padrão

| Item | Padrão |
|---|---|
| Linguagem | Rust estável atual, edição atual; `rust-version` no `Cargo.toml` |
| HTTP | `axum` sobre `tokio`; `tower` / `tower-http` para middlewares |
| Banco | `sqlx` (feature `postgres`, `runtime-tokio`, TLS por `rustls`); SQL à mão, macros verificadas |
| Migração | `sqlx migrate` (`migrations/<timestamp>_nome.sql`), embutidas com `sqlx::migrate!()` |
| Serialização | `serde` / `serde_json`; `chrono` para datas |
| Erros | `thiserror` na biblioteca e na API; `anyhow` só na `main` |
| Teste | `cargo test`; integração com `#[sqlx::test]` ou `testcontainers` (PostgreSQL real) |
| Lint/formato | `cargo fmt` + `cargo clippy --all-targets -- -D warnings` |
| Logs | `tracing` + `tracing-subscriber` (JSON no stdout) |
| Config | Variáveis de ambiente lidas uma vez na `main` em uma `struct` |
| Sessão | Cookie `HttpOnly` + tabela `sessoes` no PostgreSQL (hash do token); `argon2` |

Versões: "estável atual"; as efetivas ficam em `agentes/ambiente.md` e no `Cargo.lock`. O
ecossistema muda rápido (ex.: sintaxe de caminho do axum `/:id` virou `/{id}` na 0.8):
conferir a documentação da versão fixada antes de copiar código.

## Alternativas e quando trocar

| Padrão | Alternativa | Trocar quando |
|---|---|---|
| `axum` | `actix-web` | Time já domina; precisa do ecossistema actix; custo: runtime e modelo de extractors próprios, fora do `tower` |
| `sqlx` | `diesel` (+ `diesel-async`) | Quer DSL tipada de consultas e schema gerado; custo: DSL para aprender, async por crate à parte, menos SQL visível |
| `sqlx` | `sea-orm` | CRUD volumoso e desejo de entidades/relacionamentos; custo: abstração sobre o SQL e constraints menos visíveis; só com ADR |
| SQL à mão | `query_as` em runtime | Consulta dinâmica (filtros opcionais); perde a verificação em compilação |
| validação manual | `validator` | Muitos DTOs com regras repetitivas de campo (conferir versão) |
| sessão manual | `tower-sessions` (+ store sqlx) | Quer reaproveitar expiração/renovação prontas (conferir) |
| `thiserror` | `anyhow` no domínio | Nunca na API: perde o tipo que mapeia para status |

Troca exige ADR; o catálogo do produto traz um padrão por linguagem.

## Regras de decisão

| Situação | Faça | Evite | Referência |
|---|---|---|---|
| Organizar código | `lib.rs` + `main.rs`; módulo por funcionalidade | Pastas por camada global (`models/`, `handlers/`) | estrutura-convencoes |
| Montar a aplicação | `pub fn app(estado) -> Router` em `lib.rs` | Montar rotas só na `main` (não testável) | estrutura-convencoes |
| Valor com regra (e-mail, período) | Newtype com `TryFrom`/`new` que valida | `String` solta validada em vários lugares | estrutura-convencoes |
| Ausência | `Option<T>`; serviço devolve `Err(NaoEncontrado)` | `unwrap()`, `Option` escondendo erro | estrutura-convencoes |
| Erro da API | `enum ApiError` + `IntoResponse` em Problem Details | `StatusCode` solto em cada handler | api-axum-erros |
| JSON inválido | Extractor próprio que devolve `ApiError` 400 | Rejeição padrão em texto puro | api-axum-erros |
| Violação de `EXCLUDE`/`UNIQUE` | `SQLSTATE` `23P01`/`23505` -> 409 | `SELECT` "existe?" antes | api-axum-erros |
| Middleware | `tower`/`tower-http` (`TraceLayer`, timeout, limite) | Middleware escrito à mão sem necessidade | middlewares-paginacao |
| Hash de senha / CPU | `tokio::task::spawn_blocking` | `argon2` direto no handler async | async-concorrencia |
| Encerrar | `with_graceful_shutdown` + sinal | Matar o processo com requests em voo | async-concorrencia |
| Várias escritas atômicas | `pool.begin()` ... `commit()` | Escritas soltas no pool | persistencia-sqlx |
| Consulta fixa | `query_as!` (verificada) + `.sqlx` offline | Concatenar SQL com entrada | persistencia-sqlx |
| Login | Sessão em cookie + tabela no banco | Token em `localStorage` | seguranca-sessao |
| Autorização | Papel **e** dono do recurso | Só middleware por rota | seguranca-sessao |
| Teste da regra | Função pura, sem banco | Banco para testar `if` | testes-rust |
| Teste de constraint/SQL | `#[sqlx::test]`/testcontainers com PostgreSQL | SQLite/mock do driver | testes-rust |

## Estrutura (resumo)

```
backend/
  Cargo.toml  rust-toolchain.toml  .sqlx/  (consultas verificadas, versionado)
  src/
    main.rs            composição: config, tracing, pool, migrações, servidor
    lib.rs             pub fn app(Estado) -> Router; módulos
    config.rs  erro.rs  estado.rs
    reserva/   mod.rs handler.rs service.rs repository.rs modelo.rs
    sala/  auth/
  migrations/  20250101000000_criar_salas.sql ...
  tests/               integração (Router + banco real)
```

Detalhe em `references/estrutura-convencoes.md`.

## Fluxo de trabalho (Dev, por T de backend)

1. Ler a T, o `openapi.yaml`, as constraints do `docs/dados/esquema.sql` e os CT.
2. Migração primeiro (se a T muda o schema); nunca editar migração já aplicada;
   `cargo sqlx prepare` para atualizar `.sqlx/`.
3. Teste primeiro (vermelho): regra pura; depois integração do endpoint (`oneshot`).
4. Implementar: tipos de domínio -> serviço -> repositório -> handler fino.
5. Mapear erros: JSON/validação -> 400, regra violada -> 409/404, sem sessão -> 401, sem
   permissão -> 403, tudo em Problem Details.
6. Rodar `cargo fmt`, `cargo clippy --all-targets -- -D warnings`, `cargo test`.
7. Conferir `openapi.yaml`, a checklist abaixo e registrar o que for na T.

## Comandos

| Ação | Comando |
|---|---|
| Formatar | `cargo fmt` (CI: `cargo fmt --check`) |
| Lint | `cargo clippy --all-targets -- -D warnings` |
| Testes | `cargo test` (PostgreSQL via `DATABASE_URL` para `#[sqlx::test]`) |
| Compilar sem rodar | `cargo check --all-targets` |
| Migrar | `sqlx migrate add -r nome`; `sqlx migrate run` (CLI `sqlx-cli`) |
| Cache offline | `cargo sqlx prepare` (e `--check` no CI) |
| Rodar | `cargo run` |
| Auditoria de dependências | `cargo audit`, `cargo deny check` (open source, conferir) |
| Cobertura | `cargo llvm-cov` (open source, conferir) |

## Checklist de revisão Rust

- [ ] Handler só extrai, valida formato, delega e traduz; sem SQL nem regra nele.
- [ ] Sem `unwrap()`/`expect()`/`panic!`/indexação `[i]` em caminho de produção (exceto invariante comentada).
- [ ] Erros com `thiserror` e `#[from]`; `ApiError` mapeia cada variante a um status; nenhum `anyhow` na API.
- [ ] Tipos ricos: valor com regra é newtype; `Option` para ausência; sem `String` "mágica".
- [ ] Nenhum `std::sync::Mutex` segurado atravessando `.await`; nenhuma E/S bloqueante no runtime.
- [ ] Hash de senha em `spawn_blocking`; resposta de login igual para usuário inexistente e senha errada.
- [ ] Invariantes como constraint; `SQLSTATE` `23P01`/`23505`/`23503` traduzido em 409.
- [ ] SQL parametrizado (`$1`), nunca `format!` com entrada; `.sqlx/` atualizado e `prepare --check` verde.
- [ ] Transação com `begin()`/`commit()` (rollback no `Drop`); sem `.await` de rede dentro dela.
- [ ] JSON com limite de corpo; extractor próprio devolve Problem Details; campos que o cliente não define não existem no request.
- [ ] Sessão: cookie `HttpOnly`/`Secure`/`SameSite`, token aleatório, só o hash no banco; CSRF tratado.
- [ ] Todo endpoint protegido checa o dono do recurso; 401 x 403/404 corretos.
- [ ] Servidor com timeout, limite de corpo e graceful shutdown; tarefas em segundo plano com dono.
- [ ] `clippy -D warnings` e `fmt --check` verdes; `cargo audit` sem pendência crítica.
- [ ] Config por ambiente lida na `main`; sem segredo em código; logs sem senha/token (`#[instrument(skip(...))]`).
- [ ] Imagem Docker multi-stage, mínima, usuário não root.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `SELECT` de conflito e depois `INSERT` | Duas requisições passam; dado inválido | Constraint + traduzir `SQLSTATE` em 409 |
| `.unwrap()` em `Option`/`Result` de entrada | `panic` derruba a task; 500 | `?` com erro de domínio; `ok_or(Erro::...)` |
| `argon2` direto no handler | Bloqueia a thread do runtime; latência de todos | `spawn_blocking` |
| `MutexGuard` (std) mantido em `.await` | `Future` não-`Send` ou deadlock | Reduzir o escopo do lock antes do `.await` |
| `format!("... {}", entrada)` no SQL | Injeção de SQL | `query_as!`/binds `$1` |
| Esquecer `cargo sqlx prepare` | Build no CI/Docker falha sem banco | Versionar `.sqlx/`; `SQLX_OFFLINE=true` |
| Comparar `err.to_string()` | Quebra com tradução/wrap | Casar variante do `enum`; `SQLSTATE` por código |
| Rota com sintaxe da versão errada (`/:id` x `/{id}`) | 404 em tudo / panic na subida | Conferir a versão do axum |
| `clone()` para calar o borrow checker | Cópia cara ou lógica errada | Rever propriedade; emprestar ou mover |
| Token de sessão em texto no banco | Vazamento do banco = sessões roubadas | Guardar SHA-256 do token |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/estrutura-convencoes.md` | Ao criar o projeto: `Cargo.toml`, módulos, `lib.rs`, newtypes, erros de domínio, nomes, clippy |
| `references/api-axum-erros.md` | Para rotas, extractors, JSON, validação, Problem Details e 409 por constraint |
| `references/middlewares-paginacao.md` | Para camadas tower/tower-http, usuário autenticado como extractor e paginação |
| `references/async-concorrencia.md` | Para tokio, não bloquear o runtime, `spawn_blocking`, tarefas, graceful shutdown |
| `references/persistencia-sqlx.md` | Para pool, `query_as!`, modo offline, transações, migrações e alternativas |
| `references/seguranca-sessao.md` | Para login, sessão em cookie, `argon2`, autorização por recurso e CSRF |
| `references/testes-rust.md` | Para testes unitários, `oneshot`, `#[sqlx::test]`, testcontainers e fakes |
| `references/config-observabilidade-docker.md` | Para configuração por ambiente, `tracing`, healthcheck e Docker |
