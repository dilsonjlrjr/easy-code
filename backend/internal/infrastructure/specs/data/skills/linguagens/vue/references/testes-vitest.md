# Testes no front com Vitest + Vue Test Utils

Vale a skill `estrategia-testes` (pirâmide/troféu, determinismo, dublês). Aqui, o que é
específico do front.

## O que testar no front (e o que não)

| Testar | Nível | Não testar |
|---|---|---|
| Regra pura do front (validação de formulário, formatação, cálculo) | Unitário | Getter/setter, template trivial |
| Composable com lógica (estado de carregamento, cancelamento) | Unitário | Código do Vue/Router/Pinia |
| Componente: o que o usuário **vê e faz** (renderiza, emite, mostra erro) | Componente | Detalhes internos (nome de variável, método privado) |
| Função de API: monta URL, trata 401 e Problem Details | Unitário com `fetch` simulado | O backend (isso é do teste de API) |
| Guarda de rota (logado x anônimo, retorno) | Unitário/integração leve | O roteamento do Vue Router em si |
| Jornada entre telas, foco real, navegador | **e2e do QA** (`playwright-e2e`) | Duplicar e2e em componente |

Prioridade por risco: formulário (validação, erros do backend), fluxo de login/401, estados
da tela (carregando, vazio, erro), acessibilidade básica (papéis, rótulos). Estilo visual
não se testa em unitário (compare com o protótipo, QA e e2e).

## Configuração mínima

```ts
// vitest.config.ts (ou dentro do vite.config.ts; conferir a versão)
import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config'

export default mergeConfig(viteConfig, defineConfig({
  test: { environment: 'jsdom', globals: false, restoreMocks: true },
}))
```

- Ambiente DOM: `jsdom` ou `happy-dom` (conferir qual o gerador configurou).
- `restoreMocks` para limpar entre testes; **sem estado compartilhado**.
- Tipos do ambiente de teste em `tsconfig.vitest.json` (gerado; conferir).

## Componente com Vue Test Utils

```ts
import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import CampoTexto from '@/components/CampoTexto.vue'

describe('CampoTexto', () => {
  it('liga o rótulo ao campo e mostra o erro acessível', () => {
    const w = mount(CampoTexto, {
      props: { id: 'nome', rotulo: 'Nome', modelValue: '', erro: 'Informe o nome.' },
    })
    const campo = w.get('input')
    expect(w.get('label').attributes('for')).toBe('nome')
    expect(campo.attributes('aria-invalid')).toBe('true')
    expect(w.get('[role="alert"]').text()).toBe('Informe o nome.')
  })

  it('atualiza o v-model ao digitar', async () => {
    const w = mount(CampoTexto, { props: { id: 'nome', rotulo: 'Nome', modelValue: '' } })
    await w.get('input').setValue('Ana')
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['Ana'])
  })
})
```

Regras:

- **Interaja como o usuário**: `trigger('click')`, `setValue`, `await` (o DOM atualiza no
  próximo tick). Esqueceu o `await`, o teste lê o DOM antigo.
- Consulte por **papel/texto/rótulo**, não por classe CSS. Com Testing Library
  (`@testing-library/vue`, open source; conferir) é natural: `getByRole('button', { name:
  'Reservar' })`. Com VTU, prefira seletores de atributo semântico (`[role]`, `label`) e
  `data-testid` só como último recurso (o QA também usa; combinar nomes).
- Verifique **resultado observável**: texto, atributo, evento emitido (`emitted`), chamada de
  API simulada com argumentos corretos.
- **Sem snapshot de componente inteiro** (frágil e ninguém lê). Snapshot só para saída
  pequena e estável.
- `shallowMount` raramente: esconde integração entre pai e filho; prefira `mount`.

## Composable e função pura

```ts
it('ignora a resposta antiga quando a busca muda', async () => {
  // simula duas chamadas: a primeira demora mais e não deve sobrescrever a segunda
})

it.each([
  ['', false],
  ['2026-03-10T10:00', true],
])('validarInicio(%s) -> %s', (valor, ok) => { expect(validarInicio(valor)).toBe(ok) })
```

Para composable que usa `onUnmounted`/`watch`, monte-o dentro de um componente de teste
mínimo (`defineComponent` com `setup`) para ter o ciclo de vida.

## Simular a API

Duas formas, em ordem de preferência:

1. **Simular `fetch`** no teste da camada `api/` (resposta e status fixos, inclusive 401,
   409 e Problem Details real do contrato). Prova a conversão de erro e o 401.
2. **Simular o módulo `api/`** no teste do componente (`vi.mock('@/api/reservas')`), para o
   componente não depender de rede. A **forma** da resposta simulada vem do tipo gerado do
   OpenAPI (assim o teste quebra se o contrato mudar).

```ts
vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
  JSON.stringify({ type: '/problemas/reserva-conflito', status: 409, detail: 'Sala ocupada.' }),
  { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
)))
await expect(criarReserva(nova)).rejects.toMatchObject({ status: 409 })
```

(`vi.fn`, `vi.mock`, `vi.stubGlobal`, `vi.useFakeTimers`: conferir assinatura na versão
usada. Servidor falso de rede por interceptação, como MSW, é alternativa open source;
conferir.)

## Pinia e roteador nos testes

- Pinia: `setActivePinia(createPinia())` em `beforeEach` para store isolada; ou
  `createTestingPinia` (pacote `@pinia/testing`, conferir) para simular ações.
- Router: crie um router de teste com as rotas necessárias, `await router.push(...)`,
  `await router.isReady()` e passe em `global.plugins`. Teste a **guarda** com store e router
  reais e a API simulada.

```ts
const router = createRouter({ history: createMemoryHistory(), routes })
router.push('/reservas'); await router.isReady()
const w = mount(App, { global: { plugins: [router, createPinia()] } })
```

## Tempo e foco

- Relógio controlado (`vi.useFakeTimers()` e `vi.setSystemTime`) para debounce, mensagens
  temporizadas e datas ("hoje"); sempre restaure (`vi.useRealTimers()`).
- Foco: `document.activeElement` é verificável em jsdom se o elemento está **anexado ao
  documento** (`mount(..., { attachTo: document.body })`). Foco real em navegador e ordem de
  Tab ficam para o e2e.

## Acessibilidade em teste de componente

- Consultar por papel e nome já valida semântica.
- Afirmar `aria-invalid`, `aria-describedby` apontando para elemento existente, `role="alert"`
  no erro, `label` ligado.
- Rodar **axe** (`axe-core`/`vitest-axe` ou `jest-axe`, conferir) em componentes de
  formulário e modal é barato; é complemento do e2e do QA.

## Cobertura e velocidade

- Cobertura do front como **mapa** (provider `v8`, `@vitest/coverage-v8`; conferir). Meta
  global fixa não; olhar o que de risco ficou descoberto (validação, tratamento de erro).
- Testes de componente rodam em segundos; se passam de poucos segundos, há rede, timer real ou
  montagem excessiva.
- `npm run test` roda uma vez e sai (`vitest run`); `vitest` em modo observação é para o ciclo
  de TDD.

## Exemplo: formulário de reserva (resumo dos casos)

| Caso | Prova |
|---|---|
| Campos vazios: mostra erros, foco no resumo, não chama API | Validação + foco |
| Fim antes do início: erro no campo fim | Regra |
| Sucesso: chama API com o payload certo, navega | Fluxo feliz |
| 409: mostra mensagem de conflito do Design, mantém valores | Erro do backend |
| 422 com campos: mostra erro no campo correto | Mapeamento |
| 401: aciona o callback de não autenticado | Cliente HTTP |
| Envio duplo: segunda chamada ignorada enquanto `enviando` | Robustez |

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Esquecer `await` após `trigger`/`setValue` | Lê DOM antigo; teste intermitente | `await` sempre |
| Seletor por classe CSS | Quebra ao estilizar | Papel/texto/label |
| Snapshot de componente inteiro | Frágil, não lido | Asserções pontuais |
| Resposta simulada inventada | Diverge do contrato | Usar tipos gerados e Problem Details reais |
| `shallowMount` em tudo | Integração não testada | `mount` |
| Estado vazando entre testes (Pinia, timers, `fetch` global) | Ordem muda o resultado | `beforeEach` com reset e `restoreMocks` |
| Testar o framework | Custo sem risco | Testar o seu comportamento |
| Duplicar e2e em componente | Suíte lenta | Jornada fica com o QA |
