# WCAG 2.2 nível AA, critério por critério, para web app

Referência: **WCAG 2.2** (W3C). O nível AA exige todos os critérios A e AA. Abaixo, os
critérios relevantes para aplicação web interativa, com **o que exige**, **como desenhar** e
**como verificar**. Siglas de verificação: **[A]** automática (axe-core, Lighthouse, pa11y),
**[T]** teclado, **[L]** leitor de tela, **[V]** inspeção visual/medição, **[R]** revisão de
conteúdo.

Notas de versão: o critério 4.1.1 (Parsing) foi removido na 2.2 por ser obsoleto. Os critérios
novos da 2.2 neste nível: 2.4.11, 2.5.7, 2.5.8, 3.2.6, 3.3.7 e 3.3.8 (marcados **novo 2.2**).
Critérios de mídia (1.2.x) valem se houver vídeo/áudio; no web app típico, quase nunca.

## 1. Perceptível

| Critério (nível) | Exige | Como desenhar | Verificar |
|---|---|---|---|
| **1.1.1** Conteúdo não textual (A) | Alternativa textual para imagem, ícone e controle com imagem | Imagem informativa: `alt` descritivo; decorativa: `alt=""`; ícone dentro de botão com texto: `aria-hidden="true"`; botão só com ícone: nome acessível (`aria-label` ou texto oculto visualmente) | [A] alt ausente; [L] nomes fazem sentido |
| **1.3.1** Informações e relações (A) | Estrutura transmitida por semântica | Títulos `h1`-`h6`, listas, tabelas com `<th scope>`, `<label>`, `<fieldset>/<legend>`, regiões (`header`, `nav`, `main`) | [A] [L] navegar por títulos e regiões |
| **1.3.2** Sequência significativa (A) | A ordem do código faz sentido | Ordem do DOM = ordem visual; sem reordenar só por CSS | [T] [L] |
| **1.3.3** Características sensoriais (A) | Instruções não dependem só de forma, cor, posição, som | "Clique em Reservar" e não "clique no botão verde à direita" | [R] |
| **1.3.4** Orientação (AA) | Não restringir a uma orientação | Sem bloqueio retrato/paisagem | [V] rotacionar |
| **1.3.5** Identificar a finalidade da entrada (AA) | Campos de dados do usuário com `autocomplete` correto | `autocomplete="email"`, `name`, `username`, `current-password` | [A] |
| **1.4.1** Uso de cor (A) | Cor não é o único meio | Erro/ocupado/obrigatório com texto ou ícone; links no texto sublinhados | [V] em escala de cinza |
| **1.4.2** Controle de áudio (A) | Áudio automático > 3 s tem controle | Evitar áudio automático | [R] |
| **1.4.3** Contraste mínimo (AA) | Texto >= 4,5:1; texto grande >= 3:1 | Checar cada par de tokens (ver `design-tokens-sistema-visual.md`); texto grande = >= 24 px ou >= 18,66 px em negrito | [A] [V] ferramenta de contraste |
| **1.4.4** Redimensionar texto (AA) | Até 200% sem perda | Unidades relativas (`rem`), contêineres flexíveis, sem altura fixa em texto | [V] zoom de texto 200% |
| **1.4.5** Imagens de texto (AA) | Evitar texto em imagem | Texto real com CSS (exceto logotipo) | [R] |
| **1.4.10** Reflow (AA) | Sem rolagem em duas dimensões em 320 px CSS (equivale a zoom 400% em 1280 px) | Layout fluido; tabelas largas em cartões ou rolagem contida no componente; sem larguras fixas | [V] viewport 320 px |
| **1.4.11** Contraste não textual (AA) | Componentes de UI e gráficos >= 3:1 contra o fundo adjacente | Borda de campo, ícones, estados de foco, controles; não depender do preenchimento claro | [V] ferramenta de contraste |
| **1.4.12** Espaçamento de texto (AA) | Funciona com line-height 1,5, parágrafo 2x, letras 0,12 em, palavras 0,16 em | Sem altura fixa; sem `overflow:hidden` cortando texto | [V] com bookmarklet/extensão de espaçamento |
| **1.4.13** Conteúdo em hover ou foco (AA) | Tooltip/menu que aparece em hover/foco é dispensável, pairável e persistente | Esc fecha; ponteiro pode ir para dentro do tooltip; não some sozinho | [T] [V] |

## 2. Operável

| Critério (nível) | Exige | Como desenhar | Verificar |
|---|---|---|---|
| **2.1.1** Teclado (A) | Toda função por teclado | Elementos nativos (`button`, `a`, `input`); componente custom com teclado completo; sem eventos só de mouse | [T] percorrer todas as telas |
| **2.1.2** Sem armadilha de teclado (A) | Foco sai de qualquer componente | Diálogo: foco preso dentro *enquanto aberto* e Esc sai; resto da página sem armadilha | [T] |
| **2.1.4** Atalhos de tecla (A) | Atalho de tecla única pode ser desativado/remapeado ou só ativa com foco | Evitar atalhos de tecla única; usar Ctrl/Alt ou ativar só no componente | [R] [T] |
| **2.2.1** Tempo ajustável (A) | Limites de tempo podem ser estendidos | Aviso antes de expirar a sessão e opção de estender; mensagens persistem | [R] |
| **2.2.2** Pausar, parar, ocultar (A) | Movimento/atualização automática > 5 s pode ser pausado | Carrosséis e animações com controle; sem auto-atualização que tire o foco | [V] |
| **2.3.1** Três flashes (A) | Nada pisca mais de 3 vezes por segundo | Sem flashes | [R] |
| **2.4.1** Ignorar blocos (A) | Mecanismo para pular repetição | Link "Pular para o conteúdo" como primeiro foco; `<main>` e regiões | [T] [L] |
| **2.4.2** Página com título (A) | `<title>` descritivo | `<title>Nova reserva - Reserva de Salas</title>`, único por tela | [A] |
| **2.4.3** Ordem do foco (A) | Ordem lógica | DOM na ordem de uso; evitar `tabindex` positivo | [T] |
| **2.4.4** Finalidade do link no contexto (A) | O texto do link (ou contexto) diz o destino | "Ver minhas reservas", não "clique aqui"; vários "Cancelar" distinguidos por `aria-label` ou texto oculto ("Cancelar reserva de 10h") | [A] [L] lista de links |
| **2.4.5** Várias formas (AA) | Mais de uma forma de achar páginas | Em app pequeno, menu consistente já atende (conjunto pequeno); em grande, adicionar busca/mapa | [R] |
| **2.4.6** Cabeçalhos e rótulos (AA) | Descrevem tópico/finalidade | Títulos e rótulos claros e específicos | [R] |
| **2.4.7** Foco visível (AA) | Indicador de foco visível | Nunca `outline: none` sem substituto; como boa prática, contorno com contraste >= 3:1 e >= 2 px (a exigência formal de aparência do foco é do 2.4.13, nível AAA) | [T] [V] |
| **2.4.11** Foco não obscurecido (mínimo) (AA) **novo 2.2** | O elemento focado não fica totalmente escondido por conteúdo fixo | Cabeçalho/rodapé fixo com `scroll-padding-top`; banners e barras não cobrem o foco | [T] com cabeçalho fixo |
| **2.5.1** Gestos de ponteiro (A) | Gestos de múltiplos toques/caminho têm alternativa de toque simples | Sem pinça/arraste obrigatório | [R] |
| **2.5.2** Cancelamento do ponteiro (A) | Ação dispara no `up`, não no `down`, ou pode ser cancelada | Usar `click` nativo | [R] |
| **2.5.3** Rótulo no nome (A) | Nome acessível contém o texto visível | `aria-label` não contradiz o texto do botão; comece com o texto visível | [A] [L] |
| **2.5.4** Atuação por movimento (A) | Sem função só por sacudir/inclinar | Sem esses recursos | [R] |
| **2.5.7** Movimentos de arrastar (AA) **novo 2.2** | Toda função de arrastar tem alternativa de clique simples | Reordenar com botões subir/descer; seletor de intervalo com campos | [T] [R] |
| **2.5.8** Tamanho do alvo (mínimo) (AA) **novo 2.2** | Alvo de ponteiro >= 24x24 px CSS, ou espaçamento suficiente (círculo de 24 px sem sobrepor outro alvo) | `min-height`/`min-width` 24 px (recomendado 44 px em mobile); link em texto corrido isento | [V] medição; [A] axe `target-size` |

## 3. Compreensível

| Critério (nível) | Exige | Como desenhar | Verificar |
|---|---|---|---|
| **3.1.1** Idioma da página (A) | `lang` válido | `<html lang="pt-BR">` | [A] |
| **3.1.2** Idioma das partes (AA) | Trechos em outro idioma marcados | `<span lang="en">` quando houver | [R] |
| **3.2.1** Em foco (A) | Receber foco não muda contexto | Sem navegação ou envio ao focar | [T] |
| **3.2.2** Na entrada (A) | Alterar valor não muda contexto sem aviso | Select que não navega sozinho; botão explícito de aplicar | [T] |
| **3.2.3** Navegação consistente (AA) | Navegação repetida na mesma ordem | Mesmo menu, mesma ordem em todas as telas | [V] |
| **3.2.4** Identificação consistente (AA) | Mesma função, mesmo nome/ícone | "Cancelar reserva" sempre igual | [R] |
| **3.2.6** Ajuda consistente (A) **novo 2.2** | Ajuda/contato no mesmo lugar relativo nas telas em que aparece | Rodapé com "Ajuda e contato" em todas | [V] |
| **3.3.1** Identificação do erro (A) | Erro descrito em texto | Mensagem textual, não só cor/ícone | [A] [L] |
| **3.3.2** Rótulos ou instruções (A) | Campos com rótulo/instrução | `<label>`, formato esperado, obrigatoriedade | [A] |
| **3.3.3** Sugestão de erro (AA) | Se o erro tem sugestão conhecida, informá-la | "Escolha uma data a partir de hoje" | [R] |
| **3.3.4** Prevenção de erro (legal, financeiro, dados) (AA) | Reversível, verificado ou confirmado | Confirmação ou desfazer em ações destrutivas e que alteram dados do usuário | [R] [T] |
| **3.3.7** Entrada redundante (A) **novo 2.2** | Não pedir de novo informação já dada no mesmo processo | Preencher com o que já foi informado; não pedir e-mail duas vezes (exceto segurança) | [R] |
| **3.3.8** Autenticação acessível (mínimo) (AA) **novo 2.2** | Sem teste cognitivo (memorizar, decifrar) sem alternativa | Permitir colar senha e gerenciadores; sem CAPTCHA de enigma sem alternativa; OTP aceitando colar | [T] testar com gerenciador/colar |

## 4. Robusto

| Critério (nível) | Exige | Como desenhar | Verificar |
|---|---|---|---|
| **4.1.2** Nome, função, valor (A) | Todo componente de UI expõe nome, papel e estado | Elementos nativos; ARIA completa (`aria-expanded`, `aria-selected`, `aria-checked`) em componentes custom; estados atualizados | [A] [L] |
| **4.1.3** Mensagens de status (AA) | Mudanças de status anunciadas sem mover o foco | `role="status"` (educado) ou `role="alert"` (urgente), região presente no DOM antes da mudança | [L] |

## 5. Mídia (se houver)

1.2.1 a 1.2.5: legendas para vídeo gravado (1.2.2) e ao vivo (1.2.4), alternativa em texto ou
audiodescrição (1.2.3, 1.2.5), transcrição para só áudio (1.2.1). Aplique apenas se o sistema
tiver vídeo/áudio.

## 6. Roteiro de verificação de uma tela (10 passos)

1. **Automática:** axe-core/Lighthouse/pa11y na tela (cada estado relevante). Corrigir tudo.
2. **Teclado:** Tab, Shift+Tab, Enter, Espaço, setas em componentes compostos, Esc em
   diálogos. Ordem lógica? Foco sempre visível? Alguma armadilha?
3. **Foco e cabeçalho fixo:** o foco fica escondido sob barra fixa? (2.4.11)
4. **Leitor de tela (NVDA ou Orca):** navegar por títulos, regiões, formulários; ouvir nomes de
   botões, rótulos de campos, erros e mensagens de status.
5. **Zoom 200% e reflow 320 px:** sem perda nem rolagem horizontal na página.
6. **Espaçamento de texto:** aplicar o ajuste de 1.4.12; nada cortado.
7. **Contraste:** medir textos, bordas de campos, ícones e foco.
8. **Cor:** olhar em escala de cinza; ainda dá para entender?
9. **Alvos:** medir alvos pequenos (>= 24 px).
10. **Conteúdo:** títulos, rótulos, links e mensagens fazem sentido fora de contexto?

Registre o resultado por tela em uma tabela (tela, critério, resultado, evidência, correção).

## 7. Armadilhas frequentes (e correção)

| Armadilha | Critério | Correção |
|---|---|---|
| `outline: none` global | 2.4.7 | Estilo de foco próprio com `:focus-visible` |
| Cabeçalho fixo cobre o campo focado | 2.4.11 | `scroll-padding-top` igual à altura do cabeçalho |
| `<div>` clicável | 2.1.1, 4.1.2 | `<button>` ou `<a>` |
| Ícone-botão sem nome | 4.1.2, 1.1.1 | Texto oculto visualmente ou `aria-label` |
| Placeholder como rótulo | 1.3.1, 3.3.2 | `<label>` visível |
| Cinza claro sobre branco | 1.4.3 | Escurecer token; medir |
| Erro só em vermelho | 1.4.1, 3.3.1 | Texto + ícone |
| Toast de erro que some | 2.2.1, 4.1.3 | Persistir; `role="alert"` |
| Modal sem gerência de foco | 2.4.3, 2.1.2 | `<dialog>` modal ou foco preso e retorno |
| Menu só em hover | 2.1.1, 1.4.13 | Abrir por clique/teclado; Esc fecha |
| Colar bloqueado na senha | 3.3.8 | Permitir colar |
| Tabela sem cabeçalhos associados | 1.3.1 | `<th scope="col">` ou `scope="row"`, `<caption>` |
| Link "clique aqui" | 2.4.4 | Texto descritivo |
| Texto cortado ao ampliar | 1.4.4, 1.4.12 | Remover alturas fixas |
| Alvo de 16 px | 2.5.8 | `min-width/min-height: 24px`, espaçamento |

## 8. O que a automação não pega

Qualidade do texto alternativo, ordem de foco sensata, nomes úteis, clareza de erros e rótulos,
uso correto de ARIA (válido não é o mesmo que correto), operabilidade de componentes
customizados, conteúdo em hover/foco, foco obscurecido, arrastar sem alternativa, autenticação.
Por isso o gate exige também o roteiro manual.

## 9. Heurísticas de especialista

- **HTML nativo primeiro:** cobre dezenas de critérios de graça.
- **Primeira regra do ARIA:** se um elemento nativo faz o trabalho, use-o.
- **Um erro de ARIA é pior que ausência de ARIA** (promete comportamento que não existe).
- **Teste com teclado antes de qualquer outra coisa:** revela 80% dos problemas estruturais.
- **Contraste nasce nos tokens:** corrigir no token corrige o sistema todo.
- **Verifique os estados**, não só a tela padrão (erro, foco, desabilitado, hover).
- **Acessibilidade vale no protótipo e no produto:** o Dev preserva o que o Design entregou.

## 10. Checklist rápido por critério (para o `mapa` de acessibilidade)

- [ ] 1.1.1 alternativas textuais  - [ ] 1.3.1 semântica  - [ ] 1.3.2 ordem
- [ ] 1.3.4 orientação  - [ ] 1.3.5 autocomplete  - [ ] 1.4.1 cor  - [ ] 1.4.3 contraste
- [ ] 1.4.4 texto 200%  - [ ] 1.4.10 reflow  - [ ] 1.4.11 contraste de componentes
- [ ] 1.4.12 espaçamento  - [ ] 1.4.13 hover/foco
- [ ] 2.1.1 teclado  - [ ] 2.1.2 sem armadilha  - [ ] 2.1.4 atalhos  - [ ] 2.2.1 tempo
- [ ] 2.4.1 pular bloco  - [ ] 2.4.2 título  - [ ] 2.4.3 ordem de foco  - [ ] 2.4.4 links
- [ ] 2.4.6 títulos e rótulos  - [ ] 2.4.7 foco visível  - [ ] 2.4.11 foco não obscurecido
- [ ] 2.5.3 rótulo no nome  - [ ] 2.5.7 arrastar  - [ ] 2.5.8 alvo 24 px
- [ ] 3.1.1 idioma  - [ ] 3.2.3/3.2.4 consistência  - [ ] 3.2.6 ajuda consistente
- [ ] 3.3.1 a 3.3.4 erros e prevenção  - [ ] 3.3.7 entrada redundante  - [ ] 3.3.8 autenticação
- [ ] 4.1.2 nome/papel/valor  - [ ] 4.1.3 mensagens de status
