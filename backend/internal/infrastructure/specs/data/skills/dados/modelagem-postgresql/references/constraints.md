# Constraints como regra de negócio

Princípio: toda regra que **nunca** pode ser violada e que o banco consegue expressar vira
constraint. A aplicação valida também (mensagem boa, resposta rápida), mas **a constraint é
a garantia**. Cada constraint recebe nome, e cada nome tem um mapeamento para erro da API.

## Mapa: regra -> mecanismo -> SQLSTATE -> resposta da API

| Regra | Mecanismo | SQLSTATE | Resposta típica |
|---|---|---|---|
| Campo obrigatório | `NOT NULL` | 23502 | 400 |
| Valor válido (faixa, formato, ordem entre colunas) | `CHECK` | 23514 | 400 |
| Não duplicar | `UNIQUE` / índice único | 23505 | 409 |
| Referência existente | `FOREIGN KEY` | 23503 | 400 (ao criar) ou 409 (ao apagar) |
| Intervalos não se sobrepõem | `EXCLUDE USING gist` | 23P01 | 409 |
| Deadlock | (detectado pelo banco) | 40P01 | repetir ou 500 |
| Falha de serialização | (isolamento serializable/repeatable read) | 40001 | repetir a transação |

A aplicação identifica a violação pelo **nome da constraint** (campo `constraint_name` do
erro) e não pelo texto da mensagem. Tabela de mapeamento fica em `estrutura.md` ou na ADR de erros.

## NOT NULL

Padrão: `NOT NULL`. `NULL` só quando "desconhecido/não se aplica" tem significado no
negócio (ex.: `cancelada_em`). Em coluna booleana, nunca nulo. Adicionar `NOT NULL` a
coluna existente exige cuidado (ver `migracoes.md`).

## CHECK

Regras de valor e entre colunas da **mesma linha**:

```sql
CONSTRAINT reservas_fim_apos_inicio_ck CHECK (fim > inicio),
CONSTRAINT salas_capacidade_ck         CHECK (capacidade BETWEEN 1 AND 500),
CONSTRAINT usuarios_email_ck           CHECK (position('@' in email) > 1),
CONSTRAINT reservas_status_ck          CHECK (status IN ('ATIVA','CANCELADA'))
```

- `CHECK` não pode consultar outras tabelas (não use para integridade referencial).
- Regras temporais em relação a "agora" (ex.: "não reservar no passado") **não** vão em
  `CHECK` (`now()` muda e a linha existente ficaria inválida): é regra de serviço.
- Coerência entre colunas: `CHECK ((status = 'CANCELADA') = (cancelada_em IS NOT NULL))`.

## UNIQUE e UNIQUE parcial

```sql
-- unicidade simples
CONSTRAINT usuarios_email_uk UNIQUE (email)

-- e-mail sem diferença de caixa
CREATE UNIQUE INDEX usuarios_email_lower_uk ON usuarios (lower(email));

-- unicidade só entre registros "vivos"
CREATE UNIQUE INDEX salas_nome_ativa_uk ON salas (nome) WHERE ativa;

-- no máximo uma reserva ATIVA por usuário e horário de início
CREATE UNIQUE INDEX reservas_usuario_inicio_uk
  ON reservas (usuario_id, inicio) WHERE status = 'ATIVA';
```

- `NULL` em colunas `UNIQUE` são consideradas distintas por padrão; em versões recentes
  existe `UNIQUE NULLS NOT DISTINCT` (PostgreSQL 15 em diante: conferir).
- Unicidade com soft delete: sempre índice parcial `WHERE deleted_at IS NULL`.
- `UNIQUE` já cria índice; não crie outro igual.

## FOREIGN KEY e ON DELETE conscientes

```sql
sala_id bigint NOT NULL REFERENCES salas(id) ON DELETE RESTRICT
```

| `ON DELETE` | Efeito | Use quando |
|---|---|---|
| `RESTRICT` / `NO ACTION` | Impede apagar o pai com filhos | **Padrão**; preserva histórico |
| `CASCADE` | Apaga filhos junto | O filho não tem sentido sem o pai e sem valor de histórico (itens de um rascunho) |
| `SET NULL` | Zera a referência | Referência opcional (coluna aceita `NULL`) |
| `SET DEFAULT` | Usa o default | Raro |

Regras:
- Pergunte "o que o negócio quer ao apagar o pai?" e registre; **CASCADE por hábito** apaga
  dados valiosos (reservas passadas ao apagar uma sala).
- Para entidades com histórico, **desativar** (`ativa = false`) em vez de apagar.
- **Toda coluna de FK ganha índice** (a FK não cria): sem ele, apagar o pai varre a tabela
  filha e `JOIN` fica lento.
- `ON UPDATE` raramente importa com PK imutável.
- Ciclos de FK: evite; se inevitável, `DEFERRABLE INITIALLY DEFERRED`.

## EXCLUDE: sobreposição de intervalos

Caso clássico: duas reservas da mesma sala nunca podem sobrepor. Exige a extensão
`btree_gist` para que `=` em colunas escalares funcione dentro de um índice GiST.

### Com uma coluna de intervalo

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE reservas (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sala_id    bigint    NOT NULL REFERENCES salas(id),
  periodo    tstzrange NOT NULL,
  CONSTRAINT reservas_periodo_valido_ck
    CHECK (NOT isempty(periodo) AND NOT lower_inf(periodo) AND NOT upper_inf(periodo)),
  CONSTRAINT reservas_sem_sobreposicao_ex
    EXCLUDE USING gist (sala_id WITH =, periodo WITH &&)
);
```

### Com duas colunas (expressão)

```sql
CONSTRAINT reservas_sem_sobreposicao_ex
  EXCLUDE USING gist (sala_id WITH =, tstzrange(inicio, fim, '[)') WITH &&)
```

- Use limites `'[)'` (fechado no início, aberto no fim): reserva 10:00–11:00 e 11:00–12:00
  **não** sobrepõem. Defina e documente a convenção; o padrão de `tstzrange(a,b)` já é `'[)'`.
- Reservas **canceladas** não devem bloquear: restrinja com predicado parcial
  `EXCLUDE USING gist (sala_id WITH =, periodo WITH &&) WHERE (status = 'ATIVA')`.
- Violação: SQLSTATE `23P01` (`exclusion_violation`), nome da constraint no erro -> 409
  `conflito-de-horario`.
- A garantia vale sob concorrência: duas transações que inserem períodos conflitantes, uma
  falha (se a primeira ainda não confirmou, a segunda espera o resultado dela).
- Também serve a atualização: mudar horário pode violar; trate o mesmo erro.
- Para intervalos de datas sem hora, `daterange`.

## Constraints entre tabelas (o que o CHECK não faz)

| Necessidade | Opção |
|---|---|
| Ex.: "reserva só em sala ativa" | Regra de serviço + `SELECT ... FOR SHARE` na sala; ou FK composta com coluna redundante (`ativa`); avalie custo |
| Total de itens = valor do pedido | Calcular em consulta, ou coluna `GENERATED`/trigger com ADR |
| Cota por usuário | Serviço em transação com lock; constraint não expressa agregados |

Trigger é último recurso: regra escondida, difícil de testar. Se usar, ADR e teste.

## Dados e domínio

- **Domínios** (`CREATE DOMAIN`) reutilizam CHECK entre colunas (ex.: `email_t`); use só com
  muita repetição.
- Colunas **geradas** (`GENERATED ALWAYS AS (...) STORED`) para derivados estáveis.
- `DEFAULT` coerente com `NOT NULL`.

## Teste das constraints

Cada constraint tem um **teste de integração** contra PostgreSQL real que tenta violá-la e
espera o erro mapeado: duplicado -> 409; período inválido -> 400; sobreposição simultânea ->
uma 201 e uma 409. Isso vira CT ou critério de pronto da T.

## Checklist

- [ ] Toda regra inviolável tem constraint; cada constraint nomeada.
- [ ] Mapa constraint -> status/erro da API definido.
- [ ] `NOT NULL` por padrão; `NULL` documentado quando existe.
- [ ] `CHECK` só de linha; nada com `now()`.
- [ ] `UNIQUE` (incluindo parciais) para toda unicidade de negócio.
- [ ] `ON DELETE` escolhido por FK e **índice** em cada FK.
- [ ] `EXCLUDE` com `btree_gist`, limites `'[)'`, filtro de cancelados.
- [ ] Testes de violação para cada constraint crítica.

## Erros comuns

| Erro | Correção |
|---|---|
| Sobreposição por `SELECT` + `INSERT` | `EXCLUDE` (ou lock explícito, ver concorrência) |
| Esquecer `CREATE EXTENSION btree_gist` | Na migração base; checar permissão em produção |
| Capturar o erro pelo texto | Pelo nome da constraint e SQLSTATE |
| Reservas canceladas bloqueando horário | Predicado `WHERE (status = 'ATIVA')` |
| `CHECK` com `now()` | Regra de serviço |
| `CASCADE` apagando histórico | `RESTRICT` e desativar |
| FK sem índice | Índice na coluna |
| Limite fechado nos dois lados (`[]`) | `'[)'` para blocos contíguos |
