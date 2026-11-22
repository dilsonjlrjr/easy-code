# Design tokens e sistema visual mínimo

Tokens são **decisões de design com nome**: cor, tipografia, espaçamento etc. guardadas em
variáveis. Para o protótipo, viram variáveis CSS em `estilo.css`; para o Dev (Vue), viram a
mesma fonte de verdade. O `estilo.md` documenta; o `estilo.css` implementa. Os dois devem
bater.

## 1. Princípios

1. **Semântico, não literal:** `--cor-erro`, não `--vermelho-500` nos componentes. A paleta
   bruta existe uma vez; o uso por papel é que vai nos componentes.
2. **Pouca escolha:** uma escala curta para cada dimensão; sem valor solto.
3. **Contraste verificado na definição:** cada par texto/fundo é medido antes de virar token.
4. **Estados são tokens:** foco, hover, desabilitado, erro, sucesso fazem parte do sistema.
5. **Mínimo viável:** o sistema cabe em uma tela de documentação; cresce só com necessidade.

## 2. Estrutura em camadas

| Camada | Exemplo | Quem usa |
|---|---|---|
| Primitivos (paleta bruta) | `--azul-700: #1d4ed8` | Só a definição dos tokens semânticos |
| Semânticos (papel) | `--cor-primaria`, `--cor-texto`, `--cor-erro` | Componentes e páginas |
| Componentes | `--botao-raio`, `--campo-borda` | Dentro do componente |

Em aplicação pequena, primitivos + semânticos bastam.

## 3. Cor

Defina por papel e **registre a razão de contraste** de cada par de uso.

| Token | Papel | Cuidados |
|---|---|---|
| `--cor-fundo` | Fundo da página | Neutro claro |
| `--cor-superficie` | Cartões, painéis | Distinguível do fundo por borda se o contraste for baixo |
| `--cor-texto` | Texto principal | Contraste >= 4,5:1 (alvo 7:1) sobre fundo e superfície |
| `--cor-texto-suave` | Texto secundário | >= 4,5:1; nunca cinza claro "decorativo" |
| `--cor-primaria` | Ação principal, links | Texto do botão sobre ela >= 4,5:1 |
| `--cor-primaria-hover` | Hover | Mantém contraste |
| `--cor-borda` | Bordas de campo | >= 3:1 contra o fundo (1.4.11) |
| `--cor-foco` | Indicador de foco | >= 3:1 contra todos os fundos onde aparece |
| `--cor-erro` | Erro | Texto >= 4,5:1; sempre com ícone e texto |
| `--cor-sucesso` | Sucesso | Idem |
| `--cor-aviso` | Atenção | Idem; amarelo puro sobre branco falha |
| `--cor-info` | Informação | Idem |
| `--cor-desabilitado` | Controles desabilitados | Isentos de contraste, mas legíveis o bastante para entender |

### Tabela de contraste (documente em `estilo.md`)

| Par | Frente | Fundo | Razão | Requisito | Ok? |
|---|---|---|---|---|---|
| Texto principal | `#1f2937` | `#ffffff` | ~14:1 | 4,5:1 | sim |
| Botão primário (texto) | `#ffffff` | `#1d4ed8` | ~6,7:1 | 4,5:1 | sim |
| Borda de campo | `#6b7280` | `#ffffff` | ~4,8:1 | 3:1 | sim |
| Erro (texto) | `#b91c1c` | `#ffffff` | ~6,5:1 | 4,5:1 | sim |

Os números acima são ilustrativos: **calcule com ferramenta** antes de adotar (as razões
dependem dos valores exatos).

Regras: texto sobre cor de marca deve ser testado; modo escuro (se houver) é outro conjunto de
pares a medir; não use cor como único sinal (acompanhar com ícone/texto).

### Como calcular

Razão de contraste = (L1 + 0,05) / (L2 + 0,05), com L a luminância relativa (WCAG). Use uma
ferramenta open source ou o script do projeto; registre o resultado na tabela.

## 4. Tipografia

| Token | Valor sugerido | Observação |
|---|---|---|
| `--fonte-base` | pilha de sistema: `system-ui, -apple-system, "Segoe UI", Roboto, sans-serif` | Sem download, boa legibilidade |
| `--fonte-mono` | `ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace` | Códigos, ids |
| `--texto-base` | `1rem` (16 px) | Nunca menor para corpo |
| Escala | `--texto-sm: 0.875rem`, `--texto-md: 1rem`, `--texto-lg: 1.25rem`, `--texto-xl: 1.5rem`, `--texto-2xl: 2rem` | Razão simples |
| `--peso-normal` / `--peso-forte` | 400 / 600 | Duas bastam |
| `--altura-linha` | 1.5 (corpo), 1.25 (títulos) | Compatível com 1.4.12 |
| Largura de linha | até ~70 caracteres (`max-width: 70ch`) | Leitura confortável |

Unidades em `rem` para respeitar o tamanho de fonte do usuário (1.4.4). Títulos em escala e na
ordem semântica (o nível do `<h>` não é escolhido pelo tamanho; o tamanho vem do CSS).

## 5. Espaçamento e layout

Escala base de 4 px ou 8 px:

| Token | Valor | Uso típico |
|---|---|---|
| `--espaco-1` | 0.25rem | Ajustes finos |
| `--espaco-2` | 0.5rem | Entre ícone e texto |
| `--espaco-3` | 0.75rem | Padding de campo |
| `--espaco-4` | 1rem | Padrão entre elementos |
| `--espaco-6` | 1.5rem | Entre grupos |
| `--espaco-8` | 2rem | Entre seções |
| `--espaco-12` | 3rem | Blocos grandes |

Outros: `--raio` (4 a 8 px), `--sombra` (um nível discreto), `--largura-conteudo` (ex.: 72rem),
breakpoints documentados (ex.: 40rem e 64rem) e **alvo mínimo** `--alvo-min: 2.75rem` (44 px)
para toque, nunca abaixo de 24 px (2.5.8).

## 6. Foco, estados e movimento

```css
:root {
  --cor-foco: #1d4ed8;
  --foco-largura: 3px;
  --foco-offset: 2px;
}
:focus-visible {
  outline: var(--foco-largura) solid var(--cor-foco);
  outline-offset: var(--foco-offset);
}
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { animation-duration: 0.01ms !important; transition-duration: 0.01ms !important; }
}
```

Regras: foco com contraste suficiente contra o fundo e contra o componente; **nunca**
`outline: none` sem substituto; hover não é único sinal; desabilitado distinto e
explicado quando relevante; animações curtas e dispensáveis.

## 7. Componentes mínimos (para `estilo.md`)

Documente cada um com **propósito, variantes, estados, acessibilidade** e o **HTML de
referência** (copiável).

| Componente | Variantes | Estados | Acessibilidade chave |
|---|---|---|---|
| Botão | primário, secundário, perigoso | padrão, hover, foco, ativo, desabilitado, carregando | `<button>`; um primário por tela; nome claro |
| Link | padrão | padrão, hover, foco, visitado | `<a href>`; sublinhado no texto |
| Campo de texto | texto, e-mail, data, hora | padrão, foco, erro, desabilitado | Rótulo, dica, erro ligado |
| Select | nativo | idem | `<select>` nativo |
| Rádio/checkbox | grupo | idem | `<fieldset>/<legend>` |
| Mensagem | sucesso, erro, aviso, info | - | Ícone + texto; `role` conforme urgência |
| Tabela/lista | dados | vazio, carregando | `<caption>`, `<th scope>` |
| Cartão | informativo | - | Título com nível correto |
| Diálogo | confirmação | aberto | Foco preso, Esc, título e descrição |
| Navegação | principal | item atual | `<nav>`, `aria-current="page"` |
| Indicador de carregamento | texto + animação | - | Região de status |

## 8. Exemplo de `estilo.css` mínimo

```css
:root {
  /* primitivos */
  --azul-700: #1d4ed8; --cinza-900: #1f2937; --cinza-600: #4b5563; --cinza-500: #6b7280;
  --branco: #ffffff; --vermelho-700: #b91c1c; --verde-700: #15803d; --ambar-800: #92400e;

  /* semânticos */
  --cor-fundo: #f9fafb; --cor-superficie: var(--branco);
  --cor-texto: var(--cinza-900); --cor-texto-suave: var(--cinza-600);
  --cor-primaria: var(--azul-700); --cor-borda: var(--cinza-500); --cor-foco: var(--azul-700);
  --cor-erro: var(--vermelho-700); --cor-sucesso: var(--verde-700); --cor-aviso: var(--ambar-800);

  /* tipografia */
  --fonte-base: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  --texto-md: 1rem; --texto-lg: 1.25rem; --texto-xl: 1.5rem; --altura-linha: 1.5;

  /* espaço e forma */
  --espaco-2: .5rem; --espaco-3: .75rem; --espaco-4: 1rem; --espaco-6: 1.5rem; --espaco-8: 2rem;
  --raio: .375rem; --alvo-min: 2.75rem;
}
body { font-family: var(--fonte-base); font-size: var(--texto-md); line-height: var(--altura-linha);
       color: var(--cor-texto); background: var(--cor-fundo); margin: 0; }
```

Valores de cor acima são pontos de partida: **meça o contraste** antes de aprovar.

## 9. Documentação em `estilo.md`

Seções: princípios; tokens (tabela nome, valor, uso); tabela de contraste; escala tipográfica;
espaçamento; componentes (propósito, variantes, estados, HTML); regras de uso ("um botão
primário por tela"); lista de componentes reaproveitados por tela. Os nomes de tokens em
`estilo.md` são **idênticos** aos de `estilo.css`.

## 10. Heurísticas de especialista

- **Defina os pares de contraste primeiro**, a identidade visual depois.
- **Menos cores:** uma de marca, neutros, e quatro de status.
- **Reutilize:** cada componente novo tem custo (acessibilidade, teste, manutenção).
- **Nomeie por função**, para a marca poder mudar sem refazer componentes.
- **Tema escuro** só se pedido; dobra a verificação de contraste.
- **`rem` e relativos** para respeitar preferências do usuário.
- **Documente o HTML do componente**, não só a aparência: a semântica faz parte do design.

## 11. Anti-padrões

- Cores "bonitas" sem medir contraste.
- Tokens literais (`--azul`) usados diretamente em muitos lugares.
- Várias escalas de espaçamento misturadas.
- `outline: none`.
- Fontes de texto em `px` fixo.
- Componente documentado só por imagem.
- Três botões "primários" na mesma tela.

## 12. Checklist

- [ ] Tokens de cor, tipografia, espaçamento, foco, forma e alvo definidos.
- [ ] Nomes semânticos; `estilo.md` e `estilo.css` coincidem.
- [ ] Tabela de contraste com razões calculadas por ferramenta; todas passam.
- [ ] Foco visível definido globalmente; `prefers-reduced-motion` respeitado.
- [ ] Componentes mínimos documentados com estados e HTML de referência.
- [ ] Cor nunca como único sinal.
