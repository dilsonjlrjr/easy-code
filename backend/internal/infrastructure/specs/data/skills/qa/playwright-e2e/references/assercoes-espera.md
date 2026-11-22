# Asserções e espera automática

O Playwright tem dois mecanismos de espera que eliminam quase todo `sleep`:

1. **Auto-espera nas ações** (`click`, `fill`...): antes de agir, aguarda o elemento estar
   anexado, visível, estável (sem animação), habilitado e receber eventos.
2. **Asserções web-first** (`await expect(locator)...`): repetem a verificação até passar ou
   até `expect.timeout` (padrão 5 s) estourar.

## 1. Regra de ouro

Esperar pelo **estado observável** que o CT promete, nunca por tempo. Se você sente
necessidade de `waitForTimeout`, falta identificar o sinal de pronto (um texto, um papel, um
resultado de rede, uma contagem).

## 2. Asserções web-first (com espera)

| Asserção | Verifica |
|---|---|
| `toBeVisible()` / `toBeHidden()` | presença visível / ausência |
| `toBeEnabled()` / `toBeDisabled()` | habilitado |
| `toHaveText(texto \| regex \| [lista])` | texto exato (ou lista na ordem) |
| `toContainText(...)` | contém |
| `toHaveValue(...)` | valor de input |
| `toHaveCount(n)` | quantidade de elementos |
| `toHaveURL(url \| regex)` | URL da página |
| `toHaveTitle(...)` | título |
| `toBeChecked()` / `toBeFocused()` | estado |
| `toHaveAttribute(nome, valor)` | atributo (ex.: `aria-invalid`, `aria-current`) |
| `toHaveAccessibleName(...)`, `toHaveRole(...)` | nome/papel acessível (versões recentes; conferir) |
| `toHaveScreenshot()` | comparação visual (use com parcimônia) |

```ts
await expect(page.getByRole('alert')).toContainText('Horário indisponível');
await expect(page.getByRole('row')).toHaveCount(3);                 // 2 linhas + cabeçalho
await expect(page.getByRole('row')).toHaveText([/Sala/, /Azul/, /Verde/]); // ordem
await expect(page).toHaveURL(/\/minhas-reservas$/);
await expect(page.getByLabel('Título')).toHaveAttribute('aria-invalid', 'true');
```

Ajustar o limite de uma asserção específica: `expect(loc).toBeVisible({ timeout: 10_000 })`
(use com justificativa; operação realmente lenta, não correção de flakiness).

## 3. Asserções sem espera (cuidado)

`expect(valor).toBe(...)` com valor **já resolvido** (`await locator.textContent()`,
`await locator.isVisible()`, `await locator.count()`) **não repete**: lê uma vez.

| Errado | Certo |
|---|---|
| `expect(await el.isVisible()).toBe(true)` | `await expect(el).toBeVisible()` |
| `expect(await el.textContent()).toBe('x')` | `await expect(el).toHaveText('x')` |
| `expect(await lista.count()).toBe(3)` | `await expect(lista).toHaveCount(3)` |
| `expect(await input.inputValue()).toBe('a')` | `await expect(input).toHaveValue('a')` |

`isVisible()` é instantâneo (não espera). Use as formas `expect(locator)`.

## 4. Quando precisa de polling manual

Para condições que não são do DOM (estado no servidor, fila assíncrona):

```ts
// expect.poll repete a função até o valor satisfazer a asserção
await expect.poll(async () => {
  const r = await apiFunc.listarReservas(sala.id);
  return r.length;
}, { timeout: 10_000, intervals: [250, 500, 1000] }).toBe(1);

// expect(...).toPass repete um bloco de asserções
await expect(async () => {
  const r = await api.get(`/jobs/${id}`);
  expect((await r.json()).status).toBe('concluido');
}).toPass({ timeout: 15_000 });
```

Use para processamento assíncrono legítimo (job, e-mail, evento). Não para esconder condição
de corrida da própria aplicação (isso é bug; ver `depuracao-flakiness.md`).

## 5. Esperar a rede (quando é o sinal certo)

Preferir o efeito visível. Quando o sinal correto é a requisição:

```ts
const resposta = page.waitForResponse(r =>
  r.url().endsWith('/reservas') && r.request().method() === 'POST');
await page.getByRole('button', { name: 'Reservar' }).click();
const r = await resposta;                // registrar a espera ANTES da ação
expect(r.status()).toBe(409);
```

Regras:
- Criar a promessa **antes** de disparar a ação (senão perde a resposta rápida).
- Assere o status e, se útil, o corpo (`await r.json()`): confirma o contrato por baixo da tela.
- **Não** use `waitForLoadState('networkidle')` como padrão: instável com polling, websockets e
  analytics; o próprio guia do Playwright desencoraja. Use o estado da UI.

## 6. Ações e verificações: ordem recomendada

1. Ação do usuário (`click`, `fill`, `press`).
2. Asserção de **interface** (mensagem, linha na lista, URL).
3. Asserção de **estado** (API/lista), principalmente em negativos.

```ts
await page.getByRole('button', { name: 'Reservar' }).click();
await expect(page.getByRole('alert')).toContainText('Horário indisponível'); // UI
expect(await apiFunc.listarReservas(sala.id)).toHaveLength(1);               // estado
```

## 7. Asserções suaves e rótulos

- `expect.soft(...)`: registra falha mas continua o teste. Útil em CT de tela com várias
  verificações independentes; **não** use para esconder CT com assuntos misturados.
- Mensagem personalizada: `expect(valor, 'reserva deveria estar cancelada').toBe(...)`
  melhora o texto da evidência no ACH.

## 8. Animações e estabilidade visual

- A auto-espera cobre "estabilidade" (elemento parou de se mover). Para transições longas ou
  skeletons, assere o conteúdo final, não o carregamento.
- No ambiente de teste, peça ao Dev flag para desativar animações, ou injete CSS:
  `await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' })`
  — apenas se o RNF/CT não verificar animação.
- Para respeitar `prefers-reduced-motion`, configure `use: { reducedMotion: 'reduce' }` (ou
  teste o comportamento com `page.emulateMedia`).

## 9. Timeouts: o que significam

| Timeout | Padrão | Onde |
|---|---|---|
| `timeout` (teste) | 30 s | tempo total do teste |
| `expect.timeout` | 5 s | cada asserção web-first |
| `actionTimeout` | sem limite próprio | cada ação (`use.actionTimeout`) |
| `navigationTimeout` | sem limite próprio | `goto`, redirecionamentos |

Aumentar timeouts globalmente é último recurso. Se algo leva > 5 s de verdade, é achado de
desempenho (RNF) ou espera mal feita.

## 10. Resumo: proibido e preferido

| Proibido | Preferido |
|---|---|
| `page.waitForTimeout(n)` | asserção web-first / `expect.poll` |
| `networkidle` como padrão | estado da UI ou `waitForResponse` específico |
| `isVisible()` dentro de `expect` direto | `expect(locator).toBeVisible()` |
| `try/catch` para "tentar de novo" | uma causa corrigida |
| `{ force: true }` | entender por que o clique é bloqueado |
| `test.slow()` sem motivo | investigar lentidão |
