# Estrutura e convenções (Java)

Complementa `padroes-arquitetura-codigo` e `qualidade-codigo` (princípios, nomes, funções):
aqui só o "como em Java". Exemplos usam Reserva de Salas.

## pom.xml essencial

Use o parent do Spring Boot (gerencia versões) e o wrapper (`mvn wrapper:wrapper` uma vez;
versionar `mvnw`, `.mvn/`). Versões: "estável atual", conferir em `agentes/ambiente.md`.

```xml
<parent>
  <groupId>org.springframework.boot</groupId>
  <artifactId>spring-boot-starter-parent</artifactId>
  <version><!-- Spring Boot estável atual --></version>
</parent>

<properties>
  <java.version><!-- LTS atual --></java.version>
</properties>

<dependencies>
  <dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-web</artifactId></dependency>
  <dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-validation</artifactId></dependency>
  <dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-jdbc</artifactId></dependency>
  <dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-security</artifactId></dependency>
  <dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-actuator</artifactId></dependency>
  <dependency><groupId>org.springframework.session</groupId><artifactId>spring-session-jdbc</artifactId></dependency>
  <dependency><groupId>org.flywaydb</groupId><artifactId>flyway-core</artifactId></dependency>
  <dependency><groupId>org.flywaydb</groupId><artifactId>flyway-database-postgresql</artifactId></dependency>
  <dependency><groupId>org.postgresql</groupId><artifactId>postgresql</artifactId><scope>runtime</scope></dependency>

  <dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-test</artifactId><scope>test</scope></dependency>
  <dependency><groupId>org.springframework.security</groupId><artifactId>spring-security-test</artifactId><scope>test</scope></dependency>
  <dependency><groupId>org.testcontainers</groupId><artifactId>junit-jupiter</artifactId><scope>test</scope></dependency>
  <dependency><groupId>org.testcontainers</groupId><artifactId>postgresql</artifactId><scope>test</scope></dependency>
</dependencies>
```

Notas (conferir na versão fixada): o Flyway recente separa o módulo do PostgreSQL
(`flyway-database-postgresql`); o Testcontainers 2.x renomeou artefatos/classes (ex.:
`testcontainers-postgresql`); o BOM do Spring Boot gerencia as versões desses artefatos.
Se trocar JDBC por JPA, trocar `starter-jdbc` por `starter-data-jpa`.

Plugins: `spring-boot-maven-plugin`, `maven-surefire-plugin` (testes `*Test`),
`maven-failsafe-plugin` (testes `*IT`, de integração), `spotless-maven-plugin`
(`<googleJavaFormat/>`, `check` ligado à fase `verify`), `maven-checkstyle-plugin` (regras
`google_checks.xml` ou arquivo do projeto, `failOnViolation` ligado). Ver o fluxo de lint em
`observabilidade-config-docker.md`.

## Estrutura por funcionalidade

```
backend/src/main/java/br/exemplo/reservas/
  ReservasApplication.java
  reserva/
    ReservaController.java      borda HTTP
    ReservaService.java         regra de negócio e transação
    ReservaRepository.java      acesso a dados (JdbcClient)
    Reserva.java                modelo de domínio (record ou classe)
    ReservaConflitoException.java
    dto/  CriarReservaRequest.java, ReservaResponse.java
  sala/
  auth/        AuthController, SegurancaConfig, UsuarioAutenticado
  comum/       ErrosHandler (ProblemDetail), ConflitoConstraint, WebConfig
```

- Agrupar **por funcionalidade**, não por tipo técnico: quem muda "reserva" mexe em uma pasta.
- Dependência entre funcionalidades só na direção necessária (`reserva` usa `sala`, nunca o
  contrário em ciclo). `comum/` não conhece funcionalidade alguma.
- Visibilidade mínima: classes do pacote sem `public` quando só o pacote usa (repositório,
  mapeadores). Só controller, serviço e DTOs que cruzam pacote são `public`.
- Interface só quando há 2+ implementações reais ou fronteira para teste que a justifique;
  `ReservaRepository` pode ser classe concreta, testada com Testcontainers.

## DTO como record

```java
public record CriarReservaRequest(
    @NotNull Long salaId,
    @NotNull @Future OffsetDateTime inicio,
    @NotNull OffsetDateTime fim,
    @NotBlank @Size(max = 120) String titulo) {

  @AssertTrue(message = "fim deve ser depois de inicio")
  @JsonIgnore
  public boolean isIntervaloValido() {
    return inicio == null || fim == null || fim.isAfter(inicio);
  }
}

public record ReservaResponse(
    Long id, Long salaId, OffsetDateTime inicio, OffsetDateTime fim, String titulo) {

  static ReservaResponse de(Reserva r) {
    return new ReservaResponse(r.id(), r.salaId(), r.inicio(), r.fim(), r.titulo());
  }
}
```

- Entrada e saída separadas (`CriarReservaRequest` x `ReservaResponse`); campo que o cliente
  não pode definir (`id`, `usuarioId`, `criadoEm`) **não existe** no request (evita
  mass assignment).
- Datas com fuso: `OffsetDateTime` (ou `Instant`) <-> `timestamptz`. Nunca `LocalDateTime` para
  instante de reserva; nunca `java.util.Date`.
- Regra cruzada de campos de entrada pode ficar como `@AssertTrue` no record; regra que
  depende de estado do banco fica no serviço.

## Injeção por construtor e controller fino

```java
@RestController
@RequestMapping("/api/reservas")
class ReservaController {

  private final ReservaService servico;

  ReservaController(ReservaService servico) { // um construtor: sem @Autowired
    this.servico = servico;
  }

  @PostMapping
  ResponseEntity<ReservaResponse> criar(
      @Valid @RequestBody CriarReservaRequest req, @AuthenticationPrincipal UsuarioAutenticado u) {
    Reserva criada = servico.criar(u.id(), req.salaId(), req.inicio(), req.fim(), req.titulo());
    URI local = URI.create("/api/reservas/" + criada.id());
    return ResponseEntity.created(local).body(ReservaResponse.de(criada));
  }

  @GetMapping("/{id}")
  ReservaResponse buscar(@PathVariable long id, @AuthenticationPrincipal UsuarioAutenticado u) {
    return ReservaResponse.de(servico.buscar(id, u));
  }

  @DeleteMapping("/{id}")
  ResponseEntity<Void> cancelar(@PathVariable long id, @AuthenticationPrincipal UsuarioAutenticado u) {
    servico.cancelar(id, u);
    return ResponseEntity.noContent().build();
  }
}
```

- O controller **não** tem `if` de regra, SQL, `try/catch` nem `@Transactional`.
- Status: `201` + `Location` ao criar; `204` sem corpo ao apagar; `200` ao ler/atualizar.
- Rota e verbo seguem o `openapi.yaml`; divergência vira ACH, não improviso.
- O serviço não recebe `HttpServletRequest` nem DTO de web quando der: recebe valores/objetos
  de domínio (facilita teste unitário sem Spring).

## Serviço com regra e dependências explícitas

```java
@Service
public class ReservaService {

  private final ReservaRepository reservas;
  private final SalaRepository salas;
  private final Clock relogio; // injetado: teste controla o "agora"

  public ReservaService(ReservaRepository reservas, SalaRepository salas, Clock relogio) {
    this.reservas = reservas;
    this.salas = salas;
    this.relogio = relogio;
  }

  @Transactional
  public Reserva criar(long usuarioId, long salaId, OffsetDateTime inicio, OffsetDateTime fim, String titulo) {
    if (!inicio.isAfter(OffsetDateTime.now(relogio))) {
      throw new EntradaInvalidaException("inicio deve estar no futuro");
    }
    salas.buscar(salaId).orElseThrow(() -> new NaoEncontradoException("sala " + salaId));
    return reservas.inserir(new Reserva(null, salaId, usuarioId, inicio, fim, titulo));
    // sobreposição: a constraint EXCLUDE reage; ver api-validacao-erros.md
  }
}
```

- `Clock` como bean (`Clock.systemUTC()`) e injetado: nada de `LocalDateTime.now()` solto.
- Exceções de domínio são `RuntimeException` próprias (`NaoEncontradoException`,
  `EntradaInvalidaException`, `ConflitoException`); traduzidas em um só lugar
  (`ErrosHandler`).

## Nulidade e Optional

- Repositório devolve `Optional<T>` em busca por chave; o serviço decide: `orElseThrow(...)`.
- `Optional` **só** em retorno; nunca em campo, parâmetro ou DTO.
- Coleção vazia, não `null`. `List.of()`/`Collections.emptyList()`.
- `Objects.requireNonNull(x, "x")` em construtor de classe de domínio com invariante.

## Convenções de código

| Tema | Convenção |
|---|---|
| Nomes | Classe `PascalCase`, método/variável `camelCase`, constante `MAIOR_CAIXA`; verbo no método, substantivo na classe |
| Tamanho | Método que cabe numa tela; classe com uma razão de mudar |
| Imutabilidade | `final` em campo; `record` para valor; coleções imutáveis (`List.copyOf`) na saída |
| Exceção | Checada só em fronteira que o chamador pode tratar; resto `RuntimeException` de domínio |
| Log | SLF4J (`LoggerFactory.getLogger`), parametrizado (`log.info("reserva {}", id)`); sem concatenar |
| Comentário | Só o "porquê"; Javadoc em API pública que não se explica pelo nome |
| Lombok | Não é padrão: `record` e IDE resolvem; evitar a dependência |
| `var` | Quando o tipo é óbvio na linha; nunca para esconder tipo importante |
| Formatação | google-java-format via Spotless; ninguém formata à mão |

## Configuração do tempo e do mapeamento JSON

```java
@Configuration
class WebConfig {
  @Bean
  Clock relogio() {
    return Clock.systemUTC();
  }
}
```

```yaml
spring:
  jackson:
    default-property-inclusion: non_null
    serialization:
      write-dates-as-timestamps: false   # ISO 8601 no JSON
```

- Datas no JSON em ISO 8601 com fuso, como manda o `openapi.yaml`.
- Campo desconhecido no JSON de entrada: por padrão o Spring Boot **ignora**. Para contrato
  estrito (recomendado em API pública) usar
  `spring.jackson.deserialization.fail-on-unknown-properties: true`.

## Erros comuns

| Erro | Correção |
|---|---|
| Pacotes `controller/`, `service/`, `repository/` globais | Pacote por funcionalidade |
| `@Autowired` em campo | Construtor |
| Entidade/Reserva de domínio usada como body de request | `record` de request separado |
| `LocalDateTime` para instante | `OffsetDateTime`/`Instant` |
| `Optional` como parâmetro | Dois métodos ou valor nulo tratado na borda |
| Serviço lendo `HttpServletRequest` | Passar só o necessário (id do usuário) |
