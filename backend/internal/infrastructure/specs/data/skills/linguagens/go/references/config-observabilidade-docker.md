# Configuração, observabilidade, lint e Docker (Go)

Base de ambiente e Compose: `ambiente-docker`. Logs e segredos em geral: `qualidade-codigo`.
Aqui: o "como em Go".

## Configuração por ambiente

Ler o ambiente **uma vez**, na `main`, num struct tipado; validar e falhar cedo.

```go
// internal/plataforma/config/config.go
package config

type Config struct {
	Porta         string
	DatabaseURL   string
	OrigemFront   string // origem permitida (CORS/CSRF)
	CookieSeguro  bool
	LogNivel      slog.Level
}

func Carregar() (Config, error) {
	var erros []error
	obrig := func(chave string) string {
		v := os.Getenv(chave)
		if v == "" {
			erros = append(erros, fmt.Errorf("variável %s é obrigatória", chave))
		}
		return v
	}
	opc := func(chave, padrao string) string {
		if v := os.Getenv(chave); v != "" {
			return v
		}
		return padrao
	}

	cfg := Config{
		Porta:        opc("PORT", "8080"),
		DatabaseURL:  obrig("DATABASE_URL"), // sem padrão: falha se faltar
		OrigemFront:  opc("CORS_ORIGEM", "http://localhost:5173"),
		CookieSeguro: opc("COOKIE_SEGURO", "true") == "true", // seguro por padrão
	}
	if err := cfg.LogNivel.UnmarshalText([]byte(opc("LOG_NIVEL", "info"))); err != nil {
		erros = append(erros, fmt.Errorf("LOG_NIVEL inválido: %w", err))
	}
	return cfg, errors.Join(erros...) // todos os problemas de uma vez
}
```

- **Segredo só por ambiente** (`DATABASE_URL` com senha): `.env` fora do Git, `.env.example`
  versionado sem valores reais; no Compose, `env_file`/`environment`.
- Padrão seguro: `CookieSeguro` verdadeiro por padrão; só o ambiente de dev local sem TLS o
  desliga, explicitamente.
- Nada de `os.Getenv` espalhado pelo código: só em `config`. O resto recebe o valor pronto.
- Sem biblioteca de configuração no padrão (Viper e afins) — o struct acima basta; troque só
  se houver arquivo de config em vários formatos de verdade.
- Nunca logue a `Config` inteira (tem senha); logue campos seguros.

## Logs estruturados com slog

```go
log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogNivel}))
slog.SetDefault(log) // opcional; preferir injetar *slog.Logger

log.InfoContext(ctx, "reserva criada", "reserva_id", r.ID, "sala_id", r.SalaID)
log.ErrorContext(ctx, "falha ao criar reserva", "erro", err)
```

- JSON no **stdout** (12-factor); o Docker/orquestrador coleta. Sem arquivo de log na imagem.
- Pares chave/valor (ou `slog.Attr`/`slog.Group`), nunca interpolação em string:
  `"reserva_id", id`, não `fmt.Sprintf`.
- Níveis: `Info` para evento de negócio relevante, `Warn` para recuperável, `Error` para falha
  (com o erro), `Debug` só fora de produção.
- Correlação: middleware coloca `request_id` no `context` e um `slog.Handler` próprio (ou
  `log.With("request_id", id)` por requisição) o inclui em toda linha; devolva o
  `X-Request-Id` na resposta e no Problem Details de 500 (ver `middlewares-paginacao.md`).
- **Nunca** logar: senha, cookie `sessao`, token, corpo de login, dado pessoal completo.
  Para valor sensível implemente `slog.LogValuer` que o oculta.
- Logar **uma vez**, onde o erro é tratado (o handler/`main`), não em cada camada que o
  repassa.
- `context.Canceled` por desconexão do cliente: nível `Debug`/`Info`, não `Error`.

## Health e observabilidade mínima

```go
mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { // liveness: o processo está de pé
	w.WriteHeader(http.StatusNoContent)
})
mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) { // readiness: o banco responde
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		http.Error(w, "banco indisponível", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
})
```

- Rotas de saúde **fora** da autenticação, sem detalhe interno na resposta.
- Healthcheck do Compose usa `/ready` (inclui banco). Imagem `distroless` não tem `curl`/`wget`:
  implemente o healthcheck no próprio binário (`api -healthcheck`, que faz um GET em
  `localhost` e sai 0/1) e use `test: ["CMD", "/app/api", "-healthcheck"]`.
- Métricas (Prometheus) e tracing (OpenTelemetry) só se houver RNF; em aplicação pequena, logs e health
  bastam. `net/http/pprof` **nunca** exposto publicamente (porta separada/local).
- Expor estatística do pool (`pool.Stat()`) em log periódico ajuda a achar vazamento de
  conexão.

## Lint e formato

```yaml
# .golangci.yml (formato da v2; conferir chaves na versão adotada)
version: "2"
linters:
  enable:
    - errcheck        # erro ignorado
    - govet
    - staticcheck
    - ineffassign
    - unused
    - errorlint       # %w, errors.Is/As em vez de ==/type assertion
    - bodyclose       # http.Response.Body fechado
    - noctx           # requisição HTTP sem context
    - sqlclosecheck   # rows/stmt fechados
    - rowserrcheck    # rows.Err() conferido
    - gosec           # problemas de segurança comuns
    - misspell
formatters:
  enable:
    - gofmt
    - goimports
```

- Rode `go vet ./...` e `golangci-lint run` no CI; lint verde é critério de pronto.
- `gofmt` não é discussão: ninguém formata à mão.
- Verificação de nulidade (opcional): `nilaway` (open source) acha dereferência de `nil`
  possível; adote com critério.
- Vulnerabilidades: `govulncheck ./...` (conferir) no CI; ver `revisao-seguranca`
  (varreduras open source).
- `//nolint` só com a regra e a justificativa (`//nolint:gosec // valor fixo, não vem de entrada`).

## Docker

```dockerfile
# syntax=docker/dockerfile:1
# --- build ---
FROM golang:<versão> AS build                 # mesma versão do go.mod (conferir a tag)
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download                           # camada de cache das dependências
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# --- execução ---
FROM gcr.io/distroless/static-debian12:nonroot   # conferir a tag atual; já usa usuário não root
COPY --from=build /out/api /app/api
USER nonroot:nonroot
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/app/api"]
```

- **Multi-stage**: o toolchain Go só no build; a imagem final tem **só o binário** (estático,
  `CGO_ENABLED=0`).
- **`distroless/static` (tag `nonroot`)**: sem shell, sem gerenciador de pacotes, com
  certificados CA e `/etc/passwd` mínimo; menor superfície. `scratch` também serve, mas
  exige copiar certificados (`/etc/ssl/certs/ca-certificates.crt`) e criar o usuário.
- **Não root**: `USER nonroot`; porta > 1024.
- Migrações **embutidas** no binário (`embed`): a imagem é autossuficiente.
- Tag de imagem fixa, não `latest`; `.dockerignore`: `.git`, `.env`, `*.md`, binários.
- `-trimpath -ldflags="-s -w"`: binário reproduzível e menor (sem símbolos de depuração).
- `-race` não vale para o binário de produção (precisa de CGO); use nos testes
  (imagem/CI com toolchain C).
- Encerramento limpo: `ENTRYPOINT` em forma **exec** (o processo é o PID 1 e recebe `SIGTERM`);
  o `Shutdown` (ver `context-concorrencia.md`) cabe no `stop_grace_period` do Compose.
- Compose: a API `depends_on` o banco com `condition: service_healthy`; healthcheck do próprio
  binário; reinício `unless-stopped`; ver `ambiente-docker`.
- Varredura da imagem (Trivy, open source) no CI antes do gate de segurança.

## Checklist deste arquivo

- [ ] Configuração lida só em `config.Carregar`; falta de variável obrigatória derruba a subida.
- [ ] Nenhum segredo em código, Dockerfile ou log; `.env.example` sem valores reais.
- [ ] Logs JSON no stdout com `request_id`, sem dado sensível.
- [ ] `/health` e `/ready` fora da autenticação; healthcheck sem depender de `curl` na imagem.
- [ ] `golangci-lint run` e `go vet` verdes; `govulncheck` no CI.
- [ ] Imagem multi-stage, binário estático, distroless/scratch, usuário não root, tag fixa.

## Erros comuns

| Erro | Correção |
|---|---|
| `os.Getenv` espalhado | Struct `Config` lido na `main` |
| Segredo no Dockerfile/imagem | Variável de ambiente em tempo de execução |
| Imagem final com toolchain Go | Multi-stage, só o binário |
| Contêiner como root | `USER nonroot` |
| Healthcheck com `curl` em imagem sem `curl` | Subcomando `-healthcheck` no binário |
| `fmt.Println` como log | `slog` JSON |
| Logar a `Config` inteira | Logar só campos seguros |
| `pprof` exposto publicamente | Porta local/separada |
