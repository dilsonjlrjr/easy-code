---
name: backend-go
description: Convenções e boas práticas para backend Go de API REST com PostgreSQL - stack padrão (net/http com ServeMux 1.22+, pgx/pgxpool, golang-migrate, testing e testcontainers-go, golangci-lint, slog), estrutura cmd/internal por funcionalidade, interfaces pequenas no consumidor, erros idiomáticos (wrap, sentinelas, errors.Is/As, nunca nil nil), JSON e validação, middlewares, Problem Details e tradução de pgconn.PgError para 409, context, timeouts e graceful shutdown, transações com pgx, autenticação com sessão em cookie e tabela no PostgreSQL, hash de senha, checagem por recurso, CSRF, concorrência segura com race detector, testes de tabela, httptest e integração, configuração por ambiente e Docker (multi-stage, distroless, não root). Use em Arquitetura, Dev, QA e Segurança em projetos com backend Go.
---

# Backend Go

API REST em Go simples, idiomática e testável: biblioteca padrão primeiro, SQL explícito,
erros como valores, constraints do PostgreSQL protegendo as invariantes. O backend **é a
fonte da regra e da autorização**; o front só apresenta. Stack padrão abaixo; a Arquitetura
pode mudar com ADR. Skills vizinhas (não duplicadas aqui; esta skill dá só o "como em Go"):
`estrategia-testes` e `qualidade-codigo` (Dev), `padroes-arquitetura-codigo`,
`modelagem-postgresql`, `ambiente-docker` e `arquitetura-web` (Arquitetura),
`revisao-seguranca` (Segurança).

## Princípios

1. **Simplicidade primeiro.** Função e struct simples antes de abstração; sem interface
   "por precaução"; sem camada que só repassa chamada; sem framework onde a stdlib basta.
2. **Handler fino, regra no serviço.** O handler decodifica, valida formato, chama e
   traduz o resultado; a regra vive em tipo sem dependência de `net/http`.
3. **O banco protege a invariante.** Unicidade, sobreposição e FK são constraints
   (`UNIQUE`, `EXCLUDE`, `FOREIGN KEY`); a aplicação traduz o `pgconn.PgError` em 409.
   Checar "antes" sozinho é condição de corrida.
4. **Erros são valores.** Retornar `error`, embrulhar com contexto (`%w`), decidir com
   `errors.Is/As`. Nunca `(nil, nil)`; nunca `panic` para fluxo normal.
5. **Interfaces pequenas, definidas onde são usadas** (no consumidor), com 1-3 métodos.
   "Aceite interfaces, devolva structs".
6. **`context.Context` em toda E/S**, primeiro parâmetro, propagado até o banco; timeouts no
   servidor e no pool.
7. **Concorrência com dono e fim.** Toda goroutine tem quem a espere e como pará-la; teste
   com `-race`.
8. **Segurança por padrão.** Sessão em cookie `HttpOnly`, hash adaptativo, checagem por
   recurso em todo endpoint protegido, segredos só por ambiente.

## Stack padrão

| Item | Padrão |
|---|---|
| Linguagem | Go estável atual, módulos (`go.mod`) |
| HTTP | `net/http` com roteamento por método e caminho do `ServeMux` (Go 1.22+) |
| Banco | `pgx/v5` com `pgxpool`; SQL à mão por padrão, `sqlc` por critério |
| Migração | `golang-migrate` (`migrations/000001_nome.up.sql` / `.down.sql`), embutidas no binário |
| Teste | `testing` + `net/http/httptest`; integração com `testcontainers-go` (PostgreSQL real) |
| Lint/formato | `gofmt` + `go vet` + `golangci-lint` |
| Logs | `log/slog` (JSON no stdout) |
| Config | Variáveis de ambiente lidas uma vez na `main` |
| Sessão | Cookie `HttpOnly` + tabela `sessoes` no PostgreSQL (hash do token); `golang.org/x/crypto/bcrypt` |

Versões: "estável atual"; as efetivas ficam em `agentes/ambiente.md`. A stdlib evolui (roteamento
1.22, `slog` 1.21, `http.CrossOriginProtection` 1.25): conferir o mínimo exigido no `go.mod`.

## Alternativas e quando trocar

| Padrão | Alternativa | Trocar quando |
|---|---|---|
| `ServeMux` 1.22+ | `chi` | Precisa de grupos de rotas com middleware por grupo e `Mount` com frequência; é compatível com `net/http` e a troca é barata |
| `ServeMux` 1.22+ | `echo`, `gin`, `fiber` | Time já padronizado neles; custo: handlers fora da assinatura `http.Handler` (principalmente `fiber`) e mais dependência |
| SQL à mão | `sqlc` | Muitas consultas e desejo de tipos gerados e checados contra o schema; custo: etapa de geração no build |
| `pgx` | `database/sql` + `pgx/stdlib`, `sqlx` | Precisa de compatibilidade com outras ferramentas que exigem `database/sql`; perde recursos nativos do pgx |
| `pgx` | `gorm`, `ent` | Quase nunca no padrão: ORM esconde SQL e constraints; só com ADR e ganho claro |
| `golang-migrate` | `goose`, `atlas` | Preferência do time por migrações em Go ou por diff declarativo (Atlas) |
| validação manual | `go-playground/validator` | Muitos DTOs com regras repetitivas de campo |

Troca exige ADR; o catálogo do produto traz um padrão por linguagem.

## Regras de decisão

| Situação | Faça | Evite | Referência |
|---|---|---|---|
| Organizar código | `cmd/api` + `internal/<funcionalidade>` | `pkg/` gigante, pastas por camada global | estrutura-convencoes |
| Dependência de banco no serviço | Interface pequena **no pacote do serviço** | Interface grande no pacote do repositório | estrutura-convencoes |
| Erro de domínio | Sentinela/tipo + `errors.Is/As` no handler | Comparar texto da mensagem | estrutura-convencoes |
| Não encontrado | Devolver `ErrNaoEncontrado` | `(nil, nil)` | estrutura-convencoes |
| Decodificar JSON | `MaxBytesReader` + `DisallowUnknownFields` | `json.Unmarshal` sem limite | api-http-erros |
| Violação de `EXCLUDE`/`UNIQUE` | `errors.As(err, &pgErr)`, `23P01`/`23505` -> 409 | `SELECT` "existe?" antes | api-http-erros |
| Chamada que faz E/S | `ctx` do request, primeiro parâmetro | `context.Background()` no meio | context-concorrencia |
| Servidor HTTP | Timeouts + `Shutdown` com sinal | `http.ListenAndServe` cru | context-concorrencia |
| Várias escritas atômicas | Transação com `pgx.BeginFunc`/`Begin` | Escritas soltas | persistencia-pgx |
| Login | Sessão em cookie + tabela no banco | Token em `localStorage` | seguranca-sessao |
| Autorização | Papel **e** dono do recurso | Só middleware por rota | seguranca-sessao |
| Concorrência | `errgroup`, `sync`, canais com dono | `go func()` solto | context-concorrencia |
| Teste da regra | Tabela, sem banco | Banco para testar `if` | testes-go |
| Teste de constraint/SQL | `testcontainers-go` com PostgreSQL | SQLite/mock do driver | testes-go |

## Estrutura (resumo)

```
backend/
  cmd/api/main.go            composição: config, pool, rotas, servidor
  internal/
    reserva/   handler.go service.go repository.go reserva.go erros.go
    sala/  auth/
    plataforma/  http/ (middlewares, erros, json)  db/ (pool, migrações)
  migrations/  000001_criar_salas.up.sql ...
  go.mod
```

Detalhe em `references/estrutura-convencoes.md`.

## Fluxo de trabalho (Dev, por T de backend)

1. Ler a T, o `openapi.yaml`, as constraints do `docs/dados/esquema.sql` e os CT.
2. Migração primeiro (se a T muda o schema); nunca editar migração já aplicada.
3. Teste primeiro (vermelho): tabela da regra; depois integração do endpoint.
4. Implementar: tipos de domínio -> serviço -> repositório -> handler fino.
5. Mapear erros: JSON/validação -> 400, regra violada -> 409/404, sem sessão -> 401, sem
   permissão -> 403, tudo em Problem Details.
6. Rodar `gofmt`, `go vet`, `golangci-lint run`, `go test -race ./...`.
7. Conferir `openapi.yaml`, a checklist abaixo e registrar o que for na T.

## Comandos

| Ação | Comando |
|---|---|
| Formatar | `gofmt -w .` (ou `golangci-lint fmt`, conferir) |
| Análise básica | `go vet ./...` |
| Lint | `golangci-lint run` |
| Testes (com race) | `go test -race ./...` |
| Só unitários rápidos | `go test -short ./...` |
| Cobertura | `go test -race -coverprofile=cover.out ./...` e `go tool cover -func=cover.out` |
| Rodar | `go run ./cmd/api` |
| Dependências | `go mod tidy`; `govulncheck ./...` (open source, conferir) |

## Checklist de revisão Go

- [ ] Handler só decodifica, valida formato, delega e traduz; sem SQL nem regra nele.
- [ ] Erros embrulhados com `%w`; sentinelas via `errors.Is/As`; nenhum `(nil, nil)`.
- [ ] Interfaces pequenas, no consumidor; sem interface com uma só implementação sem motivo.
- [ ] `ctx` primeiro parâmetro, propagado até o pgx; nenhum `context.Background()` no caminho do request.
- [ ] Servidor com `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` e `Shutdown`.
- [ ] Invariantes como constraint; `pgconn.PgError` traduzido por código (`23P01`/`23505`/`23503`) em 409.
- [ ] Todo `Rows` fechado (`defer rows.Close()`) e `rows.Err()` conferido; transação com `defer Rollback`.
- [ ] JSON com limite de corpo e campos desconhecidos recusados; erros em Problem Details.
- [ ] Sessão: cookie `HttpOnly`/`Secure`/`SameSite`, token aleatório, só o hash no banco; CSRF tratado.
- [ ] Hash de senha adaptativo; resposta de login igual para usuário inexistente e senha errada.
- [ ] Todo endpoint protegido checa o dono do recurso; 401 x 403/404 corretos.
- [ ] Nenhuma goroutine solta; `go test -race ./...` verde; sem variável global mutável.
- [ ] Config por ambiente lida na `main`; sem segredo em código; logs sem senha/token.
- [ ] Imagem Docker multi-stage, distroless/scratch, usuário não root.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `SELECT` de conflito e depois `INSERT` | Duas requisições passam; dado inválido | Constraint + traduzir `pgconn.PgError` em 409 |
| `return nil, nil` para "não achou" | `nil` pointer adiante, 500 | `ErrNaoEncontrado` |
| Comparar `err.Error() == "..."` | Quebra com wrap | `errors.Is/As` |
| `defer` dentro de laço longo | Recursos acumulam até o fim da função | Função auxiliar por iteração |
| Esquecer `rows.Close()`/`rows.Err()` | Vazamento de conexão, erro engolido | `defer rows.Close()`; checar `rows.Err()` |
| `http.ListenAndServe` sem timeouts | Slowloris, conexões presas | `http.Server` com timeouts |
| `context.Background()` no handler | Request cancelado continua no banco | `r.Context()` |
| Goroutine sem fim | Vazamento | `errgroup` + `ctx`; esperar sempre |
| Interface enorme no pacote do repositório | Mock difícil, acoplamento | Interface mínima no consumidor |
| Token de sessão em texto no banco | Vazamento do banco = sessões roubadas | Guardar SHA-256 do token |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/estrutura-convencoes.md` | Ao criar o projeto: módulo, pacotes, interfaces no consumidor, erros idiomáticos, nomes |
| `references/api-http-erros.md` | Para rotas, JSON, validação, Problem Details e 409 por constraint |
| `references/middlewares-paginacao.md` | Para middlewares (request id, log, recover), contexto do usuário e paginação |
| `references/context-concorrencia.md` | Para context, timeouts, graceful shutdown, goroutines, race detector |
| `references/persistencia-pgx.md` | Para pgx/pgxpool, consultas, transações, sqlc e migrações |
| `references/seguranca-sessao.md` | Para login, sessão em cookie, hash de senha, autorização por recurso e CSRF |
| `references/testes-go.md` | Para testes de tabela, httptest, integração com testcontainers e `-race` |
| `references/config-observabilidade-docker.md` | Para configuração por ambiente, slog, healthcheck, lint e Docker |
