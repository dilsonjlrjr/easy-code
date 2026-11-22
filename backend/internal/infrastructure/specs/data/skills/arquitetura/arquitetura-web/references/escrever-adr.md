# Escrever boas ADR

Template: `agentes/templates/ADR.md`; esquema: `specs/dominio/artefatos/adr.schema.json`. Este arquivo ensina o **conteúdo**: como uma ADR deixa de
ser burocracia e passa a evitar retrabalho.

## Para que serve

A ADR responde "por que é assim?" para quem chega depois: o Dev que implementa sem poder
perguntar, a Segurança que revisa, o próprio agente de Arquitetura numa Onda de retrabalho.
Se a decisão não sobrevive a um leitor sem contexto, a ADR falhou.

## Quando escrever (e quando não)

| Situação | ADR? |
|---|---|
| Linguagem, framework, banco, autenticação, padrão de erros | Sempre |
| Regra de integridade garantida no banco | Sempre |
| Organização do código (camadas, fatias) | Sim, curta |
| Escolha de biblioteca de utilidade (formatador de data, logger) | Não; uma linha em `estrutura.md` |
| Padrão já definido por skill e seguido sem desvio | Não |
| Desvio de recomendação de skill | Sim, explicando o desvio |
| Decisão fácil de reverter em minutos | Não |
| Decisão que o aprovador humano reservou para si | Não decidir: propor opções ao aprovador humano |

## Anatomia de uma boa ADR

1. **Contexto:** o problema e as restrições, com referência a US/RNF. Sem justificativa de
   solução aqui. Duas a seis linhas.
2. **Opções:** pelo menos duas que um profissional razoável escolheria. Inclua **a mais
   simples** sempre. Nunca opção de palha (feita para perder).
3. **Decisão:** a opção, no imperativo ("Usar X"), e a razão ligada ao contexto.
4. **Consequências:** o que fica mais fácil **e** o que fica mais difícil ou obrigatório.
   Consequência só positiva é sinal de ADR enviesada.
5. **Skills usadas** (`skills`) e **vínculos**: `motivada_por` (ao menos 1 US/RNF) e `substitui` (ADR substituída).

## Critérios para comparar opções

Compare nos eixos que importam ao contexto, não em todos:

| Eixo | Pergunta |
|---|---|
| Atende o requisito | Cumpre a US/RNF que motivou? Sob concorrência? |
| Complexidade | Quantas peças novas, quantas linhas a mais? |
| Custo de mudança | Se estiver errada, quanto custa trocar? |
| Risco | O que pode dar errado e com que probabilidade? |
| Operação | Precisa de algo novo para rodar (serviço, extensão, configuração)? |
| Familiaridade | A linguagem e o ecossistema do projeto têm bom suporte? |
| Segurança | Cria superfície nova? (apontar para `revisao-seguranca`) |

Regra: se a diferença entre as opções é pequena, a decisão é **a mais simples** e a ADR
tem poucas linhas. Reserve texto para decisões caras de reverter.

## Exemplo bom: persistência

Cabeçalho YAML exigido pelo esquema `adr.schema.json` (campos próprios da ADR; vale o esquema):

```yaml
---
id: ADR-02
tipo: ADR
titulo: Acesso a dados com SQL explícito e migrações versionadas
autor: architect-agent
onda: ARQ-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: ARQ-01, descricao: criação}
contexto: Modelo de 4 tabelas e 1 regra crítica no banco (ADR-03); a listagem por sala e período (US-05) exige respostas em até 300 ms (RNF-02).
opcoes:
  - nome: A — ORM completo
    pros: [Menos código para CRUD, Mapeamento automático]
    contras: [Esconde o SQL das consultas de período, Risco de N+1]
  - nome: B — Query builder / SQL explícito
    pros: [Controle total do SQL, Fácil usar tstzrange]
    contras: [Mais código de mapeamento]
decisao: Usar a opção B; poucas tabelas e consultas específicas pesam mais que economizar CRUD.
consequencias:
  - Cada consulta é revisável como SQL e o plano de execução é previsível
  - Mapeamento manual exige testes de integração contra PostgreSQL real
  - Se o modelo passar de ~15 tabelas com CRUD uniforme, reavaliar ORM (nova ADR)
skills: [arquitetura-web, modelagem-postgresql]
motivada_por: [US-05, RNF-02]
---
```

Corpo Markdown (livre, nunca contradiz o YAML): pode repetir o raciocínio em prosa, com tabela de
opções, quando ajuda o leitor.

Observe: gatilho de reavaliação explícito, consequência negativa explícita, opção simples (A ou B,
conforme a linguagem) tratada com respeito.

## Exemplo ruim e por quê

```yaml
opcoes:
  - {nome: A — Usar Framework X, pros: [Moderno, popular, rápido], contras: [nenhum]}
  - {nome: B — Fazer tudo à mão, pros: [Trabalhoso], contras: [Lento, inseguro]}
decisao: Opção A, porque é melhor.
```

Problemas: opção B é de palha; "moderno, popular" não é critério ligado ao contexto; "nenhum"
contra; decisão sem razão; sem consequências; sem US/RNF.

## Padrões úteis

- **Gatilho de reavaliação:** "se X acontecer, revisitar". Evita decisão eterna. Ex.: volume
  acima de N linhas, segundo cliente de API, necessidade de tempo real.
- **Decisão adiada com data:** quando faltam dados, registrar a pergunta, o responsável e o
  que bloqueia. Estado `rascunho`, não decisão fingida.
- **Uma decisão por ADR.** "Framework e banco e autenticação" em uma ADR só impede substituir
  uma sem a outra.
- **ADR curta para decisão pequena:** contexto de 2 linhas, tabela com 2 opções, decisão de
  1 linha, 1 consequência. Pequeno não é dispensado, é enxuto.
- **Números ajudam:** "4 tabelas", "5 US", "300 ms", "até 50 usuários". Substituem adjetivos.

## Exemplo: autenticação (resumo)

| Opção | Prós | Contras |
|---|---|---|
| A — Sessão no servidor com cookie `HttpOnly`/`SameSite` | Revogação imediata; sem token no JS | Estado no servidor ou no banco; CSRF a tratar |
| B — JWT curto + refresh | Sem estado por requisição | Revogação difícil; refresh a guardar com cuidado; mais peças |

Para um front único falando com uma API própria, A costuma ser a mais simples. Detalhes em
`autenticacao-autorizacao.md`.

## Substituir uma ADR

1. Criar ADR nova com `substitui: ADR-antiga` e explicar o que mudou no contexto.
2. Marcar a antiga `estado: substituido`, com `substituido_por: ADR-nova`, e registrar no `historico`.
3. Atualizar T afetadas; subir `versao`.
Nunca reescrever o conteúdo de uma ADR aprovada para "combinar" com o código.

## Checklist por ADR

- [ ] Contexto cita US/RNF e restrições.
- [ ] Duas ou mais opções reais; a mais simples está presente.
- [ ] Decisão em uma frase imperativa, com a razão ligada ao contexto.
- [ ] Consequências positivas e negativas.
- [ ] Gatilho de reavaliação quando a decisão é cara de reverter.
- [ ] Skills usadas listadas; desvio de skill explicado.
- [ ] Nenhuma versão ou preço de memória.

## Erros comuns

| Erro | Correção |
|---|---|
| ADR escrita depois do código para justificar | Escrever antes; o Dev segue a ADR |
| Opção de palha | Reescrever com opção que alguém escolheria |
| Consequências só boas | Perguntar "o que isso complica?" |
| Várias decisões numa ADR | Separar |
| ADR genérica copiada de outro projeto | Reancorar em US/RNF deste projeto |
| Decisão que contradiz outra ADR sem mencionar | Vincular e substituir |
