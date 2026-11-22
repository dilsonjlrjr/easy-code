# Integração com banco real, teste de API e contrato

## Objetivo

Provar o que o unitário não alcança: **SQL, constraints, migrations, transações, mapeamento,
autenticação e o contrato HTTP**, com PostgreSQL de verdade. É a camada de maior retorno em
backend web.

## Obter um PostgreSQL para o teste

| Opção | Uso | Observação |
|---|---|---|
| **Testcontainers** | Sobe um container descartável pelo código de teste | Java (`testcontainers`), Go (`testcontainers-go`), Rust (crate `testcontainers`), Python (`testcontainers`), Node (`testcontainers`). Conferir módulo/API na versão usada. Precisa de Docker no ambiente |
| **Serviço do Compose** | O Compose do projeto expõe um Postgres de teste | Simples, um banco para tudo; combina com `ambiente-docker` |
| Banco embutido/SQLite/H2 "compatível" | **Evitar** | Dialeto e recursos diferentes (`EXCLUDE`, `tstzrange`, `jsonb`); esconde bug |

Regras:

- **Mesma versão major** do PostgreSQL de produção (tag fixa da imagem, a do Compose).
- **Um container por execução da suíte**, não por teste (sobe uma vez, reaproveita).
- Aplicar as **migrations reais** do projeto no início (Flyway, golang-migrate, sqlx migrate,
  Alembic ou o que a Arquitetura definiu). O teste valida também que a migration roda do zero.
- Nunca apontar a suíte para um banco compartilhado, de desenvolvimento ou de produção.

```java
// Java (Testcontainers + JUnit 5); conferir módulo `postgresql` na versão usada
@Testcontainers
class ReservaRepositoryIT {
    @Container static PostgreSQLContainer<?> pg = new PostgreSQLContainer<>("postgres:16");
    // apontar o DataSource do teste para pg.getJdbcUrl(), rodar migrations
}
```

```go
// Go (testcontainers-go): TestMain sobe 1 vez, guarda a URL, roda migrations e m.Run()
func TestMain(m *testing.M) {
    ctx := context.Background()
    // pgC, _ := postgres.Run(ctx, "postgres:16", ...)  // módulo postgres (conferir API)
    // dsn, _ := pgC.ConnectionString(ctx, "sslmode=disable")
    os.Exit(m.Run())
}
```

```python
# Python (pytest): fixture de escopo "session"
@pytest.fixture(scope="session")
def pg():
    with PostgresContainer("postgres:16") as c:   # testcontainers.postgres (conferir)
        yield c.get_connection_url()
```

Em Rust, `#[sqlx::test]` (sqlx) cria um banco isolado por teste a partir de um servidor
existente e aplica as migrations (conferir na versão usada).

## Isolamento entre testes

Cada teste parte de um estado conhecido e **não depende** de outro. Escolha uma estratégia:

| Estratégia | Como | Quando | Cuidado |
|---|---|---|---|
| **Transação revertida** | Abre transação no início, `ROLLBACK` no fim | Repositório/serviço sem commit explícito; muito rápido | Não vale se o código sob teste faz `COMMIT` próprio, usa várias conexões ou testa concorrência |
| **TRUNCATE entre testes** | `TRUNCATE ... RESTART IDENTITY CASCADE` das tabelas de dados | Teste de API ponta a ponta com várias conexões | Mais lento; manter lista de tabelas atualizada |
| **Schema/banco por teste ou por worker** | `CREATE DATABASE ... TEMPLATE modelo` (clone rápido) | Execução em paralelo | Gerenciar nomes e limpeza |
| **Dados únicos por teste** | Cada teste cria seus próprios ids/nomes e filtra por eles | Quando nada pode ser limpo | Nunca assumir tabela vazia |

Padrão recomendado para aplicação pequena: **banco único por execução + TRUNCATE (ou transação quando
couber) entre testes + dados criados pelo próprio teste**. Paralelismo: um banco clonado por
worker (`TEMPLATE`).

Regra: **nenhum teste assume a ordem** nem o conteúdo deixado por outro.

## Dados de teste

- **Builders/fábricas** criam o mínimo válido; o teste sobrescreve só o que importa.
- Valores literais legíveis (sala "Azul", 9h–10h), não aleatórios, salvo teste de propriedade.
- **Sem dado real** de pessoa; e-mails em domínio reservado (`@exemplo.com`).
- Seed de referência (papéis, catálogos) vem **da migration**, igual a produção; dados de
  cenário vêm do teste.
- Senhas/hashes de teste geradas no setup; segredo real nunca no repositório.

```python
def uma_sala(db, nome="Azul", capacidade=8): ...
def uma_reserva(db, sala, usuario, inicio, fim): ...
```

## Teste de repositório/consulta

Foque no que é do banco:

| Caso | Prova |
|---|---|
| Inserir e ler de volta | Mapeamento e tipos (inclusive fuso/`timestamptz`, nulos) |
| Violar constraint única/exclusão/FK | A garantia existe e o erro tem o tipo esperado |
| Consulta com filtro/paginação/ordenação | Resultado e ordem corretos com 0, 1, N+1 linhas |
| Limite de página, página além do fim | Sem erro, vazio |
| Soft delete / estado | Consultas respeitam o filtro |

```sql
-- o que o teste de sobreposição realmente exercita
ALTER TABLE reserva ADD CONSTRAINT sem_sobreposicao
  EXCLUDE USING gist (sala_id WITH =, periodo WITH &&);
```

O teste insere A (9h–10h), tenta B (9h30–10h30) e espera o **erro de violação**, traduzido
pelo código em erro de domínio. Depois confere que só A existe (estado final).

## Teste de API (HTTP)

Suba a aplicação (ou o handler em memória do framework) com o banco real e chame pelo HTTP.

Para cada endpoint relevante, cobrir:

| Eixo | Casos mínimos |
|---|---|
| Sucesso | Status, cabeçalhos (ex.: `Location`), corpo conforme o contrato, **estado no banco** |
| Validação | Campo faltando/inválido: 400/422 com Problem Details e campo apontado; nada gravado |
| Autenticação | Sem credencial: 401; credencial inválida/expirada: 401 |
| Autorização | Outro usuário/papel errado: 403 (ou 404 para não vazar existência) conforme a ADR |
| Regra de negócio | Conflito: 409; não encontrado: 404; estado inválido: 409/422 |
| Erro interno | Falha de dependência vira 5xx **sem vazar** stack/SQL |

Ferramentas por linguagem (conferir API na versão): Spring `MockMvc`/`WebTestClient` ou
`TestRestTemplate` (Java), `httptest` (Go), `axum::Router` + `tower::ServiceExt::oneshot` ou
`reqwest` contra servidor local (Rust), `TestClient` do FastAPI/`httpx` ou cliente de teste do
framework (Python), `supertest` (Node).

```go
func TestCriarReserva_Sobreposta_Retorna409ENaoGrava(t *testing.T) {
    app := novaApp(t)                    // banco real, tabelas limpas
    app.criarReserva(t, "Azul", "09:00", "10:00")
    resp := app.POST(t, "/reservas", `{"salaId":"azul","inicio":"...","fim":"..."}`, comoAna)
    assertStatus(t, resp, 409)
    assertProblem(t, resp, "reserva-conflito")
    if n := app.contarReservas(t, "Azul"); n != 1 { t.Fatalf("esperava 1, tem %d", n) }
}
```

## Teste de contrato contra o OpenAPI

O `openapi.yaml` é lei (instruções do Dev). Prove que a implementação o respeita:

1. **Validar respostas reais contra o esquema**: no teste de API, valide o corpo e o status
   retornados contra a definição da operação no OpenAPI (biblioteca validadora de resposta do
   ecossistema: ex. `openapi-core`/`jsonschema` em Python, validadores de OpenAPI para
   Java/Go/TypeScript; **conferir** a que o projeto adotar).
2. **Teste baseado em propriedades gerado do OpenAPI**: Schemathesis (open source, Python)
   gera casos a partir do contrato e procura 5xx, violações de esquema e respostas não
   documentadas; funciona contra qualquer backend por HTTP (conferir a CLI na versão usada).
3. **Cobertura do contrato**: todo status documentado de um endpoint tem teste; todo status
   que a aplicação devolve está documentado.
4. **Erro de contrato = ACH para Arquitetura (`etapa_destino: arquitetura`)**, não ajuste silencioso do código nem do
   contrato.

O que contrato **não** substitui: regra de negócio (continua nos testes acima).

Quando o front é Vue: os tipos TypeScript gerados do mesmo OpenAPI fecham o ciclo (ver
`frontend-vue`).

## Migrations

- Um teste (ou o próprio setup) aplica **todas** as migrations do zero e falha se alguma
  quebrar.
- Para mudança sobre dados existentes: teste com dados no estado anterior (insere antes,
  migra, confere depois).
- Migration e modelo (`docs/dados/modelo-dados.dbml`) concordam; divergência = ACH para o DBA (`categoria: inconsistencia_dados`, `etapa_destino: dados`).

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| H2/SQLite no lugar do PostgreSQL | Verde local, falha em produção | Mesmo SGBD e versão major |
| Container por teste | Suíte de minutos | Um por suíte, isolar por TRUNCATE/transação |
| Transação revertida com código que faz `COMMIT` | Teste vaza dados ou não prova a transação | TRUNCATE ou banco por teste |
| Assumir tabela vazia | Quebra em paralelo/ordem | Dados próprios do teste |
| Só conferir status HTTP | Efeito colateral passa | Conferir também o estado no banco |
| Teste de API com mocks de service | Não é teste de API | Pilha real, só a borda externa dublada |
| Seed fora da migration | Teste difere de produção | Referência na migration |
| Ignorar fuso no `timestamptz` | Falha só em outro ambiente | Fixar UTC e testar a conversão |
