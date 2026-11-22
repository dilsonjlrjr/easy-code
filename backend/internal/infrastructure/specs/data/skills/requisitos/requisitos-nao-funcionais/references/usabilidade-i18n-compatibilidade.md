# RNF de usabilidade, internacionalização e compatibilidade

Categorias (`categoria`): `usabilidade` (inclui idioma/i18n, fuso, formatos) e `compatibilidade`
(navegadores, dispositivos, resoluções). O **como desenhar** está na skill `ux-acessibilidade`;
aqui está o que o RNF precisa afirmar e como verificar.

## 1. Perguntas-chave

**Usuários e tarefas**
- Quem usa e com que frequência (diário, eventual)? Treinamento disponível?
- Qual a tarefa mais comum e quanto tempo/passos o cliente espera?
- Há usuários de baixa familiaridade digital?

**Idioma e localização**
- Só pt-BR? Haverá outros idiomas no futuro (isso muda se textos ficam fora do código)?
- Fuso horário: único (da empresa) ou por usuário? Horário de verão não existe hoje no Brasil,
  mas **declare o fuso** (ex.: America/Sao_Paulo).
- Formatos de data, hora, número e moeda (dd/mm/aaaa, 24 h, vírgula decimal).

**Dispositivos**
- Desktop, celular ou ambos? Quais navegadores a empresa padroniza?
- Há versão mínima de navegador (padrão corporativo)?
- Há uso em quiosque, tela grande, impressão?

## 2. Usabilidade: requisitos mensuráveis

Usabilidade é difícil de medir sem usuários, mas dá para exigir **condições objetivas**.

| Tema | Métrica | Verificação |
|---|---|---|
| Eficiência da tarefa principal | Reservar sala em no máximo 3 passos a partir da tela inicial (contagem de interações) | Revisão do fluxo no protótipo |
| Tempo de tarefa | Usuário novo conclui a tarefa principal em <= 60 s sem ajuda | Teste com 3 a 5 pessoas (quando disponível); em aplicação pequena, roteiro simulado |
| Prevenção de erro | Ação destrutiva exige confirmação; formulários validam antes do envio | Revisão + teste e2e |
| Mensagens de erro | Toda mensagem diz o que aconteceu e como resolver | Revisão de lista de mensagens |
| Consistência | Mesmo componente/rótulo para a mesma ação em todas as telas | Revisão de guia de estilo |
| Estados | Cada tela tem estado vazio, carregando, erro, sucesso | Revisão do protótipo (ver `ux-acessibilidade`) |
| Feedback | Toda ação mostra resultado em até 1 s ou indicador de progresso | Teste manual/e2e |
| Aprendizagem | Sem manual necessário para a tarefa principal | Teste com usuário novo |

**Medidas padronizadas** (quando houver usuários reais): taxa de conclusão de tarefa, tempo
na tarefa, número de erros, e escala **SUS** (System Usability Scale) com meta (ex.: pontuação
>= 68 é referência de "média"). Use apenas se houver teste com pessoas; em aplicação pequena com agentes,
declare como "não aplicável" e use as condições objetivas.

## 3. Idioma e formatos (i18n/l10n)

| Tema | Requisito | Verificação |
|---|---|---|
| Idioma da interface | pt-BR, incluindo mensagens de erro da API visíveis ao usuário | Revisão + varredura de textos em inglês |
| Atributo de idioma | `lang="pt-BR"` | Varredura |
| Textos fora do código | Textos de interface em arquivo de mensagens/recursos, não espalhados (facilita idioma futuro) | Revisão de código |
| Datas e horas | Exibidas em dd/mm/aaaa e 24 h; armazenadas em UTC ou com fuso explícito; fuso de exibição declarado | Teste automatizado com datas de virada de dia |
| Números e moeda | Vírgula decimal, ponto de milhar, R$ (se aplicável) | Teste de formatação |
| Ordenação/busca | Ordenação alfabética respeita acentuação (`é`, `ç`) | Teste com dados acentuados |
| Codificação | UTF-8 de ponta a ponta; acentos preservados em campos | Teste de ida e volta |
| Nomes de pessoas | Sem restrições artificiais (apóstrofos, acentos, hífen, um só nome) | Teste de validação |

Armadilhas de tempo: **intervalo de reserva é local ao fuso da sala/empresa**; guarde instante
(UTC) e fuso de exibição, ou data+hora local com fuso do local. Sobreposição e "no passado"
dependem do fuso, então o RNF declara qual.

## 4. Compatibilidade

| Tema | Métrica | Verificação |
|---|---|---|
| Navegadores | Funciona nas **duas últimas versões estáveis** dos navegadores listados (ex.: Chrome, Firefox, Edge, Safari) ou na lista que o cliente impor | Teste e2e multi-navegador (ex.: Playwright com Chromium, Firefox e WebKit) |
| Largura de tela | Funciona sem rolagem horizontal de 360 px a 1920 px | Teste em viewports definidos |
| Orientação | Retrato e paisagem sem perda de função (WCAG 1.3.4) | Teste manual |
| Toque | Alvos de toque adequados (ver acessibilidade) | Medição |
| Zoom | Funciona com zoom de 200% | Teste manual |
| Sem script | Conteúdo essencial e mensagens de erro de rede não quebram a página | Revisão |
| Impressão | (se pedido) página de agenda imprime em A4 legível | Teste manual |
| Rede lenta | Utilizável em 4G simulado | Teste com throttling |

Defina lista **fechada** de navegadores: "funciona em todos" não é verificável.

## 5. Exemplos completos

Bom:

```yaml
categoria: compatibilidade
requisito: "o sistema funciona em navegadores atuais, em desktop e celular"
metrica: "nas duas últimas versões estáveis de Chrome, Firefox, Edge e Safari; sem rolagem horizontal em viewports de 360 px a 1920 px; fluxos \"entrar\", \"reservar\" e \"cancelar\" completos em todos"
verificacao:
  tipo: teste_automatizado
  detalhe: "teste e2e executado em Chromium, Firefox e WebKit e em dois viewports (360x740 e 1366x768)"
prioridade: must
origem:
  tipo: boa_pratica
  detalhe: "boa prática (lista de navegadores a confirmar com o cliente)"
```

Bom (usabilidade):

```yaml
categoria: usabilidade
requisito: "a interface é em português do Brasil, com datas em dd/mm/aaaa e horas em 24 h"
metrica: "100% dos textos visíveis (inclusive erros) em pt-BR; 0 ocorrências de formato mm/dd ou de 12 h"
verificacao:
  tipo: teste_automatizado
  detalhe: "revisão da lista de mensagens + teste automatizado de formatação"
prioridade: must
origem:
  tipo: boa_pratica
  detalhe: "boa prática (idioma pt-BR do público; confirmar com o cliente se houver usuários de outros idiomas)"
```

Ruim:

```yaml
requisito: "interface intuitiva e compatível com todos os dispositivos"
metrica: "usuários satisfeitos"
verificacao:
  tipo: teste_automatizado
  detalhe: "pesquisa"
```

Por quê: "intuitiva" e "todos" não são mensuráveis; "satisfeitos" sem escala.

## 6. Heurísticas de especialista

- **Lista fechada** de navegadores e viewports; diga "as duas últimas versões estáveis".
- **Mobile first de verdade**: se o celular é usado para reservar na porta da sala, exija
  funcionamento a 360 px; senão, declare "desktop primário, celular tolerado".
- **Eficiência** (passos, cliques) é o RNF de usabilidade mais fácil de verificar no protótipo.
- **Mensagem de erro** é um RNF transversal subestimado.
- **Fuso e datas** geram bugs sutis: declare e teste a virada de dia.
- **i18n pensando no futuro:** custo baixo de manter textos centralizados; custo alto de
  retrofit.
- **Não confunda responsivo com acessível:** são RNF separados.

## 7. Anti-padrões

- "Intuitivo", "amigável", "moderno".
- "Funciona em todos os navegadores".
- Sem fuso declarado em sistema com horários.
- Formatação de data americana por padrão de biblioteca.
- Textos de erro vindos crus da API ("Internal Server Error", mensagens em inglês).
- Exigir SUS sem usuários para testar.

## 8. Checklist

- [ ] Idioma e formatos (data, hora, número) declarados.
- [ ] Fuso horário declarado; estratégia de armazenamento/exibição coerente.
- [ ] Lista fechada de navegadores e de viewports.
- [ ] Eficiência da tarefa principal em número de passos.
- [ ] Prevenção de erro e mensagens de erro como requisito.
- [ ] Verificação executável (e2e multi-navegador, viewports).
- [ ] Valores de boa prática marcados para aprovação.
