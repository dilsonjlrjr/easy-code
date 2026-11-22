# Estrutura e convenções (Go)

Complementa `padroes-arquitetura-codigo` e `qualidade-codigo` (princípios, nomes, funções):
aqui só o "como em Go". Exemplos usam Reserva de Salas.

## Módulo e layout

```
backend/
  go.mod                      module exemplo.com/reservas ; go 1.xx (versão mínima real)
  cmd/api/main.go             só composição: config, pool, migração, rotas, servidor
  internal/
    reserva/
      reserva.go              tipos de domínio
      erros.go                erros do domínio (sentinelas)
      service.go              regra + dependências como interfaces
      repository.go           implementação pgx
      handler.go              HTTP: rotas, decode, tradução de erro
      service_test.go  handler_test.go  repository_integration_test.go
    sala/
    auth/
    plataforma/
      http/                   middlewares, resposta de erro, JSON
      db/                     pool, migrações
  migrations/                 000001_criar_salas.up.sql / .down.sql
```

- **`cmd/<binário>/main.go`** é fino: lê config, cria dependências, liga rotas, sobe o
  servidor. Nenhuma regra nele.
- **`internal/`** impede importação externa: todo o código da aplicação mora aqui. Não crie
  `pkg/` sem consumidor externo real.
- Pacote **por funcionalidade** (`reserva`, `sala`), não por camada técnica (`models`,
  `controllers`, `services` globais).
- Nome de pacote curto, minúsculo, singular, sem `_` e sem repetir o contexto: `reserva.Service`
  (não `reserva.ReservaService`); `auth.Sessao`, não `auth.AuthSessao`.
- Um pacote não importa o de uma funcionalidade irmã em ciclo (o compilador proíbe ciclos);
  se duas funcionalidades se precisam, extraia o tipo comum ou passe um valor/interface.
- Sem `init()` com efeito colateral, sem variável global mutável (pool, config, logger):
  injete pelo construtor.

## Interfaces pequenas, definidas no consumidor

O serviço declara **o que precisa**; o repositório concreto apenas as satisfaz (sem importar
a interface):

```go
// internal/reserva/service.go
package reserva

import (
	"context"
	"fmt"
	"time"
)

// repositorio é o que o Service precisa; fica aqui, não no pacote do pgx.
type repositorio interface {
	Inserir(ctx context.Context, r Reserva) (Reserva, error)
	Buscar(ctx context.Context, id int64) (Reserva, error)
	Remover(ctx context.Context, id int64) error
}

type salas interface {
	Existe(ctx context.Context, id int64) (bool, error)
}

type Service struct {
	reservas repositorio
	salas    salas
	agora    func() time.Time // injetável: teste controla o relógio
}

func NewService(r repositorio, s salas, agora func() time.Time) *Service {
	return &Service{reservas: r, salas: s, agora: agora}
}

func (s *Service) Criar(ctx context.Context, usuarioID, salaID int64, inicio, fim time.Time, titulo string) (Reserva, error) {
	if !inicio.After(s.agora()) {
		return Reserva{}, fmt.Errorf("%w: início deve estar no futuro", ErrEntradaInvalida)
	}
	if !fim.After(inicio) {
		return Reserva{}, fmt.Errorf("%w: fim deve ser depois do início", ErrEntradaInvalida)
	}
	ok, err := s.salas.Existe(ctx, salaID)
	if err != nil {
		return Reserva{}, fmt.Errorf("verificar sala %d: %w", salaID, err)
	}
	if !ok {
		return Reserva{}, fmt.Errorf("sala %d: %w", salaID, ErrNaoEncontrado)
	}
	r, err := s.reservas.Inserir(ctx, Reserva{SalaID: salaID, UsuarioID: usuarioID, Inicio: inicio, Fim: fim, Titulo: titulo})
	if err != nil {
		return Reserva{}, fmt.Errorf("criar reserva: %w", err) // ErrConflito vem do repositório
	}
	return r, nil
}
```

- Interface com **1 a 3 métodos**; nome pelo comportamento (`repositorio`, `salas`), não
  `IReservaRepository`. Não exportar se só o pacote usa.
- "Aceite interfaces, devolva structs": `NewService` recebe interfaces, devolve `*Service`.
- Só crie a interface quando houver 2+ implementações **ou** fronteira de teste real; um
  repositório testado com banco real nem precisa de interface na própria definição.
- Zero value útil: sempre que possível, o valor zero do tipo funciona (ex.: `bytes.Buffer`).

## Erros idiomáticos

```go
// internal/reserva/erros.go
package reserva

import "errors"

var (
	ErrNaoEncontrado   = errors.New("não encontrado")
	ErrConflito        = errors.New("conflito com o estado atual")
	ErrEntradaInvalida = errors.New("entrada inválida")
	ErrSemPermissao    = errors.New("sem permissão")
)
```

- **Embrulhar com contexto**: `fmt.Errorf("criar reserva: %w", err)`. Cada camada acrescenta o
  que **ela** estava fazendo; o texto lido de cima para baixo conta a história.
- **Decidir com `errors.Is`** (sentinela) e `errors.As` (tipo com dados):
  ```go
  if errors.Is(err, reserva.ErrNaoEncontrado) { ... }

  var ve *ValidacaoErro
  if errors.As(err, &ve) { /* ve.Campos */ }
  ```
- Erro com dados vira tipo:
  ```go
  type ValidacaoErro struct{ Campos map[string]string }
  func (e *ValidacaoErro) Error() string { return "dados inválidos" }
  ```
- **Nunca `(nil, nil)`**: não encontrado é `ErrNaoEncontrado`. Quem recebe `(T, error)` confia
  que `error == nil` significa `T` utilizável. Para busca opcional legítima devolva `(T, bool)`.
- **Traduza erros de infraestrutura na fronteira**: `pgx.ErrNoRows` -> `ErrNaoEncontrado` e
  `*pgconn.PgError` com `23P01` -> `ErrConflito`, **dentro do repositório**; o serviço e o
  handler só conhecem erros do domínio (ver `persistencia-pgx.md`, `api-http-erros.md`).
- Embrulhe com `%w` só quando o chamador pode agir sobre o erro de dentro; para esconder
  detalhe de implementação, use `%v` (quebra a cadeia de propósito).
- Não logar **e** retornar o mesmo erro (duplica). Retorne; o handler/`main` loga uma vez.
- Mensagens de erro: minúsculas, sem ponto final, sem "falha ao"/"erro ao" repetido
  (`"criar reserva: ..."`, não `"erro ao criar reserva: erro ao inserir: ..."`).
- `panic` só para bug de programação (invariante impossível) e na inicialização; o
  middleware `recover` transforma `panic` em 500 sem derrubar o processo.
- Múltiplos erros: `errors.Join(e1, e2)` (Go 1.20+).

## Tipos de domínio

```go
// internal/reserva/reserva.go
package reserva

import "time"

type Reserva struct {
	ID        int64
	SalaID    int64
	UsuarioID int64
	Inicio    time.Time // sempre com fuso (timestamptz); compare em UTC
	Fim       time.Time
	Titulo    string
}
```

- Tipos de **entrada/saída HTTP** (JSON) ficam no `handler.go` (`criarRequest`,
  `reservaResponse`) com `json:"..."`; o domínio não carrega tags JSON.
- `time.Time` para instante; nunca string de data nem `int` de epoch na regra.
- Dinheiro/quantidade: tipo próprio ou inteiro de menor unidade, não `float64`.
- Enumerações: `type Papel string` com constantes, validado na borda.

## Construtores e opções

- `NewX(deps...) *X` para tipos com dependências; valide e falhe cedo (`error`) se algo
  obrigatório faltar.
- Muitas opções opcionais: *functional options* (`WithTimeout(d)`); poucas: struct de
  configuração. Não use opções para dependências obrigatórias.

## Convenções de código

| Tema | Convenção |
|---|---|
| Formatação | `gofmt` (ninguém formata à mão); imports agrupados (`goimports`) |
| Nomes | `camelCase` interno, `PascalCase` exportado; siglas inteiras (`ID`, `URL`, `HTTP`); receptor curto e consistente (`s *Service`) |
| Tamanho | Função que cabe numa tela; arquivo por responsabilidade |
| Receptor | Ponteiro se muta ou o tipo é grande/tem mutex; valor para tipos pequenos imutáveis; não misturar no mesmo tipo |
| Retorno | `(T, error)`; erro **por último**; sem retornos nomeados salvo para `defer` que ajusta erro |
| Comentário | Doc de todo exportado, começando pelo nome (`// Service aplica ...`); só o "porquê" no resto |
| Variáveis de laço | Go 1.22+ cria variável por iteração; ainda assim, evite capturar em closure sem necessidade |
| `any`/genéricos | Evitar `any` no domínio; genéricos só quando removem duplicação real |
| Testes | `_test.go` ao lado; pacote `x_test` para testar a API pública |

## Composição no main

```go
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Carregar() // lê o ambiente, valida, falha cedo
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	pool, err := db.Abrir(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("banco: %w", err)
	}
	defer pool.Close()

	repo := reserva.NewRepository(pool)
	svc := reserva.NewService(repo, sala.NewRepository(pool), time.Now)
	mux := http.NewServeMux()
	reserva.NewHandler(svc).Registrar(mux)
	// ... servidor e shutdown: ver context-concorrencia.md
	_ = log
	return nil
}
```

- `main` chama `run()` que **retorna erro**: `defer` roda (o `os.Exit` direto pula os
  `defer`).
- Dependências são criadas aqui e passadas para baixo (injeção manual); sem contêiner de DI.

## Erros comuns

| Erro | Correção |
|---|---|
| Pacotes `models/`, `services/`, `controllers/` | Pacote por funcionalidade |
| Interface `IReservaRepository` gigante no pacote do repositório | Interface mínima no consumidor |
| `return nil, nil` | `ErrNaoEncontrado` ou `(T, bool)` |
| Comparar mensagem do erro | `errors.Is/As` |
| `fmt.Errorf("...: %v", err)` quando precisa agir no erro | `%w` |
| Variável global de pool/config | Injeção pelo construtor |
| Tags JSON na struct de domínio | Tipos de request/response no handler |
| `os.Exit` espalhado | `run() error` e uma saída na `main` |
