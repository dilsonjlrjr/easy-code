# Elicitação e roteiro de perguntas ao cliente

Objetivo: sair da ideia bruta com atores, objetivos, regras e lacunas mapeados, e fazer **uma
única interação** de perguntas de alto valor. O agente de Requisitos normalmente não tem conversa
contínua com o cliente: cada pergunta mal escolhida custa uma interação inteira.

## 1. Leitura analítica da ideia

Leia o texto de entrada três vezes, com um propósito por leitura.

| Leitura | O que marcar | Produto |
|---|---|---|
| 1. Substantivos | Entidades do domínio (sala, reserva, funcionário) e atributos citados (nome, capacidade) | Rascunho do glossário |
| 2. Verbos | Ações que alguém executa (reservar, cancelar, cadastrar, ver) | Candidatas a US |
| 3. Restrições | "não pode", "só", "sempre", "nunca", "cada", "no máximo", "apenas" | Regras de negócio e CA negativos |

Depois classifique cada item em três colunas:

- **Explícito:** está escrito. Vira US/regra com Origem literal.
- **Implícito:** qualquer pessoa assumiria, mas não está escrito (ex.: cancelar reserva já
  passada). Decida e registre como suposição, ou pergunte se mudar escopo.
- **Ausente:** necessário para escrever CA e não dá para inferir. Vira pergunta.

## 2. Atores e objetivos

Para cada papel citado ou sugerido:

1. Nome do papel (use o termo do cliente: "Funcionário", "Administrador").
2. Objetivos: o que ele quer **alcançar** (não o que clica).
3. Poder: o que só ele pode fazer, o que ele não pode.
4. Papéis ocultos que a ideia não nomeia mas o sistema exige: visitante não autenticado,
   sistema/rotina automática (e-mail, expiração), operador de suporte.

Teste do papel: se dois atores têm o mesmo conjunto de poderes, é um ator só. Se um tem poder
extra, é papel distinto (Administrador estende Funcionário? pergunte, não assuma).

## 3. Técnicas de elicitação úteis sem conversa contínua

| Técnica | Como aplicar | Quando rende |
|---|---|---|
| Ciclo de vida da entidade | Para cada entidade: criar, ver, alterar, remover, expirar. Quais existem na ideia? | Descobre US ausentes (ninguém falou em editar sala) |
| Cenário ponta a ponta | Narre um dia de cada ator do início ao fim | Descobre passos de transição (login, confirmação) |
| Caso de abuso | "Como alguém burlaria a regra X?" | Gera CA negativos e RNF de segurança |
| Pré-mortem | "Daqui a 3 meses o sistema foi um fracasso: por quê?" | Expõe riscos e métricas de sucesso |
| Lista de "e se" | e se vazio, e se duplicado, e se dois ao mesmo tempo, e se o ator some, e se for no passado | Limites e concorrência |
| Perguntar "para quê" até 2 níveis | Esclarece o benefício real do "Para" | US que o cliente descreveu como solução |
| Análise de fluxo de dados | De onde vem cada dado exibido? Quem o cria? | Descobre cadastros e integrações escondidos |

## 4. Quais perguntas fazer (e quais não)

**Faça** pergunta se a resposta muda ao menos um destes:

- o escopo (entra/não entra uma US);
- uma regra de negócio (limite, exceção, quem pode);
- a prioridade de algo caro;
- um risco legal/privacidade (dado pessoal, retenção);
- uma integração externa.

**Não faça** pergunta sobre: aparência, nomes de campo, tecnologia, layout, textos de botão,
qualquer coisa que Design ou Arquitetura decide, nem o que o bom senso resolve (padrão de
mercado).

Teste final por pergunta: "se o cliente responder A ou B, eu escrevo US/CA diferentes?". Se
não, cancele a pergunta e registre suposição.

## 5. Formato do bloco de perguntas

Uma mensagem só, numerada, agrupada por tema, cada pergunta com **sugestão de resposta** e a
consequência de cada caminho quando for relevante. Assim o cliente só confirma ou corrige.

```markdown
### Regras de reserva
1. Uma reserva pode atravessar a meia-noite ou durar mais de um dia?
   Sugestão: não; reserva dentro de um único dia. (Se sim, o cálculo de conflito e a tela de
   agenda mudam.)
2. Existe duração mínima/máxima?
   Sugestão: mínimo 30 min, máximo 8 h, em blocos de 30 min.
3. Funcionário pode reservar no passado?
   Sugestão: não.

### Papéis
4. Administrador também faz reservas como funcionário?
   Sugestão: sim.

### Dados e privacidade
5. Quem pode ver quem reservou uma sala em determinado horário?
   Sugestão: todo funcionário vê o nome de quem reservou (necessário para combinar trocas).
```

### Ruim x bom

| Ruim | Por quê | Bom |
|---|---|---|
| "Como você quer que a tela de reservas seja?" | Design; aberta demais | (nenhuma; Design decide) |
| "Quais são os requisitos de segurança?" | Cliente leigo não sabe responder | "Os dados de reserva podem ser vistos por qualquer funcionário? Sugestão: sim, só leitura" |
| "Precisa de relatórios?" | Convida escopo infinito | "Alguém precisa saber taxa de uso das salas? Se sim, para decidir o quê?" |
| "O sistema vai ser usado por muita gente?" | Sem número, não vira RNF | "Quantos funcionários e quantas salas? Sugestão: 200 e 15" |

## 6. Roteiro de perguntas por tema (banco para qualquer aplicação web)

Escolha só os blocos que a ideia deixa em aberto.

**Atores e acesso**
- Quem entra no sistema? Existe auto-cadastro ou só contas criadas por alguém?
- Quantos papéis? O que cada um não pode fazer?
- Há usuário externo (cliente, fornecedor, convidado)?
- Como o usuário prova quem é (login próprio, SSO da empresa)?

**Entidades e regras**
- Quais dados identificam cada entidade? Algum é único?
- Quais estados a entidade percorre (rascunho, ativo, cancelado)? Quem muda cada estado?
- Pode editar depois de criada? Até quando?
- Remover é apagar ou desativar (histórico importa)?
- Existe limite (quantidade, tamanho, período, valor)?
- Pode haver duplicidade? O que é "igual"?

**Tempo**
- Datas no passado: permitidas? Fuso horário único?
- Há prazos, expiração, antecedência mínima/máxima?
- Dois usuários ao mesmo tempo na mesma coisa: o que ganha?

**Notificação e integração**
- Alguém precisa ser avisado (e-mail, tela)? Quando?
- Integra com algo existente (agenda corporativa, diretório de usuários, ERP)?
- Importa ou exporta dados?

**Dados pessoais e histórico**
- Que dados de pessoas o sistema guarda? Para quê cada um?
- Por quanto tempo? Pode pedir exclusão?
- Precisa de trilha de auditoria (quem fez o quê)?

**Escala e contexto de uso**
- Quantos usuários, quantos registros em um ano?
- Horário de uso (comercial, 24×7)? Quanto tempo fora do ar é tolerável?
- Celular, desktop ou ambos?

**Sucesso e escopo**
- Como saberemos que deu certo (métrica)?
- O que **explicitamente não** entra agora?

## 7. Registrando o que decidiu sozinho

Cada suposição vai para `suposicoes` do BACKLOG (`{id: S-n, descricao, motivo}`; o `motivo`
inclui o impacto se estiver errada). Se o cliente deve confirmar, vira também pergunta aberta na
US afetada. Exemplo (tabela de apoio no corpo do backlog):

| # | Suposição | Impacto se errada | Confirmar? |
|---|---|---|---|
| S1 | Reserva só dentro de um dia | Muda conflito e agenda | Sim |
| S2 | Fuso horário único (da empresa) | Baixo | Não |

## 8. Anti-padrões de elicitação

- **Perguntar tudo** (30 perguntas): o cliente responde mal ou não responde. Limite prático:
  de 5 a 12 perguntas por bloco.
- **Pergunta sem sugestão**: obriga o cliente a projetar a solução.
- **Perguntas em cascata** que dependem uma da outra: reorganize para que cada uma seja
  respondível sozinha, ou use "se sim... se não...".
- **Assumir em silêncio** uma regra que muda custo: deve ser pergunta, não suposição.
- **Escrever US antes de ter os atores fechados.**
- **Confundir pedido com necessidade**: "preciso de um botão exportar" pode ser "preciso mostrar
  a ocupação ao meu chefe". Pergunte o "para quê".

## 9. Perguntas-chave (resumo para o agente)

1. Quem são todos os atores, inclusive os não nomeados?
2. Qual o objetivo de cada ator, em uma frase?
3. Que regra, se violada, torna o sistema inútil?
4. O que acontece quando a regra é violada (mensagem? bloqueio?)?
5. Que dado pessoal entra e por quê?
6. O que fica explicitamente fora?
7. Qual decisão minha, se errada, custa mais caro? (Essa vira pergunta, não suposição.)
