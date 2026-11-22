# Paginação, consultas de lista e seed

## Princípios

1. **Toda lista é paginada** (com limite máximo), mesmo que hoje tenha 10 linhas.
2. **Ordenação determinística:** sempre um desempate único (`id`) no `ORDER BY`.
3. **Colunas explícitas**, nunca `SELECT *` em consulta da aplicação.
4. **Filtro no banco**, nunca carregar tudo para filtrar em memória.
5. **Seed é código de ambiente**, não de produção.

## Offset vs keyset

| Critério | Offset (`LIMIT/OFFSET`) | Keyset (cursor) |
|---|---|---|
| Implementação | Trivial | Um pouco mais elaborada |
| Pular para a página N | Sim | Não (só próxima/anterior) |
| Custo em páginas profundas | Cresce (o banco lê e descarta o offset) | Estável com índice |
| Estabilidade com inserções durante a leitura | Linhas repetem ou somem | Estável |
| Total de itens | `COUNT(*)` extra | Normalmente sem total |
| Quando usar | Lista pequena/administrativa, UI com páginas numeradas | Listas grandes, rolagem infinita, feeds, API pública |

Padrão recomendado: **offset** para telas administrativas pequenas (até milhares de
linhas); **keyset** quando a lista cresce ou a UI é "carregar mais".

### Offset

```sql
SELECT id, sala_id, inicio, fim
  FROM reservas
 WHERE usuario_id = $1
 ORDER BY inicio DESC, id DESC
 LIMIT $2 OFFSET $3;           -- $3 = (pagina - 1) * tamanho
```

- `tamanho` com teto (ex.: 100); validar `pagina >= 1`.
- Total: `SELECT count(*) ...` com o mesmo filtro, em consulta separada; `count(*)` em
  tabelas grandes é caro: ofereça total opcional ou aproximado se necessário.

### Keyset

Cursor = valores da última linha da página, na ordem da consulta. Comparação de linha
(`row value`) mantém a ordem composta correta:

```sql
-- ordem: inicio DESC, id DESC ; cursor = (inicio_ultimo, id_ultimo)
SELECT id, sala_id, inicio, fim
  FROM reservas
 WHERE usuario_id = $1
   AND (inicio, id) < ($2, $3)         -- omitir na 1ª página
 ORDER BY inicio DESC, id DESC
 LIMIT $4 + 1;                         -- 1 a mais para saber se há próxima página
```

- Índice que acompanha: `(usuario_id, inicio DESC, id DESC)` (ou `(usuario_id, inicio, id)`,
  o btree percorre nos dois sentidos).
- O **desempate por `id`** é obrigatório para não perder/duplicar linhas com `inicio` igual.
- Devolver `proximoCursor` **opaco** (ex.: valores serializados e codificados), validado
  ao receber; o cliente não monta cursor.
- Ordem mista (ASC em um campo, DESC em outro) quebra a comparação de linha: ajuste a
  expressão ou use apenas ordens homogêneas.
- Não há "total" nem "ir para a página 50"; documentar no contrato (`api-rest-openapi.md`).

## Filtros e ordenação seguros

- Filtros por parâmetros tipados e **parametrizados** (`$1`, `?`), nunca concatenados em SQL.
- Campo de ordenação permitido por **lista branca** (`inicio`, `criado_em`); nunca nome vindo cru do cliente.
- Combinação filtro+ordem que vira consulta frequente merece índice (`indices.md`).
- Período: `WHERE inicio < $fim AND fim > $inicio` (sobreposição), ou `periodo && tstzrange($a,$b)`.

## Evitar N+1

Consulta de lista que depois consulta o detalhe de cada item dispara N consultas. Resolva
com `JOIN` ou segunda consulta `WHERE id = ANY($1)` para o lote. Verifique logs de SQL do
endpoint de listagem (contagem de consultas por requisição).

## Seed: dados de teste e referência

Distinguir:

| Tipo | Exemplo | Onde mora | Roda em produção? |
|---|---|---|---|
| Dado de referência necessário | Papéis, tipos de sala fixos | Migração | Sim |
| Seed de desenvolvimento/teste | Usuários de teste, salas, reservas de exemplo | Script/arquivo de seed | **Não** |
| Seed de volume | Milhares de linhas para medir desempenho | Script gerador | Não |

### Seed de desenvolvimento

- Arquivo `seed.sql` (ou comando `make seed`) **idempotente**: `INSERT ... ON CONFLICT DO NOTHING`
  ou limpeza prévia controlada.
- Usuários de teste **por papel** (um `USUARIO`, um `ADMIN`), com senhas de teste definidas
  em `.env.example`/seed e hash gerado pelo mesmo mecanismo da aplicação (não gravar hash
  copiado sem saber o algoritmo). Nunca reutilizar senha real.
- Dados que exercitam os CT: uma sala ativa, uma inativa, uma reserva existente que permite
  testar conflito, reservas passadas e futuras.
- Proteção: o seed recusa rodar se `APP_ENV=production` (ou equivalente).

```sql
INSERT INTO salas (nome, capacidade) VALUES
  ('Sala Alfa', 6), ('Sala Beta', 12)
ON CONFLICT (nome) DO NOTHING;
```

### Seed de volume (para medir índices e paginação)

```sql
INSERT INTO reservas (sala_id, usuario_id, inicio, fim)
SELECT 1 + (g % 20), 1 + (g % 50),
       timestamptz '2026-01-01 08:00-03' + (g * interval '1 hour'),
       timestamptz '2026-01-01 08:00-03' + (g * interval '1 hour') + interval '50 minutes'
  FROM generate_series(1, 200000) AS g;
-- ajustar para respeitar as constraints (ex.: EXCLUDE) ou carregar antes de criá-las
ANALYZE reservas;
```

Rode `ANALYZE` após carregar, para o planejador ter estatísticas, e então `EXPLAIN (ANALYZE, BUFFERS)`.

## Dados pessoais no seed

Dados fictícios, claramente falsos (`@exemplo.local`), nunca cópia de produção. Se for
imprescindível usar dados reais anonimizados, anonimizar antes e justificar em ADR; apontar
para `../../seguranca/revisao-seguranca/`.

## Reset de ambiente

Banco de teste recriável do zero: derrubar volume, subir, migrar, semear (`ambiente-docker`).
Testes de integração criam seus próprios dados e limpam (transação revertida, esquema
temporário ou truncar entre testes); não dependem da ordem dos outros.

## Checklist

- [ ] Toda lista paginada, com teto de `tamanho`.
- [ ] `ORDER BY` determinístico (com `id`).
- [ ] Offset para lista pequena; keyset documentado para lista grande.
- [ ] Campo de ordenação por lista branca; consultas parametrizadas.
- [ ] Sem N+1 nos endpoints de lista.
- [ ] Seed idempotente, separado das migrações, bloqueado em produção.
- [ ] Usuário de teste por papel; dados fictícios.
- [ ] Seed de volume e `ANALYZE` para medir consultas críticas.

## Erros comuns

| Erro | Correção |
|---|---|
| `OFFSET` enorme em lista grande | Keyset |
| `ORDER BY inicio` sem desempate | Acrescentar `id` |
| `SELECT *` | Colunas explícitas |
| Ordenação por nome de campo vindo do cliente | Lista branca |
| Seed rodando em produção | Verificação de ambiente |
| Seed que duplica ao rodar duas vezes | `ON CONFLICT DO NOTHING` |
| Hash de senha de seed incompatível com a aplicação | Gerar pela própria aplicação/ferramenta do projeto |
| Medir desempenho com poucas linhas | Seed de volume |
| Cursor montado pelo cliente | Cursor opaco validado |
