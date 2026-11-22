# Persistência (Java): JDBC x JPA, transações, armadilhas, Flyway

Modelagem, índices e constraints: `modelagem-postgresql`. Aqui: acesso a dados em Spring.

## JdbcClient ou JPA: critério

| Pergunta | JdbcClient (padrão) | JPA/Hibernate |
|---|---|---|
| O domínio é CRUD + consultas com `JOIN`/agregação/`RANGE` do PostgreSQL? | Sim, SQL direto | Exige JPQL/nativa de qualquer jeito |
| Quer ver e revisar o SQL exato? | Sim | Gerado; precisa ligar log para ver |
| Usa recursos do PostgreSQL (`EXCLUDE`, `tstzrange`, `RETURNING`, CTE)? | Natural | Atrito |
| Muitas entidades com grafo navegável e atualização por dirty checking? | Verboso | Ponto forte |
| Curva de aprendizado/armadilhas (lazy, N+1, flush, proxy) | Baixa | Alta |
| Time já experiente em JPA e modelo é de grafo | Possível | Pode compensar |

Recomendação para APIs deste tipo (poucas tabelas, regra de conflito no banco): **`JdbcClient`**.
Não misture os dois no mesmo agregado sem motivo; se a ADR escolher JPA, siga a seção
"JPA, quando escolhido". Alternativas (jOOQ, MyBatis): citar na ADR com o critério de troca da
`SKILL.md`.

## JdbcClient: repositório completo

`JdbcClient` (Spring Framework 6.1+) é a API fluente sobre `JdbcTemplate`; parâmetros nomeados
com `:nome`. Bean criado pelo Spring Boot com o `starter-jdbc` (conferir na versão fixada).

```java
@Repository
class ReservaRepository {

  private static final RowMapper<Reserva> MAPEADOR = (rs, n) -> new Reserva(
      rs.getLong("id"), rs.getLong("sala_id"), rs.getLong("usuario_id"),
      rs.getObject("inicio", OffsetDateTime.class), rs.getObject("fim", OffsetDateTime.class),
      rs.getString("titulo"));

  private static final String COLUNAS = "id, sala_id, usuario_id, inicio, fim, titulo";

  private final JdbcClient jdbc;

  ReservaRepository(JdbcClient jdbc) {
    this.jdbc = jdbc;
  }

  Optional<Reserva> buscar(long id) {
    return jdbc.sql("SELECT " + COLUNAS + " FROM reservas WHERE id = :id")
        .param("id", id)
        .query(MAPEADOR)
        .optional();
  }

  List<Reserva> daSalaNoDia(long salaId, OffsetDateTime de, OffsetDateTime ate) {
    return jdbc.sql("SELECT " + COLUNAS + """
         FROM reservas
         WHERE sala_id = :sala AND inicio >= :de AND inicio < :ate
         ORDER BY inicio, id""")
        .param("sala", salaId).param("de", de).param("ate", ate)
        .query(MAPEADOR)
        .list();
  }

  boolean remover(long id, long usuarioId) {
    int linhas = jdbc.sql("DELETE FROM reservas WHERE id = :id AND usuario_id = :u")
        .param("id", id).param("u", usuarioId)
        .update();
    return linhas == 1;
  }
}
```

- **Sempre parâmetro** (`:nome` + `.param`), nunca concatenar valor na string (injeção de
  SQL). Concatenar só constantes do código (lista de colunas).
- `.optional()` para busca por chave; `.single()` quando a linha **tem** de existir (lança
  exceção se faltar); `.list()` para listas; `.update()` devolve linhas afetadas.
- `INSERT ... RETURNING` com `.query(MAPEADOR).single()` devolve a linha criada (ver
  `api-validacao-erros.md`).
- `query(Classe.class)` mapeia por nome de coluna para record/bean (com conversão
  `snake_case`->`camelCase`); aceitável, mas o `RowMapper` explícito é mais seguro (conferir).
- Mapeador de `timestamptz`: `rs.getObject("col", OffsetDateTime.class)`. Datas sempre com
  fuso.
- Autorização embutida no SQL (`AND usuario_id = :u`) é uma boa defesa em profundidade para
  IDOR; o serviço ainda decide o status (404 se `remover` devolveu `false`).

## Transações

```java
@Service
public class ReservaService {
  @Transactional                       // escrita: um commit atômico
  public Reserva criar(...) { ... }

  @Transactional(readOnly = true)      // leitura: dica ao driver/ORM
  public Reserva buscar(long id, UsuarioAutenticado u) { ... }
}
```

- Fronteira transacional no **serviço**, nunca no controller nem no repositório (o caso de
  uso é a unidade atômica).
- Transação **curta**: sem chamada HTTP externa, sem `Thread.sleep`, sem espera de fila dentro.
- Por padrão, só `RuntimeException`/`Error` fazem rollback; exceção checada **não**. Use
  exceções de domínio unchecked (como em `api-validacao-erros.md`) ou `rollbackFor`.
- **Auto-invocação não abre transação**: `this.outroMetodoTransacional()` ignora o proxy. Mova
  para outro bean ou ajuste a fronteira.
- `@Transactional` só funciona em método **público** de bean gerenciado, chamado de fora.
- Duas operações que precisam ser atômicas (ex.: criar reserva + registrar auditoria) ficam
  **no mesmo método transacional**.
- Concorrência: a invariante vai para constraint (`EXCLUDE`/`UNIQUE`); para "contador"/estoque
  use `UPDATE ... SET x = x - 1 WHERE x > 0` (atômico) ou lock otimista (`@Version` em JPA /
  coluna `versao` e `WHERE versao = :v` em JDBC; 0 linhas = conflito = 409). `SELECT ... FOR
  UPDATE` só quando realmente precisar bloquear.
- Nível de isolamento padrão (`READ COMMITTED`) basta com constraint; `SERIALIZABLE` exige
  repetir a transação em falha `40001`: evitar em aplicação pequena.

## JPA, quando escolhido

Dependência `spring-boot-starter-data-jpa`; configuração mínima segura:

```yaml
spring:
  jpa:
    open-in-view: false          # padrão é true: desligar
    hibernate:
      ddl-auto: none             # schema é do Flyway (ou validate)
    properties:
      hibernate.jdbc.time_zone: UTC
```

Entidade enxuta, **separada do DTO**:

```java
@Entity
@Table(name = "reservas")
class ReservaEntidade {
  @Id @GeneratedValue(strategy = GenerationType.IDENTITY) private Long id;
  @ManyToOne(fetch = FetchType.LAZY, optional = false) @JoinColumn(name = "sala_id") private SalaEntidade sala;
  @Column(nullable = false) private OffsetDateTime inicio;
  @Column(nullable = false) private OffsetDateTime fim;
  @Column(nullable = false) private String titulo;
  protected ReservaEntidade() {}       // exigido pelo JPA
  // construtor de negócio + getters necessários; sem setters públicos de tudo
}
```

Armadilhas e correção:

| Armadilha | Sintoma | Correção |
|---|---|---|
| **N+1**: listar reservas e acessar `r.getSala().getNome()` em laço | 1 consulta + N | `JOIN FETCH` na consulta, `@EntityGraph`, ou consulta direta a **DTO** (`SELECT new ...`/projeção) |
| `FetchType.EAGER` em tudo | Joins gigantes sempre | `LAZY` por padrão, buscar sob demanda |
| Lazy fora da transação (`LazyInitializationException`) | Erro ao serializar | Montar o DTO **dentro** do serviço transacional; não devolver entidade |
| `open-in-view: true` (padrão) | Conexão presa até o fim da resposta, lazy "funciona" e esconde N+1 | `open-in-view: false` |
| Violação de constraint só no commit | 409 não traduzido, erro estranho | `saveAndFlush` dentro do `try` ou capturar na fronteira do serviço |
| `equals/hashCode` por `id` gerado | Entidade nova "igual" a outra | Chave de negócio ou `id` não nulo |
| `ddl-auto: update/create` | Schema diverge das migrações | `none`/`validate` |
| `@Data`/setters em tudo | Estado mutável sem regra | Métodos de negócio na entidade |
| Entidade na API | Vazamento e acoplamento | DTO `record` |

- Consulta que devolve só leitura/lista para tela: **projeção/DTO**, sem carregar o grafo.
- Ligue `spring.jpa.properties.hibernate.generate_statistics` ou log de SQL **só em teste**
  para provar que a lista faz 1 consulta (teste de N+1).

## Flyway

```
backend/src/main/resources/db/migration/
  V1__criar_usuarios_e_salas.sql
  V2__criar_reservas_com_exclusion.sql
  V3__criar_tabelas_de_sessao.sql
```

- Nome `V<versão>__<descrição>.sql` (dois sublinhados). Versão crescente e única.
- **Migração aplicada é imutável**: erro vira uma `V` nova de correção, nunca edição (o
  checksum quebra o `validate`).
- O Spring Boot roda o Flyway na subida (`spring.flyway.enabled=true` é o padrão com o
  starter e um `DataSource`). Em Docker Compose, a API sobe depois de o banco estar saudável
  (`depends_on` com `condition: service_healthy`; ver `ambiente-docker`).
- A mesma migração deve rodar em **PostgreSQL limpo** nos testes (Testcontainers): isso valida
  o `esquema.sql` derivado do DBML.
- Dados de referência (papéis, salas-semente) em migração `V`; dados de **demo** só em perfil
  ou script separado, nunca em `V`.
- Mudança destrutiva (remover coluna) em dois passos: parar de usar no código -> migração
  remove em release seguinte.
- Tabelas do Spring Session: crie por migração (copie o DDL de
  `org/springframework/session/jdbc/schema-postgresql.sql` do jar da versão fixada) e use
  `spring.session.jdbc.initialize-schema=never` para o Flyway ser o único dono do schema.
- Migração longa em tabela grande (índice em tabela cheia): `CREATE INDEX CONCURRENTLY` exige
  rodar fora de transação (`executeInTransaction=false` num arquivo `.sql.conf`, conferir);
  em aplicação pequena não é necessário.

## Pool de conexões

- HikariCP é o padrão do Spring Boot. Ajuste só se medir:
  `spring.datasource.hikari.maximum-pool-size` (padrão 10 costuma bastar).
- Pool maior que o necessário piora o PostgreSQL; some `max_connections` de todas as
  instâncias antes de aumentar.
- `spring.datasource.hikari.connection-timeout` curto o bastante para falhar rápido e virar
  503/500 controlado, não pendurar a requisição.

## Erros comuns

| Erro | Correção |
|---|---|
| Concatenar valor no SQL | `:nome` + `.param` |
| `@Transactional` por `this.` | Outro bean ou mover fronteira |
| Chamada HTTP dentro da transação | Fazer antes/depois da transação |
| Editar migração aplicada | Nova migração `V` |
| `ddl-auto: update` | `none` + Flyway |
| Lista com lazy em laço | `JOIN FETCH`/DTO por consulta; teste conta consultas |
| `H2` para testar SQL do PostgreSQL | Testcontainers |
| `findAll()` sem limite | Paginação com teto |
