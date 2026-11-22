# Índices

Índice é troca: acelera leitura, custa escrita, espaço e manutenção. Regra de ouro:
**crie índice para uma consulta que existe, e meça**. Em app pequeno, poucos índices bastam:
os das PKs, UNIQUEs, FKs e 1 ou 2 por listagem relevante.

## Princípios

1. Índice nasce de uma consulta concreta (do OpenAPI/US), não de intuição.
2. Verificar com `EXPLAIN (ANALYZE, BUFFERS)` em volume realista antes e depois.
3. Tabela pequena (milhares de linhas) frequentemente vai bem com varredura sequencial; o
   planejador pode ignorar o índice e acerta.
4. Cada índice custa em `INSERT`/`UPDATE`/`DELETE`. Menos é mais.
5. PK, `UNIQUE` e `EXCLUDE` já criam índice; não duplique.

## Tipos e quando usar

| Tipo | Serve a | Exemplo |
|---|---|---|
| **btree** (padrão) | `=`, `<`, `>`, `BETWEEN`, `ORDER BY`, prefixo de `LIKE 'abc%'` (com collation adequada: conferir) | `(usuario_id)` |
| **Composto btree** | Filtro por várias colunas e ordenação | `(sala_id, inicio)` |
| **Parcial** | Subconjunto frequente (ativos, pendentes) | `WHERE status = 'ATIVA'` |
| **Expressão** | Consulta por função | `(lower(email))` |
| **GiST** | Intervalos, geometria, `EXCLUDE`, buscas por sobreposição | `(sala_id, periodo)` com `btree_gist` |
| **GIN** | `jsonb`, arrays, busca textual | `jsonb_path_ops` conforme uso |
| **BRIN** | Tabelas enormes append-only correlacionadas com a ordem física (logs por data) | `(created_at)` |
| **Hash** | Só igualdade; raramente superior ao btree | evitar por padrão |

## Índice composto: ordem das colunas

Regra: colunas com **igualdade primeiro**, depois a de **intervalo/ordenação**.

```sql
-- consulta: WHERE sala_id = $1 AND inicio >= $2 ORDER BY inicio
CREATE INDEX reservas_sala_inicio_idx ON reservas (sala_id, inicio);
```

- O índice `(a, b)` serve a `WHERE a = ?` e `WHERE a = ? AND b ...`, mas **não** a `WHERE b = ?` sozinho.
- Um índice `(a, b)` torna `(a)` redundante.
- Ordem pedida na consulta deve coincidir com a do índice para evitar `Sort`.

## Índice parcial

```sql
-- só reservas ativas são consultadas por período
CREATE INDEX reservas_ativas_inicio_idx ON reservas (inicio) WHERE status = 'ATIVA';

-- unicidade condicional (também é índice parcial)
CREATE UNIQUE INDEX salas_nome_ativa_uk ON salas (nome) WHERE ativa;
```

O predicado da consulta precisa implicar o predicado do índice, caso contrário ele não é usado.

## Índices de FK

Cada coluna de FK recebe índice (exceto se já é prefixo de outro índice/constraint). Motivos:
`JOIN`, listagens por pai, e `DELETE`/`UPDATE` no pai (que verificam a tabela filha).

```sql
CREATE INDEX reservas_usuario_idx ON reservas (usuario_id);
-- sala_id já é prefixo de reservas_sala_inicio_idx e da EXCLUDE: não precisa de outro
```

## GiST para intervalos

A `EXCLUDE USING gist (sala_id WITH =, periodo WITH &&)` cria um índice utilizável para
consultas "reservas da sala X que sobrepõem o período Y":

```sql
SELECT * FROM reservas WHERE sala_id = $1 AND periodo && tstzrange($2, $3);
```

Dispensa um segundo índice para essa consulta. Requer `btree_gist` para o `=` escalar.

## Quando NÃO indexar

| Situação | Motivo |
|---|---|
| Tabela com poucas centenas/milhares de linhas | Varredura sequencial é igual ou melhor |
| Coluna de baixa seletividade sozinha (booleano, status com 2–3 valores) | O planejador ignora; use parcial se um valor é raro e consultado |
| Tabela com escrita intensa e leitura rara | Custo de manutenção sem benefício |
| Índice que duplica prefixo de outro | Redundância |
| Colunas que nunca aparecem em `WHERE`/`JOIN`/`ORDER BY` | Sem consulta que o use |
| "Por via das dúvidas" | Meça primeiro |

## Medir

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT id, sala_id, inicio, fim FROM reservas
WHERE sala_id = 3 AND inicio >= now() ORDER BY inicio LIMIT 20;
```

Procure: `Index Scan`/`Index Only Scan` esperado vs `Seq Scan` em tabela grande, `Sort`
evitável, estimativa de linhas muito diferente do real (estatísticas: `ANALYZE`).
Para teste de desempenho, carregue volume realista (ver seed em `paginacao-e-seed.md`).
Visão geral de uso: `pg_stat_user_indexes` (índices nunca usados são candidatos a remoção).

## Criar índice em tabela em uso

```sql
CREATE INDEX CONCURRENTLY reservas_usuario_idx ON reservas (usuario_id);
```

`CONCURRENTLY` evita bloquear escritas, mas **não roda dentro de transação** (e se falhar
deixa um índice `INVALID` a remover). Muitas ferramentas de migração exigem configuração
para isso (ver `migracoes.md`). Em tabela vazia/nova (migração base), `CREATE INDEX`
normal basta.

## Colunas incluídas

`CREATE INDEX ... (a) INCLUDE (b)` permite `Index Only Scan` sem pôr `b` na chave. Só com
medida de ganho; não é padrão para app pequeno.

## Busca textual e `LIKE`

- `LIKE 'abc%'` pode usar btree (com `text_pattern_ops` ou collation `C`; conferir).
- `LIKE '%abc%'` exige trigramas (`pg_trgm` + GIN) ou busca textual. Só se uma US exigir busca.
- Não implemente busca com `lower(col) LIKE` sem índice de expressão/trigrama em tabela grande.

## Checklist

- [ ] Cada índice (além de PK/UNIQUE/EXCLUDE) liga-se a uma consulta nomeada.
- [ ] Toda FK com índice (próprio ou prefixo de outro).
- [ ] Compostos: igualdade primeiro, intervalo/ordem depois.
- [ ] Parciais com predicado coerente com a consulta.
- [ ] `EXPLAIN (ANALYZE, BUFFERS)` conferido em volume realista nas consultas críticas.
- [ ] `CONCURRENTLY` em tabelas em uso; migração configurada para isso.
- [ ] Sem índices redundantes ou nunca usados.

## Erros comuns

| Erro | Correção |
|---|---|
| Indexar tudo | Só com consulta e medida |
| Índice composto na ordem errada | Igualdade antes do intervalo |
| Índice em booleano | Parcial, se um valor é raro |
| Esquecer índice de FK | Criar |
| Índice duplicado de `UNIQUE` | Remover |
| `CREATE INDEX` comum em tabela grande em uso | `CONCURRENTLY` |
| Avaliar desempenho com 50 linhas | Seed com volume realista |
| Função na coluna do `WHERE` sem índice de expressão | Índice de expressão ou reescrever |
