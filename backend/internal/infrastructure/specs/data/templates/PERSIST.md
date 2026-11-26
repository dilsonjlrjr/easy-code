# Template — Estratégia de persistência (PERSIST) — v1

- **Esquema:** specs/dominio/artefatos/persist.schema.json
- **Quem preenche:** agente DBA.
- **Quem lê:** Arquiteto (validação técnica), Desenvolvedor, Segurança, QA.
- **Gate confere:** toda consulta frequente com índice justificado; estratégia de migração
  definida; backup e recuperação com RPO/RTO (ou "sem requisito" justificado); permissões de
  acesso ao banco com privilégio mínimo.
- **Arquivo:** `docs/dados/PERSIST.md` (um por projeto, versionado).

## Molde

````markdown
---
id: PERSIST
tipo: PERSIST
titulo: Estratégia de persistência — <projeto>
autor: dba-agent
onda: DBA-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido
vinculos: []                  # RNF, ADR, DIC (relação genérica)
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: DBA-<nn>, descricao: criação}
banco: {sgbd: <SGBD>, versao: <versão alvo>}
acesso: <padrão de acesso (SQL explícito, ORM), conforme ADR>
concorrencia: []              # [] se não há disputa
#  - {situacao: <onde há disputa>, garantia: <como o banco garante a regra>}
indices: []
#  - {nome: <idx>, tabela: <tabela>, colunas: [<col>], tipo: btree, justificativa: <consulta/regra>, custo_escrita_aceito: true}
migracoes: {ferramenta: <ferramenta>, convencao: <nomes; migração aplicada nunca editada; destrutivas em duas fases>}
desempenho: []
#  - {consulta: <consulta>, volume: <volume>, meta: <meta>, plano_conferido: true, rnf: RNF-NN}
backup:                       # uma das duas formas
  rpo: <perda máxima aceita>
  rto: <tempo máximo para voltar>
  estrategia: <frequência, retenção, teste de restauração>
# backup: {sem_requisito: true, justificativa: <por quê>}
permissoes:                   # ao menos 1; aplicação sem DDL em produção
  - {papel: <usuario_app>, privilegios: [SELECT, INSERT, UPDATE, DELETE], ddl_em_producao: false}
protecao_dados_pessoais: <proteção, mascaramento em logs/ambientes de teste>
---

# Estratégia de persistência — <projeto>

## 1. Banco e acesso
## 2. Índices
## 3. Migrações
## 4. Desempenho
## 5. Backup e recuperação
## 6. Segurança e permissões
````

## Regras

- Integridade crítica sob concorrência é constraint, não verificação na aplicação.
- Índice sem consulta que o justifique não entra.
- Números de RPO/RTO e desempenho vêm de RNF; sem RNF, registrar suposição para aprovação.
