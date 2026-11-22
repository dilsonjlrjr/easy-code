# Autenticação e `storageState` por papel

Logar pela interface em todo teste é lento e fonte de flakiness. O padrão: **logar uma vez
por papel**, salvar cookies/localStorage em arquivo (`storageState`) e reaproveitar.

## 1. Quando logar pela interface

- Nos testes **da própria história de login** (US de autenticação): login real, passo a passo,
  sem `storageState`.
- Em todos os outros testes: `storageState`.

## 2. Projeto `setup` (padrão recomendado)

`tests/auth.setup.ts`:

```ts
import { test as setup, expect } from '@playwright/test';
import path from 'node:path';
import { env } from '../support/env';

const arquivo = (papel: string) => path.join(__dirname, '..', '.auth', `${papel}.json`);

async function entrar(page, email: string, senha: string) {
  await page.goto('/login');
  await page.getByLabel('E-mail').fill(email);
  await page.getByLabel('Senha').fill(senha);
  await page.getByRole('button', { name: 'Entrar' }).click();
  // esperar um sinal de que a sessão foi estabelecida (não só a URL)
  await expect(page.getByRole('navigation')).toBeVisible();
  await expect(page).not.toHaveURL(/\/login/);
}

setup('autenticar funcionário', async ({ page }) => {
  await entrar(page, env.func.email, env.func.senha);
  await page.context().storageState({ path: arquivo('funcionario') });
});

setup('autenticar admin', async ({ page }) => {
  await entrar(page, env.admin.email, env.admin.senha);
  await page.context().storageState({ path: arquivo('admin') });
});
```

Config: `projects: [{ name: 'setup', testMatch: /.*\.setup\.ts/ }, { name: 'chromium', dependencies: ['setup'], ... }]` (ver `estrutura-config.md`).

O arquivo `.auth/*.json` contém credenciais de sessão: **no `.gitignore`**.

## 3. Usando por papel nos testes

Por arquivo ou bloco:

```ts
import { test, expect } from '@playwright/test';

test.describe('como funcionário', () => {
  test.use({ storageState: '.auth/funcionario.json' });

  test('CT-07 reserva sala livre', async ({ page }) => { /* ... */ });
});

test.describe('como admin', () => {
  test.use({ storageState: '.auth/admin.json' });
  test('CT-33 admin cadastra sala', async ({ page }) => { /* ... */ });
});
```

Anônimo (CT de "sem login"): `test.use({ storageState: { cookies: [], origins: [] } })`.

## 4. Fixtures por papel (vários usuários no mesmo teste)

CT de permissão precisa de **dois usuários** no mesmo teste (A cria, B tenta agir). Crie
contextos separados, cada um com seu `storageState`:

```ts
// fixtures/index.ts
import { test as base, Page } from '@playwright/test';

type Papeis = { paginaFuncionario: Page; paginaAdmin: Page };

export const test = base.extend<Papeis>({
  paginaFuncionario: async ({ browser }, use) => {
    const ctx = await browser.newContext({ storageState: '.auth/funcionario.json' });
    const page = await ctx.newPage();
    await use(page);
    await ctx.close();
  },
  paginaAdmin: async ({ browser }, use) => {
    const ctx = await browser.newContext({ storageState: '.auth/admin.json' });
    const page = await ctx.newPage();
    await use(page);
    await ctx.close();
  },
});
export { expect } from '@playwright/test';
```

**Dois usuários do mesmo papel** (dono e "outro"): crie um segundo funcionário no seed
(`funcionario2`) e um segundo arquivo de estado. Sem isso o CT de IDOR não pode ser escrito.

## 5. Autenticação via API (mais rápida e estável)

Se a API de login devolve cookie/token, pule a tela no setup (exceto na US de login):

```ts
setup('autenticar via API', async ({ request }) => {
  const r = await request.post(`${env.apiURL}/auth/login`, {
    data: { email: env.func.email, senha: env.func.senha },
  });
  expect(r.ok()).toBeTruthy();
  await request.storageState({ path: '.auth/funcionario.json' });
});
```

Funciona quando a sessão é **cookie**. Se o token fica em `localStorage`/memória (SPA), injete
o estado: salve o token e use `context.addInitScript` para gravar no `localStorage` antes da
página carregar, ou faça o login pela tela no setup. Para chamadas autenticadas de API
(preparar dados), use o token em `extraHTTPHeaders`:

```ts
const api = await request.newContext({
  baseURL: env.apiURL,
  extraHTTPHeaders: { Authorization: `Bearer ${token}` },
});
```

## 6. Sessão expirada e renovação

- Sessões curtas (RNF) podem expirar no meio da suíte: gere o estado no **início** da execução
  (projeto `setup` roda sempre antes) e mantenha a suíte rápida.
- CT de expiração: apague os cookies do contexto durante o teste
  (`await context.clearCookies()`) e verifique o redirecionamento ao login; não espere o tempo
  real expirar. Se o requisito é "expira em 15 min" e o app permite configurar TTL curto no
  ambiente de teste, use isso e documente.
- Se o ambiente reinicia e invalida sessões (segredo JWT novo), regenere sempre o estado: não
  versione `.auth`.

## 7. Verificar que o estado está válido

Antes dos testes, uma verificação barata no setup (`GET /me` autenticado retorna o papel
esperado) evita 80 falhas por um login quebrado:

```ts
const eu = await request.get(`${env.apiURL}/me`);
expect((await eu.json()).papel).toBe('funcionario');
```

## 8. Segurança do ambiente de teste

- Credenciais só em variável de ambiente; usuários exclusivos do ambiente local.
- Nunca reaproveitar senha real; nunca apontar para outro ambiente.
- Não registrar senha no trace: o trace guarda o que é digitado nos campos. Como são
  credenciais do ambiente local, aceitável, mas **não** anexe o trace do `auth.setup` a ACH
  público; a evidência de um teste de negócio usa `storageState` (não contém digitação de senha).

## 9. Anti-padrões

- Reaproveitar um único usuário para "dono" e "outro".
- Login pela UI em cada `beforeEach`.
- Commitar `.auth/*.json`.
- `storageState` gerado uma vez e usado semanas depois (sessão expirada, falha misteriosa).
- Testar login com `storageState` já ativo (o teste de login precisa de contexto limpo).
