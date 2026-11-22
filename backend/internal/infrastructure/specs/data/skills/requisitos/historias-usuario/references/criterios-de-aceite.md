# Critérios de aceite (CA)

O CA é a parte da US que QA transforma em CT e Dev transforma em teste. No esquema da US (`criterios_aceite`, ao menos 1), cada CA é um objeto com três campos de texto:

```yaml
criterios_aceite:
  - id: CA-1
    dado: <contexto>
    quando: <ação>
    entao: <resultado verificável>
```

Nos exemplos abaixo, "Dado ..., Quando ..., Então ..." em uma linha é só a forma compacta de
leitura dos três campos.

## 1. Anatomia Dado / Quando / Então

| Parte | Responde | Regra |
|---|---|---|
| **Dado** | Em que estado o mundo está? | Pré-condições relevantes e só elas: dados existentes, papel do ator, estado da entidade. Concreto: valores, horários, papéis |
| **Quando** | O que o ator faz? | **Uma** ação do ator (ou um evento). Verbo no presente, voz do ator |
| **Então** | O que ele observa? | Resultado **observável** e verificável: mensagem, item na lista, bloqueio, estado persistido visível. Sem "corretamente" |

Várias pré-condições: encadeie com "e" no Dado. No "Então", se há duas consequências do mesmo
comportamento (ex.: reserva criada **e** aparece na lista) pode manter junto; se são
comportamentos distintos (cria reserva e envia e-mail), separe em dois CA.

## 2. Regras de qualidade

1. **Um CA = um comportamento.** Teste: dá para escrever um único CT que passa ou falha?
2. **Concreto > abstrato.** "Sala reservada das 10h às 11h" em vez de "sala ocupada".
3. **Observável pelo ator ou verificável pela API/estado**, nunca por "o código faz X".
4. **Sem solução.** Não cite componente, rota, tabela, cor, posição.
5. **Independente de ordem.** O Dado recria o contexto; não depende de CA anterior ter rodado.
6. **Sem vagueza.** Proibidos: rápido, fácil, intuitivo, adequado, corretamente, amigável,
   simples, apropriado, robusto, "etc.", "entre outros".
7. **Números e unidades** quando há limite ("no máximo 8 h", "até 100 caracteres").
8. **Mensagem de erro**: descreva o conteúdo mínimo ("informa que a sala já está reservada
   naquele horário"), não o texto final exato, salvo se o cliente exigiu. O texto fica com Design.

## 3. Cobertura: de onde vêm os CA

Para cada US, percorra esta grade. Nem toda célula se aplica, mas decida cada uma
conscientemente.

| Fonte | Pergunta | Tipo de CA |
|---|---|---|
| Objetivo principal | O ator consegue o que quer? | Caminho feliz |
| Cada regra de negócio | Há CA que exercita a regra? E que a viola? | Positivo + negativo |
| Papéis | Quem não pode? O que vê? | Autorização (403/redirect/oculto) |
| Autenticação | Sem login? Sessão expirada? | Acesso |
| Entradas | Vazio, formato inválido, tamanho máximo/mínimo, caractere especial | Validação |
| Limites | Valor exato do limite, um acima, um abaixo, encostado | Limite |
| Estado da entidade | Já cancelada? Já passou? Já existe? | Estado inválido |
| Concorrência | Dois atores ao mesmo tempo no mesmo recurso | Concorrência |
| Vazio | Nenhum registro existe | Estado vazio |
| Falha externa | Rede/serviço fora | Erro (quando for requisito) |

**Piso mínimo por US:** 1 caminho feliz + 1 falha esperada (permissão, validação ou conflito) +
1 limite relevante quando a US envolve valor, intervalo ou quantidade.

### Técnicas de especialista

- **Partição em classes de equivalência:** agrupe entradas que se comportam igual e escreva
  um CA por classe (horário válido / fim antes do início / fora do expediente).
- **Análise de valor-limite:** para cada limite, o valor exato e o vizinho imediato dos dois
  lados. Para duração máxima de 8 h: 8 h (aceita), 8 h 30 (recusa).
- **Tabela de decisão** quando várias condições combinam (seção 6).
- **Estado:** para entidade com ciclo de vida, um CA por transição permitida e um por
  transição proibida.
- **Caso de abuso:** como burlar? Cada resposta plausível vira CA negativo ou RNF.

## 4. Exemplos completos

### US: Reservar sala

Bom:

```markdown
- **CA-1:** Dado sala livre das 10h às 11h de 2026-03-10, Quando reservo essa sala nesse horário,
  Então a reserva é criada e aparece em "minhas reservas" com sala, data e horário.
- **CA-2:** Dado sala reservada das 10h às 11h, Quando tento reservar das 10h30 às 11h30,
  Então recebo mensagem de conflito indicando o horário ocupado e nenhuma reserva é criada.
- **CA-3:** Dado sala reservada das 10h às 11h, Quando reservo das 11h às 12h,
  Então a reserva é criada (horário encostado não é conflito).
- **CA-4:** Dado horário de fim anterior ao de início, Quando tento reservar,
  Então recebo erro de validação no campo de horário e nada é criado.
- **CA-5:** Dado data no passado, Quando tento reservar, Então a reserva é recusada com
  mensagem de data inválida.
- **CA-6:** Dado usuário sem login, Quando tento reservar, Então sou levado à tela de login
  e nada é criado.
- **CA-7:** Dado dois funcionários tentando reservar a mesma sala e horário ao mesmo tempo,
  Quando ambos confirmam, Então exatamente uma reserva é criada e o outro recebe conflito.
```

Por que é bom: concreto, cada CA um comportamento, cobre feliz (1), limite (3), conflito (2),
validação (4,5), autenticação (6), concorrência (7). Cada Então é verificável por CT.

Ruim:

```markdown
- **CA-1:** O sistema deve permitir reservar salas de forma rápida e intuitiva.
- **CA-2:** Reservas devem funcionar corretamente sem conflitos.
- **CA-3:** Dado que o usuário clica no botão verde "Reservar", o sistema salva no banco.
```

Por quê: CA-1 vago e sem Dado/Quando/Então; CA-2 "corretamente" não é verificável e não
define conflito; CA-3 descreve tela e banco (solução), sem resultado observável do ator.

### US: Cancelar reserva (autorização)

```markdown
- **CA-1:** Dado reserva minha futura, Quando cancelo, Então ela some de "minhas reservas" e o
  horário volta a ficar livre para outros.
- **CA-2:** Dado reserva de outro funcionário, Quando tento cancelar, Então a ação é negada
  e a reserva permanece.
- **CA-3:** Dado reserva minha que já terminou, Quando tento cancelar, Então a ação é recusada
  informando que a reserva já ocorreu.
```

### Separação de "Então" duplo

Ruim: `Então a reserva é criada, o e-mail é enviado e o calendário é atualizado.`
Bom: três CA (ou apenas os que o cliente pediu); cada um falha por motivo próprio.

## 5. Ruim -> bom (reescritas rápidas)

| Ruim | Problema | Bom |
|---|---|---|
| "Mensagem de erro amigável" | Subjetivo | "Então a mensagem informa o motivo (sala ocupada) e o horário conflitante" |
| "A lista carrega rápido" | Sem métrica; é RNF | RNF: "Listagem em até 500 ms (p95) com 1.000 reservas" e CA só do conteúdo |
| "Só admin pode cadastrar" | Sem Dado/Quando/Então | "Dado funcionário comum, Quando acesso o cadastro de salas, Então o acesso é negado" |
| "Campos obrigatórios validados" | Quais? Como? | Um CA por campo-regra: "Dado nome vazio, Quando salvo a sala, Então erro no campo nome e sala não criada" |
| "Funciona em qualquer navegador" | Vago | RNF de compatibilidade com lista de navegadores |
| "Dado o sistema, Quando uso, Então funciona" | Vazio | Reescrever do zero |

## 6. Tabela de decisão para regras combinadas

Quando o resultado depende de várias condições, tabele antes de escrever CA.

Regra de cancelamento (exemplo):

| Ator | Reserva é dele? | Já ocorreu? | Resultado |
|---|---|---|---|
| Funcionário | sim | não | Cancela |
| Funcionário | sim | sim | Recusa (já ocorreu) |
| Funcionário | não | qualquer | Nega |
| Administrador | qualquer | não | Cancela |
| Administrador | qualquer | sim | Recusa (já ocorreu) |

Cada linha vira um CA (ou linhas equivalentes se agrupam). O gate de QA vai cobrar linha por
linha.

## 7. Regras de negócio versus CA

A regra é a afirmação geral ("horário encostado não é conflito"). O CA é o caso concreto que a
exercita (CA-3 acima). **Toda regra da seção "Regras de negócio" deve ser exercitada por ao
menos um CA**; toda regra sem CA é regra não testável. Veja
`glossario-e-regras-de-negocio.md`.

## 8. Linguagem Gherkin: o que usar e o que não

O artefato guarda cada CA em três campos (`dado`, `quando`, `entao`). Não precisa de `Funcionalidade:`/`Cenário:`.
Convenções:

- Pode usar `E` dentro do Dado para encadear pré-condições, e `E` no Então para consequências
  inseparáveis.
- Não usar `Esquema do Cenário` neste formato; se uma regra tem muitas variações de dado, use
  tabela na seção "Regras de negócio" e CA representativos (um por classe de equivalência).
- Nada de passos de interface ("clico em", "vejo o botão") se a US pode ser testada também por
  API. Prefira verbos de intenção ("reservo", "cancelo"). Verbos de tela só quando o resultado
  visual **é** o requisito (ex.: "a sala aparece marcada como ocupada").

## 9. Anti-padrões

- **CA decorativo:** não pode falhar. Se nunca pode reprovar, não testa nada.
- **CA de implementação:** "o endpoint retorna 409". 409 é solução; o requisito é "informa conflito".
  (QA e Arquitetura mapearão ao código HTTP.)
- **CA copiado** entre US com variações mínimas: prefira regra comum única.
- **CA que depende de outro:** "Dado que o CA-1 passou...".
- **Dado vago:** "Dado um usuário" sem papel.
- **Então que repete o Quando:** "Quando salvo, Então salvou".
- **Esquecer o estado vazio:** lista sem itens também precisa de comportamento.

## 10. Perguntas-chave

1. Que CT eu escreveria para este CA? Há exatamente um?
2. O que significa "pronto" para um cliente observando?
3. Qual a pior entrada possível aqui e o que o ator vê?
4. Alguém pode fazer isto sem ter o direito? Qual o resultado?
5. E se for a mesma coisa duas vezes? E ao mesmo tempo?
6. Qual o valor-limite exato e o vizinho?

## 11. Checklist de revisão de CA

- [ ] Formato Dado/Quando/Então em todos.
- [ ] Um comportamento por CA; "Então" verificável.
- [ ] Dado concreto, com papel do ator.
- [ ] Nenhum termo vago ou de tecnologia/tela.
- [ ] Piso: feliz + falha + limite (quando aplicável).
- [ ] Toda regra de negócio exercitada por CA.
- [ ] Autorização coberta (quem não pode).
- [ ] Concorrência considerada onde há recurso disputado.
- [ ] Ids sequenciais CA-1..CA-n, sem lacuna.
