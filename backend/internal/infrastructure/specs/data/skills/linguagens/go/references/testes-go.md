# Testes (Go)

Pirâmide, TDD, dublês, flakiness e cobertura: `estrategia-testes` (não repetidos). Aqui: o
"como em Go" com `testing`, `httptest` e `testcontainers-go`. A biblioteca padrão basta;
asserções com `if got != want { t.Errorf(...) }`. Bibliotecas de asserção (`testify`, ...) são
opcionais e só com acordo do time.

## Escolha do nível

| O que testar | Nível | Ferramenta | Banco? |
|---|---|---|---|
| Regra do serviço, validação, tradução de erro | Unitário | `testing` + teste de tabela + dublê à mão | Não |
| Handler (status, JSON, Problem Details) | Unitário de borda | `httptest` + serviço falso | Não |
| SQL, constraint, mapeamento, transação, migração | Integração | `testcontainers-go` + PostgreSQL real | Sim |
| Endpoint ponta a ponta (sessão, CSRF, 409 real) | Integração | `httptest.NewServer` + PostgreSQL real | Sim |

Regra: o máximo no nível barato; banco real só onde o comportamento **depende** dele.

## Teste de tabela (unitário)

```go
func TestServiceCriar(t *testing.T) {
	agora := time.Date(2030, 1, 1, 10, 0, 0, 0, time.UTC)
	relogio := func() time.Time { return agora }

	casos := []struct {
		nome    string
		inicio  time.Time
		fim     time.Time
		sala    bool  // a sala existe?
		wantErr error // nil = sucesso
	}{
		{"cria quando válida", agora.Add(time.Hour), agora.Add(2 * time.Hour), true, nil},
		{"recusa início no passado", agora.Add(-time.Hour), agora.Add(time.Hour), true, ErrEntradaInvalida},
		{"recusa fim antes do início", agora.Add(2 * time.Hour), agora.Add(time.Hour), true, ErrEntradaInvalida},
		{"recusa sala inexistente", agora.Add(time.Hour), agora.Add(2 * time.Hour), false, ErrNaoEncontrado},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			t.Parallel()
			svc := NewService(&repoFalso{}, salasFalsas{existe: c.sala}, relogio)

			_, err := svc.Criar(context.Background(), 1, 10, c.inicio, c.fim, "Daily")

			if !errors.Is(err, c.wantErr) { // errors.Is(nil, nil) é true
				t.Fatalf("erro = %v, esperado %v", err, c.wantErr)
			}
		})
	}
}
```

- Nome do caso diz o comportamento (`"recusa início no passado"`), não `caso1`.
- `t.Run` por caso; `t.Parallel()` quando os casos não compartilham estado (Go 1.22+ não
  precisa copiar a variável do laço).
- `t.Helper()` em funções auxiliares; `t.Cleanup(...)` para desfazer; `t.TempDir()`,
  `t.Setenv(...)` para isolar arquivo e ambiente.
- Relógio injetado (`func() time.Time`): nada de `time.Now()` real na regra.
- Compare erros com `errors.Is/As`, não por texto; compare structs com `reflect.DeepEqual` ou
  `cmp.Diff` (`github.com/google/go-cmp`, opcional) para mensagem de diferença legível.

### Dublê à mão

```go
type repoFalso struct {
	salvas []Reserva
	errInserir error
}

func (r *repoFalso) Inserir(_ context.Context, in Reserva) (Reserva, error) {
	if r.errInserir != nil {
		return Reserva{}, r.errInserir
	}
	in.ID = int64(len(r.salvas) + 1)
	r.salvas = append(r.salvas, in)
	return in, nil
}
// Buscar e Remover análogos...

type salasFalsas struct{ existe bool }

func (s salasFalsas) Existe(context.Context, int64) (bool, error) { return s.existe, nil }
```

Interfaces pequenas (definidas no consumidor) tornam o dublê de 3 métodos trivial. Não
gere mocks por reflexo sem necessidade; não mocke o driver `pgx` (use o banco real).

## Handler com httptest

```go
func TestHandlerCriarMapeiaConflitoPara409(t *testing.T) {
	svc := &servicoFalso{errCriar: &ConflitoErro{Codigo: "reserva-sobreposta"}}
	mux := http.NewServeMux()
	NewHandler(svc, slog.New(slog.DiscardHandler)).Registrar(mux) // slog.DiscardHandler: Go 1.24+; antes, handler para io.Discard

	corpo := `{"sala_id":1,"inicio":"2030-01-01T10:00:00Z","fim":"2030-01-01T12:00:00Z","titulo":"A"}`
	req := httptest.NewRequest(http.MethodPost, "/api/reservas", strings.NewReader(corpo))
	req = req.WithContext(auth.ComUsuario(req.Context(), auth.Usuario{ID: 1}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, esperado 409; corpo: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q", ct)
	}
	var p struct{ Type string `json:"type"`; Status int `json:"status"` }
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.Type, "reserva-sobreposta") || p.Status != 409 {
		t.Errorf("problema = %+v", p)
	}
}
```

- `httptest.NewRequest` + `NewRecorder`: sem rede, rápido. Chame o `mux`/handler direto.
- Cenários por endpoint: feliz, 400 (JSON inválido e campo inválido), 401, 403/404 de outro
  dono, 409 (quando há constraint) — são os CT da T.
- Teste também o corpo de erro (Problem Details) e o `Location` do 201.

## Integração com testcontainers-go

Um container **por pacote de teste** (`TestMain`), schema pelas **mesmas migrações** da
produção, limpeza por teste:

```go
var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:17-alpine", // mesma major do Compose
		postgres.WithDatabase("reservas"),
		postgres.WithUsername("teste"),
		postgres.WithPassword("teste"),
		postgres.BasicWaitStrategy(),
	)
	if err != nil {
		log.Fatalf("subir postgres: %v", err)
	}
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("url: %v", err)
	}
	if err := db.Migrar(url); err != nil { // aplica as migrações reais
		log.Fatalf("migrar: %v", err)
	}
	pool, err = db.Abrir(ctx, url)
	if err != nil {
		log.Fatalf("pool: %v", err)
	}

	codigo := m.Run()

	pool.Close()
	_ = testcontainers.TerminateContainer(ctr) // ou deixar o Ryuk limpar
	os.Exit(codigo)
}

func limpar(t *testing.T) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"TRUNCATE reservas, salas, usuarios RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
}
```

Imports: `github.com/testcontainers/testcontainers-go` e
`github.com/testcontainers/testcontainers-go/modules/postgres`. A API do módulo
(`postgres.Run`, `ConnectionString`, `BasicWaitStrategy`, `TerminateContainer`) muda entre
versões: **conferir na versão fixada**.

- Docker precisa estar disponível onde os testes rodam (CI inclusive).
- Pule integração no ciclo rápido: `if testing.Short() { t.Skip("integração") }` e
  `go test -short ./...`; o CI roda sem `-short`.
- Uma etiqueta de build (`//go:build integration`) é alternativa; escolher uma só convenção.

### Teste de constraint e da tradução para 409

```go
func TestRepositoryRejeitaReservasSobrepostas(t *testing.T) {
	limpar(t)
	repo := NewRepository(pool)
	sala := criarSala(t, "Sala 1")
	usuario := criarUsuario(t, "a@x.com")
	t0 := time.Date(2030, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := repo.Inserir(ctx, Reserva{SalaID: sala, UsuarioID: usuario, Inicio: t0, Fim: t0.Add(2 * time.Hour), Titulo: "A"}); err != nil {
		t.Fatal(err)
	}

	_, err := repo.Inserir(ctx, Reserva{SalaID: sala, UsuarioID: usuario, Inicio: t0.Add(time.Hour), Fim: t0.Add(3 * time.Hour), Titulo: "B"})

	var conf *ConflitoErro
	if !errors.As(err, &conf) || conf.Codigo != "reserva-sobreposta" {
		t.Fatalf("erro = %v, esperado ConflitoErro reserva-sobreposta", err)
	}
}

func TestRepositoryAceitaReservasEncostadas(t *testing.T) {
	// fim == início da próxima é permitido pelo intervalo [) do tstzrange
}
```

## Endpoint ponta a ponta

`httptest.NewServer(handlerCompleto)` com o pool real e `http.Client` com `cookiejar`:

- Login real (`POST /api/auth/login`), guarda o cookie `sessao`, acessa rota protegida; sem
  cookie -> 401.
- Duas criações sobrepostas -> 201 depois 409 com `Content-Type: application/problem+json`.
- Requisição `POST` com `Origin` de outro site -> 403 (CSRF).
- Pelo menos um teste exercita **concorrência real**: N goroutines criando a mesma reserva
  simultaneamente; exatamente uma tem 201 e as demais 409 (prova que a constraint, não o
  `SELECT`, protege a invariante). Rodar com `-race`.

## Execução

| Ação | Comando |
|---|---|
| Rápidos | `go test -short ./...` |
| Tudo com detector de corrida | `go test -race ./...` |
| Um teste | `go test -run TestServiceCriar ./internal/reserva` |
| Repetir para achar flakiness | `go test -race -count=20 ./...` |
| Cobertura | `go test -race -coverprofile=cover.out ./...` ; `go tool cover -func=cover.out` |
| Benchmark (quando houver RNF) | `go test -bench=. -benchmem ./...` |

- Sem `time.Sleep` para esperar: use canal, `sync.WaitGroup` ou espera com prazo.
- Sem ordem entre testes; sem variável global compartilhada mutável; dados únicos por teste.
- Cobertura é sinal, não meta.
- Fuzz (`go test -fuzz`) vale para parsers/validadores de entrada; opcional.

## Erros comuns

| Erro | Correção |
|---|---|
| Mock do driver `pgx` | Banco real com testcontainers |
| Teste dependendo de `time.Now()` | Relógio injetado |
| Casos de tabela sem nome | `nome` descreve o comportamento |
| Não rodar `-race` | `go test -race ./...` no CI |
| Testar só o caminho feliz | 400, 401, 403/404, 409 por endpoint |
| `time.Sleep` para sincronizar | Canal/`WaitGroup`/prazo |
| Banco compartilhado sem limpeza | `TRUNCATE` por teste |
| Migração de teste diferente da real | Mesmas migrações embutidas |
