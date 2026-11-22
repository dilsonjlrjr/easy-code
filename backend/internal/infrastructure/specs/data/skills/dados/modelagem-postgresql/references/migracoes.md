# Migrações versionadas e seguras

Migração é o histórico executável do esquema. Em produção, mudar o banco com a aplicação
rodando é a parte arriscada: bloqueios longos derrubam a API. Esta reference traz as regras
e as receitas de alteração segura.

## Princípios

1. **Versionada e imutável.** Cada mudança é um arquivo numerado, aplicado em ordem. Migração
   já aplicada **nunca** é editada: crie outra.
2. **Pequena e focada.** Uma intenção por migração; falha parcial fácil de entender.
3. **Compatível com a versão anterior do código** (durante a implantação, versões nova e
   antiga convivem): expand/contract.
4. **Sem bloqueio longo.** Conhecer o lock de cada comando; usar `lock_timeout`.
5. **Do zero ao atual, sempre.** Aplicar todas as migrações em banco vazio tem de funcionar (CI).
6. **Dados de teste fora.** Seed não é migração de produção (`paginacao-e-seed.md`).

## Ferramenta

Qualquer ferramenta open source de migração SQL versionada (Flyway, Liquibase, golang-migrate,
Alembic, sqlx migrate, Diesel, entre outras): a escolha vai na ADR de persistência, segundo a
linguagem. Requisitos mínimos: ordem determinística, registro do que foi aplicado,
checksum para detectar edição, execução automatizada em CI e no ambiente. **Conferir** como
cada ferramenta lida com `CREATE INDEX CONCURRENTLY` (precisa rodar fora de transação).

Nomeação: `V001__base.sql`, `V002__reservas.sql` ou timestamp (`20261120_1030__x.sql`). Em
paralelo (várias T), prefira timestamp **ou** faixas por T no plano, para não colidir.

## Quando a migração roda

| Estratégia | Prós | Contras |
|---|---|---|
| Na subida da API | Simples; ambiente sempre consistente | Várias réplicas disputam (a ferramenta usa lock); falha impede subir |
| Passo separado (job/comando) | Controle e ordem da implantação | Mais uma etapa a lembrar |

Aplicação pequena: na subida ou por comando no Compose (`make migrate`), documentado no README.

## Expand / contract

Para mudar o que já está em uso, divida em fases, cada uma compatível com o código vigente:

1. **Expand:** adicione a estrutura nova (coluna, tabela) sem remover a antiga.
2. **Migrar dados:** copie/transforme (em lotes se grande).
3. **Código** passa a escrever nos dois e depois a ler só do novo.
4. **Contract:** remova a estrutura antiga em migração posterior, depois que nenhuma versão do código a usa.

Exemplo: renomear `salas.nome` para `salas.titulo`.

| Passo | Migração/Código |
|---|---|
| 1 | `ALTER TABLE salas ADD COLUMN titulo text;` |
| 2 | `UPDATE salas SET titulo = nome WHERE titulo IS NULL;` (em lotes se grande) |
| 3 | Código grava `nome` e `titulo`; depois lê `titulo` |
| 4 | Restrição: `titulo NOT NULL` (ver abaixo) |
| 5 | Código para de usar `nome`; migração `DROP COLUMN nome` |

Para aplicação pequena ainda sem produção, renomear direto é aceitável; o hábito de expand/contract
passa a valer quando há dados e implantação sem parada.

## Receitas seguras (PostgreSQL)

Comportamentos e versões: **conferir** no manual da versão usada.

| Objetivo | Forma segura | Cuidado |
|---|---|---|
| Adicionar coluna nula | `ADD COLUMN x tipo` | Rápido |
| Adicionar coluna com default constante | `ADD COLUMN x tipo NOT NULL DEFAULT 0` | Rápido desde PostgreSQL 11 para defaults constantes; default volátil reescreve a tabela |
| Preencher dado | `UPDATE` em lotes (`WHERE id BETWEEN ...`) com commits | Um `UPDATE` gigante trava e incha |
| Tornar coluna `NOT NULL` | 1) `ADD CONSTRAINT x_nn CHECK (x IS NOT NULL) NOT VALID` 2) `VALIDATE CONSTRAINT x_nn` 3) `SET NOT NULL` (varredura evitada quando existe CHECK válido equivalente, em versões recentes: conferir) 4) remover o CHECK | `SET NOT NULL` direto varre a tabela sob lock forte |
| Adicionar FK | `ADD CONSTRAINT ... FOREIGN KEY ... NOT VALID` e depois `VALIDATE CONSTRAINT` | `VALIDATE` usa lock mais leve; criar índice na coluna antes |
| Adicionar `CHECK` | `... NOT VALID` e depois `VALIDATE CONSTRAINT` | Idem |
| Criar índice | `CREATE INDEX CONCURRENTLY` | Fora de transação; se falhar, remover o índice `INVALID` |
| Criar `UNIQUE` | Criar índice único `CONCURRENTLY` e depois `ADD CONSTRAINT ... UNIQUE USING INDEX` | Evita bloqueio |
| Alterar tipo de coluna | Nova coluna + copiar + trocar (expand/contract) | `ALTER COLUMN TYPE` pode reescrever a tabela inteira |
| Remover coluna | Primeiro parar de usar no código; depois `DROP COLUMN` | Irreversível; backup |
| Renomear | Evitar; expand/contract | Quebra versão antiga do código |
| Remover tabela | Só depois de período sem uso; backup | Irreversível |

### Evitar espera infinita

```sql
SET lock_timeout = '5s';        -- falha em vez de esperar travando a fila de locks
SET statement_timeout = '15min'; -- teto do comando
ALTER TABLE reservas ADD COLUMN observacao text;
```

Um `ALTER TABLE` que espera um lock bloqueia **todas** as consultas seguintes na tabela;
`lock_timeout` com nova tentativa é mais seguro do que esperar.

### Preencher em lotes (esboço)

```sql
-- repetir até 0 linhas atualizadas
UPDATE reservas SET status = 'ATIVA'
 WHERE id IN (SELECT id FROM reservas WHERE status IS NULL ORDER BY id LIMIT 5000);
```

## Rollback

- Migrações "down" existem em algumas ferramentas, mas dados destruídos não voltam.
- Estratégia real de reversão: **migrações compatíveis** (expand/contract) + **backup** antes
  das destrutivas + correção para frente (nova migração).
- Documentar no cabeçalho do arquivo se a migração é destrutiva.

## Conteúdo da migração base (T-01 / primeira T de dados)

1. `CREATE EXTENSION IF NOT EXISTS btree_gist;` (se houver `EXCLUDE`).
2. Tabelas na ordem das dependências (pais antes de filhos).
3. Constraints e índices (nomeados).
4. Tabelas de domínio e dados de referência **necessários** ao sistema (ex.: papéis).
5. Nada de usuários de teste (seed).

Permissão de extensão: criar extensão pode exigir superusuário; no Compose o usuário
padrão da imagem oficial costuma poder. Em ambiente gerenciado, **conferir** se a extensão
é permitida.

## Testando migrações

- CI: banco vazio -> todas as migrações -> verificação do esquema (ex.: a aplicação sobe e os testes de integração passam).
- Opcional: aplicar migração nova sobre cópia com volume realista e medir o tempo.
- Comparar o esquema resultante com `docs/dados/esquema.sql` (do DBA) (a migração base deve reproduzi-lo).
- Ferramentas open source de lint de migração podem alertar para comandos perigosos;
  **conferir** opções para a ferramenta escolhida.

## Checklist

- [ ] Migrações numeradas, imutáveis, uma intenção cada.
- [ ] Banco vazio -> estado atual funciona em CI.
- [ ] Mudanças em tabela usada seguem expand/contract.
- [ ] `lock_timeout` configurado; sem comando de lock longo em horário de uso.
- [ ] Índices em tabela em uso com `CONCURRENTLY` e ferramenta configurada para isso.
- [ ] `NOT NULL`/FK/CHECK adicionados com `NOT VALID` + `VALIDATE` quando a tabela é grande.
- [ ] Preenchimento de dados em lotes.
- [ ] Migrações destrutivas marcadas e com backup.
- [ ] Seed separado das migrações de produção.
- [ ] Regra anti-colisão de numeração entre T paralelas.

## Erros comuns

| Erro | Correção |
|---|---|
| Editar migração aplicada | Nova migração |
| `ALTER COLUMN TYPE` direto em tabela grande | Nova coluna e troca |
| `CREATE INDEX` comum em tabela em uso | `CONCURRENTLY` |
| Migrar e remover no mesmo release | Expand, depois contract |
| `UPDATE` único em milhões de linhas | Lotes |
| Seed de teste na migração | Seed separado |
| Duas T com a mesma numeração | Faixas no plano ou timestamp |
| Sem teste de migração do zero | CI com banco vazio |
| Confiar em "down" para recuperar dado | Backup + correção para frente |
