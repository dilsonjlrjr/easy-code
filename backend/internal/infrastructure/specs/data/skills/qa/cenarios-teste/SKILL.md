---
name: cenarios-teste
description: Como derivar, escrever, cobrir e executar cenários de teste (CT) a partir de critérios de aceite e RNF - partição de equivalência, valor limite, tabela de decisão, transição de estado, pairwise, risco - com rastreabilidade US/CA/CT, autorização, concorrência, tempo e dados, automação por nível, evidência, teste intermitente e abertura de ACH. Use no modo CENÁRIOS (antes do código), no modo EXECUÇÃO e na revisão do gate de QA.
---

# Cenários de teste

Um CT é uma promessa verificável: "dado este estado, quando esta ação, então este resultado
observável". O trabalho do QA não é escrever muitos testes, é **provar que cada critério de
aceite vale** e **procurar onde a aplicação quebra** com o menor conjunto de testes que cubra
o risco. Funciona para qualquer aplicação web; os exemplos usam reserva de salas só como
ilustração.

## Princípios

1. **Rastreabilidade primeiro.** Todo CT cobre um `US-NN / CA-N` (ou `RNF-NN`). CT sem vínculo
   é ruído; CA sem CT é buraco. O gate confere exatamente isso.
2. **Comportamento, não implementação.** No modo CENÁRIOS o código não existe. Escreva o que o
   usuário ou o cliente da API observa, nunca classe, método ou tabela interna.
3. **Resultado completo.** Verifique a resposta **e** o estado final (nada criado, nada
   alterado, nenhum e-mail enviado). Teste de recusa que só olha o status passa com bug.
4. **Técnica certa para o tipo de regra**, não "mais um caso feliz" (tabela abaixo).
5. **Risco decide a profundidade.** Dinheiro, dado pessoal, permissão e concorrência recebem
   mais casos; texto de rótulo recebe um.
6. **Nível mais baixo que prova o comportamento.** Regra pura = unitário; contrato HTTP +
   banco = integração; jornada = e2e (poucos). Complementar: skill `estrategia-testes` (Dev),
   que cuida da pirâmide do lado do código. Aqui: o que o CT exige, não como o Dev organiza o
   código de teste.
7. **Dados explícitos.** Pré-condição com valores literais (sala "Azul", 10h–11h), nunca
   "uma reserva qualquer".
8. **Não corrija a aplicação.** O QA relata (ACH); o Dev corrige.

## Formato do artefato (CT)

O CT é um arquivo `docs/qa/cenarios/CT-NN.md` (um comportamento por CT): **cabeçalho YAML validado** por
`specs/dominio/artefatos/ct.schema.json` + corpo Markdown livre que nunca contradiz o YAML (decisão D-01).
Molde e exemplo em `agentes/templates/CT.md`. **Vale o esquema.** Exemplo mínimo válido:

```yaml
---
id: CT-07
tipo: CT
titulo: Reservar sala livre cria a reserva
autor: qa-agent
onda: QA-01
versao: 1
estado: rascunho
vinculos: []
historico:
  - {versao: 1, data: "2026-11-19", onda: QA-01, descricao: criação}
cobre: {us: US-03, criterio: CA-1}
nivel: integracao
tipo_caso: positivo
precondicoes:
  - sala "Azul" livre das 10h às 11h de 2026-12-01; usuário logado
passos:
  - Reservar "Azul" das 10h às 11h de 2026-12-01
resultado_esperado:
  - resposta 201 e a reserva aparece em "minhas reservas"
---
```

- `cobre`: `{us: US-NN, criterio: CA-N}` ou, para CT não funcional, `{rnf: RNF-NN}`; vínculo no campo
  próprio (`vinculos` só para relação genérica).
- `nivel`: `unitario | integracao | e2e | nao_funcional`; `tipo_caso`: `positivo | negativo | limite | seguranca`.
- `precondicoes`, `passos`, `resultado_esperado`: listas com ao menos 1 item, sem "a definir".
- `execucao` é campo de acompanhamento (o QA o preenche na execução, sem nova versão nem reabrir etapa):
  `teste` (`arquivo::nome`), `resultado` (`passou | falhou | bloqueado | nao_executado`), `evidencia`,
  `achado` (`ACH-NN`, obrigatório se falhou) e `onda`.
- Etapa: o plano de CT nasce na atividade `qa-plano` (após `requisitos` aprovada) e é aprovado no
  `gate.validacao`; a execução é da etapa `qa`.

## Regras de decisão

| Característica do critério | Técnica | Referência |
|---|---|---|
| Campo/entrada com faixa ou formato | Partição de equivalência + valor limite | tecnicas-derivacao |
| Número, data, tamanho, intervalo | Valor limite (borda, borda±1) | tecnicas-derivacao |
| Várias condições combinadas (papel x dono x estado) | Tabela de decisão | tecnicas-derivacao |
| Entidade com ciclo de vida | Transição de estado (válidas + inválidas) | tecnicas-derivacao |
| Muitos parâmetros independentes (filtros, navegador) | Pairwise | tecnicas-derivacao |
| Pouco tempo / muito escopo | Testes baseados em risco | tecnicas-derivacao |
| Quem pode fazer o quê | Matriz papel x ação x recurso | autorizacao-seguranca-funcional |
| Unicidade, saldo, vaga, idempotência | Concorrência | concorrencia-tempo-dados |
| Data/hora, fuso, virada de dia | Tempo | concorrencia-tempo-dados |
| RNF mensurável | CT de nível "não funcional" | nao-funcionais |

## Fluxo de trabalho

### Modo CENÁRIOS (antes do código)

1. Ler as US do lote, seus CA, regras de negócio, glossário e RNF.
2. Antes de escrever: listar por US as **regras** (decisões, limites, estados, papéis).
   Critério ambíguo ou impossível de testar = ACH com `etapa_destino: requisitos`, não chute.
3. Aplicar a técnica da tabela acima a cada regra; gerar a lista bruta de casos.
4. **Podar:** juntar casos equivalentes, cortar o que não muda decisão nem risco.
5. Classificar cada caso: nível, tipo (positivo/negativo/limite/segurança), prioridade por risco.
6. Escrever os `CT-NN.md` (artefato CT; template `agentes/templates/CT.md`, esquema `ct.schema.json`), um comportamento por CT.
7. Montar a matriz de cobertura US/CA x CT e conferir buracos (cobertura-rastreabilidade).
8. Revisar com o checklist de saída (abaixo) e a revisão do gate (revisao-gate-qa).

### Modo EXECUÇÃO (depois do código)

1. Ler o README do app e subir o ambiente do zero.
2. Automatizar o que o Dev não cobriu, no nível do CT (execucao-automacao-evidencia).
3. Rodar em ambiente limpo; preencher o campo `execucao` de cada CT.
4. Falha: reproduzir 2 vezes; separar bug, CA ambíguo, teste ruim, ambiente; abrir ACH.
5. Teste intermitente conta como falhou até estabilizar.
6. Escrever o REL-QA (esquema `rel-qa.schema.json`) com `matriz`, `sem_cobertura` e `comandos` que rodam do zero.

## Checklist de saída (modo CENÁRIOS)

- [ ] Todo CA de US Must tem ao menos um CT positivo.
- [ ] CA com regra de negócio tem também negativo e limite.
- [ ] Toda US com dado de outro usuário tem CT de autorização (sem login, outro usuário, papel errado).
- [ ] Regra de unicidade/saldo tem CT de concorrência (ou justificativa de risco aceito).
- [ ] RNF verificável por teste tem CT "não funcional" com limiar numérico.
- [ ] Cada CT: `cobre`, `nivel`, `tipo_caso`, `precondicoes`, `passos`, `resultado_esperado` completos.
- [ ] Nenhum CT cita classe/função; nenhum usa "etc." ou "algum valor".
- [ ] Ids sequenciais `CT-NN`, cabeçalho YAML válido no esquema, `onda: QA-<nn>`, `cobre` com US/CA (ou RNF) existentes.
- [ ] CA ambíguos viraram ACH (`categoria: requisito_ambiguo`, `etapa_destino: requisitos`).

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Só caminho feliz | Bugs de borda e permissão passam | Aplicar limite, negativo e autorização |
| Um CT com 12 passos e 6 verificações | Falha não diz o quê quebrou | Um comportamento por CT |
| Verificar só o status HTTP | Efeito colateral indevido passa | Verificar estado final |
| Tudo em e2e | Suíte lenta e instável | Descer para integração/unitário |
| Resultado "funciona corretamente" | Não verificável | Valor ou mensagem concretos |
| Data relativa sem âncora ("amanhã") | Teste muda com o dia/fuso | Relógio controlado ou "hoje + 1 dia" fixado no setup |
| Dado compartilhado entre CT | Ordem muda o resultado | Cada CT cria o próprio dado |
| Copiar a regra do código para o teste | Teste passa mesmo com bug | Derivar do CA, com valores calculados à mão |
| CT de segurança só no relatório de Segurança | Falha funcional de acesso escapa de QA | Cobrir autorização aqui; auditoria de segurança confirma |
| Ignorar RNF | Só funcional é verificado | CT não funcional com limiar |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/tecnicas-derivacao.md` | Para derivar casos de um CA: partição, limite, tabela de decisão, estados, pairwise, risco, com exemplos completos |
| `references/cobertura-rastreabilidade.md` | Para montar a matriz US/CA x CT, medir cobertura e achar buracos |
| `references/autorizacao-seguranca-funcional.md` | Para cenários de login, papéis, dono do recurso, IDOR, entrada hostil |
| `references/concorrencia-tempo-dados.md` | Para corrida, idempotência, fuso, virada de dia, validação de dados, texto e arquivos |
| `references/nao-funcionais.md` | Para CT derivado de RNF: desempenho, acessibilidade, resiliência |
| `references/execucao-automacao-evidencia.md` | No modo EXECUÇÃO: automação por nível, ambiente limpo, evidência, intermitência, abrir ACH |
| `references/revisao-gate-qa.md` | Antes de entregar ou ao revisar o QA de outro agente: checklist com severidade |

Skill complementar: `playwright-e2e` (automação dos CT de nível e2e).
