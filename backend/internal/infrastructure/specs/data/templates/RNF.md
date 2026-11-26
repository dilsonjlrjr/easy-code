# Template — Requisito não funcional (RNF) — v1

- **Esquema:** specs/dominio/artefatos/rnf.schema.json
- **Quem preenche:** agente de Requisitos (RNF de segurança podem vir da Segurança na Arquitetura).
- **Quem lê:** Arquitetura, Dev, QA, Segurança.
- **Gate confere:** todo RNF tem métrica e forma de verificação.
- **Arquivo:** `docs/requisitos/RNF-NN-<slug>.md`.

## Molde

````markdown
---
id: RNF-NN
tipo: RNF
titulo: <título curto>
autor: requirements-agent
onda: REQ-<nn>
versao: 1
estado: rascunho              # rascunho | em_revisao | aprovado | substituido
vinculos: []                  # só relação genérica; US afetadas vão em `us_afetadas`
historico:
  - {versao: 1, data: "<AAAA-MM-DD>", onda: REQ-<nn>, descricao: criação}
categoria: seguranca          # desempenho | disponibilidade | seguranca | privacidade_lgpd | acessibilidade | usabilidade | manutenibilidade | compatibilidade | auditabilidade | observabilidade | portabilidade | custo
requisito: <o que o sistema deve garantir>
metrica: <número ou condição objetiva>
verificacao:
  tipo: teste_automatizado    # teste_automatizado | varredura | revisao_manual | medicao
  detalhe: <como verificar>
prioridade: must              # must | should | could
origem:
  tipo: cliente               # cliente | lei | norma | boa_pratica
  detalhe: <entrada do usuário, lei, norma ou prática>
us_afetadas: []               # ids de US
---

# RNF-NN — <título curto>

## Contexto
<texto livre: por que importa, como se mede na prática; não contradiz o YAML>
````

## Regras

- Sem métrica não é RNF, é desejo. "Sistema rápido" não serve; "listagem responde em até 500 ms com 1.000 reservas" serve.
- Se a métrica não veio do usuário, usar `origem.tipo: boa_pratica` e deixar o valor visível para o gate.

## Exemplo

````markdown
---
id: RNF-02
tipo: RNF
titulo: Acesso só autenticado
autor: requirements-agent
onda: REQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: REQ-01, descricao: criação}
categoria: seguranca
requisito: toda rota da API, exceto login e health check, exige usuário autenticado
metrica: 100% das rotas protegidas retornam 401 sem token válido
verificacao:
  tipo: teste_automatizado
  detalhe: teste que percorre todas as rotas sem token
prioridade: must
origem:
  tipo: cliente
  detalhe: '"Funcionários entram com login"'
us_afetadas: [US-01, US-03, US-04]
---

# RNF-02 — Acesso só autenticado

## Contexto
Só funcionários cadastrados usam o sistema.
````
