# Testes (Java)

Pirâmide, TDD, dublês, flakiness e cobertura: `estrategia-testes` (não repetidos). Aqui: o
"como em Java" com JUnit 5, AssertJ, Mockito, Testcontainers e MockMvc.

## Escolha do nível

| O que testar | Nível | Ferramenta | Sobe Spring? |
|---|---|---|---|
| Regra do serviço (`criar` recusa início no passado) | Unitário | JUnit 5 + AssertJ + dublê escrito à mão/Mockito | Não |
| Validação do DTO (`@NotBlank`, `@Future`) | Unitário | `Validator` do Bean Validation | Não |
| Handler de erros / controller isolado | Slice web | `@WebMvcTest` + `MockMvc` | Parcial |
| SQL, constraint, mapeamento, transação | Integração | Testcontainers + PostgreSQL real | Só o necessário |
| Endpoint ponta a ponta com banco e segurança | Integração | `@SpringBootTest` + `MockMvc` + Testcontainers | Sim |

Regra: o máximo de teste no nível barato; Spring só quando o comportamento **depende** dele.

## Unitário sem Spring

```java
class ReservaServiceTest {

  private final Clock relogio = Clock.fixed(Instant.parse("2030-01-01T10:00:00Z"), ZoneOffset.UTC);
  private final RepositorioFalso reservas = new RepositorioFalso();
  private final ReservaService servico = new ReservaService(reservas, new SalaRepositorioFalso(), relogio);

  @Test
  void naoCriaReservaComInicioNoPassado() {
    OffsetDateTime passado = OffsetDateTime.now(relogio).minusHours(1);

    assertThatThrownBy(() -> servico.criar(1L, 10L, passado, passado.plusHours(1), "Daily"))
        .isInstanceOf(EntradaInvalidaException.class)
        .hasMessageContaining("futuro");
  }

  @Test
  void criaReservaQuandoDadosValidos() {
    OffsetDateTime inicio = OffsetDateTime.now(relogio).plusHours(1);

    Reserva criada = servico.criar(1L, 10L, inicio, inicio.plusHours(1), "Daily");

    assertThat(criada.id()).isNotNull();
    assertThat(reservas.salvas()).hasSize(1);
  }
}
```

- `Clock.fixed`: nada de depender do relógio real.
- Dublê escrito à mão (classe `RepositorioFalso` em memória) para repositório simples;
  Mockito quando só precisa verificar uma chamada ou simular um erro:
  ```java
  var repo = mock(ReservaRepository.class);
  when(repo.buscar(99L)).thenReturn(Optional.empty());
  verify(repo, never()).remover(anyLong());
  ```
- Não mockar o que você não é dono (`JdbcClient`, `HttpServletRequest`): isso testa o mock;
  use o banco real (abaixo).
- Nome do teste = comportamento: `naoCriaReservaQuandoHorarioSobrepoe`, não `testCriar1`.
- AssertJ: `assertThat(...)`, `assertThatThrownBy`, `extracting`, `containsExactly`; uma ideia
  por teste.
- Teste parametrizado para variações de entrada: `@ParameterizedTest` + `@CsvSource`/`@MethodSource`.

### Validação do DTO sem Spring

```java
class CriarReservaRequestTest {
  private static final Validator V = Validation.buildDefaultValidatorFactory().getValidator();

  @Test
  void rejeitaFimAntesDoInicio() {
    var inicio = OffsetDateTime.now().plusDays(1);
    var req = new CriarReservaRequest(1L, inicio, inicio.minusHours(1), "x");

    assertThat(V.validate(req)).extracting(v -> v.getMessage())
        .contains("fim deve ser depois de inicio");
  }
}
```

## Integração com Testcontainers (PostgreSQL real)

Base compartilhada, container **um por execução** (rápido), schema pelo Flyway:

```java
@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
abstract class IntegracaoBase {

  @Container
  @ServiceConnection
  static final PostgreSQLContainer<?> POSTGRES = new PostgreSQLContainer<>("postgres:17-alpine");
  // tag: a mesma major do ambiente/compose; classe/pacote mudam no Testcontainers 2.x (conferir)

  @Autowired JdbcClient jdbc;

  @BeforeEach
  void limpar() {
    jdbc.sql("TRUNCATE reservas, salas, usuarios RESTART IDENTITY CASCADE").update();
  }
}
```

- `@ServiceConnection` (Spring Boot 3.1+) liga o `DataSource` ao container sem
  `@DynamicPropertySource`. Em versão sem isso, usar `@DynamicPropertySource` com
  `spring.datasource.url/username/password`.
- Imports de `@AutoConfigureMockMvc` e de slices mudam entre majors do Spring Boot (conferir).
- Isolamento: `TRUNCATE` por teste (rápido e explícito) ou dados com chaves únicas por teste.
  Não dependa da ordem dos testes.
- O Flyway roda na subida: isso valida as migrações em PostgreSQL limpo de graça.
- Mesma imagem/major do PostgreSQL do `docker-compose` (ver `ambiente-docker`).
- Docker precisa estar disponível onde os testes rodam (CI inclusive).

### Teste de constraint e da tradução para 409

```java
class ReservaRepositoryIT extends IntegracaoBase {

  @Autowired ReservaRepository repo;

  @Test
  void rejeitaReservasSobrepostasNaMesmaSala() {
    long sala = criarSala("Sala 1");
    long usuario = criarUsuario("a@x.com");
    var t0 = OffsetDateTime.parse("2030-01-01T10:00:00Z");
    repo.inserir(new Reserva(null, sala, usuario, t0, t0.plusHours(2), "A"));

    assertThatThrownBy(() ->
        repo.inserir(new Reserva(null, sala, usuario, t0.plusHours(1), t0.plusHours(3), "B")))
        .isInstanceOf(ConflitoException.class)
        .extracting("codigo").isEqualTo("reserva-sobreposta");
  }

  @Test
  void aceitaReservasEncostadasSemSobreposicao() {
    // fim == inicio da próxima é permitido pelo intervalo [) do tstzrange
  }
}
```

Testes `*IT` rodam no `failsafe` (`./mvnw verify`); unitários `*Test` no `surefire`.

## Endpoint ponta a ponta (MockMvc + sessão + CSRF)

```java
class ReservaApiIT extends IntegracaoBase {

  @Autowired MockMvc mvc;

  @Test
  @WithMockUser(username = "a@x.com")   // ou montar a sessão via login real
  void devolve409ComProblemDetailQuandoHorarioSobrepoe() throws Exception {
    long sala = criarSala("Sala 1");
    String corpo = """
        {"salaId": %d, "inicio": "2030-01-01T10:00:00Z", "fim": "2030-01-01T12:00:00Z", "titulo": "A"}
        """.formatted(sala);

    mvc.perform(post("/api/reservas").with(csrf()).contentType(APPLICATION_JSON).content(corpo))
        .andExpect(status().isCreated());

    mvc.perform(post("/api/reservas").with(csrf()).contentType(APPLICATION_JSON).content(corpo))
        .andExpect(status().isConflict())
        .andExpect(content().contentTypeCompatibleWith("application/problem+json"))
        .andExpect(jsonPath("$.status").value(409))
        .andExpect(jsonPath("$.type").value(endsWith("reserva-sobreposta")));
  }

  @Test
  void devolve401SemSessao() throws Exception {
    mvc.perform(get("/api/reservas")).andExpect(status().isUnauthorized());
  }

  @Test
  void devolve400ComListaDeCamposInvalidos() throws Exception {
    mvc.perform(post("/api/reservas").with(csrf()).with(user("a@x.com"))
            .contentType(APPLICATION_JSON).content("{}"))
        .andExpect(status().isBadRequest())
        .andExpect(jsonPath("$.erros[*].campo", hasItem("salaId")));
  }
}
```

- `@WithMockUser`/`user(...)` ignoram o login real; para o principal customizado
  (`UsuarioLogado`) construa o `Authentication` com ele (`.with(authentication(...))` de
  `SecurityMockMvcRequestPostProcessors`).
- Pelo menos **um** teste faz o login real (`POST /api/auth/login`), reaproveita o cookie
  `SESSION` e acessa uma rota protegida, provando a cadeia sessão -> principal.
- Requisições que mudam estado precisam de `.with(csrf())`.
- Cenários obrigatórios por endpoint: caminho feliz, 400, 401, 403/404 de outro dono, 409
  (quando há constraint). São os CT da T.
- `RestClient`/`TestRestTemplate` com `webEnvironment = RANDOM_PORT` quando precisar de HTTP
  real (cabeçalhos de proxy, cookie `Secure`); lembrar de gerenciar cookie e CSRF à mão.
- Interfaces de teste novas do Spring (ex.: `RestTestClient` em versões recentes): conferir
  antes de usar.

## Slice web: handler de erros isolado

```java
@WebMvcTest(ReservaController.class)
@Import({ErrosHandler.class, SegurancaConfig.class})
class ReservaControllerTest {
  @Autowired MockMvc mvc;
  @MockitoBean ReservaService servico;   // @MockBean em versões anteriores (conferir)

  @Test
  void traduzNaoEncontradoPara404() throws Exception {
    when(servico.buscar(eq(5L), any())).thenThrow(new NaoEncontradoException("reserva 5"));

    mvc.perform(get("/api/reservas/5").with(user("a@x.com")))
        .andExpect(status().isNotFound())
        .andExpect(jsonPath("$.title").value("Não encontrado"));
  }
}
```

- Útil para o mapa exceção -> status/`ProblemDetail`; rápido porque não carrega JPA/JDBC.
- Pacote e anotação de dublê (`@MockitoBean`/`@MockBean`) mudam por versão (conferir).

## Teste de N+1 (quando JPA)

Contar consultas de uma lista: habilitar estatísticas do Hibernate
(`hibernate.generate_statistics=true`) no teste, chamar o serviço e afirmar
`statistics.getPrepareStatementCount()` <= 2 para N reservas. Sem teste, o N+1 volta.

## Execução e organização

| Ação | Comando |
|---|---|
| Unitários rápidos | `./mvnw test` |
| Tudo (unit + integração + lint) | `./mvnw verify` |
| Um teste | `./mvnw -Dtest=ReservaServiceTest test` |
| Uma integração | `./mvnw -Dit.test=ReservaApiIT verify` |

- Teste espelha o pacote do código (`reserva/ReservaServiceTest`).
- Sem `Thread.sleep` para esperar; sem relógio/aleatório reais; sem ordem entre testes.
- Cobertura é sinal, não meta: JaCoCo (`jacoco-maven-plugin`) para achar regra sem teste.

## Erros comuns

| Erro | Correção |
|---|---|
| `@SpringBootTest` para testar um `if` | Unitário com `new` |
| H2 no lugar do PostgreSQL | Testcontainers |
| Mockar `JdbcClient`/driver | Banco real |
| Testes que dependem de ordem/dados do anterior | `TRUNCATE` no `@BeforeEach` |
| Esquecer `csrf()` e achar que o endpoint dá 403 "do nada" | `.with(csrf())` |
| Só testar o caminho feliz | 400, 401, 403/404, 409 por endpoint |
| `LocalDateTime.now()` no código sob teste | `Clock` injetado |
