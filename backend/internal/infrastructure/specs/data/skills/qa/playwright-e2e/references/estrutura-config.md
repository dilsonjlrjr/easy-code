# Estrutura do projeto e configuração

## 1. Estrutura recomendada

Projeto Node próprio dentro de `app/e2e/`, independente do código do app:

```
app/e2e/
  package.json
  package-lock.json            # commitado: versões reproduzíveis
  tsconfig.json
  playwright.config.ts
  .env.example                 # variáveis documentadas (sem segredo real)
  .gitignore                   # node_modules, test-results, playwright-report, .auth
  .auth/                       # storageState por papel (gerado; ignorado no git)
  tests/
    auth.setup.ts              # projeto "setup": login por papel
    us-01-entrar.spec.ts       # um arquivo por história
    us-03-reservar-sala.spec.ts
    us-05-cancelar-reserva.spec.ts
    a11y.spec.ts               # acessibilidade (axe)
  fixtures/
    index.ts                   # test estendido: api, dados, páginas
    api.ts                     # cliente de API para preparar dados
  pages/                       # page objects leves (só se necessário)
  support/
    env.ts                     # leitura tipada das variáveis
```

Regras:
- **Um arquivo por história** (`us-NN-<slug>.spec.ts`); dentro, um `test.describe` por CA se
  houver muitos testes.
- **Nome do teste começa com o id do CT:** `test('CT-08 reserva sobreposta é recusada', ...)`.
  O campo `execucao.teste` do CT aponta `tests/us-03-reservar-sala.spec.ts::CT-08 ...`.
- Testes de API puros (nível integração) podem viver em `tests/api/` usando o mesmo runner
  (`request` fixture), compartilhando fixtures.

## 2. Instalação

```bash
cd app/e2e
npm init playwright@latest        # escolher TypeScript; sem GitHub Actions se já houver CI
npm i -D @axe-core/playwright     # se há RNF de acessibilidade
npx playwright install --with-deps chromium   # --with-deps instala libs do SO (Linux)
```

- Fixe versões pelo lockfile (`npm ci` na execução limpa).
- Em aplicação pequena, **Chromium** basta; acrescente Firefox/WebKit só se o RNF de compatibilidade
  pedir (ver seção 5).
- Em máquina de desenvolvimento macOS/Windows, `--with-deps` é desnecessário; no Linux/CI é útil.
- Conferir a versão do Node exigida pela versão instalada do Playwright (`agentes/ambiente.md`).

## 3. `playwright.config.ts` de referência

```ts
import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.BASE_URL ?? 'http://localhost:3000';
const isCI = !!process.env.CI;

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,                 // testes independentes em paralelo
  forbidOnly: isCI,                    // test.only no CI quebra a execução
  retries: 0,                          // intermitente conta como falhou; não esconda
  workers: isCI ? 2 : undefined,       // ajustar ao ambiente; ver flakiness
  timeout: 30_000,                     // por teste
  expect: { timeout: 5_000 },          // por asserção (auto-espera)
  reporter: [['list'], ['html', { open: 'never' }]],

  use: {
    baseURL,
    trace: 'retain-on-failure',        // trace.zip só nas falhas
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',        // opcional; pesa mais
    locale: 'pt-BR',
    timezoneId: 'America/Sao_Paulo',   // explícito: não depender do runner
  },

  projects: [
    { name: 'setup', testMatch: /.*\.setup\.ts/ },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
      dependencies: ['setup'],         // login antes dos testes
    },
  ],
});
```

Pontos de atenção:
- `retries: 0` é escolha do projeto (teste intermitente conta como falhou). Se usar retry no
  CI **só para diagnóstico**, o relatório marca como intermitente todo teste que passou na
  segunda tentativa (o reporter HTML sinaliza como "flaky").
- `timeout` por teste e `expect.timeout` são distintos. Teste lento demais indica que algo está
  errado (setup pela UI, esperas ruins), não que o limite deva subir.
- `trace: 'on-first-retry'` é padrão do `init`, mas como `retries: 0`, use `'retain-on-failure'`.
- **Não** defina `webServer` se a aplicação sobe por Docker Compose externo; ver
  `ci-docker-compose.md`. `webServer` serve para app que o próprio teste sobe (dev server).

## 4. Variáveis de ambiente

`.env.example` (documenta; o arquivo real fica fora do git):

```
BASE_URL=http://localhost:3000        # front
API_URL=http://localhost:8080         # API (para preparar dados)
USER_FUNC_EMAIL=funcionario@example.test
USER_FUNC_PASS=<definir no ambiente local>
USER_ADMIN_EMAIL=admin@example.test
USER_ADMIN_PASS=<definir no ambiente local>
```

`support/env.ts` — falha cedo se faltar variável:

```ts
function obrigatoria(nome: string): string {
  const v = process.env[nome];
  if (!v) throw new Error(`Variável de ambiente ausente: ${nome} (ver .env.example)`);
  return v;
}
export const env = {
  baseURL: process.env.BASE_URL ?? 'http://localhost:3000',
  apiURL: obrigatoria('API_URL'),
  func: { email: obrigatoria('USER_FUNC_EMAIL'), senha: obrigatoria('USER_FUNC_PASS') },
  admin: { email: obrigatoria('USER_ADMIN_EMAIL'), senha: obrigatoria('USER_ADMIN_PASS') },
};
```

Para carregar `.env` automaticamente use `dotenv` (open source) no topo do config
(`import 'dotenv/config'`), ou o suporte nativo do Node (`--env-file`; conferir versão).
Nunca commite senha; usuários de teste são do **seed local**, sem relação com produção.

## 5. Projects: navegadores e viewports

Cada `project` é uma combinação de navegador/dispositivo; o mesmo teste roda em todos.

```ts
projects: [
  { name: 'setup', testMatch: /.*\.setup\.ts/ },
  { name: 'chromium', use: { ...devices['Desktop Chrome'] }, dependencies: ['setup'] },
  { name: 'firefox',  use: { ...devices['Desktop Firefox'] }, dependencies: ['setup'] },
  { name: 'mobile',   use: { ...devices['Pixel 7'] },        dependencies: ['setup'] },
],
```

- Compatibilidade (RNF) entra como `project`; **não duplique testes**.
- Use `testIgnore`/`grep` para limitar projetos pesados aos testes que importam (ex.: só
  `@smoke` no mobile).
- Tags no título: `test('CT-08 ... @smoke', ...)` e `npx playwright test --grep @smoke`.

## 6. Scripts do `package.json`

```json
{
  "scripts": {
    "test": "playwright test",
    "test:smoke": "playwright test --grep @smoke",
    "test:flaky": "playwright test --repeat-each=10",
    "report": "playwright show-report",
    "trace": "playwright show-trace"
  }
}
```

Comandos nativos e estáveis do CLI: `npx playwright test`, `-g "<texto>"`,
`--project=<nome>`, `--repeat-each=N`, `--workers=N`, `--headed`, `--debug`, `--ui`,
`--trace on`, `npx playwright show-report`, `npx playwright show-trace <arquivo.zip>`.

## 7. TypeScript

`tsconfig.json` mínimo:

```json
{
  "compilerOptions": {
    "target": "ES2022", "module": "ESNext", "moduleResolution": "Bundler",
    "strict": true, "esModuleInterop": true, "skipLibCheck": true,
    "types": ["node"]
  },
  "include": ["tests", "fixtures", "pages", "support", "playwright.config.ts"]
}
```

- `strict: true` evita `undefined` silencioso em dados de resposta.
- Tipar o corpo das respostas da API usada no setup (interfaces mínimas).

## 8. `.gitignore`

```
node_modules/
test-results/
playwright-report/
blob-report/
.auth/
.env
```

## 9. Anti-padrões

- Misturar o projeto de teste no `package.json` do app (acopla versões e dificulta rodar isolado).
- `baseURL` fixo no teste.
- Lockfile fora do git (versões flutuam, intermitência "do nada").
- `workers` alto numa máquina fraca (falsa intermitência).
- Testes dependentes de ordem + `fullyParallel: true` (quebra de cara; corrija os testes, não o paralelismo).
