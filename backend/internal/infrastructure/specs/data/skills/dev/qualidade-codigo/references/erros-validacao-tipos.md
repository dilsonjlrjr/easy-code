# Erros, nulidade, validação na borda e tipos ricos

## Tratamento de erros

### Princípios

1. **Falhar cedo:** valide no início da operação; recuse antes de gastar trabalho ou gravar.
2. **Dois tipos de erro, dois tratamentos:**

| Tipo | Exemplo | Tratamento |
|---|---|---|
| **Esperado** (regra de negócio, entrada ruim) | Sala ocupada, reserva já iniciada, e-mail inválido | Erro de **domínio tipado**, tratado e traduzido na borda (409, 422...) |
| **Inesperado** (bug, infraestrutura) | Banco indisponível, NPE, disco cheio | Deixar **subir**; um único ponto na borda registra e responde 500 genérico |

3. **Nunca engolir.** `catch` vazio, `except: pass`, `_ = err` sem motivo são bugs futuros.
4. **Não use erro para controle de fluxo normal** quando existe consulta (`existe()` vs
   exceção de "não achou", se "não achar" é comum).
5. **Preserve a causa.** Ao envolver um erro, mantenha o original encadeado.
6. **Trate onde sabe o que fazer.** Camada que não sabe tratar, propaga.

### Erros de domínio tipados

Cada recusa de regra é um tipo (ou código) próprio, sem conhecer HTTP.

```java
// Java
sealed interface ResultadoReserva permits Criada, Conflito {}
class ReservaSobreposta extends RuntimeException { ... }   // ou exceção de domínio do projeto
```

```go
// Go: sentinela ou tipo; wrap com %w
var ErrReservaSobreposta = errors.New("reserva sobreposta")
return fmt.Errorf("criar reserva na sala %s: %w", salaID, ErrReservaSobreposta)
// quem chama: errors.Is(err, ErrReservaSobreposta)
```

```rust
// Rust: enum de erro; `thiserror` (conferir) para derivar Display/From
enum ErroReserva { Sobreposta, JaIniciada, NaoEncontrada }
fn criar(...) -> Result<Reserva, ErroReserva>
```

```python
# Python
class ReservaSobreposta(ErroDeDominio): ...
raise ReservaSobreposta(sala_id) from erro_original
```

```ts
// TypeScript: union discriminada no retorno, sem exceção para fluxo esperado
type Resultado = { ok: true; reserva: Reserva } | { ok: false; erro: 'sobreposta' | 'ja-iniciada' }
```

### Mapeamento na borda (um lugar só)

Tabela única no handler/middleware que converte erro de domínio em resposta, no formato
Problem Details do contrato (`application/problem+json`):

| Erro de domínio | HTTP |
|---|---|
| Validação de campo | 400 ou 422 (o que o contrato definir) com lista de campos |
| Não autenticado | 401 |
| Sem permissão | 403 (ou 404 para não revelar existência, conforme ADR) |
| Não encontrado | 404 |
| Conflito de regra (sobreposição) | 409 |
| Inesperado | 500 com mensagem genérica e id de correlação (nunca stack/SQL) |

A regra de negócio **não** conhece status HTTP. O contrato (`openapi.yaml`) manda no formato.

### Falha em chamadas externas

- **Timeout em toda chamada** de rede (banco, HTTP). Sem timeout padrão infinito.
- Retry só para operação **idempotente** e erro transitório, com limite e espera crescente com
  variação; nunca retry em laço infinito.
- Fechar recursos sempre (`try-with-resources`, `defer`, RAII/`Drop`, `with`, `finally`).
- Falha parcial (gravou e falhou ao notificar): decidir e testar o que deve acontecer, em
  vez de deixar ao acaso (transação, ou notificação depois do commit).

### Registrar erro sem duplicar

Logue **uma vez**, na borda que trata. Camada intermediária que loga e relança gera a mesma
falha 5 vezes no log. Detalhe em `logging-config-segredos.md`.

## Nulidade explícita

Null é a fonte clássica de erro em produção. Torne a ausência **visível no tipo**.

| Linguagem | Faça |
|---|---|
| Java | `Optional<T>` em retorno que pode faltar (não em campo/parâmetro); anotações de nulidade e checagem estática se o projeto usa; `Objects.requireNonNull` na borda |
| Go | Retornar `(valor, error)` e um erro `ErrNaoEncontrado` para ausência; **nunca** `(nil, nil)`; ponteiro só quando ausência é parte do modelo |
| Rust | `Option<T>`/`Result<T, E>`; sem `unwrap()` em código de produção (só em testes e invariantes comentados) |
| Python | `T | None` nos tipos, checado com `mypy`/`pyright`; evitar devolver `None` para sinalizar erro; lançar exceção de domínio |
| TypeScript | `strict` ligado (`strictNullChecks`); `T | undefined` explícito; sem `!` (non-null assertion) e sem `as` para calar o compilador |

Regras:

- Busca por id que **deve** existir: erro "não encontrado" tipado, não `null` passeando.
- Busca que pode não achar: opcional explícito, tratado na hora.
- Coleção vazia, nunca `null`.
- Valor padrão só se for o padrão **de negócio**, não para esconder ausência.

## Validação na borda

Toda entrada externa é **hostil** até validada: corpo HTTP, query, cabeçalhos, JSON de fila,
variável de ambiente, arquivo. Valide **uma vez, na borda**, e converta para tipos de domínio.

Camadas e responsabilidades:

| Camada | Valida | Exemplo |
|---|---|---|
| Borda (handler/DTO) | Forma e formato | Campo obrigatório, tipo, tamanho, formato de data/e-mail, enum |
| Domínio (construtor/tipo) | Invariantes do conceito | `Intervalo` com fim > início; `Email` bem formado |
| Regra de negócio (serviço) | Contexto/estado | Sala livre, usuário pode cancelar |
| Banco (constraint) | Garantia final | `NOT NULL`, `UNIQUE`, `EXCLUDE`, `CHECK`, FK |

Regra: **banco como última linha de defesa**, não a primeira. Regra que precisa valer sob
concorrência (unicidade, sobreposição) **tem** constraint; a validação em código só melhora a
mensagem.

- **Lista de permitidos** (allowlist), não de bloqueados.
- Limite tamanho de corpo, string, página (`limit` máximo), upload.
- Normalize antes de validar (trim, caixa) conforme a regra.
- Retorne **todos** os erros de campo de uma vez, com o campo indicado (alimenta o formulário
  do front).
- Nunca confie em validação do front: ela é conforto de UX, não segurança.

## Tipos ricos: estado inválido não representável

Troque primitivos por tipos que carregam significado e se validam na construção.

```java
// Java: record com invariante no construtor compacto
record Intervalo(Instant inicio, Instant fim) {
    Intervalo {
        if (!fim.isAfter(inicio)) throw new IntervaloInvalido(inicio, fim);
    }
}
```

```rust
// Rust: construtor que devolve Result; campos privados
pub struct Intervalo { inicio: DateTime<Utc>, fim: DateTime<Utc> }
impl Intervalo {
    pub fn novo(i: DateTime<Utc>, f: DateTime<Utc>) -> Result<Self, ErroIntervalo> {
        if f <= i { return Err(ErroIntervalo::FimAntesDoInicio) }
        Ok(Self { inicio: i, fim: f })
    }
}
```

```go
// Go: tipo próprio + função construtora
type Email string
func NovoEmail(s string) (Email, error) { /* valida */ }
```

```python
# Python: dataclass frozen + __post_init__; ou Pydantic na borda (conferir versão)
@dataclass(frozen=True)
class Intervalo:
    inicio: datetime; fim: datetime
    def __post_init__(self):
        if self.fim <= self.inicio: raise IntervaloInvalido(...)
```

```ts
// TypeScript: união discriminada e tipos "marcados" (branded) para ids
type SalaId = string & { readonly __marca: 'SalaId' }
type EstadoReserva = 'ativa' | 'cancelada' | 'concluida'    // não string solta
```

Boas apostas: ids tipados (não misturar `SalaId` com `UsuarioId`), dinheiro (valor + moeda,
nunca `float`), datas com fuso (`timestamptz`, instante em UTC), estados como enum fechado,
`Duration` em vez de número com unidade implícita.

Máquina de estados: expor **transições** (`cancelar`, `concluir`) que validam a origem, em vez
de um setter de `estado`.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `catch (Exception e) {}` | Falha invisível | Tratar, ou propagar |
| `return null` para "erro" | NPE adiante | Opcional ou erro tipado |
| Validar só no front | Qualquer cliente fura | Validar na borda do backend |
| Validação em código sem constraint, sob concorrência | Duplicata em produção | Constraint no banco |
| Status HTTP na regra de negócio | Domínio acoplado | Mapear na borda |
| Mensagem de erro com SQL/stack | Vazamento de informação | 500 genérico + id de correlação |
| `float` para dinheiro | Erro de arredondamento | Inteiro em centavos ou decimal |
| `unwrap()`/`!`/`as any` para calar o compilador | Erro em produção | Tratar o caso ausente |
| Retry sem limite ou em operação não idempotente | Duplicidade, tempestade | Limite, espera, idempotência |
