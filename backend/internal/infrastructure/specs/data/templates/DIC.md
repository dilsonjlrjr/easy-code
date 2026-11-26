# Template — Dicionário de dados (DIC) — v1

- **Esquema:** specs/dominio/artefatos/dic.schema.json
- **Quem preenche:** agente DBA.
- **Quem lê:** Desenvolvedor, QA, Segurança (dados pessoais), Arquiteto.
- **Gate confere:** toda tabela e coluna do `modelo-dados.dbml` descrita; dado pessoal marcado
  com finalidade; toda regra de negócio garantida no banco aponta para sua constraint.
- **Arquivo:** `docs/dados/DIC.md` (um por projeto, versionado).

## Molde

````markdown
---
id: DIC
tipo: DIC
titulo: Dicionário de dados — <projeto>
autor: dba-agent
onda: DBA-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido
vinculos: []                  # ex.: ADR de persistência (relação genérica)
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: DBA-<nn>, descricao: criação}
modelo_dbml: docs/dados/modelo-dados.dbml
tabelas:                      # ao menos 1
  - nome: <tabela>
    descricao: <o que cada linha representa>
    colunas:
      - {nome: id, tipo: bigint identity, nulo: false, padrao: null, descricao: Identificador, dado_pessoal: false}
    constraints:              # tipo: primary_key | foreign_key | unique | check | exclude | not_null
      - nome: <nome_explicito>
        tipo: exclude
        definicao: <definição>
        regra: US-NN/CA-n     # opcional: US-NN/CA-n ou US-NN/RN-n
    indices: []               # nomes; detalhe em PERSIST
dados_pessoais: []            # obrigatório, mesmo vazio
#  - campo: tabela.coluna
#    finalidade: <finalidade>
#    base_legal: a_confirmar  # a_confirmar | consentimento | obrigacao_legal | politica_publica | pesquisa | execucao_contrato | exercicio_direitos | protecao_vida | tutela_saude | legitimo_interesse | protecao_credito
#    retencao: <prazo e regra>
---

# Dicionário de dados — <projeto>

## <tabela>
<descrição e tabelas legíveis de colunas e constraints; uma seção por tabela>

## Dados pessoais
````

## Regras

- O DBML é a fonte; o dicionário explica e não contradiz. Mudou o modelo, atualiza os dois.
- Nomes de constraint explícitos: o Dev traduz a violação pelo nome/SQLSTATE.
- Toda coluna com `dado_pessoal: true` aparece em `dados_pessoais` como `tabela.coluna`.
