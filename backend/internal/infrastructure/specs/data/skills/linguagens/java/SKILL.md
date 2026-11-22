---
name: backend-java
description: Convenções e boas práticas para backend Java de API REST com PostgreSQL - stack padrão (Spring Boot, Maven wrapper, Flyway, JUnit 5, AssertJ, Mockito, Testcontainers, Spotless, Checkstyle, springdoc), estrutura por funcionalidade, records para DTO, validação com Bean Validation, erros com ProblemDetail e tradução de violação de constraint para 409, paginação, persistência (JPA versus JdbcClient, transações, N+1, open-in-view, Flyway), autenticação com Spring Security (sessão em cookie, Spring Session JDBC, hash de senha, checagem por recurso, CSRF), testes (unitário sem Spring, slices, integração com Testcontainers, MockMvc), logs estruturados, Actuator, configuração por ambiente e Docker (multi-stage, JRE, não root). Use em Arquitetura, Dev, QA e Segurança em projetos com backend Java.
---

# Backend Java

API REST em Java simples, idiomática e testável: Spring Boot, regra no domínio/serviço, SQL e
constraints no PostgreSQL, erros em Problem Details. O backend **é a fonte da regra e da
autorização**; o front só apresenta. Stack padrão abaixo; a Arquitetura pode mudar com ADR.
Skills vizinhas (não duplicadas aqui; esta skill dá só o "como em Java"): `estrategia-testes` e
`qualidade-codigo` (Dev), `padroes-arquitetura-codigo`, `modelagem-postgresql`,
`ambiente-docker` e `arquitetura-web` (Arquitetura), `revisao-seguranca` (Segurança).

## Princípios

1. **Simplicidade primeiro.** Classe e método simples antes de abstração; sem interface com
   uma só implementação "por padrão"; sem camada que só repassa chamada.
2. **Controller fino, regra no serviço.** Controller valida formato, delega e traduz o
   resultado; a regra de negócio vive em classe sem dependência de HTTP.
3. **O banco protege a invariante.** Unicidade, sobreposição e FK são constraints
   (`UNIQUE`, `EXCLUDE`, `FOREIGN KEY`); a aplicação traduz a violação em 409. Verificar
   "antes" na aplicação sozinho é condição de corrida.
4. **Nunca expor entidade na API.** DTO de entrada e de saída como `record`; o contrato é o
   `openapi.yaml`.
5. **Dependência explícita.** Injeção por construtor, campos `final`; sem `@Autowired` em
   campo; sem estado mutável em singleton.
6. **Ausência é explícita.** `Optional` no retorno de busca; "não encontrado" vira exceção de
   domínio no serviço; nada de `null` atravessando camadas.
7. **Segurança por padrão.** Sessão em cookie `HttpOnly`, hash adaptativo, checagem por
   recurso em todo endpoint protegido, segredos só por ambiente.

## Stack padrão

| Item | Padrão |
|---|---|
| Linguagem | Java LTS atual |
| Framework | Spring Boot estável atual (Web, Validation, Security, JDBC ou Data JPA, Actuator) |
| Build | Maven com wrapper (`./mvnw`) |
| Migração | Flyway (`src/main/resources/db/migration/V1__criar_salas.sql`) |
| Persistência | `JdbcClient` (SQL explícito) por padrão; JPA quando o modelo é de grafo de objetos |
| Sessão | Spring Security + Spring Session JDBC (cookie `HttpOnly`, sessão no PostgreSQL) |
| Teste | JUnit 5 + AssertJ + Mockito; integração com Testcontainers (PostgreSQL real) |
| Lint/formato | Spotless (google-java-format) + Checkstyle |
| Documentação da API | springdoc-openapi, **conferido** contra o `openapi.yaml` da Arquitetura |
| Logs | Logging estruturado do Spring Boot (JSON) + Actuator `health` |

Versões: "LTS atual" e "estável atual"; as efetivas ficam em `agentes/ambiente.md`. Major do Spring
Boot muda pacotes de teste e de auto-configuração: conferir a documentação da versão fixada.

## Alternativas e quando trocar

| Padrão | Alternativa | Trocar quando |
|---|---|---|
| Spring Boot | Quarkus, Micronaut | Requisito real de startup/memória (nativo, serverless) ou equipe já dominar; custo: sair do ecossistema Spring Security/Session |
| `JdbcClient` | JPA/Hibernate | Muitas entidades com relações navegáveis e escrita por grafo; time experiente em JPA |
| `JdbcClient` | jOOQ, MyBatis | Muito SQL dinâmico/complexo e desejo de tipagem das consultas |
| Maven | Gradle | Build multi-módulo grande ou time já padronizado em Gradle |
| Spring Session JDBC | Token (JWT) | Clientes não-navegador sem cookie; aceitar revogação mais difícil (ADR) |
| Spotless + Checkstyle | Só Spotless | Time pequeno que aceita só formatação automática |

Troca de framework ou de persistência exige ADR; aqui o catálogo é um padrão por linguagem.

## Regras de decisão

| Situação | Faça | Evite | Referência |
|---|---|---|---|
| Organizar pacotes | Por funcionalidade (`reserva/`, `sala/`) | Pacotes `controller/`, `service/` globais | estrutura-convencoes |
| DTO | `record` + Bean Validation | Entidade JPA no JSON | estrutura-convencoes, api-validacao-erros |
| Validar entrada | `@Valid` na borda + regra no serviço | Só validar no front | api-validacao-erros |
| Erro de regra | Exceção de domínio + `@RestControllerAdvice` -> `ProblemDetail` | `try/catch` no controller | api-validacao-erros |
| Violação de `EXCLUDE`/`UNIQUE` | Capturar, ler SQLSTATE `23P01`/`23505`, responder 409 | Consultar "existe?" e depois inserir | api-validacao-erros |
| Escolher JPA ou JDBC | `JdbcClient` por padrão; JPA por critério | JPA "porque todo mundo usa" | persistencia-jpa-jdbc |
| Transação | `@Transactional` no serviço, curta, sem I/O externo dentro | `@Transactional` no controller; transação aberta durante chamada HTTP | persistencia-jpa-jdbc |
| Lista de itens relacionados (JPA) | `JOIN FETCH`/`@EntityGraph`/DTO por consulta | Lazy dentro de laço (N+1) | persistencia-jpa-jdbc |
| Login | Sessão em cookie + Spring Session JDBC | Token em `localStorage` | seguranca-sessao-spring-security |
| Autorização | Checar papel **e** dono do recurso | Só `hasRole` na rota | seguranca-sessao-spring-security |
| Teste da regra | Unitário sem Spring | `@SpringBootTest` para testar `if` | testes-java |
| Teste de constraint/SQL | Testcontainers com PostgreSQL real | H2 "compatível" | testes-java |
| Config por ambiente | Variáveis de ambiente + `application.yml` base | Senha em arquivo versionado | observabilidade-config-docker |

## Estrutura (resumo)

```
backend/src/main/java/<pacote>/
  reserva/   ReservaController, ReservaService, ReservaRepository, Reserva, dto/
  sala/
  auth/
  comum/     erros (Problem Details), configuração
backend/src/main/resources/db/migration/   V1__...sql
backend/src/test/java/<pacote>/            espelha o main
```

Detalhe em `references/estrutura-convencoes.md`.

## Fluxo de trabalho (Dev, por T de backend)

1. Ler a T, o `openapi.yaml`, as constraints do `docs/dados/esquema.sql` e os CT.
2. Migração Flyway primeiro (se a T muda o schema); nunca editar migração já aplicada.
3. Teste primeiro (vermelho): unitário da regra; depois integração do endpoint.
4. Implementar: DTO `record` -> serviço -> repositório -> controller fino.
5. Mapear erros: validação -> 400, regra violada -> 409/404, sem sessão -> 401, sem permissão
   -> 403, tudo em `ProblemDetail`.
6. Rodar `./mvnw verify` (compila, formata, lint, testes unitários e de integração).
7. Conferir springdoc/`openapi.yaml`, a checklist abaixo e registrar o que for na T.

## Comandos

| Ação | Comando |
|---|---|
| Build + testes + lint | `./mvnw verify` |
| Só testes | `./mvnw test` (unitários) / `./mvnw verify` (inclui integração) |
| Formatar | `./mvnw spotless:apply` |
| Checar formato | `./mvnw spotless:check` |
| Rodar local | `./mvnw spring-boot:run` |
| Dependências desatualizadas | `./mvnw versions:display-dependency-updates` |

## Checklist de revisão Java

- [ ] Controller só valida, delega e traduz; sem regra nem SQL nele.
- [ ] DTOs como `record` com Bean Validation; nenhuma entidade na resposta.
- [ ] Injeção por construtor; sem `@Autowired` em campo; sem estado mutável em singleton.
- [ ] Invariantes (unicidade, sobreposição, FK) como constraint; violação vira 409 por SQLSTATE.
- [ ] Erros em `ProblemDetail`; 400 (formato/regra de entrada) x 409 (conflito de estado) corretos.
- [ ] `@Transactional` no serviço, curta; `open-in-view` desligado; sem N+1 nas listas.
- [ ] Migrações Flyway versionadas e imutáveis; `ddl-auto` em `none`/`validate`.
- [ ] Sessão: cookie `HttpOnly`/`Secure`/`SameSite`, ID trocado no login, CSRF tratado.
- [ ] Hash de senha adaptativo; mensagem de login igual para usuário inexistente e senha errada.
- [ ] Todo endpoint protegido checa o dono do recurso; 401 x 403 corretos.
- [ ] Teste unitário sem Spring; integração com PostgreSQL real; nomes dizem o comportamento.
- [ ] Sem segredo em código/`application.yml`; logs sem senha, token ou dado pessoal.
- [ ] `/actuator/health` exposto só o necessário; demais endpoints do Actuator fechados.
- [ ] Imagem Docker multi-stage, JRE, usuário não root, porta e config por ambiente.
- [ ] `./mvnw verify` verde; springdoc conferido contra o `openapi.yaml`.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Checar conflito com `SELECT` e depois `INSERT` | Duas requisições simultâneas passam; dado inválido | Constraint `EXCLUDE`/`UNIQUE` + capturar e responder 409 |
| Entidade JPA devolvida no controller | Lazy exception, vazamento de campos, contrato instável | DTO `record` |
| `spring.jpa.open-in-view` padrão (ligado) | Conexão presa durante a serialização; N+1 escondido | `open-in-view: false`, buscar o que precisa no serviço |
| `@Transactional` em método chamado por `this` | Transação não abre (proxy) | Chamar por outro bean ou mover a fronteira |
| `ddl-auto: update` | Schema fora do controle das migrações | `none`/`validate` + Flyway |
| H2 nos testes de integração | Constraint e SQL de produção não testados | Testcontainers com PostgreSQL |
| Senha com hash rápido (SHA-256) | Quebra por força bruta | bcrypt/argon2 via `PasswordEncoder` |
| Só `hasRole("USER")` na rota | Usuário A altera recurso de B (IDOR) | Checar dono do recurso no serviço |
| `catch (Exception e) { return null; }` | Erro engolido, `null` espalhado | Propagar; `@RestControllerAdvice` decide a resposta |
| Stack trace na resposta HTTP | Vazamento de detalhe interno | `server.error.include-stacktrace=never`; `ProblemDetail` sem interno |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/estrutura-convencoes.md` | Ao criar o projeto: pom, pacotes, records, injeção, controller fino, nomes |
| `references/api-validacao-erros.md` | Para validação, `ProblemDetail`, `@RestControllerAdvice`, 409 por constraint e paginação |
| `references/persistencia-jpa-jdbc.md` | Para escolher JPA ou JDBC, escrever repositório, transações, armadilhas e Flyway |
| `references/seguranca-sessao-spring-security.md` | Para login, sessão, hash de senha, autorização por recurso e CSRF |
| `references/testes-java.md` | Para testes unitários, slices, integração com Testcontainers e MockMvc |
| `references/observabilidade-config-docker.md` | Para logs estruturados, Actuator, configuração por ambiente, lint e Docker |
