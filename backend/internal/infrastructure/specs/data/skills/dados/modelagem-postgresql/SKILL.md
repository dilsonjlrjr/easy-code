---
name: modelagem-postgresql
description: Modelagem relacional em PostgreSQL com DBML - tipos certos (timestamptz, numeric, text, uuid vs identity, enum vs tabela vs check), constraints como regra de negócio (NOT NULL, CHECK, UNIQUE parcial, FK com ON DELETE consciente, EXCLUDE com tstzrange e btree_gist), índices, concorrência e transações (isolamento, locks, versão otimista, violação de constraint como 409), migrações versionadas seguras, paginação (offset vs keyset), seed e checklist de revisão. Cobre também dicionário de dados e estratégia de persistência (backup, recuperação, permissões). Use na etapa de Dados (agente DBA) ao modelar, na Validação técnica ao emitir parecer sobre persistência e na de Dev ao escrever migrações e consultas.
---

# Modelagem em PostgreSQL

Skill usada principalmente pelo agente **DBA** (modelo, DDL, dicionário, estratégia de
persistência, parecer na Validação técnica) e também pelo **Dev** (migrações, consultas). Serve a
qualquer aplicação web com PostgreSQL; os exemplos usam reserva de salas só como ilustração.
Recursos citados são do PostgreSQL estável atual; onde a versão importa há "(conferir)".

## Para que serve neste processo

O modelo de dados é um contrato (junto do OpenAPI). O DBA entrega, em `docs/dados/`,
`modelo-dados.dbml`, o `esquema.sql` derivado, o `DIC.md` (artefato DIC) e o `PERSIST.md`
(artefato PERSIST). DIC e PERSIST têm cabeçalho YAML validado por JSON Schema (seção abaixo). A Arquitetura define só o padrão de acesso a dados na ADR; o DBA trabalha em
paralelo com o Design e tem **autonomia para reprovar** decisão de persistência (inclusive do
Arquiteto) no seu parecer da Validação técnica. Dev parte do DDL para as migrações; QA testa as
regras que o banco garante. **A garantia mais barata de uma regra de negócio é a que o
banco impõe**: não depende de nenhum cliente, vale sob concorrência e sobrevive a bugs.

## Princípios

1. **O banco é a última linha de defesa.** O que não pode ser violado vira constraint.
2. **Tipos precisos.** O tipo certo remove classes inteiras de bug (fuso, arredondamento).
3. **Simples e normalizado.** 3ª forma normal por padrão; desnormalize só com medida.
4. **Nomes previsíveis.** `snake_case`, tabelas no plural, FK `<entidade>_id`.
5. **Toda FK tem índice** (PostgreSQL não cria automaticamente) e todo índice tem consulta que o justifica.
6. **Migração versionada e reversível em intenção.** Nunca editar migração aplicada.
7. **Violação de constraint é resultado esperado**, não exceção: a API a traduz (409, 400).
8. **Sem extensão ou recurso "porque existe".** Cada um precisa de requisito.

## Fluxo de trabalho

1. **Extrair entidades e regras** das US (substantivos, invariantes, unicidades, estados).
2. **Esboçar o modelo** em DBML (`references/dbml-e-tipos.md`).
3. **Escolher tipos** e chaves (`references/dbml-e-tipos.md`).
4. **Converter regras em constraints** (`references/constraints.md`).
5. **Planejar índices** a partir das consultas das US (`references/indices.md`).
6. **Definir concorrência**: quais operações disputam o mesmo dado e como o banco as protege
   (`references/concorrencia-transacoes.md`).
7. **Gerar `esquema.sql`** do DBML e rodar em PostgreSQL limpo; complementar o que DBML não
   expressa (EXCLUDE, índices parciais, extensões).
8. **Planejar migrações** (`references/migracoes.md`) e seed/paginação (`references/paginacao-e-seed.md`).
9. **Escrever o dicionário de dados** (`docs/dados/DIC.md`, artefato DIC) e a **estratégia
   de persistência** (`docs/dados/PERSIST.md`, artefato PERSIST): ver a seção abaixo.
10. **Revisar** com o checklist ao fim deste arquivo e registrar o parecer do DBA no
    `VALIDACAO-NN.md` na Validação técnica (aprovado ou reprovado, com ressalvas).

## Regras de decisão

| Situação | Decisão |
|---|---|
| Instante no tempo (início, criado em) | `timestamptz`, nunca `timestamp` sem fuso |
| Dinheiro ou quantidade exata | `numeric(p,s)` ou inteiro em centavos; nunca `float` |
| Texto | `text` (+ `CHECK` de tamanho se houver regra); `varchar(n)` só para limite real do domínio |
| PK interna simples | `bigint GENERATED ALWAYS AS IDENTITY` |
| ID exposto na API ou gerado fora do banco | `uuid` (`gen_random_uuid()`) |
| Lista fechada que quase nunca muda | `CHECK (col IN (...))` ou enum |
| Lista que muda por admin ou tem atributos | Tabela de domínio + FK |
| Intervalo de tempo que não pode sobrepor | `tstzrange` + `EXCLUDE USING gist` (`btree_gist`) |
| Unicidade condicional (só para ativos) | Índice `UNIQUE` parcial (`WHERE`) |
| Apagar pai com filhos | `ON DELETE RESTRICT` por padrão; `CASCADE` só se o filho não existe sem o pai |
| Dois usuários alteram a mesma linha | Versão otimista ou `SELECT ... FOR UPDATE` |
| Consulta lenta | Medir com `EXPLAIN (ANALYZE)` antes de criar índice |
| Lista grande paginada | Keyset; offset só em listas pequenas |
| Mudar coluna em tabela usada | Expand/contract em migrações separadas |
| Dado de teste | Seed separado da migração, nunca em produção |

## Dicionário de dados e estratégia de persistência

DIC e PERSIST são artefatos únicos por projeto (ids fixos `DIC` e `PERSIST`): **cabeçalho YAML
validado** por `specs/dominio/artefatos/dic.schema.json` e `persist.schema.json` + corpo Markdown
livre que não os contradiz (decisão D-01). Moldes e exemplos em `agentes/templates/DIC.md` e
`PERSIST.md`. **Vale o esquema.** Ambos são da etapa `dados` (gate.dados).

- **Dicionário (DIC):** campos `tabelas` (cada uma com `colunas` {nome, tipo, nulo, padrao, descricao,
  dado_pessoal}, `constraints` {nome, tipo, definicao, regra} e `indices`) e `dados_pessoais`
  (obrigatório, `[]` se não houver; `campo: tabela.coluna`, `finalidade`, `base_legal`, `retencao`).
  Toda coluna com `dado_pessoal: true` aparece em `dados_pessoais`; nomes de tabela, coluna e
  constraint em `snake_case`; `tipo` de constraint em `primary_key | foreign_key | unique | check |
  exclude | not_null`; `regra` opcional liga a constraint à US (`US-NN/CA-n` ou `US-NN/RN-n`).
  O DBML é a fonte; o dicionário explica e não o contradiz.
- **Estratégia de persistência (PERSIST):** índices justificados por consulta,
  concorrência e transações, migrações, desempenho (plano de execução das consultas críticas),
  **backup e recuperação** (RPO/RTO vindos de RNF; frequência, retenção e teste de restauração;
  sem RNF, registrar suposição para aprovação) e **permissões** (usuário da aplicação com
  privilégio mínimo e sem DDL em produção; migração com outro usuário; dado pessoal protegido em
  logs e ambientes de teste). Campos: `banco`, `acesso`, `concorrencia`, `indices`, `migracoes`,
  `desempenho`, `backup` (`rpo`/`rto`/`estrategia`, ou `sem_requisito: true` com `justificativa`),
  `permissoes` (`ddl_em_producao: false` para a aplicação) e `protecao_dados_pessoais`.

Exemplos mínimos válidos (trechos próprios do artefato; o cabeçalho comum é o do template):

```yaml
---
id: DIC
tipo: DIC
titulo: Dicionário de dados — Reserva de salas
autor: dba-agent
onda: DBA-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: DBA-01, descricao: criação}
modelo_dbml: docs/dados/modelo-dados.dbml
tabelas:
  - nome: reservas
    descricao: Reserva de uma sala por um funcionário em um período
    colunas:
      - {nome: id, tipo: bigint identity, nulo: false, padrao: null, descricao: Identificador, dado_pessoal: false}
      - {nome: sala_id, tipo: bigint, nulo: false, padrao: null, descricao: Sala reservada, dado_pessoal: false}
      - {nome: usuario_id, tipo: bigint, nulo: false, padrao: null, descricao: Funcionário dono da reserva, dado_pessoal: false}
      - {nome: periodo, tipo: tstzrange, nulo: false, padrao: null, descricao: Intervalo reservado, dado_pessoal: false}
    constraints:
      - {nome: reservas_pkey, tipo: primary_key, definicao: PRIMARY KEY (id)}
      - {nome: reservas_sem_sobreposicao, tipo: exclude, definicao: "EXCLUDE USING gist (sala_id WITH =, periodo WITH &&)", regra: US-03/RN-1}
    indices: [reservas_usuario_idx]
dados_pessoais:
  - {campo: usuarios.email, finalidade: Autenticar o funcionário, base_legal: execucao_contrato, retencao: Enquanto a conta existir}
---
```

```yaml
---
id: PERSIST
tipo: PERSIST
titulo: Estratégia de persistência — Reserva de salas
autor: dba-agent
onda: DBA-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: DBA-01, descricao: criação}
banco: {sgbd: PostgreSQL, versao: estável atual}
acesso: SQL explícito com mapeamento manual, conforme ADR-02
concorrencia:
  - {situacao: Duas reservas simultâneas da mesma sala e horário, garantia: Exclusion constraint reservas_sem_sobreposicao}
indices:
  - {nome: reservas_usuario_idx, tabela: reservas, colunas: [usuario_id], tipo: btree, justificativa: Listagem "minhas reservas" (US-03) e FK, custo_escrita_aceito: true}
migracoes: {ferramenta: ferramenta de migração da ADR-02, convencao: Migração aplicada nunca é editada; mudança destrutiva em duas fases}
desempenho:
  - {consulta: Disponibilidade por sala e dia, volume: 5.000 reservas, meta: p95 <= 300 ms, plano_conferido: true, rnf: RNF-02}
backup:
  rpo: 24 h
  rto: 4 h
  estrategia: Dump diário com retenção de 7 dias e teste de restauração a cada release
permissoes:
  - {papel: usuario_app, privilegios: [SELECT, INSERT, UPDATE, DELETE], ddl_em_producao: false}
protecao_dados_pessoais: E-mail mascarado em logs e ausente do seed
---
```

## Esqueleto de tabela (referência)

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE salas (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  nome        text        NOT NULL CHECK (length(btrim(nome)) > 0),
  capacidade  integer     NOT NULL CHECK (capacidade > 0),
  ativa       boolean     NOT NULL DEFAULT true,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT salas_nome_uk UNIQUE (nome)
);

CREATE TABLE reservas (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sala_id     bigint      NOT NULL REFERENCES salas(id)    ON DELETE RESTRICT,
  usuario_id  bigint      NOT NULL REFERENCES usuarios(id) ON DELETE RESTRICT,
  periodo     tstzrange   NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT reservas_periodo_valido CHECK (NOT isempty(periodo) AND lower_inf(periodo) = false AND upper_inf(periodo) = false),
  CONSTRAINT reservas_sem_sobreposicao EXCLUDE USING gist (sala_id WITH =, periodo WITH &&)
);
CREATE INDEX reservas_usuario_idx ON reservas (usuario_id);
```

A `EXCLUDE` já cria índice GiST em (`sala_id`, `periodo`), que serve para buscas por sala.

## Checklist de revisão de modelo

- [ ] Entidades e relações cobrem todas as US; nada sobrando.
- [ ] PK em toda tabela; tipo da PK decidido e registrado (ADR ou dicionário).
- [ ] `timestamptz` para instantes; `created_at`/`updated_at` onde úteis.
- [ ] `numeric` ou inteiro para valores exatos; sem `float` para dinheiro.
- [ ] `NOT NULL` por padrão; `NULL` só com significado claro (e documentado).
- [ ] `CHECK` para regras de valor; `UNIQUE` para unicidades (inclusive parciais).
- [ ] Regras de sobreposição/exclusão com `EXCLUDE`.
- [ ] FK com `ON DELETE` escolhido e **índice** em cada coluna de FK.
- [ ] Índices justificados por consulta; sem índice "por via das dúvidas".
- [ ] Mapa constraint -> erro da API (409/400) definido.
- [ ] `esquema.sql` roda em banco limpo, sem erro, e é idempotente em ambiente de teste (recriável).
- [ ] Plano de migração: primeira migração = DDL base; mudanças seguem expand/contract.
- [ ] Seed de desenvolvimento/teste separado e sem dados reais.
- [ ] `DIC.md` (DIC) válido no esquema: todas as tabelas e colunas; `dados_pessoais` com finalidade, `base_legal` e `retencao`.
- [ ] `PERSIST.md` (PERSIST) válido no esquema, com backup/recuperação (RPO/RTO ou "sem requisito" justificado) e permissões de privilégio mínimo.
- [ ] Contrato da API conferido contra o modelo (campo sem origem ou regra sem garantia no banco = ACH `categoria: lacuna_contrato`, `etapa_destino: arquitetura`; ver `specs/dominio/achado.md`).
- [ ] Nada de versão ou recurso sem "(conferir)" quando a disponibilidade é incerta.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| `timestamp` sem fuso | Reserva deslocada em horário de verão/fuso | `timestamptz` |
| `float` para dinheiro | Centavos errados | `numeric(12,2)` ou centavos |
| Unicidade só no código | Duplicata sob concorrência | `UNIQUE` |
| Sobreposição checada com `SELECT` e depois `INSERT` | Corrida | `EXCLUDE` |
| FK sem índice | Deleção e junção lentas, locks longos | Índice na coluna da FK |
| `ON DELETE CASCADE` por hábito | Apaga histórico sem querer | `RESTRICT`; `CASCADE` só quando consciente |
| Enum para lista que muda | Migração a cada valor | Tabela de domínio |
| `ALTER TABLE` pesado em horário comum | Bloqueia a aplicação | Expand/contract, `lock_timeout` |
| Editar migração já aplicada | Bancos divergem | Nova migração |
| Seed dentro da migração de produção | Dados de teste em produção | Seed separado |
| `SELECT *` em consulta de aplicação | Quebra com coluna nova | Colunas explícitas |
| Índice em tudo | Escrita lenta e espaço | Só com consulta e medição |

## Índice de references

| Arquivo | Quando ler |
|---|---|
| `references/dbml-e-tipos.md` | Ao escrever o DBML e escolher tipos, chaves e enum vs tabela vs check |
| `references/constraints.md` | Ao transformar regra de negócio em NOT NULL/CHECK/UNIQUE/FK/EXCLUDE |
| `references/indices.md` | Ao decidir índices (btree, composto, parcial, GiST) e quando não indexar |
| `references/concorrencia-transacoes.md` | Ao tratar disputa: isolamento, locks, versão, tradução de erro para 409 |
| `references/migracoes.md` | Ao escrever migrações e alterar esquema sem lock longo |
| `references/paginacao-e-seed.md` | Ao paginar listas (offset vs keyset) e preparar dados de teste |

Skills relacionadas: `../../arquitetura/arquitetura-web/` (ADR, contrato, T),
`../../arquitetura/padroes-arquitetura-codigo/` (onde a regra mora),
`../../arquitetura/ambiente-docker/` (banco no Compose, seed). Templates: `agentes/templates/DIC.md`
e `agentes/templates/PERSIST.md`; esquemas em `specs/dominio/artefatos/`.
