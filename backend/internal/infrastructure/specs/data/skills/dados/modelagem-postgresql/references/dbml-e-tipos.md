# DBML, tipos e chaves

## DBML: papel e limites

DBML (Database Markup Language) descreve tabelas, colunas, relações e índices de forma
legível; ferramentas open source (por exemplo o conversor `dbml2sql` do pacote
`@dbml/cli`) geram DDL e diagramas a partir dele. Fluxo do DBA:
`docs/dados/modelo-dados.dbml` -> `esquema.sql` -> revisão -> migração inicial do Dev.

**Limites:** DBML não expressa tudo do PostgreSQL (`EXCLUDE`, índice parcial complexo,
extensões, `CHECK` elaborado, tipos de intervalo em alguns conversores). Estratégia:
registre o que DBML não cobre em `Note:` e **complemente o `esquema.sql` à mão**, com a mesma
regra documentada. Valide o resultado rodando-o em PostgreSQL limpo. **Conferir** o suporte
da versão do conversor a cada recurso.

## Exemplo DBML

```dbml
Project reserva_salas {
  database_type: 'PostgreSQL'
  Note: 'Modelo de reserva de salas. Constraint EXCLUDE de reservas fica no esquema.sql (DBML nao expressa).'
}

Enum papel_usuario {
  USUARIO
  ADMIN
}

Table usuarios {
  id          bigint [pk, increment]
  email       text   [not null, unique]
  senha_hash  text   [not null]
  papel       papel_usuario [not null, default: 'USUARIO']
  created_at  timestamptz [not null, default: `now()`]
  updated_at  timestamptz [not null, default: `now()`]
}

Table salas {
  id          bigint [pk, increment]
  nome        text    [not null, unique]
  capacidade  integer [not null, note: 'CHECK (capacidade > 0)']
  ativa       boolean [not null, default: true]
  created_at  timestamptz [not null, default: `now()`]
  updated_at  timestamptz [not null, default: `now()`]
}

Table reservas {
  id          bigint [pk, increment]
  sala_id     bigint [not null]
  usuario_id  bigint [not null]
  inicio      timestamptz [not null]
  fim         timestamptz [not null, note: 'CHECK (fim > inicio)']
  created_at  timestamptz [not null, default: `now()`]

  indexes {
    sala_id
    usuario_id
    (sala_id, inicio)
  }
  Note: 'EXCLUDE USING gist (sala_id WITH =, tstzrange(inicio, fim) WITH &&) no esquema.sql'
}

Ref: reservas.sala_id    > salas.id    [delete: restrict]
Ref: reservas.usuario_id > usuarios.id [delete: restrict]
```

Observação: neste exemplo `inicio`/`fim` são duas colunas e a `EXCLUDE` usa a expressão
`tstzrange(inicio, fim)`. A alternativa é uma coluna `periodo tstzrange`. Escolha em ADR:
duas colunas são mais legíveis para o front/API; uma coluna de intervalo simplifica a
constraint. Ver `constraints.md`.

## Convenções de nomes

| Elemento | Convenção | Exemplo |
|---|---|---|
| Tabela | `snake_case`, plural | `reservas` |
| Coluna | `snake_case`, singular | `usuario_id` |
| PK | `id` | `id` |
| FK | `<entidade>_id` | `sala_id` |
| Constraint | `<tabela>_<regra>_<tipo>` | `reservas_fim_apos_inicio_ck` |
| Índice | `<tabela>_<colunas>_idx` | `reservas_sala_inicio_idx` |
| Booleano | adjetivo/estado | `ativa` |
| Instante | `_em` ou `_at`, um só padrão | `created_at` |

Nomear constraints explicitamente permite mapear violação a mensagem de erro.

## Tipos: escolhas e porquês

| Necessidade | Tipo | Observação |
|---|---|---|
| Instante | `timestamptz` | Armazena em UTC; converte na exibição. `timestamp` sem fuso faz o dado mudar de significado conforme a sessão |
| Data sem hora | `date` | Aniversário, vencimento |
| Duração | `interval` ou inteiro com unidade no nome (`duracao_min`) | |
| Dinheiro | `numeric(12,2)` ou `bigint` em centavos | Nunca `real`/`double precision` |
| Inteiros | `integer`, `bigint` | `smallint` só com limite natural |
| Texto | `text` | No PostgreSQL `varchar(n)` não é mais rápido; use `CHECK` para regra |
| Booleano | `boolean NOT NULL` | Evite `NULL` como terceiro estado |
| Identificador | `bigint identity` ou `uuid` | Ver abaixo |
| E-mail | `text` + unicidade sobre `lower(email)` | Ou extensão `citext` (conferir necessidade) |
| JSON | `jsonb` | Para dado realmente semiestruturado; não para fugir da modelagem |
| Intervalo | `tstzrange`, `daterange` | Com `EXCLUDE` para não sobrepor |

### uuid vs identity

| Critério | `bigint identity` | `uuid` |
|---|---|---|
| Simplicidade/tamanho | Menor, rápido, legível | 16 bytes |
| Exposição na API | Enumerável (adivinhável) | Não adivinhável |
| Gerado fora do banco / offline | Não | Sim |
| Ordenação por inserção/localidade do índice | Ótima | UUID aleatório fragmenta o índice; versões ordenáveis por tempo (ex.: UUIDv7, nativa só em versões recentes: conferir) |
| Mesclar bases | Difícil | Fácil |

Recomendação para app pequeno: **PK `bigint identity`**; se for preciso expor id não
enumerável, adicionar coluna `public_id uuid` única, ou usar `uuid` como PK por decisão em
ADR. `gen_random_uuid()` é nativa em PostgreSQL recentes (a partir da 13; antes exigia
extensão: conferir). Prefira `GENERATED ALWAYS AS IDENTITY` a `serial`.

## Enum vs tabela vs CHECK

| Opção | Use quando | Prós | Contras |
|---|---|---|---|
| `CHECK (col IN ('A','B'))` | Lista curta e estável, sem atributos | Simples; mudar é uma migração leve | Valores repetidos como texto; lista duplicada no código |
| `ENUM` (tipo do PostgreSQL) | Lista fechada e estável, usada em várias tabelas | Armazenamento compacto; erro claro de valor inválido | Remover/renomear valor é trabalhoso; `ADD VALUE` tem restrições dentro de transação (conferir por versão) |
| Tabela de domínio + FK | Lista editável por admin, ou com atributos (cor, ordem, ativo) | Flexível; integridade referencial | JOIN extra; seed obrigatório |

Regra: **CHECK por padrão**; **tabela** se admin altera ou se há atributo; **enum** se a
lista é fechada e transversal. Qualquer opção: a lista também entra no OpenAPI.

## Estados

Estado como coluna `status text NOT NULL CHECK (status IN (...))` com default. Transições
permitidas são regra de serviço; se precisar impedir transições inválidas no banco, trigger
cobre mas esconde lógica (use com ADR). Guardar também `cancelada_em timestamptz` em vez de
só um booleano preserva histórico.

## Auditoria e remoção

- `created_at`/`updated_at` `timestamptz NOT NULL DEFAULT now()`. `updated_at` atualizado
  pela aplicação ou por trigger simples (padronizar em um só lugar).
- **Soft delete** (`deleted_at`) só com requisito de recuperação/histórico; custa em toda
  consulta e em `UNIQUE` (precisa de índice parcial `WHERE deleted_at IS NULL`).
- Dado pessoal: guardar o mínimo; nunca senha em claro; apontar para `revisao-seguranca`.

## Normalização pragmática

- 3FN por padrão: cada fato em um lugar.
- Desnormalizar (coluna calculada, tabela de resumo) só com consulta medida que não cabe
  em índice; documentar e manter consistência (coluna `GENERATED` ou transação).
- Evitar `jsonb` para o que tem estrutura conhecida e é filtrado/unido.
- Evitar EAV (chave-valor genérico).

## Checklist

- [ ] DBML válido e consistente com `esquema.sql`; o que DBML não expressa está em `Note` e no DDL.
- [ ] Convenção de nomes uniforme e constraints nomeadas.
- [ ] Tipos conforme tabela (instante, dinheiro, texto, id).
- [ ] Decisão de PK (identity vs uuid) em ADR.
- [ ] Enum vs tabela vs CHECK decidido por critério.
- [ ] Soft delete só com requisito.

## Erros comuns

| Erro | Correção |
|---|---|
| `serial` em esquema novo | `GENERATED ALWAYS AS IDENTITY` |
| `varchar(255)` por hábito | `text` + CHECK se houver regra |
| `timestamp` sem fuso | `timestamptz` |
| UUID aleatório como PK em tabela enorme sem motivo | identity ou UUID ordenável por tempo |
| Booleano nulo | `NOT NULL DEFAULT` |
| `jsonb` para tudo | Colunas tipadas |
| DBML sem as regras que ele não expressa | `Note` + DDL manual |
| Enum alterado a cada release | Tabela de domínio |
