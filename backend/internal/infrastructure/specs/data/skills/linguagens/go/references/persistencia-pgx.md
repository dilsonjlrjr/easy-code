# Persistência (Go): pgx, transações, sqlc e migrações

Modelagem, índices e constraints: `modelagem-postgresql`. Aqui: acesso a dados com
`github.com/jackc/pgx/v5`. Assinaturas abaixo são do pgx v5; conferir na versão fixada.

## Pool

```go
// internal/plataforma/db/db.go
package db

func Abrir(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse da URL do banco: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("criar pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping no banco: %w", err)
	}
	return pool, nil
}
```

- **Um** `*pgxpool.Pool` por processo, criado na `main`, fechado no fim (`defer pool.Close()`);
  é seguro para uso concorrente. Nunca crie pool por requisição.
- `MaxConns` dimensionado pelo `max_connections` do PostgreSQL dividido pelas instâncias;
  maior não é melhor.
- URL de conexão vem do ambiente (`DATABASE_URL`), nunca fixa no código; `sslmode` explícito
  conforme o ambiente.
- Ping na subida: falha cedo e clara em vez de no primeiro request.

## Consultas

Repositório com SQL explícito e **sempre parametrizado** (`$1`, `$2`); nunca montar SQL
concatenando valor de entrada.

```go
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const colunas = "id, sala_id, usuario_id, inicio, fim, titulo"

func (r *Repository) Buscar(ctx context.Context, id int64) (Reserva, error) {
	var res Reserva
	err := r.pool.QueryRow(ctx,
		"SELECT "+colunas+" FROM reservas WHERE id = $1", id,
	).Scan(&res.ID, &res.SalaID, &res.UsuarioID, &res.Inicio, &res.Fim, &res.Titulo)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reserva{}, ErrNaoEncontrado // nunca (nil, nil)
	}
	if err != nil {
		return Reserva{}, fmt.Errorf("buscar reserva %d: %w", id, err)
	}
	return res, nil
}

func (r *Repository) Inserir(ctx context.Context, in Reserva) (Reserva, error) {
	var res Reserva
	err := r.pool.QueryRow(ctx, `
		INSERT INTO reservas (sala_id, usuario_id, inicio, fim, titulo)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+colunas,
		in.SalaID, in.UsuarioID, in.Inicio, in.Fim, in.Titulo,
	).Scan(&res.ID, &res.SalaID, &res.UsuarioID, &res.Inicio, &res.Fim, &res.Titulo)
	if err != nil {
		return Reserva{}, fmt.Errorf("inserir reserva: %w", traduzirPG(err)) // 23P01 -> ErrConflito
	}
	return res, nil
}

func (r *Repository) Listar(ctx context.Context, usuarioID int64, limite, desloc int) ([]Reserva, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT "+colunas+" FROM reservas WHERE usuario_id = $1 ORDER BY inicio, id LIMIT $2 OFFSET $3",
		usuarioID, limite, desloc)
	if err != nil {
		return nil, fmt.Errorf("listar reservas: %w", err)
	}
	lista, err := pgx.CollectRows(rows, pgx.RowToStructByName[reservaLinha])
	if err != nil {
		return nil, fmt.Errorf("ler reservas: %w", err)
	}
	out := make([]Reserva, 0, len(lista)) // [] e não null no JSON
	for _, l := range lista {
		out = append(out, Reserva(l))
	}
	return out, nil
}

// reservaLinha casa colunas por nome (tag db) com a linha do SELECT.
type reservaLinha struct {
	ID        int64     `db:"id"`
	SalaID    int64     `db:"sala_id"`
	UsuarioID int64     `db:"usuario_id"`
	Inicio    time.Time `db:"inicio"`
	Fim       time.Time `db:"fim"`
	Titulo    string    `db:"titulo"`
}
```

- `QueryRow(...).Scan(...)`: uma linha; `pgx.ErrNoRows` -> `ErrNaoEncontrado` (erro do domínio).
- `Query` + `pgx.CollectRows` com `pgx.RowToStructByName[T]` lê tudo, fecha `rows` e devolve
  `rows.Err()`; evita o clássico vazamento por esquecer `Close`. Com laço manual:
  `defer rows.Close()` e conferir `rows.Err()` ao final.
- Conversão `Reserva(l)` entre structs de mesmos campos e tipos é válida; senão, copie campo
  a campo. Tags `db` só na struct de linha, não no domínio.
- `timestamptz` <-> `time.Time` direto (com fuso); `NULL` <-> ponteiro ou `pgtype`
  (`*string`, `pgtype.Text`); `numeric` <-> `pgtype.Numeric` (evite `float64` para dinheiro).
- `Exec` para escrita sem retorno: `tag, err := pool.Exec(ctx, ...)`; `tag.RowsAffected()`
  detecta "0 linhas" (recurso inexistente ou de outro dono) -> `ErrNaoEncontrado`.
- Autorização no SQL (`WHERE id = $1 AND usuario_id = $2`) é boa defesa em profundidade
  contra IDOR; o serviço ainda decide o status.
- SQL longo: constantes (`const sqlBuscar = ...`) ou arquivos `.sql` via `embed`; SQL
  reutilizado em testes e revisão fica visível.
- Consulta dinâmica (filtros opcionais): monte `WHERE` com fragmentos **constantes** e lista
  de argumentos; nunca interpole valor. `ORDER BY` dinâmico só de lista branca.

## Transações

```go
func (s *Service) Transferir(ctx context.Context, ...) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.reservas.RemoverTx(ctx, tx, antigaID); err != nil {
			return err // retornar erro => rollback automático
		}
		if _, err := s.reservas.InserirTx(ctx, tx, nova); err != nil {
			return err
		}
		return nil // nil => commit
	})
}
```

- `pgx.BeginFunc` (e `BeginTxFunc` com `pgx.TxOptions`) faz `Begin`, `Commit` em caso de
  sucesso e `Rollback` em erro/panic: **prefira** a fazer à mão.
- Forma manual, quando necessário:
  ```go
  tx, err := pool.Begin(ctx)
  if err != nil { return err }
  defer tx.Rollback(ctx) // no-op após Commit; garante liberação em qualquer saída
  // ... operações em tx ...
  return tx.Commit(ctx)
  ```
- Para que o repositório sirva dentro e fora de transação, dependa de uma interface mínima
  que `*pgxpool.Pool` e `pgx.Tx` satisfazem:
  ```go
  type executor interface {
      Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
      Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
      QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
  }
  ```
  e passe `tx` (ou o pool) ao método. A fronteira da transação é do **serviço**.
- Transação **curta**: sem chamada HTTP externa nem espera dentro; todo o trabalho com `ctx`.
- Uma `Tx` não é segura para uso concorrente; não a compartilhe entre goroutines.
- Conflito/serialização: o padrão (`READ COMMITTED`) basta com constraint; `SERIALIZABLE`
  exige repetir em `40001`: evitar em aplicação pequena.
- Concorrência de dado: invariante em constraint (`EXCLUDE`/`UNIQUE`); contador atômico com
  `UPDATE ... SET x = x - 1 WHERE x > 0` (e `RowsAffected`); versão otimista com coluna
  `versao` e `WHERE versao = $n` (0 linhas = conflito = 409); `SELECT ... FOR UPDATE` só
  quando realmente preciso.
- Erro de violação de constraint **dentro** de uma transação a aborta (`25P02` nas próximas
  instruções): trate o erro e saia; não continue usando a `Tx`.

## Traduzir erros do banco

Faça no repositório (ver `api-http-erros.md` para `traduzirPG` completo): `errors.As` em
`*pgconn.PgError`, decisão por `Code` (`23P01`, `23505`, `23503`) e `ConstraintName`,
devolvendo erros do domínio. O serviço e o handler não importam `pgconn`.

## sqlc: quando adotar

| Use sqlc quando | Fique no SQL à mão quando |
|---|---|
| Dezenas de consultas e o schema muda com frequência | Poucas consultas (aplicação pequena) |
| Quer erro de compilação se a consulta quebrar o schema | A etapa de geração é custo sem benefício |
| Time confortável com SQL e geração de código | Consultas dinâmicas dominam |

`sqlc.yaml` mínimo (versão 2; conferir campos na versão adotada):

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "internal/reserva/consultas.sql"
    schema: "migrations"
    gen:
      go:
        package: "reservadb"
        out: "internal/reserva/reservadb"
        sql_package: "pgx/v5"
```

```sql
-- name: BuscarReserva :one
SELECT id, sala_id, usuario_id, inicio, fim, titulo FROM reservas WHERE id = $1;
```

- Código gerado é **commitado** e conferido no CI (`sqlc generate` + `git diff --exit-code`).
- O repositório continua traduzindo `pgx.ErrNoRows` e `PgError` para erros do domínio; sqlc
  gera as consultas, não as regras.

## Migrações com golang-migrate

```
migrations/
  000001_criar_usuarios_e_salas.up.sql
  000001_criar_usuarios_e_salas.down.sql
  000002_criar_reservas.up.sql
  000002_criar_reservas.down.sql
  000003_criar_sessoes.up.sql
```

```go
//go:embed migrations/*.sql
var arquivos embed.FS // no pacote que fica ao lado da pasta migrations/ (ex.: raiz do backend)

func Migrar(url string) error {
	src, err := iofs.New(arquivos, "migrations")
	if err != nil {
		return fmt.Errorf("fonte das migrações: %w", err)
	}
	// O driver pgx v5 do golang-migrate usa o esquema "pgx5://" no lugar de "postgres://" (conferir).
	urlMigracao := strings.Replace(url, "postgres://", "pgx5://", 1)
	m, err := migrate.NewWithSourceInstance("iofs", src, urlMigracao)
	if err != nil {
		return fmt.Errorf("iniciar migração: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("aplicar migrações: %w", err)
	}
	return nil
}
```

Imports (conferir caminhos na versão adotada): `github.com/golang-migrate/migrate/v4`,
`.../database/pgx/v5` (registro do driver), `.../source/iofs`.

- Numeração crescente e única; par `.up.sql`/`.down.sql`. Migração aplicada é **imutável**:
  erro vira migração nova.
- **Embutir** as migrações no binário (`embed`) elimina divergência entre imagem e arquivos.
- Onde rodar: na subida da API (simples; com várias instâncias, o lock do golang-migrate
  serializa) **ou** um comando/serviço separado (`cmd/migrate`, ou a CLI `migrate` num serviço
  do Compose que roda antes da API). Em aplicação pequena: na subida, ou serviço `migrate` no Compose.
- Teste de integração aplica as mesmas migrações em PostgreSQL limpo (valida o `docs/dados/esquema.sql`).
- Estado "dirty" (migração falhou no meio): corrigir à mão o banco de dev e `migrate force`;
  em produção, migração em transação por arquivo e pequena reduz o risco.
- `.down.sql` serve a desenvolvimento; em produção, prefira migração "para frente".

## Erros comuns

| Erro | Correção |
|---|---|
| Concatenar valor no SQL | `$1`, `$2` |
| Esquecer `rows.Close()`/`rows.Err()` | `pgx.CollectRows` ou `defer rows.Close()` + `rows.Err()` |
| `(nil, nil)` quando `ErrNoRows` | `ErrNaoEncontrado` |
| Pool por requisição | Um pool na `main` |
| `Tx` sem `Rollback` em caminho de erro | `pgx.BeginFunc` ou `defer tx.Rollback(ctx)` |
| Compartilhar `Tx` entre goroutines | Uma goroutine por `Tx` |
| Continuar usando `Tx` após erro de constraint | Sair e fazer rollback |
| Editar migração aplicada | Nova migração |
| `float64` para dinheiro | `numeric` + inteiro de menor unidade |
