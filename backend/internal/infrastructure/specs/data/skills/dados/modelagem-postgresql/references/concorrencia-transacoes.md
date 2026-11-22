# Concorrência e transações

Aplicações web atendem requisições simultâneas; duas delas podem tentar alterar o mesmo
dado ao mesmo tempo. Esta reference diz **como o banco protege** e **como a API reage**.

## Princípios

1. **Constraint primeiro.** Se a regra cabe em `UNIQUE`/`EXCLUDE`/`CHECK`/FK, não precisa de lock manual.
2. **Transação curta.** Abra, faça o necessário, confirme. Nada de chamada externa (HTTP, e-mail) dentro de transação.
3. **Uma regra, um mecanismo.** Não misture lock, versão e constraint sem motivo.
4. **Falha de concorrência é normal.** Mapeie para resposta clara (409, ou repetir) em vez de 500.
5. **Teste com concorrência real:** duas requisições simultâneas, não só em sequência.

## Isolamento de transação

| Nível | Comportamento (PostgreSQL) | Quando |
|---|---|---|
| `READ COMMITTED` (padrão) | Cada comando vê o que foi confirmado até o seu início | A maioria das operações, com constraints/locks para as regras críticas |
| `REPEATABLE READ` | Instantâneo único da transação; erro `40001` em conflito de escrita | Relatórios consistentes; leitura-modifica-grava que aceita repetir |
| `SERIALIZABLE` | Comportamento equivalente a execução serial; pode falhar com `40001` | Regras multi-linha que nenhuma constraint expressa; exige **repetir** a transação |

Regras:
- No PostgreSQL, `READ UNCOMMITTED` se comporta como `READ COMMITTED`.
- Em `REPEATABLE READ` e `SERIALIZABLE` a aplicação **deve** estar pronta para repetir a
  transação ao receber `40001` (limite de tentativas, ex.: 3).
- Padrão recomendado: `READ COMMITTED` + constraint para regras críticas. Só suba o nível
  com requisito e teste.

## Anomalia típica: "checar e inserir"

```text
Requisição A: SELECT conflito?  -> não
Requisição B: SELECT conflito?  -> não
A: INSERT  -> ok
B: INSERT  -> ok   (sobreposição!)
```

Em `READ COMMITTED` isso acontece. Soluções, da melhor para a mais custosa:

| Solução | Como | Quando |
|---|---|---|
| **Constraint** | `EXCLUDE`/`UNIQUE`; o segundo `INSERT` falha com 23P01/23505 | Sempre que a regra cabe |
| **Lock de linha do pai** | `SELECT ... FROM salas WHERE id = $1 FOR UPDATE` e depois checa+insere | Regra que a constraint não expressa (cota por sala) |
| **Lock consultivo** | `pg_advisory_xact_lock(chave)` | Serializar um conjunto lógico sem linha natural |
| **SERIALIZABLE** | Nível de isolamento + repetir | Regras multi-linha complexas |

## Locks de linha

```sql
SELECT * FROM salas WHERE id = $1 FOR UPDATE;            -- espera
SELECT * FROM salas WHERE id = $1 FOR UPDATE NOWAIT;     -- erro imediato (55P03) se travada
SELECT * FROM tarefas WHERE estado = 'FILA'
  ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1;            -- fila de trabalho
```

- `FOR UPDATE`: vou alterar; bloqueia outros `FOR UPDATE`/`UPDATE`/`DELETE` da linha.
- `FOR SHARE`: vou ler e **garantir que não mude**; permite outros `FOR SHARE`.
- Trave em **ordem consistente** (por id crescente) para evitar deadlock (40P01).
- Lock só dura até o fim da transação: mantenha-a curta.
- `lock_timeout` e `statement_timeout` limitam espera e execução (configurar por sessão/rota).

## Concorrência otimista (coluna de versão)

Para edição de um registro por usuários diferentes (ex.: editar uma sala), sem manter lock:

```sql
ALTER TABLE salas ADD COLUMN versao integer NOT NULL DEFAULT 1;

UPDATE salas
   SET nome = $2, capacidade = $3, versao = versao + 1, updated_at = now()
 WHERE id = $1 AND versao = $4;      -- $4 = versão que o cliente leu
-- 0 linhas afetadas -> alguém alterou antes -> 409 (ou 412 com If-Match)
```

- API: devolve `versao` (ou `ETag`) no `GET`; o cliente envia no `PUT`/`PATCH` (`If-Match`).
- 0 linhas afetadas = conflito: responder 409 (ou 412 *Precondition Failed* quando usar
  `If-Match`), com Problem Details orientando recarregar.
- Alternativa: comparar `updated_at`; a versão inteira evita problemas de precisão.
- Use quando conflito é raro e perder alteração é inaceitável; evita lock longo.

## Traduzir violação de constraint em 409

Fluxo no serviço/handler:

1. Executa o `INSERT`/`UPDATE` e **deixa o banco decidir**.
2. Captura o erro do driver, lê **SQLSTATE** e **nome da constraint**.
3. Mapeia por tabela:

| SQLSTATE | Constraint | Resposta |
|---|---|---|
| `23P01` | `reservas_sem_sobreposicao_ex` | 409 `conflito-de-horario` |
| `23505` | `usuarios_email_uk` | 409 `email-ja-cadastrado` |
| `23503` | `reservas_sala_fk` (ao criar) | 400 `sala-inexistente` |
| `23503` | FK ao apagar pai | 409 `possui-dependentes` |
| `23514` | `reservas_fim_apos_inicio_ck` | 400 `periodo-invalido` |
| `40001` | (serialização) | repetir; esgotadas as tentativas: 409/503 |
| `40P01` | (deadlock) | repetir; persistente: 500 |
| `55P03` | (lock indisponível) | 409/503 conforme o caso |

Pseudocódigo agnóstico:

```text
try:
    inserir(reserva)
except ErroBanco as e:
    if e.sqlstate == "23P01" and e.constraint == "reservas_sem_sobreposicao_ex":
        raise Conflito("conflito-de-horario")
    raise            # desconhecido: 500, logar, nunca vazar SQL
```

Nunca vazar o texto do erro do banco ao cliente. Logar com o `traceId`.

## Transação no nível do caso de uso

- Uma requisição = uma transação (na maioria dos casos), aberta no serviço, não no handler.
- Operações com várias escritas que devem ser tudo-ou-nada: mesma transação.
- Efeitos externos (e-mail, webhook) **depois** do commit; se precisar de garantia, padrão
  outbox (tabela de eventos na mesma transação). Só com requisito.
- Leituras simples fora de transação explícita.
- **Idempotência**: repetição segura de `POST` via `Idempotency-Key` guardada em tabela
  com `UNIQUE`, na mesma transação do efeito.

## Pool de conexões

- Tamanho do pool pequeno e finito (dezenas, não centenas); conexão cara.
- Devolver a conexão sempre (fechar/commit/rollback em `finally`).
- Transação esquecida aberta segura locks e conexão: `idle_in_transaction_session_timeout` (conferir) como rede de segurança.

## Testando concorrência

```text
dispara N=10 requisições simultâneas POST /reservas (mesma sala, mesmo horário)
espera: exatamente 1 com 201, N-1 com 409; 0 com 500
```

Esse teste vira CT/critério de pronto da T da regra. Use threads/goroutines/tarefas
assíncronas do teste, com banco real em contêiner.

## Checklist

- [ ] Para cada regra crítica: mecanismo escolhido (constraint, lock, versão, serializable).
- [ ] Constraints preferidas a locks manuais.
- [ ] Transações curtas, sem I/O externo dentro.
- [ ] Mapa SQLSTATE/constraint -> resposta da API.
- [ ] Edição concorrente: versão ou `ETag` se perder alteração é inaceitável.
- [ ] Repetição em `40001` implementada quando usar nível superior.
- [ ] Ordem de locks consistente.
- [ ] Teste de concorrência real para as regras críticas.

## Erros comuns

| Erro | Correção |
|---|---|
| "Verificar e depois inserir" | Constraint; ou lock |
| Subir para `SERIALIZABLE` sem retry | Implementar repetição ou voltar ao padrão com constraint |
| Capturar erro pelo texto da mensagem | SQLSTATE + nome da constraint |
| `500` em violação de constraint | 409/400 mapeado |
| Transação aberta durante chamada HTTP externa | Mover para depois do commit |
| Lock em ordem aleatória | Ordenar por id |
| Pool gigante | Dimensionar pelo banco, não pelo cliente |
| Testar concorrência só em sequência | Disparo simultâneo |
