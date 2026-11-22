# API: validação, erros e paginação (Java)

Contrato de erros e regra 400/409 vêm de `arquitetura-web` (Problem Details, RFC 9457). Aqui:
como implementar em Spring. Namespace `jakarta.*` (Spring Boot 3+).

## Quem responde o quê

| Situação | Status | Onde nasce |
|---|---|---|
| JSON malformado, tipo errado, campo obrigatório ausente, formato inválido | 400 | Bean Validation / Jackson |
| Regra de entrada violada (fim antes do início, início no passado) | 400 | Bean Validation ou serviço |
| Sem sessão válida | 401 | Spring Security (entry point) |
| Autenticado sem permissão sobre o recurso | 403 (ou 404 para não revelar existência) | Serviço / `@PreAuthorize` |
| Recurso não existe | 404 | Serviço (`NaoEncontradoException`) |
| **Conflito com o estado** (sala já reservada, e-mail já usado) | **409** | Constraint do banco traduzida |
| Falha inesperada | 500 | Handler genérico (sem detalhe interno) |

Regra: **400 = o pedido está mal formado/inválido por si**; **409 = o pedido é válido, mas
conflita com o estado atual**.

## Validação com Bean Validation

```java
public record CriarSalaRequest(
    @NotBlank @Size(max = 80) String nome,
    @NotNull @Min(1) @Max(500) Integer capacidade) {}
```

- `@Valid @RequestBody` no controller dispara a validação; falha lança
  `MethodArgumentNotValidException`.
- Parâmetros de rota/consulta: `@Validated` na classe do controller + `@Min`, `@Size` no
  parâmetro. Nas versões atuais do Spring a falha é `HandlerMethodValidationException`
  (Spring 6.1+); em `ConstraintViolationException` quando vem de `@Validated` em bean comum.
  Trate os dois no handler.
- Mensagens: texto curto no idioma do produto; o front usa o `campo` para ligar ao input.
- Validação **não substitui** constraint do banco: `NOT NULL`, `CHECK`, `UNIQUE`, `EXCLUDE`
  continuam no DDL.

## Exceções de domínio

```java
public class NaoEncontradoException extends RuntimeException {
  public NaoEncontradoException(String o) { super(o + " não encontrado(a)"); }
}
public class EntradaInvalidaException extends RuntimeException {
  public EntradaInvalidaException(String m) { super(m); }
}
public class ConflitoException extends RuntimeException {
  private final String codigo; // ex.: "reserva-sobreposta"
  public ConflitoException(String codigo, String m) { super(m); this.codigo = codigo; }
  public String codigo() { return codigo; }
}
```

## Handler central: ProblemDetail

`ResponseEntityExceptionHandler` já trata as exceções do Spring MVC e devolve `ProblemDetail`
quando se estende; sobrescreva só o que precisa.

```java
@RestControllerAdvice
class ErrosHandler extends ResponseEntityExceptionHandler {

  private static final Logger log = LoggerFactory.getLogger(ErrosHandler.class);
  private static final String BASE = "https://exemplo.local/erros/";

  @Override
  protected ResponseEntity<Object> handleMethodArgumentNotValid(
      MethodArgumentNotValidException ex, HttpHeaders headers, HttpStatusCode status, WebRequest request) {
    ProblemDetail pd = problema(HttpStatus.BAD_REQUEST, "validacao", "Dados inválidos",
        "Um ou mais campos são inválidos.");
    List<Map<String, String>> erros = ex.getBindingResult().getFieldErrors().stream()
        .map(e -> Map.of("campo", e.getField(), "mensagem", String.valueOf(e.getDefaultMessage())))
        .toList();
    pd.setProperty("erros", erros);
    return ResponseEntity.badRequest().body(pd);
  }

  @ExceptionHandler(NaoEncontradoException.class)
  ProblemDetail naoEncontrado(NaoEncontradoException ex) {
    return problema(HttpStatus.NOT_FOUND, "nao-encontrado", "Não encontrado", ex.getMessage());
  }

  @ExceptionHandler(EntradaInvalidaException.class)
  ProblemDetail entradaInvalida(EntradaInvalidaException ex) {
    return problema(HttpStatus.BAD_REQUEST, "entrada-invalida", "Entrada inválida", ex.getMessage());
  }

  @ExceptionHandler(ConflitoException.class)
  ProblemDetail conflito(ConflitoException ex) {
    return problema(HttpStatus.CONFLICT, ex.codigo(), "Conflito", ex.getMessage());
  }

  @ExceptionHandler(BadCredentialsException.class)
  ProblemDetail credenciais(BadCredentialsException ex) {
    return problema(HttpStatus.UNAUTHORIZED, "credenciais-invalidas", "Não autenticado",
        "E-mail ou senha incorretos."); // mesma mensagem para qualquer causa
  }

  @ExceptionHandler(AccessDeniedException.class)
  ProblemDetail negado(AccessDeniedException ex) {
    return problema(HttpStatus.FORBIDDEN, "acesso-negado", "Acesso negado", "Sem permissão para este recurso.");
  }

  @ExceptionHandler(Exception.class)
  ProblemDetail inesperado(Exception ex) {
    log.error("falha inesperada", ex); // detalhe só no log
    return problema(HttpStatus.INTERNAL_SERVER_ERROR, "interno", "Erro interno", "Tente novamente mais tarde.");
  }

  private static ProblemDetail problema(HttpStatus status, String tipo, String titulo, String detalhe) {
    ProblemDetail pd = ProblemDetail.forStatusAndDetail(status, detalhe);
    pd.setType(URI.create(BASE + tipo));
    pd.setTitle(titulo);
    return pd;
  }
}
```

- Resposta sai com `Content-Type: application/problem+json`.
- `AccessDeniedException` lançada **dentro do controller/serviço** (método seguro) chega ao
  handler; a lançada pelo filtro de segurança é tratada por `exceptionHandling` do Spring
  Security (ver `seguranca-sessao-spring-security.md`).
- O `500` nunca devolve mensagem da exceção nem stack trace; `server.error.include-*` ficam
  em `never`. Inclua um identificador de correlação (`pd.setProperty("requestId", ...)`) se o
  projeto tiver (ver `observabilidade-config-docker.md`).
- `spring.mvc.problemdetails.enabled=true` faz o Spring devolver `ProblemDetail` nas exceções
  padrão sem handler próprio; com o handler acima fica redundante.

## Violação de constraint -> 409

Padrão: **tentar a operação e traduzir** a falha. Nada de `SELECT` "existe?" antes.

DDL (ver `modelagem-postgresql`):

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;
ALTER TABLE reservas ADD CONSTRAINT reservas_sem_sobreposicao
  EXCLUDE USING gist (sala_id WITH =, tstzrange(inicio, fim) WITH &&);
ALTER TABLE usuarios ADD CONSTRAINT usuarios_email_unico UNIQUE (email);
```

SQLSTATE relevantes: `23P01` exclusion, `23505` unique, `23503` foreign key, `23502` not null,
`23514` check. Extraia o SQLSTATE e o **nome da constraint** (`getServerErrorMessage()
.getConstraint()` no driver PostgreSQL), e mapeie por nome para um código estável:

```java
final class ConstraintViolada {
  private ConstraintViolada() {}

  record Info(String sqlState, String constraint) {}

  /** Percorre a cadeia de causas até achar o erro do PostgreSQL. */
  static Optional<Info> de(Throwable t) {
    for (Throwable c = t; c != null; c = c.getCause()) {
      if (c instanceof PSQLException pg) {
        String nome = pg.getServerErrorMessage() == null ? null : pg.getServerErrorMessage().getConstraint();
        return Optional.of(new Info(pg.getSQLState(), nome));
      }
    }
    return Optional.empty();
  }
}
```

Uso no repositório/serviço (SQL explícito falha na hora do `INSERT`):

```java
Reserva inserir(Reserva r) {
  try {
    return jdbc.sql("""
        INSERT INTO reservas (sala_id, usuario_id, inicio, fim, titulo)
        VALUES (:sala, :usuario, :inicio, :fim, :titulo)
        RETURNING id, sala_id, usuario_id, inicio, fim, titulo""")
        .param("sala", r.salaId()).param("usuario", r.usuarioId())
        .param("inicio", r.inicio()).param("fim", r.fim()).param("titulo", r.titulo())
        .query(MAPEADOR).single();
  } catch (DataIntegrityViolationException e) {
    throw ConstraintViolada.de(e)
        .filter(i -> "23P01".equals(i.sqlState()))
        .map(i -> new ConflitoException("reserva-sobreposta", "A sala já está reservada nesse horário."))
        .orElseThrow(() -> e);
  }
}
```

- Traduza **por SQLSTATE e nome da constraint**, nunca por texto da mensagem.
- A tradução fica no repositório ou serviço (conhece o significado de negócio); o handler só
  vê `ConflitoException`.
- Com JPA a violação pode surgir **só no flush/commit**; use `saveAndFlush` dentro do `try`
  ou aceite a exceção na fronteira do serviço (ver `persistencia-jpa-jdbc.md`). Pode chegar
  embrulhada em `DataIntegrityViolationException` ou `TransactionSystemException`; a busca na
  cadeia de causas acima resolve os dois.
- Teste cada tradução com PostgreSQL real (ver `testes-java.md`): duas reservas sobrepostas ->
  a segunda dá 409 com o `type` esperado.

## Paginação

Padrão simples: `page` (base 0) e `size` com **teto**, ordenação estável.

```java
record Pagina<T>(List<T> itens, int page, int size, long total) {}

@GetMapping
Pagina<ReservaResponse> listar(
    @RequestParam(defaultValue = "0") @Min(0) int page,
    @RequestParam(defaultValue = "20") @Min(1) @Max(100) int size,
    @AuthenticationPrincipal UsuarioAutenticado u) {
  return servico.listar(u, page, size);
}
```

```java
List<Reserva> listar(long usuarioId, int limite, int deslocamento) {
  return jdbc.sql("""
      SELECT id, sala_id, usuario_id, inicio, fim, titulo FROM reservas
      WHERE usuario_id = :u
      ORDER BY inicio, id            -- desempate por id: ordem determinística
      LIMIT :limite OFFSET :desloc""")
      .param("u", usuarioId).param("limite", limite).param("desloc", deslocamento)
      .query(MAPEADOR).list();
}
```

- Sempre `ORDER BY` com desempate; sem isso, páginas repetem/pulam linhas.
- Teto de `size` no servidor (aqui 100); nunca confiar no cliente.
- `OFFSET` fica lento em tabelas grandes: para feeds/listas longas usar paginação por cursor
  (`WHERE (inicio, id) > (:ultimoInicio, :ultimoId) ORDER BY inicio, id LIMIT n`). Em listas
  pequenas (aplicação pequena) `OFFSET` basta.
- Se usar Spring Data JPA: `Pageable`/`PageRequest.of(page, size, Sort.by(...))` e mapear
  `Page<Entidade>` para o record `Pagina<Dto>` (não devolver `Page` direto: acopla o contrato
  ao formato interno).
- O formato (`itens`/`page`/`size`/`total`) é o do `openapi.yaml`; conferir.

## springdoc conferido contra o openapi.yaml

- `springdoc-openapi-starter-webmvc-ui` (conferir artefato da versão do Boot) gera
  `/v3/api-docs`. A **fonte da verdade é o `openapi.yaml` da Arquitetura**.
- Em teste de contrato: subir a aplicação, baixar `/v3/api-docs` e comparar caminhos, verbos,
  status e esquemas com o `openapi.yaml` (diff simples ou validador de contrato open source).
  Divergência = bug no código ou ACH para a Arquitetura, nunca "ajustar o yaml em silêncio".
- Em produção, desligar a UI e o `/v3/api-docs` se a API não for pública
  (`springdoc.api-docs.enabled=false`, `springdoc.swagger-ui.enabled=false` no perfil `prod`).

## Erros comuns

| Erro | Correção |
|---|---|
| Devolver `Map` ad hoc como erro | `ProblemDetail` único |
| 409 por `SELECT` prévio | Constraint + tradução do SQLSTATE |
| Traduzir pelo texto da exceção | SQLSTATE e nome da constraint |
| `catch (Exception)` no controller | Handler central |
| `Page<Entidade>` serializada | Record `Pagina<Dto>` |
| Paginação sem `ORDER BY` | Ordem estável com desempate |
| Mensagem de exceção no 500 | Log interno; resposta genérica |
