# Estrutura do projeto Vue 3 + Vite + TypeScript strict

## Criação e árvore

Criar com o gerador oficial do Vue (`npm create vue@latest`, escolhendo TypeScript, Router,
Pinia, Vitest, ESLint, Prettier; conferir as opções na versão atual). Remova exemplos
gerados que não serão usados.

```
frontend/
  index.html
  package.json
  vite.config.ts
  tsconfig.json  tsconfig.app.json  tsconfig.vitest.json   (nomes dependem do gerador, conferir)
  eslint.config.*   .prettierrc
  .env.example
  Dockerfile  nginx.conf
  public/                  config.js (opcional, config em runtime)
  src/
    main.ts  App.vue
    api/
      cliente.ts           cliente HTTP único
      erros.ts             Problem Details -> mensagem
      tipos.gen.ts         tipos gerados do OpenAPI (não editar)
      reservas.ts  salas.ts   funções por recurso
    assets/  styles/       tokens.css (tokens do Design), base.css
    components/            usados em 2+ views (BotaoPrimario, CampoTexto, Modal)
    composables/           useAnuncio.ts, useForm.ts
    router/index.ts        rotas + guardas
    stores/auth.ts
    views/                 uma por tela do protótipo
  tests/                   ou *.spec.ts ao lado do arquivo
```

Regras:

- **Uma `view` por tela do protótipo**; nome espelha o protótipo (`ReservasView.vue`).
- `components/` só para o que **é reutilizado**. Componente usado por uma view só fica ao
  lado dela (`views/reservas/FormReserva.vue`).
- `api/` é a **única** pasta que conhece URLs e `fetch`.
- Sem pasta `utils/` genérica; nomeie por conceito (`formatacao-data.ts`).
- Ordem das importações e alias `@/` para `src/` (configurado em `vite.config.ts` e no
  `tsconfig`).

## TypeScript strict

`tsconfig` com `"strict": true` e, além disso (conferir nomes na versão do TypeScript):

| Opção | Efeito |
|---|---|
| `noUncheckedIndexedAccess` | `arr[i]` pode ser `undefined`; força tratar |
| `noImplicitOverride`, `noFallthroughCasesInSwitch` | Menos descuido |
| `exactOptionalPropertyTypes` | Opcional não é `undefined` explícito (opcional; pode ser rígido demais) |
| `verbatimModuleSyntax` / `isolatedModules` | Importa tipos com `import type` |

Verificação de tipos dos `.vue`: **`vue-tsc --noEmit`** (o Vite não checa tipos no build).
Faça parte do gate: `npm run lint && npx vue-tsc --noEmit`.

Regras de tipo:

- **Sem `any`**; use `unknown` e estreite com validação.
- Sem `as` para calar erro; `as const` e `satisfies` (conferir) são aceitáveis.
- Props e emits **tipados** (ver `composition-api-componentes.md`).
- Dados externos (resposta da API, `localStorage`, JSON) entram como `unknown` e são
  convertidos para tipo de domínio na borda.

## Scripts do `package.json`

```json
{
  "scripts": {
    "dev": "vite",
    "build": "vue-tsc --noEmit && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "test:watch": "vitest",
    "lint": "eslint . && prettier --check .",
    "typecheck": "vue-tsc --noEmit"
  }
}
```

Os nomes de comando seguem a tabela da `SKILL.md`. Um comando só (`npm run lint`) deve cobrir
o que o Dev e o QA rodam.

## Lint e formatação

- ESLint com a configuração oficial para Vue 3 (`eslint-plugin-vue`, conjunto *recommended*
  ou *strongly-recommended*; conferir o nome na versão) e `typescript-eslint`.
- Regras que valem a pena ligar: `vue/no-v-html` (alerta XSS), `vue/require-default-prop`
  quando aplicável, `vue/block-order`, `@typescript-eslint/no-explicit-any`.
- Plugin de acessibilidade para templates (`eslint-plugin-vuejs-accessibility`, open source;
  conferir) como rede de segurança, **não** substitui teste de teclado.
- Prettier formata; o linter não discute estilo.

## Variáveis de ambiente (Vite)

- Prefixo `VITE_` é **embutido no bundle em tempo de build** e é **público**. Exemplo:
  `VITE_API_URL`.
- Acessar por módulo único e tipado:

```ts
// src/config.ts
export const config = {
  apiUrl: import.meta.env.VITE_API_URL ?? '/api',
} as const
```

- `.env.example` lista cada variável com valor fictício; `.env.local` fica no `.gitignore`.
- Para mudar por ambiente **sem rebuild** (imagem única), ver `seguranca-build-docker.md`.
- Nunca colocar segredo em variável `VITE_*`.

## Aliases, imports e CSS

- Alias `@` para `src` (`vite.config.ts`: `resolve.alias`), espelhado em `tsconfig`.
- Importe tipos com `import type`.
- CSS: tokens do Design como variáveis (`:root { --cor-primaria: ... }`) em `tokens.css`,
  importado em `main.ts`. `<style scoped>` por componente para o resto. Sem valor mágico:
  tudo que o Design nomeou usa o token.
- Foco visível nunca removido (`outline: none` sem substituto é proibido).

## `main.ts` mínimo

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './assets/styles/tokens.css'

createApp(App).use(createPinia()).use(router).mount('#app')
```

Tratamento global de erro não capturado (`app.config.errorHandler`) registra e mostra a
tela de erro genérica do Design; não engole em silêncio.

## `index.html`

- `<html lang="pt-BR">`; `<title>` descritivo; `<meta name="viewport" ...>`.
- Elemento raiz `#app`. Título por rota atualizado pelo router (ver `roteamento-estado.md`).

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Confiar que `vite build` checa tipos | Erro de tipo em produção | `vue-tsc --noEmit` no gate e no `build` |
| `any` para "destravar" | Perde o valor do TS | `unknown` + estreitamento |
| Segredo em `VITE_*` | Vaza no bundle | Segredo só no backend |
| `components/` virar depósito | Difícil achar | Só reutilizados; o resto ao lado da view |
| Valores de cor/espaçamento soltos | Diverge do Design | Tokens CSS |
| Remover `outline` do foco | Teclado perdido | Estilo de foco visível próprio |
| `lang` ausente no `html` | Leitor de tela pronuncia errado | `lang="pt-BR"` |
