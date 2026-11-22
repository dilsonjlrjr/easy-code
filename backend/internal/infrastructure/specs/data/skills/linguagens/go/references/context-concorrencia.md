# Context, timeouts, shutdown e concorrência (Go)

Teoria de concorrência e flakiness em testes: `estrategia-testes`. Aqui: o "como em Go".

## Context

Regras (da própria documentação do pacote `context`):

1. `ctx context.Context` é o **primeiro parâmetro** de toda função que faz E/S (banco, HTTP,
   fila) ou pode demorar; nome `ctx`.
2. **Não** guardar `Context` em struct; passar explicitamente.
3. **Não** passar `nil`; sem contexto real use `context.TODO()` (sinaliza dívida) ou
   `context.Background()` só na `main`/testes/inicialização.
4. No handler, use `r.Context()`: ele é cancelado quando o cliente desconecta ou o servidor
   encerra. Propague até o pgx; a consulta é abortada junto.
5. `context.Value` só para dados do **escopo da requisição** que atravessam APIs (usuário
   autenticado, request id), com **chave de tipo próprio**; nunca para parâmetro opcional ou
   dependência.

```go
type chaveUsuario struct{}

func ComUsuario(ctx context.Context, u Usuario) context.Context {
	return context.WithValue(ctx, chaveUsuario{}, u)
}

func Usuario(ctx context.Context) (Usuario, bool) {
	u, ok := ctx.Value(chaveUsuario{}).(Usuario)
	return u, ok
}
```

### Timeouts e cancelamento por operação

```go
func (r *Repository) Buscar(ctx context.Context, id int64) (Reserva, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second) // teto da consulta
	defer cancel()                                          // sempre: libera o timer
	// ... r.pool.QueryRow(ctx, ...)
}
```

- Todo `WithTimeout`/`WithCancel` tem `defer cancel()` logo em seguida (o `go vet` avisa
  quando falta).
- Timeout por camada fica **dentro** do timeout maior da requisição; o mais interno que
  estourar devolve `context.DeadlineExceeded`, que o handler traduz em 503/504 ou 500,
  nunca em 400.
- `errors.Is(err, context.Canceled)`: cliente foi embora; não é erro do servidor, não logar
  como `error`.
- Chamada a serviço externo: `http.NewRequestWithContext(ctx, ...)` e `http.Client{Timeout: ...}`;
  nunca `http.Get` (cliente padrão sem timeout).
- Não faça operação que **deve** terminar mesmo com o cliente cancelado (ex.: gravar auditoria)
  com o `ctx` do request; use `context.WithoutCancel(ctx)` (Go 1.21+) com timeout próprio.

## Servidor: timeouts e shutdown gracioso

`http.ListenAndServe` cru não tem timeouts. Use `http.Server` configurado:

```go
srv := &http.Server{
	Addr:              ":" + cfg.Porta,
	Handler:           handler,
	ReadHeaderTimeout: 5 * time.Second,  // defesa contra Slowloris
	ReadTimeout:       15 * time.Second, // cabeçalho + corpo
	WriteTimeout:      30 * time.Second, // da leitura do cabeçalho até o fim da resposta
	IdleTimeout:       60 * time.Second, // keep-alive ocioso
	MaxHeaderBytes:    1 << 20,
	ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
}

ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer parar()

erros := make(chan error, 1)
go func() {
	log.Info("servidor ouvindo", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		erros <- err
	}
	close(erros)
}()

select {
case err := <-erros:
	return fmt.Errorf("servidor: %w", err) // falhou ao subir/rodar
case <-ctx.Done():
	log.Info("encerrando")
}

shutdownCtx, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
defer cancelar()
if err := srv.Shutdown(shutdownCtx); err != nil { // para de aceitar e espera requisições em curso
	return fmt.Errorf("shutdown: %w", err)
}
return nil // main fecha o pool (defer pool.Close()) depois
```

- `SIGTERM` é o que o Docker/orquestrador envia no `stop`; `signal.NotifyContext` transforma em
  cancelamento do `ctx`.
- `Shutdown` pára de aceitar conexões e **espera** as requisições em curso até o prazo; só
  depois feche o pool (a ordem inversa derrubaria requisições no meio).
- O prazo do shutdown é menor que o `stop_grace_period` do Compose/orquestrador.
- Para requisição com contexto base próprio, `BaseContext` do `http.Server` herda o `ctx` do
  processo; opcional.
- `WriteTimeout` curto quebra resposta longa (stream/export): ajuste por rota com
  `http.NewResponseController(w).SetWriteDeadline(...)` quando preciso.
- Endpoints `/health` (liveness) e `/ready` (consulta o banco com timeout curto) para o
  healthcheck (ver `config-observabilidade-docker.md`).

## Goroutines: dono, fim e erro

Regra: **quem inicia a goroutine garante que ela termina e recebe seu erro.**

```go
import "golang.org/x/sync/errgroup"

func (s *Service) Painel(ctx context.Context, usuarioID int64) (Painel, error) {
	var p Painel
	g, ctx := errgroup.WithContext(ctx) // cancela as demais se uma falhar
	g.Go(func() (err error) {
		p.Proximas, err = s.reservas.Proximas(ctx, usuarioID)
		return err
	})
	g.Go(func() (err error) {
		p.Salas, err = s.salas.Favoritas(ctx, usuarioID)
		return err
	})
	if err := g.Wait(); err != nil { // espera TODAS; devolve o primeiro erro
		return Painel{}, fmt.Errorf("montar painel: %w", err)
	}
	return p, nil
}
```

- Cada goroutine escreve em **campo/variável distinto** (sem corrida); `g.Wait()` cria a
  barreira que torna a leitura de `p` segura depois.
- `errgroup.SetLimit(n)` limita paralelismo (evita estourar o pool de conexões).
- Para só esperar (sem erro): `sync.WaitGroup` (`Add` antes de iniciar a goroutine; no Go
  1.25 existe `wg.Go(f)`, conferir o mínimo do `go.mod`).
- **Sem `go func()` solto** dentro do handler que continua depois da resposta: ele perde o
  contexto, vaza e perde o erro. Trabalho de fundo real vai para um worker com ciclo de vida
  próprio (iniciado/parado na `main`) e fila (canal limitado ou tabela de jobs no
  PostgreSQL).
- Quem envia fecha o canal; quem recebe nunca fecha. Canal com buffer **limitado**; todo
  `select` com `case <-ctx.Done()` para não bloquear para sempre.
- **Panic em goroutine** derruba o processo inteiro (o `recover` do middleware só protege a
  goroutine do handler). Em goroutine criada por você, recupere e converta em erro.
- Simplicidade: o PostgreSQL já serializa o que importa. Prefira **constraint e transação**
  a mutex para coordenar escritas de negócio.

### Estado compartilhado em memória

| Necessidade | Use |
|---|---|
| Contador/flag simples | `sync/atomic` (`atomic.Int64`, `atomic.Bool`) |
| Mapa/estrutura lida e escrita por várias goroutines | `sync.Mutex`/`sync.RWMutex` ao lado do dado, ou confinar a uma goroutine dona |
| Inicialização única | `sync.Once`/`sync.OnceValue` |
| Cache local | Só com necessidade medida; invalidação é problema difícil |

```go
type ContadorLogins struct {
	mu sync.Mutex
	n  map[string]int // protegido por mu
}

func (c *ContadorLogins) Falha(email string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[email]++
	return c.n[email]
}
```

- Mutex **ao lado** do campo que protege, comentado; nunca copie struct com mutex (`go vet`
  detecta; use receptor ponteiro).
- Estado em memória some no reinício e **não** vale com várias instâncias: o que precisa ser
  consistente (sessão, limite de tentativas) vai para o PostgreSQL.

## Race detector

```bash
go test -race ./...        # obrigatório no CI e antes de entregar
go run -race ./cmd/api     # opcional, em desenvolvimento
```

- Detecta corrida **que ocorreu na execução**: quanto mais concorrência o teste exercita,
  melhor. Um teste de handler com requisições paralelas (`t.Parallel`, `errgroup`) aumenta a
  chance de pegar.
- Exige CGO habilitado (gcc/toolchain C na imagem de **teste**); não use `CGO_ENABLED=0`
  nem imagem Alpine sem toolchain para rodar `-race`. O binário final de produção pode ser
  sem CGO.
- Corrida reportada é bug, nunca "falso positivo": corrija a sincronização.
- Estouro de tempo com `-race` (5-10x mais lento): ajuste timeouts de teste, não desligue o
  `-race`.

## Vazamento de goroutine

- Sintoma: número de goroutines cresce; memória sobe; shutdown trava.
- Causas típicas: canal sem receptor, `select` sem `ctx.Done()`, `time.After` em laço,
  goroutine esperando `Rows` não fechado.
- Teste: verificar que, após o caso de uso terminar, nada ficou rodando (biblioteca
  open source `go.uber.org/goleak` no `TestMain`; conferir).
- `time.Ticker`/`Timer`: sempre `defer t.Stop()`.

## Erros comuns

| Erro | Correção |
|---|---|
| `context.Background()` dentro do handler | `r.Context()` |
| `WithTimeout` sem `defer cancel()` | `defer cancel()` na linha seguinte |
| `go func()` solto no handler | `errgroup` com `ctx` ou worker na `main` |
| Servidor sem timeouts | `http.Server` com `ReadHeaderTimeout` etc. |
| Fechar o pool antes do `Shutdown` | `Shutdown` primeiro, `pool.Close()` depois |
| Panic em goroutine própria | `recover` e converter em erro |
| Mutex copiado por valor | Receptor ponteiro; `go vet` |
| `http.Get`/`http.DefaultClient` | `http.Client{Timeout}` + `NewRequestWithContext` |
| Rodar sem `-race` | `go test -race ./...` no CI |
| Estado de sessão em mapa em memória | Tabela no PostgreSQL |
