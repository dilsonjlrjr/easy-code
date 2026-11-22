# Revisão do gate de QA

Checklist para revisar o trabalho de QA (o próprio, antes de entregar, ou o de outro agente)
em dois momentos: **gate de cenários** (fim do modo CENÁRIOS) e **gate de execução** (fim do
modo EXECUÇÃO, aprova a etapa QA). Cada item tem severidade de **problema da revisão**:

- **Bloqueia:** o gate não passa até corrigir.
- **Alerta:** corrigir ou justificar por escrito; não bloqueia sozinho.
- **Nota:** sugestão de melhoria.

(São severidades da revisão, separadas das severidades de ACH, que descrevem defeitos do produto.)

## 1. Gate de cenários

Critério oficial: "todo critério de aceite de US Must tem ao menos um CT".

| # | Verificação | Severidade se falha |
|---|---|---|
| C1 | Todo CA de US Must tem >= 1 CT positivo | Bloqueia |
| C2 | CA com regra de negócio tem negativo **e** limite | Bloqueia |
| C3 | Cada CT tem `cobre` (US/CA ou RNF), `nivel`, `tipo_caso` válidos no esquema `ct.schema.json` | Bloqueia |
| C4 | Pré-condições, passos e resultado esperado preenchidos, sem "a definir" | Bloqueia |
| C5 | Cabeçalho YAML válido no esquema; `id` = nome do arquivo; `onda` `QA-<nn>`; `cobre` com ids existentes | Bloqueia |
| C6 | Resultado esperado observável e verificável (valor, status, mensagem, estado final) | Bloqueia |
| C7 | CT não cita classe, função ou tabela interna | Alerta |
| C8 | CA ambíguo/não testável foi para ACH (`requisito_ambiguo`, `etapa_destino: requisitos`), não adivinhado | Bloqueia |
| C9 | US com dados de outro usuário tem CT de autorização (sem login, outro usuário, papel errado) | Bloqueia se Must; Alerta se Should |
| C10 | Regra de unicidade/saldo/vaga tem CT de concorrência ou risco aceito escrito | Alerta |
| C11 | RNF verificável tem CT não funcional com limiar do RNF | Bloqueia se RNF Must; Alerta nos demais |
| C12 | Nível adequado (regra pura não está em e2e; só jornadas principais em e2e) | Alerta |
| C13 | Dados explícitos (valores literais); datas ancoradas (sem "amanhã" solto) | Alerta |
| C14 | Um comportamento por CT (sem CT com 5 verificações de assuntos diferentes) | Alerta |
| C15 | Sem duplicatas (casos equivalentes) | Nota |
| C16 | Matriz de cobertura existe e células vazias justificadas | Alerta |
| C17 | Casos de limite têm o lado inclusivo/exclusivo conforme o CA (ou ACH aberto) | Bloqueia |
| C18 | Prioridade/risco explicitado onde a profundidade varia | Nota |

## 2. Gate de execução

Critério oficial: "todo CA de US Must com CT passando e nenhum achado Crítico ou Alto aberto".

### 2.1 Resultado

| # | Verificação | Severidade se falha |
|---|---|---|
| E1 | Todo CA de US Must com todos os CT obrigatórios `passou` | Bloqueia |
| E2 | Zero ACH Crítico/Alto aberto (ou aceito explicitamente pelo aprovador humano no ACH) | Bloqueia |
| E3 | Nenhum CT intermitente marcado como `passou` | Bloqueia |
| E4 | Todo CT falho tem ACH com evidência; todo ACH tem CT/US em `artefatos_afetados` | Bloqueia |
| E5 | Resultados usam só `passou/falhou/bloqueado/nao_executado` | Alerta |

### 2.2 Qualidade da evidência e da execução

| # | Verificação | Severidade se falha |
|---|---|---|
| E6 | Cada CT executado tem `execucao` preenchida (`teste`, `resultado`, `evidencia`, `achado` se falhou) | Bloqueia |
| E7 | Os comandos do REL-QA rodam do zero em ambiente limpo (verificado por quem entrega) | Bloqueia |
| E8 | Evidência é de execução real, reproduzível e sem segredos | Bloqueia |
| E9 | Cada teste automatizado realmente assere o resultado do CT (não é "passa vazio") | Bloqueia |
| E10 | Resposta **e** estado final verificados nos CT negativos | Alerta |
| E11 | Ambiente limpo por execução e isolamento de dados por teste | Alerta |
| E12 | Nenhum `sleep`/espera fixa; sem retry mascarando falha | Alerta |
| E13 | Falha foi classificada (bug, ambíguo, teste, ambiente) antes de virar ACH | Alerta |
| E14 | Um ACH por causa; CT com mesma causa agrupados | Nota |

### 2.3 Relatório

| # | Verificação | Severidade se falha |
|---|---|---|
| R1 | Resumo com números coerentes com a matriz (conferir x de y) | Bloqueia |
| R2 | Matriz com todas as US/CA e CT, com resultado e achado | Bloqueia |
| R3 | Seção "Critérios sem cobertura" presente (escrever "Nenhum" se vazia) | Bloqueia |
| R4 | Veredito segue a regra do template | Bloqueia |
| R5 | Observações citam intermitências e limitações do ambiente | Alerta |
| R6 | Cobertura de código, se citada, não usada como critério de aprovação | Nota |

### 2.4 Segurança funcional e não funcional

| # | Verificação | Severidade se falha |
|---|---|---|
| S1 | Anônimo sem 401 em rota protegida, ou IDOR confirmado | Bloqueia (vira ACH Alto/Crítico) |
| S2 | Matriz papel x ação negada testada | Alerta (Bloqueia se Must) |
| S3 | RNF com limiar: valor medido registrado | Bloqueia se Must |
| S4 | Acessibilidade: se axe usado, relatório que **não** é prova total registrado | Nota |

## 3. Como conduzir a revisão

1. **Amostragem dirigida por risco**: sempre leia inteiros os CT de US Must com autorização,
   concorrência e limite; para o restante, amostre 1 em cada 3.
2. **Reexecute** 3 CT a sua escolha (um passou, um falhou, um de limite) com os comandos do
   REL-QA, do zero. Se qualquer comando falhar: E7 bloqueia.
3. **Cheque a mutação mental**: pegue um CT e pergunte "se o Dev introduzisse tal bug, este
   teste falharia?". Se não, a asserção é fraca (E9).
4. Registre o resultado da revisão em lista curta: itens que bloqueiam, alertas, notas.

## 4. Modelo de resultado da revisão

```markdown
## Revisão do gate — QA (execução)
Veredito da revisão: reprovado (2 bloqueios)

Bloqueios
- E9: CT-14 só verifica status 200; não confere a mensagem de horário ocupado.
- R1: resumo diz 12 de 12 CA Must cobertos; a matriz mostra US-05/CA-3 com CT falhando (11 de 12).

Alertas
- C13: CT-31 usa "amanhã" sem âncora.

Notas
- CT-22 e CT-23 são equivalentes.
```

## 5. Armadilhas do revisor

- Aprovar pelo volume (muitos CT) em vez de por cobertura de regra.
- Confiar no número do resumo sem recontar na matriz.
- Aceitar CT automatizado "verde" sem ler a asserção.
- Tratar CT bloqueado como neutro: bloqueado ainda é CA sem prova.
- Deixar passar CA ambíguo "porque o Dev entendeu" (a ambiguidade volta no próximo ciclo).
