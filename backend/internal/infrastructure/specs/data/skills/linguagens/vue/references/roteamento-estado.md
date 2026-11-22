# Roteamento, guardas e estado (Pinia)

## Rotas

```ts
// router/index.ts
import { createRouter, createWebHistory } from 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    exigeLogin?: boolean
    papel?: 'admin' | 'usuario'
    titulo: string
  }
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { titulo: 'Entrar' } },
    { path: '/reservas', name: 'reservas', component: () => import('@/views/ReservasView.vue'),
      meta: { exigeLogin: true, titulo: 'Minhas reservas' } },
    { path: '/:rest(.*)*', name: 'nao-encontrada', component: () => import('@/views/NaoEncontradaView.vue'),
      meta: { titulo: 'Página não encontrada' } },
  ],
})
export default router
```

Regras:

- **Rotas nomeadas** (`router.push({ name: 'reservas' })`), não strings soltas.
- **Carregamento preguiçoso** (`() => import(...)`) por view: bundle inicial menor.
- Rota coringa para **404** com a tela do Design.
- Parâmetros de rota são **strings**: converta e valide (`Number(route.params.id)` com
  verificação) antes de usar; id inválido mostra a tela de "não encontrado".
- Estado que deve sobreviver a recarregar/compartilhar link (filtros, página) vai na **query
  string**, não em store.
- `createWebHistory` exige o servidor devolver `index.html` nas rotas (fallback do nginx;
  ver `seguranca-build-docker.md`).

## Guardas

```ts
router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.carregado) await auth.carregarSessao()      // consulta o backend uma vez
  if (to.meta.exigeLogin && !auth.logado) {
    return { name: 'login', query: { retorno: to.fullPath } }
  }
  if (to.name === 'login' && auth.logado) return { name: 'reservas' }
})
```

Regras:

- Guarda é **conforto de navegação**, não segurança: a API recusa quem não pode (401/403).
  O teste de segurança nunca olha só o front.
- `retorno` na query vale **só para caminho interno** (validar que começa com `/` e não
  `//` ou URL externa) para evitar redirecionamento aberto.
- Evite lógica pesada e chamada de rede em cada navegação: carregue a sessão uma vez e
  guarde.
- Perfil (papel) no front só decide o que **mostrar**; o backend decide o que **pode**.
- Tratar `401` do cliente HTTP leva ao login pelo mesmo caminho (ver `cliente-http-openapi.md`).

### Título e foco a cada navegação (acessibilidade)

SPA não recarrega a página: leitor de tela não percebe a mudança. Ao navegar:

```ts
router.afterEach((to) => {
  document.title = `${to.meta.titulo} - Reserva de Salas`
  // depois do render, mover o foco para o título principal da nova tela
  nextTick(() => document.querySelector<HTMLElement>('h1')?.focus())
})
```

- Cada view tem **um `<h1>`** com `tabindex="-1"` (para receber foco programático).
- Anunciar a mudança é o efeito do foco no `h1` e do título; um link "Pular para o conteúdo"
  no topo do `App.vue` é recomendado.
- `scrollBehavior` do router: topo ao mudar de rota, restaurar posição ao voltar.

## Estado: o que entra na Pinia

| Estado | Onde |
|---|---|
| Campos de um formulário, aba aberta, modal aberto | **Local** (`ref`) |
| Lista carregada para uma tela | **Local** (composable de carregamento) |
| Filtro/página que deve virar link | **Query string** |
| **Usuário logado** (id, nome, papel) | **Pinia** (`stores/auth.ts`) |
| Preferência global (tema) | Pinia (ou composable singleton) |
| Cache de resposta do servidor | Só se medido e necessário; senão refaça a consulta |

Regra do projeto: **Pinia só para o que é compartilhado entre telas distantes**. Cada store
a mais é acoplamento e estado velho a mais.

```ts
// stores/auth.ts (setup store)
export const useAuthStore = defineStore('auth', () => {
  const usuario = ref<Usuario | null>(null)
  const carregado = ref(false)
  const logado = computed(() => usuario.value !== null)

  async function carregarSessao() {
    try { usuario.value = await api.eu() } catch { usuario.value = null }
    finally { carregado.value = true }
  }
  async function entrar(credenciais: Credenciais) { usuario.value = await api.login(credenciais) }
  async function sair() { await api.logout(); usuario.value = null }
  return { usuario, carregado, logado, carregarSessao, entrar, sair }
})
```

Regras:

- Store **não guarda token** quando o backend usa cookie `HttpOnly` (o front nem lê o token).
  Guarda só o **perfil** devolvido pela API.
- Ações assíncronas **propagam o erro** (o chamador mostra a mensagem); a store não engole.
- Não desestruture a store (`const { usuario } = store` perde reatividade): use
  `storeToRefs(store)` para estado/getters; ações podem ser desestruturadas.
- Reiniciar o estado no logout (`$reset` em setup store exige implementação própria; zere os
  `ref`s manualmente).
- Pinia **não** é cache de servidor. Se a necessidade crescer (cache, revalidação,
  deduplicação), é decisão de arquitetura (ex.: biblioteca de consulta de dados), via ADR.

## Navegação programática e links

- Links de navegação: `<RouterLink>` (gera `<a href>`), nunca `@click` + `push` em `div`.
- `router.push` depois de ação (criar reserva → lista) com mensagem de sucesso anunciada na
  tela de destino.
- Confirmação de saída com alteração pendente (`onBeforeRouteLeave`): só se o Design pedir.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Guarda como única proteção | API aberta | Backend autoriza sempre |
| `retorno` com URL arbitrária | Redirecionamento aberto | Aceitar só caminho interno |
| Tudo na Pinia | Estado velho, acoplamento | Local por padrão |
| Token da sessão guardado em store/localStorage | Roubável por XSS | Cookie `HttpOnly` |
| Desestruturar store | Perde reatividade | `storeToRefs` |
| Rota por string solta | Quebra ao renomear | Rotas nomeadas |
| Sem foco/título ao navegar | Leitor de tela perdido | `afterEach` com título e foco no `h1` |
| Parâmetro de rota usado sem validar | Erro em runtime | Converter e validar; 404 |
| Fallback do servidor ausente | 404 ao recarregar rota | `try_files ... /index.html` |
