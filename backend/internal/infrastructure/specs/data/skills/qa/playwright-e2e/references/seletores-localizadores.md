# Seletores acessíveis e localizadores

Localizador = como o teste encontra um elemento. O Playwright usa **locators**: objetos
preguiçosos que reavaliam o DOM a cada ação/asserção (por isso resistem a re-renderização).

## 1. Ordem de preferência

| # | Localizador | Quando | Exemplo |
|---|---|---|---|
| 1 | `getByRole(role, { name })` | Botões, links, campos, títulos, linhas, abas, diálogos | `page.getByRole('button', { name: 'Reservar' })` |
| 2 | `getByLabel(texto)` | Campos de formulário | `page.getByLabel('Data')` |
| 3 | `getByPlaceholder(texto)` | Campo sem rótulo (já é problema de a11y) | `page.getByPlaceholder('Buscar sala')` |
| 4 | `getByText(texto)` | Texto estático, mensagens | `page.getByText('Horário indisponível')` |
| 5 | `getByAltText`, `getByTitle` | Imagens, atributos title | `page.getByAltText('Logo')` |
| 6 | `getByTestId(id)` | Sem alternativa acessível | `page.getByTestId('grade-semana')` |

Nunca: classe CSS (`.btn-primary`), XPath (`//div[3]/span`), índice posicional frágil
(`nth(2)` sem filtro), cadeia profunda de CSS. Quebram a cada redesign e escondem problema de
acessibilidade (se o botão não tem papel/nome acessível, o teste *deveria* reclamar).

**Vantagem dupla:** achar `getByRole('button', {name:'Cancelar'})` prova que o botão tem papel
e nome acessível — o mesmo contrato que o leitor de tela usa.

## 2. `getByRole` em detalhe

Papéis (ARIA) mais usados e o elemento que os gera:

| Role | Elemento típico |
|---|---|
| `button` | `<button>`, `<input type=button/submit>` |
| `link` | `<a href>` |
| `textbox` | `<input type=text/email/...>`, `<textarea>` |
| `checkbox`, `radio`, `combobox`, `switch` | respectivos inputs/selects |
| `heading` | `<h1>`–`<h6>` (opção `level`) |
| `row`, `cell`, `columnheader`, `table` | tabela |
| `listitem`, `list` | `<li>`, `<ul>` |
| `dialog`, `alertdialog` | modal acessível |
| `alert`, `status` | mensagens ao vivo (toasts de erro/sucesso bem feitos) |
| `tab`, `tablist`, `tabpanel` | abas |
| `navigation`, `main`, `banner` | marcos da página |

Opções úteis: `name` (string ou regex; por padrão **substring, sem diferenciar maiúscula**),
`exact: true` (igualdade exata), `level`, `checked`, `pressed`, `selected`, `expanded`.

```ts
await page.getByRole('heading', { name: 'Minhas reservas', level: 1 }).waitFor();
await page.getByRole('button', { name: 'Cancelar', exact: true }).click();
await page.getByRole('combobox', { name: 'Sala' }).selectOption({ label: 'Azul' });
await page.getByRole('checkbox', { name: 'Aceito os termos' }).check();
```

Se `getByRole` não acha: o elemento não expõe papel/nome (bug de a11y, possível ACH Baixo/Médio
conforme RNF) — antes, confirme com o inspetor (`npx playwright codegen <url>` mostra o
localizador sugerido; use `--debug` ou "Pick locator" da UI).

## 3. Restringir o escopo (listas, tabelas, cartões)

Elemento repetido (um botão "Cancelar" por linha): **localize a linha pelo conteúdo único e
procure dentro dela**.

```ts
const linha = page.getByRole('row', { name: /Azul.*10:00/ });
await linha.getByRole('button', { name: 'Cancelar' }).click();
```

Ferramentas de filtro:

```ts
// Item que contém um texto/outro localizador
page.getByRole('listitem').filter({ hasText: 'Sala Azul' });
page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: 'Azul' }) });
page.getByRole('listitem').filter({ hasNotText: 'Cancelada' });

// Combinação
page.getByRole('dialog').getByRole('button', { name: 'Confirmar' });
```

- `first()`, `last()`, `nth(i)`: só quando a ordem **é** o requisito (ex.: ordenação). Para
  provar ordem, leia os textos: `await expect(page.getByRole('row')).toHaveText([...])`.
- Modo estrito: se um locator casa **mais de um** elemento ao agir, o Playwright dá erro. Isso é
  proteção — refine o localizador, não adicione `.first()` por reflexo.

## 4. Dados dinâmicos no texto

- Regex para parte variável: `getByText(/Reserva #\d+ criada/)`.
- Texto dinâmico com dado do teste: interpole o valor criado no setup
  (`getByRole('row', { name: new RegExp(titulo) })`), com título **único por teste**
  (`Reunião ${crypto.randomUUID().slice(0,8)}`).
- Não dependa de data formatada "hoje"; use o valor que o teste inseriu.

## 5. Formulários e componentes customizados

| Componente | Estratégia |
|---|---|
| Campo com rótulo | `getByLabel('Título').fill('...')` |
| `<select>` nativo | `selectOption({ label })` |
| Select/dropdown customizado (div) | clicar no `combobox` pelo nome e depois `getByRole('option', { name })`; se o componente não expõe papéis, é problema de a11y |
| Autocomplete | `fill()` + esperar `getByRole('option', ...)` aparecer, clicar |
| Datepicker | preferir campo de texto/`input type=date` (`fill('2026-12-01')`); se for calendário visual, clicar no dia pelo nome acessível do botão (`getByRole('button', {name: /1 de dezembro/})`) |
| Upload | `setInputFiles` no `input[type=file]` (`getByLabel('Anexo').setInputFiles(...)`) |
| Checkbox/Radio | `check()`/`uncheck()`; assere `toBeChecked()` |
| Modal | `getByRole('dialog', { name: '...' })`; assere visível e que o foco foi para dentro |
| Toast | `getByRole('alert')` ou `getByRole('status')`; toast sem papel é problema de a11y |

## 6. Iframes, Shadow DOM, múltiplas abas

- Iframe: `page.frameLocator('iframe[title="Pagamento"]').getByRole(...)` (título do iframe é
  um bom identificador e requisito de a11y).
- Shadow DOM aberto: os locators de papel/texto **atravessam** shadow DOM aberto
  automaticamente; CSS também. Fechado: pedir `data-testid` ao Dev (ACH Baixo se impede teste).
- Nova aba: `const [nova] = await Promise.all([context.waitForEvent('page'), link.click()])`.

## 7. Quando usar `getByTestId`

Somente quando **não há** papel/rótulo/texto estável: gráfico, canvas, grade complexa, elemento
puramente visual. Regras:
- o id vem de `data-testid` no código do app (acordar com o Dev; configurar `testIdAttribute` se a
  convenção for outra: `use: { testIdAttribute: 'data-test' }`);
- comente no teste **por que** não há alternativa;
- id estável e semântico (`grade-semana`), nunca gerado.

## 8. Depurando um localizador

1. `npx playwright codegen http://localhost:3000` — gera localizadores acessíveis ao clicar.
2. `npx playwright test --ui` ou `--debug` — modo "Pick locator" e inspetor.
3. No trace viewer, abra o passo, veja o snapshot e o localizador resolvido.
4. `await page.pause()` temporário (nunca commitar).
5. Verifique contagem: `await expect(locator).toHaveCount(1)` para provar unicidade.

## 9. Anti-padrões

| Anti-padrão | Problema | Correção |
|---|---|---|
| `page.locator('.card:nth-child(2) button')` | Frágil e opaco | `filter({hasText})` + `getByRole` |
| `page.$('#botao')` / `$$` | API antiga, sem auto-espera | `page.locator`/`getBy*` |
| `.first()` para silenciar o modo estrito | Pode clicar o elemento errado | Refinar escopo |
| Texto do botão copiado de string de i18n do app | Muda com tradução | Fixar `locale` e usar o texto do CT |
| `getByText('Salvar')` para botão | Pega também parágrafo "Salvar para depois" | `getByRole('button', {name:'Salvar'})` |
| `force: true` em `click` | Passa por cima de overlay/estado real | Entender por que não é clicável |
