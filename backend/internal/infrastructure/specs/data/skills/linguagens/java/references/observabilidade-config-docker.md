# Observabilidade, configuração, lint e Docker (Java)

Base de ambiente e Compose: `ambiente-docker`. Logs e segredos em geral: `qualidade-codigo`.
Aqui: o "como em Java/Spring".

## Configuração por ambiente

Um `application.yml` base **sem segredo**; o que muda por ambiente vem de variável de
ambiente (relaxed binding: `SPRING_DATASOURCE_URL` -> `spring.datasource.url`).

```yaml
# src/main/resources/application.yml
spring:
  datasource:
    url: ${DB_URL:jdbc:postgresql://localhost:5432/reservas}
    username: ${DB_USER:reservas}
    password: ${DB_PASSWORD}           # sem valor padrão: falha na subida se faltar
  flyway:
    enabled: true
  jpa:
    open-in-view: false
server:
  port: ${PORT:8080}
  error:
    include-stacktrace: never
    include-message: never
management:
  endpoints:
    web:
      exposure:
        include: health
  endpoint:
    health:
      probes:
        enabled: true
      show-details: never
app:
  cors:
    origem-permitida: ${CORS_ORIGEM:http://localhost:5173}
```

```yaml
# application-dev.yml  (perfil dev; sem TLS local)
server:
  servlet:
    session:
      cookie:
        secure: false
```

- **Falhar rápido** na falta de configuração obrigatória (senha do banco sem padrão).
- Segredo só em variável/secret do ambiente (`.env` fora do Git; `.env.example` versionado).
  Nunca em `application*.yml`, nunca na imagem.
- Perfis (`SPRING_PROFILES_ACTIVE`): `dev`, `test`, `prod`. Poucas diferenças e todas
  visíveis; `prod` é o comportamento seguro por padrão, o `dev` é que afrouxa.
- Config tipada em vez de `@Value` espalhado:

```java
@ConfigurationProperties(prefix = "app.cors")
@Validated
public record CorsProps(@NotBlank String origemPermitida) {}
```
(habilitar com `@ConfigurationPropertiesScan` na classe `Application`).

## Logs estruturados

- Spring Boot 3.4+ gera JSON no console sem biblioteca extra:
  ```yaml
  logging:
    structured:
      format:
        console: ecs        # ou logstash, gelf
  ```
  Em versões anteriores: `logstash-logback-encoder` com `logback-spring.xml` (conferir).
- Log para **stdout** (12-factor); o Docker/orquestrador coleta. Nada de arquivo na imagem.
- SLF4J parametrizado, nível correto: `info` para evento de negócio relevante, `warn` para
  recuperável, `error` com a exceção (`log.error("falha ao criar reserva", ex)`), `debug` só
  fora de produção.
- Correlação: filtro que coloca um `requestId` no MDC e no cabeçalho de resposta:

```java
@Component
class RequestIdFilter extends OncePerRequestFilter {
  @Override
  protected void doFilterInternal(HttpServletRequest req, HttpServletResponse res, FilterChain chain)
      throws ServletException, IOException {
    String id = Optional.ofNullable(req.getHeader("X-Request-Id")).orElseGet(() -> UUID.randomUUID().toString());
    MDC.put("requestId", id);
    res.setHeader("X-Request-Id", id);
    try {
      chain.doFilter(req, res);
    } finally {
      MDC.remove("requestId");
    }
  }
}
```
  O MDC aparece como campo no log estruturado; inclua o `requestId` no `ProblemDetail` de 500.
- **Nunca** logar: senha, cookie de sessão, token CSRF, corpo de login, dado pessoal
  completo. Mascarar e-mail/CPF se precisar correlacionar.
- Não logar a mesma exceção em vários níveis (logar uma vez, onde é tratada).

## Actuator

- Expor **só** `health` (acima). `info`, `env`, `beans`, `heapdump`, `threaddump`, `loggers`
  expõem internos: manter fechados (ou em porta de gestão separada sem rota pública:
  `management.server.port`).
- `health.probes.enabled=true` cria `/actuator/health/liveness` e `/actuator/health/readiness`.
  O healthcheck do Compose usa a readiness (inclui banco):
  ```yaml
  healthcheck:
    test: ["CMD", "wget", "-qO-", "http://localhost:8080/actuator/health/readiness"]
    interval: 10s
    timeout: 3s
    retries: 10
  ```
  (a imagem precisa ter `wget`/`curl`; senão use um `HEALTHCHECK` com `java` ou imagem que
  tenha.)
- Rota `/actuator/health/**` liberada na segurança (ver `seguranca-sessao-spring-security.md`),
  com `show-details: never`.
- Métricas (Micrometer) só se houver RNF de observabilidade; em aplicação pequena, health e logs bastam.

## Lint e formato

Spotless (formatação automática) + Checkstyle (regras que o formatador não resolve):

```xml
<plugin>
  <groupId>com.diffplug.spotless</groupId>
  <artifactId>spotless-maven-plugin</artifactId>
  <configuration>
    <java>
      <googleJavaFormat/>
      <removeUnusedImports/>
    </java>
  </configuration>
  <executions>
    <execution><phase>verify</phase><goals><goal>check</goal></goals></execution>
  </executions>
</plugin>

<plugin>
  <groupId>org.apache.maven.plugins</groupId>
  <artifactId>maven-checkstyle-plugin</artifactId>
  <configuration>
    <configLocation>checkstyle.xml</configLocation>   <!-- ou google_checks.xml -->
    <consoleOutput>true</consoleOutput>
    <failOnViolation>true</failOnViolation>
  </configuration>
  <executions>
    <execution><phase>verify</phase><goals><goal>check</goal></goals></execution>
  </executions>
</plugin>
```

- Versões dos plugins: conferir a atual (o BOM do Boot não gerencia Spotless/Checkstyle).
- `./mvnw spotless:apply` antes de commitar; CI roda `spotless:check` (falha se destoar).
- O `google_checks.xml` embutido é rigoroso (por exemplo, exige Javadoc); parta de um
  `checkstyle.xml` enxuto do projeto (imports, nomes, complexidade) e endureça aos poucos.
- Análise estática extra opcional (open source): SpotBugs (`spotbugs-maven-plugin`), Error
  Prone/NullAway (nulidade). Só adicionar com critério; cada ferramenta nova é custo.
- Dependências vulneráveis: OWASP Dependency-Check ou `osv-scanner` no CI (ver
  `revisao-seguranca`, varreduras open source).

## Docker

```dockerfile
# syntax=docker/dockerfile:1
# --- build ---
FROM eclipse-temurin:<LTS>-jdk AS build        # tag do LTS atual (conferir)
WORKDIR /src
COPY mvnw pom.xml ./
COPY .mvn .mvn
RUN ./mvnw -q -B dependency:go-offline          # camada de cache das dependências
COPY src src
RUN ./mvnw -q -B -DskipTests package

# --- execução ---
FROM eclipse-temurin:<LTS>-jre
RUN groupadd --system app && useradd --system --gid app --no-create-home app
WORKDIR /app
COPY --from=build /src/target/*.jar app.jar
USER app
ENV PORT=8080 JAVA_TOOL_OPTIONS="-XX:MaxRAMPercentage=75"
EXPOSE 8080
ENTRYPOINT ["java", "-jar", "app.jar"]
```

Camadas do jar (`java -Djarmode=tools -jar app.jar extract --layers`, conferir na versão do
Spring Boot) só valem quando o tamanho/tempo de push importar; em aplicação pequena, jar único.

- **Multi-stage**: JDK só no build; a imagem final tem **JRE**, sem Maven nem fontes.
- **Não root**: `USER app`; o processo não escreve no sistema de arquivos (use `tmpfs`/volume
  se precisar de `/tmp`).
- Tag de imagem fixa (major/versão), não `latest`.
- `JAVA_TOOL_OPTIONS=-XX:MaxRAMPercentage=75` respeita o limite de memória do contêiner.
- Porta e config **por variável de ambiente** (`PORT`, `DB_URL`, `DB_USER`, `DB_PASSWORD`,
  `CORS_ORIGEM`); a mesma imagem serve em todos os ambientes.
- `.dockerignore`: `target/`, `.git/`, `.env`, IDE.
- Compose: a API `depends_on` o banco com `condition: service_healthy`; o Flyway roda na
  subida da API; reinício `unless-stopped`; ver `ambiente-docker`.
- Varredura da imagem (Trivy, open source) no CI antes do gate de segurança.
- Encerramento limpo: `server.shutdown=graceful` e
  `spring.lifecycle.timeout-per-shutdown-phase=20s` terminam requisições em curso no
  `SIGTERM` (o `ENTRYPOINT` em forma exec garante que o Java receba o sinal).

## Checklist deste arquivo

- [ ] Nenhum segredo em `application*.yml` ou no Dockerfile; senha do banco sem padrão.
- [ ] `include-stacktrace`/`include-message` em `never`; Actuator só com `health`.
- [ ] Logs em JSON no stdout, com `requestId`, sem dado sensível.
- [ ] `spotless:check` e Checkstyle no `verify`.
- [ ] Imagem multi-stage com JRE, usuário não root, tag fixa, config por ambiente.
- [ ] Healthcheck no Compose usa a readiness; API só sobe com banco saudável.

## Erros comuns

| Erro | Correção |
|---|---|
| Segredo no `application.yml` | Variável de ambiente; `.env.example` versionado |
| Expor todo o Actuator | `include: health` |
| Imagem com JDK + Maven na execução | Multi-stage, JRE |
| Contêiner rodando como root | `USER` não root |
| Logs em arquivo dentro da imagem | stdout |
| `latest` em tag de imagem | Versão fixa |
| Healthcheck em `/` (sem checar banco) | `/actuator/health/readiness` |
