# Técnicas de derivação de cenários

Cada técnica responde a um tipo de regra. Escolha pela forma da regra, não por hábito. Todo
exemplo termina em CT no formato do esquema (`cobre`, `nivel`, `tipo_caso`).

## 1. Partição de equivalência

**Ideia:** dividir as entradas em classes que o sistema trata do mesmo jeito; testar um
representante por classe, válida e inválida. Testar dois valores da mesma classe não acha bug novo.

**Passo a passo**

1. Liste cada entrada do CA (campo, parâmetro, estado do sistema, papel do usuário).
2. Para cada uma, parta em classes **válidas** e **inválidas** com base na regra, não na tela.
3. Escolha um representante por classe, com valor literal.
4. Combine com cuidado: **um erro por vez** em casos negativos (senão não se sabe qual regra recusou); classes válidas podem ser combinadas.

**Exemplo — título de reserva (1 a 80 caracteres, obrigatório)**

| Classe | Tipo | Representante | Esperado |
|---|---|---|---|
| 1 a 80 caracteres | válida | "Reunião de equipe" | aceita |
| vazio | inválida | "" | 400, campo `titulo` apontado |
| só espaços | inválida | "   " | 400 (trim antes de validar) |
| > 80 caracteres | inválida | 81 letras | 400 |
| com acentos e emoji | válida (especial) | "Reunião café ☕" | aceita, devolve igual |
| com marcação | válida, porém perigosa | `<b>x</b>` | aceita e exibe como texto, não como HTML |

**Heurísticas**
- Pergunte "e se vier nulo? vazio? só espaço? tipo errado? muito grande?" para cada campo.
- Enum: um valor válido de cada membro **que muda comportamento**, mais um valor fora do enum.
- Classe com comportamento diferente por papel vira tabela de decisão (seção 3).

## 2. Análise de valor limite

**Ideia:** erros se concentram nas bordas. Para cada limite, teste **o valor exato, o
imediatamente abaixo e o imediatamente acima** (3 pontos; para limite aberto/fechado, deixe
explícito qual lado inclui).

**Exemplo — conflito de reserva (intervalos meio-abertos [início, fim))**

Reserva existente: Azul, 10:00–11:00.

| Pedido | Relação | Esperado | Por quê |
|---|---|---|---|
| 11:00–12:00 | encostado no fim | aceita (201) | fim exclusivo |
| 09:00–10:00 | encostado no início | aceita (201) | início inclusivo, fim exclusivo |
| 10:59–12:00 | sobrepõe 1 min | recusa (409) | limite -1 |
| 09:00–10:01 | sobrepõe 1 min | recusa (409) | limite +1 |
| 10:00–11:00 | idêntico | recusa (409) | igualdade |
| 10:15–10:45 | contido | recusa (409) | contenção |
| 09:00–12:00 | contém | recusa (409) | contenção inversa (esquecido com frequência) |
| 10:00–10:00 | duração zero | 400 | intervalo inválido |
| 11:00–10:00 | fim antes do início | 400 | intervalo invertido |

**Limites comuns a procurar:** tamanho de texto, quantidade mínima/máxima, página 0/1/última/
além da última, `limit` máximo da API, valor monetário 0,00/0,01, idade/prazo (exato no dia),
tamanho de upload (no limite e 1 byte acima), antecedência mínima/máxima, horário comercial.

**Armadilha:** o CA diz "até 2 horas". Duração de 2h00 é válida? O CA não diz -> ACH
`etapa_destino: requisitos` (`requisito_ambiguo`), não escolha.

## 3. Tabela de decisão

**Ideia:** quando o resultado depende da **combinação** de condições. Enumere as condições,
liste as combinações e o resultado de cada; cubra cada **regra** (coluna) que muda o resultado.

**Passo a passo**

1. Liste as condições (booleanas ou poucas classes) e as ações/resultados possíveis.
2. Enumere as combinações (2^n; reduza com "tanto faz" quando uma condição não importa).
3. Detecte regras impossíveis (não existe a combinação) e contraditórias (CA ambíguo -> ACH).
4. Um CT por regra efetiva.

**Exemplo — cancelar reserva** (condições: papel, é dono, estado, antecedência >= 1h)

| # | Papel | Dono | Estado | Antecedência >= 1h | Resultado |
|---|---|---|---|---|---|
| R1 | funcionário | sim | ativa | sim | cancela (204) |
| R2 | funcionário | sim | ativa | não | recusa (409, "fora do prazo") |
| R3 | funcionário | não | ativa | tanto faz | 403 |
| R4 | admin | não | ativa | tanto faz | cancela (204) |
| R5 | qualquer | sim | cancelada | tanto faz | 409 (já cancelada) |
| R6 | anônimo | tanto faz | tanto faz | tanto faz | 401 |

6 regras -> 6 CT, em vez de 2^4 x 3 papéis = 48 combinações brutas. A regra R3 vira CT de
segurança (ver `autorizacao-seguranca-funcional.md`).

## 4. Transição de estado

**Ideia:** para entidades com ciclo de vida, teste **cada transição válida** e **cada
transição inválida** (ação em estado que não a permite).

**Exemplo — reserva:** estados `ativa`, `cancelada`, `concluída` (passou do horário).

| De \ Ação | cancelar | editar | confirmar presença |
|---|---|---|---|
| ativa | -> cancelada | permanece ativa | permanece ativa |
| cancelada | inválida (409) | inválida (409) | inválida (409) |
| concluída | inválida (409) | inválida (409) | inválida (409) |

Cobertura mínima: todas as transições válidas (0-switch) + todas as inválidas da matriz. Se
há sequência relevante (cancelar -> recriar mesmo horário deve funcionar), acrescente o caminho
de 2 passos (1-switch): é ali que aparecem restrições de unicidade esquecidas.

**Heurística:** desenhe o diagrama de estados mesmo que o requisito não traga; célula vazia na
matriz é pergunta para Requisitos.

## 5. Pairwise (combinação de pares)

**Ideia:** a maioria dos defeitos de combinação aparece com **2 parâmetros**. Cobrir todos os
pares reduz muito o número de casos. Use para parâmetros **independentes** (filtros de busca,
navegador x viewport x papel), nunca para regras de negócio encadeadas (use decisão).

**Exemplo — filtros da listagem de salas:** capacidade (qualquer, 4, 10), recurso (nenhum,
projetor, TV), andar (todos, 1, 2). Total bruto 27; pairwise cobre todos os pares em ~9:

| # | Capacidade | Recurso | Andar |
|---|---|---|---|
| 1 | qualquer | nenhum | todos |
| 2 | qualquer | projetor | 1 |
| 3 | qualquer | TV | 2 |
| 4 | 4 | nenhum | 1 |
| 5 | 4 | projetor | 2 |
| 6 | 4 | TV | todos |
| 7 | 10 | nenhum | 2 |
| 8 | 10 | projetor | todos |
| 9 | 10 | TV | 1 |

Geração: ferramenta open source como `allpairspy` (Python) ou PICT (Microsoft, MIT); comando
exato: conferir em `agentes/ambiente.md`. À mão, o quadrado latino acima basta para 3x3x3.
Sempre adicione os **casos de borda por fora** (combinação sem resultado, filtro vazio).

## 6. Testes baseados em risco

**Ideia:** com tempo finito, a profundidade segue o risco = probabilidade de falha x impacto.

| Fator | Aumenta a profundidade quando |
|---|---|
| Impacto | dinheiro, dado pessoal, permissão, perda de dados, irreversível |
| Probabilidade | código novo, regra complexa, integração externa, concorrência, histórico de bug |
| Detectabilidade | falha silenciosa (dado errado sem erro), difícil de ver em produção |
| Frequência de uso | fluxo principal usado por todos |

**Procedimento:** pontue cada US/regra de 1 a 3 em impacto e em probabilidade; produto >= 6 =
profundidade total (limite, negativo, segurança, concorrência, e2e do fluxo); 3-4 = positivo +
negativo + limite; <= 2 = positivo. Registre a pontuação no corpo do CT (nota; o esquema não tem campo para isso) para
o gate entender por que a US X tem 9 CT e a Y tem 2.

## 7. Adivinhação de erros (checklist de especialista)

Depois das técnicas formais, 10 minutos de "o que o desenvolvedor esqueceu":

- entrada: nulo, vazio, espaço, unicode, emoji, aspas, `%`, `_`, `\`, sequência muito longa;
- listas: vazia, 1 item, página exatamente cheia, ordenação com empate, filtro sem resultado;
- operações: repetir (duplo clique, reenvio), desfazer, voltar do navegador, F5 após POST;
- estado: sessão expirada no meio do fluxo, dois navegadores, dado removido por outro usuário;
- API: campo extra no corpo, campo faltando, tipo errado, corpo vazio, `Content-Type` errado, id inexistente (404) vs. id de outro usuário (403/404).

## 8. Exemplo completo — do CA ao conjunto de CT

**US-03 / CA-2:** "Dado que a sala está reservada das 10h às 11h, quando outro funcionário
tenta reservar das 10h30 às 11h30, então o sistema recusa e informa que o horário está ocupado."

Regra extraída: **dois intervalos meio-abertos na mesma sala conflitam se `iniA < fimB` e
`iniB < fimA`**. Técnicas: limite (seção 2) + decisão por sala (salas diferentes não conflitam).

| CT | Cenário | Nível | Tipo |
|---|---|---|---|
| CT-08 | Sobreposição parcial (10h30–11h30) é recusada com 409 e não cria reserva | integração | negativo |
| CT-09 | Reserva encostada (11h00–12h00) é aceita | integração | limite |
| CT-10 | Sobreposição de 1 minuto (10h59–12h00) é recusada | integração | limite |
| CT-11 | Intervalo que contém a reserva existente é recusado | integração | negativo |
| CT-12 | Mesmo horário em outra sala é aceito | integração | positivo |
| CT-13 | Função de interseção de intervalos: tabela de 9 casos | unitário | limite |
| CT-14 | Mensagem de horário ocupado aparece na tela e o formulário mantém os dados | e2e | negativo |

Repare: a regra pura (CT-13) vai para unitário, o contrato e o banco (CT-08..12) para
integração, e **só um** CT de e2e para a experiência de erro. Conflito simultâneo vai para
`concorrencia-tempo-dados.md`.

**Mau CT correspondente (e por quê):**

```yaml
titulo: Testar reserva   # CT-08
passos: [Reservar uma sala, Reservar de novo]
resultado_esperado: [dá erro]
```
Sem dados, sem vínculo, "erro" não verificável, não diz que nada foi criado.

## 9. Anti-padrões de derivação

- Gerar casos pela tela em vez da regra (testa o layout, não o negócio).
- Um caso negativo com dois erros ao mesmo tempo.
- Esquecer o limite inclusivo/exclusivo e deixar de perguntar a Requisitos.
- Pairwise em regra de negócio encadeada.
- Dezenas de casos equivalentes "para garantir" (custo de manutenção sem informação nova).
