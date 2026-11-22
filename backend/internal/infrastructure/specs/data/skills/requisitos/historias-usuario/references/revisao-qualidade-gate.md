# Revisão de qualidade do gate (Requisitos)

Use antes de entregar a Etapa de Requisitos. O Gate (aprovação humana) confere o critério de
saída das instruções do agente e as regras dos templates. Esta revisão antecipa o que o Gate e as Etapas
seguintes vão reprovar. Cada item tem **severidade**:

- **Bloqueia:** reprovação certa no Gate. Corrija antes de entregar.
- **Alto:** gera Achado nas Etapas seguintes (retrabalho).
- **Médio:** reduz qualidade; corrija se houver tempo.

## 1. Critério de saída das instruções do agente (todos bloqueiam)

- [ ] Toda US tem ao menos um CA testável.
- [ ] Nenhuma pergunta aberta marcada como `bloqueante: true`.
- [ ] Todo RNF tem métrica e forma de verificação (ver skill de RNF).

## 2. Estrutura e cabeçalho

| # | Item | Severidade |
|---|---|---|
| 2.1 | Um arquivo por US, nome igual ao `id` (`US-03.md`) | Bloqueia |
| 2.2 | Cabeçalho YAML válido no esquema `us.schema.json` (campos comuns: `id`, `tipo: US`, `titulo`, `autor`, `onda`, `versao`, `estado`, `vinculos`, `historico`) | Bloqueia |
| 2.3 | `onda` no padrão `REQ-<nn>` | Alto |
| 2.4 | `id` com prefixo + 2 dígitos, sem duplicata nem lacuna injustificada | Alto |
| 2.5 | Campos obrigatórios sem "a definir" (`ator`, `quero`, `para`, `prioridade`, `origem`, `tem_interface`) | Bloqueia |
| 2.6 | `rnf` lista os RNF que afetam a US; ids existem | Alto |
| 2.7 | Quantidade de US dentro do alvo de aplicação pequena (5 a 10) ou corte proposto | Médio |

## 3. Qualidade de cada US

| # | Item | Severidade |
|---|---|---|
| 3.1 | Título com verbo no infinitivo, sem "e" | Médio |
| 3.2 | Ator é papel concreto (não "usuário" quando há papéis distintos) | Alto |
| 3.3 | "Quero" tem um único objetivo | Alto |
| 3.4 | "Para" expressa benefício real (não circular) | Médio |
| 3.5 | **Origem** é trecho literal da ideia/resposta; nenhuma inventada | Bloqueia |
| 3.6 | Sem tecnologia, tabela, endpoint, layout | Alto |
| 3.7 | `tem_interface` coerente (rotina automática = `false`) | Alto: Design se baseia nisso |
| 3.8 | INVEST: cabe em uma Onda; senão dividida | Alto |

## 4. Qualidade dos CA

| # | Item | Severidade |
|---|---|---|
| 4.1 | Todos no formato Dado/Quando/Então | Bloqueia |
| 4.2 | Resultado observável; nenhum termo vago (rápido, fácil, intuitivo, adequado, corretamente) | Bloqueia |
| 4.3 | Um comportamento por CA (sem dois "Então" distintos) | Alto |
| 4.4 | Piso por US: feliz + falha esperada + limite (quando aplicável) | Alto |
| 4.5 | Autorização: CA para quem não pode | Alto |
| 4.6 | Concorrência onde há recurso disputado | Alto |
| 4.7 | Dado concreto (valores, papel) | Médio |
| 4.8 | Ids CA-1..CA-n sem lacuna | Médio |
| 4.9 | CA não depende da execução de outro CA | Médio |

## 5. Regras, glossário e perguntas

| # | Item | Severidade |
|---|---|---|
| 5.1 | Toda regra de negócio exercitada por ao menos um CA | Alto |
| 5.2 | Toda regra é afirmação verificável (sem "adequado") | Alto |
| 5.3 | Termos de domínio no `glossario` da VISAO; um nome por conceito | Alto |
| 5.4 | Perguntas abertas têm `bloqueante: true` ou `bloqueante: false` | Bloqueia |
| 5.5 | Suposições registradas em `suposicoes` do BACKLOG com `motivo` | Alto |
| 5.6 | Nada decidido em silêncio que mude escopo/custo (deveria ser pergunta) | Alto |

## 6. Backlog e visão

| # | Item | Severidade |
|---|---|---|
| 6.1 | Toda US no backlog em ordem de prioridade, e vice-versa | Bloqueia |
| 6.2 | MoSCoW em todas; Must até ~60% do esforço relativo | Alto |
| 6.3 | "Depende de" sem ciclo; ordem respeita dependências | Médio |
| 6.4 | Won't em `adiadas` com `motivo` | Médio |
| 6.5 | VISAO válida no esquema `visao.schema.json`: `problema`, `atores`, `objetivo`, `metricas_sucesso`, `fora_de_escopo`, `glossario` | Bloqueia |
| 6.6 | Jornada principal completa no corte de Must | Alto |

## 7. Rastreabilidade (olhando para frente)

O que as Etapas seguintes vão precisar:

| Consumidor | Precisa de | Se faltar |
|---|---|---|
| QA | CA testáveis, um CT por CA | CT não escrevível -> Achado |
| Arquitetura | Regras e entidades claras, RNF com métrica | ADR sem critério |
| Design | `tem_interface` correto, CA visíveis (erro de conflito) | Estado sem tela |
| Dev | Regras de limite/inclusão explícitas | Interpretação divergente |

Teste rápido: pegue uma US ao acaso e simule ser o QA. Consegue escrever os CT sem
perguntar nada? Se não, volte.

## 8. Roteiro de autorrevisão (10 minutos)

1. **Varredura de vagueza:** busque no texto: rápido, fácil, simples, intuitivo, adequado,
   corretamente, amigável, robusto, "etc.". Cada ocorrência é defeito.
2. **Varredura de "e":** títulos e "Quero" com "e".
3. **Matriz regra x CA:** liste as regras; marque o CA de cada. Linha vazia = CA faltando.
4. **Matriz papel x US:** para cada US, o que cada papel que *não* deveria fazer vê?
5. **Origem:** confira cada citação no texto original (busca literal). Citação inexistente é
   falha grave.
6. **Valores-limite:** para cada número nas regras, existe CA no limite e vizinho?
7. **Simulação QA:** escolha uma US e escreva mentalmente 3 CT.
8. **Contagem:** US entre 5 e 10? Must <= ~60%?

## 9. Erros que mais aparecem (por frequência esperada)

1. CA só de caminho feliz.
2. Termo vago no Então.
3. Origem parafraseada ou inventada.
4. Regra de conflito sem definir encostado/sobreposto.
5. Ator "usuário".
6. `tem_interface` errado (a US de cancelar por rotina).
7. Pergunta marcada não bloqueante que na verdade impede escrever o CA.
8. Suposição escondida no texto da US em vez do backlog.
9. RNF de segurança esquecido nas US de acesso.
10. US técnica.

## 10. Como relatar o resultado ao entregar

No fim da resposta do agente: arquivos criados, suposições, perguntas não bloqueantes. Se
algum item "Alto" ficou sem resolver, diga qual e por quê, para o Gate decidir. Nunca
esconda uma pendência marcando-a como resolvida.

## 11. Retrabalho por Achado

Em Onda de retrabalho (achado `ACH-NN`): corrija **só** o que o Achado aponta; suba `versao`;
registre em `historico` do cabeçalho (nova entrada com `versao`, `data`, `onda`, `descricao` e `achado: ACH-NN`); preserve ids e `Origem`. Refaça
neste checklist apenas os itens tocados, mais 3.5 e 4.1 se CA mudou.
