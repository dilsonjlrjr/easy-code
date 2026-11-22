# Middlewares e paginação (Go)

Continuação de `api-http-erros.md` (rotas, JSON, Problem Details, 409). `ResponderErro` e
`ErroHTTP` vêm de lá.

## Middlewares

Padrão: `func(http.Handler) http.Handler`, compostos de fora para dentro.

```go
func Encadear(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// RequestID garante um identificador por requisição: aproveita o X-Request-Id recebido
// (de um proxy) ou gera um; devolve na resposta e guarda no context para log e erros.
type chaveRequestID struct{}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 { // não confiar em tamanho arbitrário
			b := make([]byte, 8)
			_, _ = rand.Read(b) // crypto/rand
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chaveRequestID{}, id)))
	})
}

// Recuperar transforma panic em 500 sem derrubar o processo.
func Recuperar(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil && rec != http.ErrAbortHandler {
					log.ErrorContext(r.Context(), "panic", "valor", rec, "stack", string(debug.Stack()))
					ResponderErro(w, r, &ErroHTTP{Status: 500}, log)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Logar registra método, caminho, status e duração.
func Logar(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inicio := time.Now()
			rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			log.InfoContext(r.Context(), "http", "metodo", r.Method, "caminho", r.URL.Path,
				"status", rec.status, "duracao_ms", time.Since(inicio).Milliseconds())
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }
```

Ordem recomendada (de fora para dentro): `RequestID` -> `Logar` -> `Recuperar` ->
(CORS, se houver) -> rotas; `Autenticar` e `ProtegerCSRF` envolvem **só** as rotas protegidas
(ver `seguranca-sessao.md`).

- Log **não** inclui query string com dado sensível, cabeçalho `Cookie`, corpo de login.
- Para o `statusWriter` expor `Flush`/`Hijack` quando necessário, use
  `http.NewResponseController(w)` (Go 1.20+).
- Em log de acesso, o caminho **sem** o valor dos parâmetros (`r.Pattern`, Go 1.23+) evita
  cardinalidade alta; conferir o mínimo do `go.mod`.
- Autenticação via middleware coloca o usuário no `context` com chave de **tipo próprio**
  (`type chave struct{}`), nunca string.

## Paginação

```go
const (
	tamanhoPadrao = 20
	tamanhoMax    = 100
)

func paginacao(r *http.Request) (limite, deslocamento int, err error) {
	q := r.URL.Query()
	page, size := 0, tamanhoPadrao
	if v := q.Get("page"); v != "" {
		if page, err = strconv.Atoi(v); err != nil || page < 0 {
			return 0, 0, erro400("page deve ser inteiro >= 0")
		}
	}
	if v := q.Get("size"); v != "" {
		if size, err = strconv.Atoi(v); err != nil || size < 1 || size > tamanhoMax {
			return 0, 0, erro400("size deve estar entre 1 e 100")
		}
	}
	return size, page * size, nil
}
```

```sql
SELECT id, sala_id, usuario_id, inicio, fim, titulo
FROM reservas
WHERE usuario_id = $1
ORDER BY inicio, id          -- desempate por id: ordem determinística
LIMIT $2 OFFSET $3
```

- Sempre `ORDER BY` com desempate; teto de `size` no servidor.
- `OFFSET` degrada em tabelas grandes: paginação por cursor
  (`WHERE (inicio, id) > ($1, $2) ORDER BY inicio, id LIMIT n`). Em aplicação pequena, `OFFSET` basta.
- Envelope `{"itens": [...], "page": 0, "size": 20, "total": N}` conforme o `openapi.yaml`;
  conferir. Lista vazia é `[]`, nunca `null` (inicialize com `make([]T, 0)`).

## Erros comuns

| Erro | Correção |
|---|---|
| `recover` ausente: um `panic` derruba a requisição sem resposta | Middleware `Recuperar` |
| Logar cabeçalho `Cookie`, query com token ou corpo de login | Registrar só método, caminho, status e duração |
| Chave de contexto `string` | Tipo próprio não exportado |
| Middleware de auth aplicado a tudo (inclusive `/health` e login) | Envolver só as rotas protegidas |
| `ORDER BY` ausente na paginação | Ordem estável com desempate |
| Lista vazia serializada como `null` | `make([]T, 0)` |
