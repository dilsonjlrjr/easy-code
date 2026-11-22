# API HTTP em Go: rotas, JSON, validação e erros

Contrato de erros e regra 400/409 vêm de `arquitetura-web` (Problem Details, RFC 9457). Aqui:
a implementação com `net/http`.

## Quem responde o quê

| Situação | Status | Onde nasce |
|---|---|---|
| JSON malformado, tipo errado, campo desconhecido, corpo grande demais | 400 (413 para corpo grande) | Decodificação |
| Campo inválido (vazio, fora da faixa, fim antes do início) | 400 | Validação na borda ou serviço |
| Sem sessão válida | 401 | Middleware de autenticação |
| Autenticado sem permissão no recurso | 403 (ou 404 para não revelar existência) | Serviço |
| Não existe | 404 | `ErrNaoEncontrado` |
| **Conflito com o estado** (sala já reservada, e-mail em uso) | **409** | Constraint traduzida |
| Falha inesperada | 500 | Middleware `recover` / erro não mapeado |

Regra: **400 = pedido inválido por si**; **409 = pedido válido que conflita com o estado**.

## Rotas com ServeMux (Go 1.22+)

```go
func (h *Handler) Registrar(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/reservas", h.criar)
	mux.HandleFunc("GET /api/reservas", h.listar)
	mux.HandleFunc("GET /api/reservas/{id}", h.buscar)
	mux.HandleFunc("DELETE /api/reservas/{id}", h.cancelar)
}
```

- O padrão `"METODO /caminho/{param}"` casa método e caminho; `r.PathValue("id")` lê o
  parâmetro. Método não previsto no caminho existente devolve **405** com `Allow`
  automaticamente; caminho inexistente, 404.
- `{id}` casa um segmento; `{resto...}` casa o restante; `{$}` casa só a raiz exata.
- Converta e valide o parâmetro: `strconv.ParseInt` falhou -> 400.
- Prefixo `/api` e agrupamento: monte um `http.NewServeMux()` por funcionalidade e use
  `http.StripPrefix` + `mux.Handle("/api/reservas/", ...)` se precisar; ou registre tudo no mux
  raiz como acima (mais simples). Sub-roteadores e grupos com middleware por grupo são o
  critério de troca para `chi`.

## Respostas JSON e leitura do corpo

```go
// internal/plataforma/http/json.go
package httpx

const limiteCorpo = 1 << 20 // 1 MiB

// Ler decodifica o corpo em destino, com limite e rejeitando campos desconhecidos.
func Ler(w http.ResponseWriter, r *http.Request, destino any) error {
	r.Body = http.MaxBytesReader(w, r.Body, limiteCorpo)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		var sintaxe *json.SyntaxError
		var tipo *json.UnmarshalTypeError
		var grande *http.MaxBytesError
		switch {
		case errors.As(err, &grande):
			return &ErroHTTP{Status: http.StatusRequestEntityTooLarge, Tipo: "corpo-grande", Titulo: "Corpo grande demais"}
		case errors.As(err, &sintaxe), errors.Is(err, io.ErrUnexpectedEOF):
			return erro400("JSON malformado.")
		case errors.As(err, &tipo):
			return erro400(fmt.Sprintf("O campo %q tem tipo inválido.", tipo.Field))
		case errors.Is(err, io.EOF):
			return erro400("O corpo da requisição está vazio.")
		default:
			return erro400("Corpo inválido: " + err.Error()) // inclui "unknown field"
		}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return erro400("O corpo deve conter um único objeto JSON.")
	}
	return nil
}

func Escrever(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v) // erro de escrita: cliente caiu; nada a fazer
	}
}
```

- **`MaxBytesReader`** protege a memória; **`DisallowUnknownFields`** torna o contrato
  estrito; a segunda `Decode` recusa lixo depois do objeto.
- Request e response são structs próprias com tags: campo que o cliente não pode definir
  (`id`, `usuario_id`) **não existe** no request (evita mass assignment).
- `time.Time` serializa em RFC 3339 (ISO 8601 com fuso), como o `openapi.yaml` pede.
- Ponteiro (`*string`) só quando for preciso distinguir "ausente" de "vazio".

## Validação

Por padrão, validação **manual**, simples e testável por tabela:

```go
type criarRequest struct {
	SalaID int64     `json:"sala_id"`
	Inicio time.Time `json:"inicio"`
	Fim    time.Time `json:"fim"`
	Titulo string    `json:"titulo"`
}

func (c criarRequest) Validar() map[string]string {
	erros := map[string]string{}
	if c.SalaID <= 0 {
		erros["sala_id"] = "obrigatório"
	}
	if c.Inicio.IsZero() {
		erros["inicio"] = "obrigatório"
	}
	if c.Fim.IsZero() || !c.Fim.After(c.Inicio) {
		erros["fim"] = "deve ser depois do início"
	}
	if t := strings.TrimSpace(c.Titulo); t == "" || utf8.RuneCountInString(t) > 120 {
		erros["titulo"] = "obrigatório, até 120 caracteres"
	}
	return erros
}
```

- Contar caracteres com `utf8.RuneCountInString`, não `len` (bytes).
- Muitos DTOs com regra repetida: `go-playground/validator` (tags `validate:"required,max=120"`)
  é o critério de troca (conferir API na versão adotada).
- Validação **não substitui** constraint do banco.

## Problem Details

```go
// internal/plataforma/http/erros.go
package httpx

type ErroHTTP struct {
	Status int
	Tipo   string            // sufixo do type: "validacao", "reserva-sobreposta"
	Titulo string
	Detalhe string
	Campos map[string]string // opcional: erros por campo
}

func (e *ErroHTTP) Error() string { return e.Titulo + ": " + e.Detalhe }

func erro400(detalhe string) *ErroHTTP {
	return &ErroHTTP{Status: http.StatusBadRequest, Tipo: "entrada-invalida", Titulo: "Entrada inválida", Detalhe: detalhe}
}

type problema struct {
	Type   string            `json:"type"`
	Title  string            `json:"title"`
	Status int               `json:"status"`
	Detail string            `json:"detail,omitempty"`
	Erros  map[string]string `json:"erros,omitempty"`
}

const base = "https://exemplo.local/erros/"

func ResponderErro(w http.ResponseWriter, r *http.Request, err error, log *slog.Logger) {
	var eh *ErroHTTP
	if !errors.As(err, &eh) {
		eh = traduzir(err)
	}
	if eh.Status >= 500 {
		log.ErrorContext(r.Context(), "falha inesperada", "erro", err, "metodo", r.Method, "caminho", r.URL.Path)
		eh = &ErroHTTP{Status: 500, Tipo: "interno", Titulo: "Erro interno", Detalhe: "Tente novamente mais tarde."}
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(eh.Status)
	_ = json.NewEncoder(w).Encode(problema{Type: base + eh.Tipo, Title: eh.Titulo, Status: eh.Status, Detail: eh.Detalhe, Erros: eh.Campos})
}

// traduzir mapeia erros de domínio para HTTP. Um único lugar.
func traduzir(err error) *ErroHTTP {
	switch {
	case errors.Is(err, reserva.ErrNaoEncontrado):
		return &ErroHTTP{Status: 404, Tipo: "nao-encontrado", Titulo: "Não encontrado", Detalhe: "O recurso não existe."}
	case errors.Is(err, reserva.ErrEntradaInvalida):
		return &ErroHTTP{Status: 400, Tipo: "entrada-invalida", Titulo: "Entrada inválida", Detalhe: err.Error()}
	case errors.Is(err, reserva.ErrConflito):
		return &ErroHTTP{Status: 409, Tipo: "conflito", Titulo: "Conflito", Detalhe: "A operação conflita com o estado atual."}
	default:
		return &ErroHTTP{Status: 500}
	}
}
```

- Um **único** `ResponderErro`; handler chama `httpx.ResponderErro(w, r, err, h.log)` e retorna.
- O 500 **nunca** devolve a mensagem do erro; ela vai para o log com o `requestId`.
- Para conflitos com `type` específico (`reserva-sobreposta`), o repositório devolve um erro
  tipado (abaixo) e `traduzir` o mapeia; evite o pacote `httpx` importar todos os domínios:
  prefira que o **domínio exponha o erro** e o handler de cada funcionalidade o mapeie, ou use
  uma interface `interface{ HTTPStatus() int }` implementada pelos erros de domínio.

## Violação de constraint -> 409

Padrão: **tentar a operação e traduzir** a falha do PostgreSQL. SQLSTATE: `23P01` exclusion,
`23505` unique, `23503` foreign key, `23502` not null, `23514` check.

```go
// internal/reserva/repository.go
const (
	pgExclusao = "23P01"
	pgUnico    = "23505"
	pgFK       = "23503"
)

// ConflitoErro carrega o código estável para o type do Problem Details.
type ConflitoErro struct{ Codigo string }

func (e *ConflitoErro) Error() string { return "conflito: " + e.Codigo }
func (e *ConflitoErro) Is(alvo error) bool { return alvo == ErrConflito }

func traduzirPG(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case pgExclusao:
		return &ConflitoErro{Codigo: "reserva-sobreposta"} // constraint: reservas_sem_sobreposicao
	case pgUnico:
		return &ConflitoErro{Codigo: "valor-duplicado"} // ConstraintName diz qual
	case pgFK:
		return fmt.Errorf("referência inexistente (%s): %w", pgErr.ConstraintName, ErrEntradaInvalida)
	}
	return err
}
```

- `pgconn.PgError` (`github.com/jackc/pgx/v5/pgconn`) traz `Code` (SQLSTATE),
  `ConstraintName`, `TableName`, `Detail`. Decida por **código e nome da constraint**, nunca
  por texto.
- A tradução fica **no repositório** (conhece a constraint); o handler só vê `ErrConflito`
  (via `errors.Is`) e `errors.As(err, &conf)` para pegar o `Codigo`.
- A mensagem do banco (`Detail`) tem valores reais: **não** a devolva ao cliente.
- Teste com PostgreSQL real: duas reservas sobrepostas -> a segunda dá 409 (ver
  `testes-go.md`).

## Handler completo (criar)

```go
func (h *Handler) criar(w http.ResponseWriter, r *http.Request) {
	var req criarRequest
	if err := httpx.Ler(w, r, &req); err != nil {
		httpx.ResponderErro(w, r, err, h.log)
		return
	}
	if campos := req.Validar(); len(campos) > 0 {
		httpx.ResponderErro(w, r, &httpx.ErroHTTP{Status: 400, Tipo: "validacao", Titulo: "Dados inválidos", Campos: campos}, h.log)
		return
	}
	u := auth.Usuario(r.Context()) // colocado pelo middleware
	criada, err := h.svc.Criar(r.Context(), u.ID, req.SalaID, req.Inicio, req.Fim, req.Titulo)
	if err != nil {
		httpx.ResponderErro(w, r, err, h.log)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/reservas/%d", criada.ID))
	httpx.Escrever(w, http.StatusCreated, paraResposta(criada))
}
```

## Erros comuns

| Erro | Correção |
|---|---|
| `json.NewDecoder(r.Body).Decode` sem limite | `MaxBytesReader` |
| Aceitar campos desconhecidos em silêncio | `DisallowUnknownFields` |
| 409 por `SELECT` prévio | Constraint + `pgconn.PgError` |
| Traduzir pelo texto da mensagem | `Code`/`ConstraintName` |
| Mensagem do erro interno no 500 | Log interno; resposta genérica |
