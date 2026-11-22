---
name: frontend-vue
description: Convenções e boas práticas para o front em Vue 3 + Vite + TypeScript strict - estrutura do projeto, Composition API com script setup (props, emits, composables, reatividade e armadilhas), Vue Router com guardas, Pinia só para estado compartilhado, cliente HTTP único (base URL, credenciais, 401, Problem Details, cancelamento), tipos a partir do OpenAPI, formulários e validação acessíveis fiéis ao protótipo, acessibilidade em componentes (foco, anúncios, semântica), testes com Vitest e Vue Test Utils, segurança no front (XSS, v-html, tokens, CSP), build e Docker (nginx não root, variáveis de build versus execução). Use em Arquitetura, Design, Dev e QA em todos os projetos.
---

# Front Vue

Front simples, tipado, acessível e fiel ao protótipo do Design. O front **apresenta e coleta**;
**a regra e a autorização de verdade são do backend**. Stack padrão abaixo; a Arquitetura pode
mudar com ADR. Skills vizinhas: `ux-acessibilidade` (Design), `estrategia-testes` e
`qualidade-codigo` (Dev), `revisao-seguranca` (Segurança), `ambiente-docker`, `cenarios-teste`
e `playwright-e2e` (QA).

## Princípios

1. **Simplicidade primeiro.** Componente e função local antes de abstração; estado local antes
   de store; HTML nativo antes de widget customizado.
2. **Tipos são o contrato.** TypeScript `strict`, sem `any`, sem `as` para calar o compilador;
   respostas da API tipadas a partir do `openapi.yaml`.
3. **Uma porta para a rede.** Todo acesso HTTP passa pelo cliente único; componente nunca
   chama `fetch` direto.
4. **Fidelidade ao protótipo.** Telas, textos, mensagens e estados (vazio, carregando, erro,
   sucesso) iguais ao Design (mapa `TELAS`: `telas[].estados` e `criterios` dizem o que cada tela mostra); divergência vira ACH, não improviso.
5. **Acessível por construção.** Semântica nativa, `label`, foco visível e gerenciado,
   erros anunciados, navegação por teclado.
6. **Nada sensível no cliente.** Tudo do bundle é público; autorização real no backend.
7. **Teste o comportamento do usuário** (o que se vê e se faz), não a implementação interna.

## Stack padrão

| Item | Padrão |
|---|---|
| Framework | Vue 3, Composition API, `<script setup lang="ts">` |
| Build | Vite |
| Linguagem | TypeScript `strict` |
| Rotas | Vue Router |
| Estado | Pinia, só o que é compartilhado entre telas (usuário logado) |
| Testes | Vitest + Vue Test Utils (+ Testing Library opcional, conferir) |
| Lint/formato | ESLint (config oficial do Vue/`typescript-eslint`) + Prettier; tipos com `vue-tsc --noEmit` |
| UI | CSS próprio com tokens do Design (`estilo.md`); sem biblioteca de componentes em aplicação pequena |

Versões: "estável atual"; as efetivas ficam em `agentes/ambiente.md`.

## Regras de decisão

| Situação | Faça | Evite | Referência |
|---|---|---|---|
| Dado usado por um componente só | `ref` local | Pinia | composition-api-componentes |
| Dado usado por 2+ telas distantes (usuário logado) | Pinia | Prop drilling profundo | roteamento-estado |
| Lógica reutilizável com estado | Composable `useX()` | Mixin, herança | composition-api-componentes |
| Valor derivado | `computed` | `watch` que escreve outro `ref` | composition-api-componentes |
| Chamar API | Função em `api/` via cliente único | `fetch`/`axios` solto no componente | cliente-http-openapi |
| Tipo da resposta | Gerado do OpenAPI (ou escrito conferido) | `any` | cliente-http-openapi |
| Erro do backend | Converter Problem Details em mensagem | Mostrar JSON cru ou "erro" genérico | cliente-http-openapi |
| Formulário | Validar na borda do front (UX) **e** confiar no back | Só validação do front | formularios-acessibilidade |
| HTML vindo de usuário | Interpolação `{{ }}` (escapada) | `v-html` | seguranca-build-docker |
| Rota que exige login | Guarda no router | Esconder botão e achar que basta | roteamento-estado |
| Variável de ambiente no front | `VITE_*` só para valor **público** | Chave secreta em `VITE_*` | seguranca-build-docker |
| Config que muda por ambiente sem rebuild | Arquivo de config carregado em runtime | Rebuild por ambiente | seguranca-build-docker |

## Estrutura (resumo)

```
frontend/src/
  api/          cliente HTTP, tipos gerados, funções por recurso
  components/   componentes reutilizados em mais de uma view
  composables/  useX()
  views/        uma por tela do protótipo
  stores/       auth.ts (só o compartilhado)
  router/       rotas e guardas
  App.vue, main.ts
```

Detalhe em `references/estrutura-projeto.md`.

## Fluxo de trabalho (Dev, por T de front)

1. Ler a T, o protótipo da tela, o `openapi.yaml` e os CT.
2. Tipos da API disponíveis (gerar ou conferir) antes de escrever a função de API.
3. Teste primeiro (vermelho): componente/composable/função de API com Vitest.
4. Implementar a view com HTML semântico, tokens do Design e os quatro estados da tela.
5. Ligar ao cliente HTTP; tratar 401, erro de validação (campos) e erro genérico.
6. Rodar `npm run lint`, `npx vue-tsc --noEmit`, `npm run test`, `npm run build`.
7. Conferir com a checklist abaixo; registrar o que for na T.

## Comandos

| Ação | Comando |
|---|---|
| Desenvolvimento | `npm run dev` |
| Testes | `npm run test` (Vitest) |
| Lint | `npm run lint` |
| Tipos | `npx vue-tsc --noEmit` |
| Build | `npm run build` |

## Fidelidade ao Design

- Textos, mensagens de erro/sucesso, ordem de campos e estados iguais ao protótipo.
- Tokens (cor, espaçamento, tipografia) vêm do `estilo.md` como variáveis CSS; sem valor
  "parecido".
- Acessibilidade do protótipo preservada: `label` ligado ao campo, foco visível, contraste,
  HTML semântico. Se o protótipo tem falha, abrir ACH (`categoria: inconsistencia_design`, `etapa_destino: design`, afetado `TELAS/TL-n`) em vez de copiar a falha.

## Checklist de revisão do front

- [ ] `<script setup lang="ts">`; props e emits tipados; sem `any`.
- [ ] Sem `fetch`/HTTP fora de `api/`; erros 401 e Problem Details tratados.
- [ ] Estado local por padrão; Pinia só para o compartilhado.
- [ ] `computed` para derivados; sem `watch` desnecessário; sem mutar prop.
- [ ] Quatro estados da tela: carregando, vazio, erro, sucesso; iguais ao protótipo.
- [ ] Formulário com `label`, erro por campo ligado por `aria-describedby`, foco no primeiro
      erro; botão não desabilitado sem explicação.
- [ ] Mudança de rota gerencia foco e título; modal prende foco e devolve ao fechar.
- [ ] Sem `v-html` com dado não confiável; sem segredo em `VITE_*`; sem token em `localStorage`
      sem ADR.
- [ ] Rotas protegidas com guarda; autorização real não depende do front.
- [ ] Testes dos comportamentos e dos erros; `vue-tsc`, lint e build verdes.
- [ ] Imagem Docker com nginx não root e config por ambiente sem rebuild (quando exigido).

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Desestruturar `props`/`reactive` e perder reatividade | UI não atualiza | `toRefs`, ou acessar `props.x`; ver composition-api-componentes |
| `fetch` espalhado nos componentes | Erro e 401 tratados de formas diferentes | Cliente único em `api/` |
| Tudo na Pinia | Acoplamento e estado velho | Estado local por padrão |
| `v-html` com texto do usuário | XSS | Interpolação; sanitizar só se inevitável |
| Token em `localStorage` | Roubável por XSS | Cookie `HttpOnly` do backend |
| Validar só no front | Burlável | Backend valida sempre |
| `any` nos dados da API | Bug de contrato só em produção | Tipos do OpenAPI |
| Esconder botão como "segurança" | Rota e API abertas | Guarda + autorização no backend |
| `div` clicável | Teclado e leitor de tela não funcionam | `button`/`a` |
| Mensagem de erro genérica "Algo deu errado" | Usuário sem saída | Mensagem do Problem Details, linguagem do Design |

## Índice dos references

| Arquivo | Ler quando |
|---|---|
| `references/estrutura-projeto.md` | Ao criar/organizar o projeto Vite + TS strict, configurar lint, alias, scripts e variáveis |
| `references/composition-api-componentes.md` | Para props, emits, `v-model`, composables, reatividade e suas armadilhas |
| `references/roteamento-estado.md` | Para rotas, guardas, foco na navegação e Pinia (o que entra e o que não entra) |
| `references/cliente-http-openapi.md` | Para o cliente HTTP único, 401, Problem Details, cancelamento e tipos do OpenAPI |
| `references/formularios-acessibilidade.md` | Para formulários, validação, mensagens e acessibilidade em componentes (foco, modal, anúncios) |
| `references/testes-vitest.md` | Para testar componentes, composables, API e rotas com Vitest + Vue Test Utils |
| `references/seguranca-build-docker.md` | Para XSS, `v-html`, tokens, CSP, build, nginx não root e variáveis de build x execução |
