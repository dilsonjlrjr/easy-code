# Composition API, `<script setup>`, composables e reatividade

## Componente padrão

```vue
<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  salaNome: string
  inicio: string          // ISO 8601
  cancelavel?: boolean
}>()

const emit = defineEmits<{
  cancelar: [reservaId: string]
}>()

const horario = computed(() => formatarHorario(props.inicio))
</script>

<template>
  <li>
    <span>{{ salaNome }} - {{ horario }}</span>
    <button v-if="cancelavel" type="button" @click="emit('cancelar', id)">Cancelar</button>
  </li>
</template>
```

Regras:

- **Sempre `<script setup lang="ts">`** (ordem dos blocos: script, template, style — a regra
  do projeto, conferida por lint).
- `defineProps` e `defineEmits` com **tipos** (sintaxe de tipo literal, acima). Valores
  padrão de props com `withDefaults(defineProps<...>(), { ... })` (conferir; versões recentes
  também aceitam desestruturar props com padrão, **conferir** na versão usada).
- **Props descem, eventos sobem.** Nunca mutar prop; o filho emite, o pai decide.
- Emits nomeados no passado/ação (`cancelar`, `salvo`), com payload tipado.
- Componente pequeno, uma responsabilidade; template legível sem lógica pesada (mova para
  `computed` ou função).

## `v-model` em componente

```vue
<!-- CampoTexto.vue -->
<script setup lang="ts">
const modelo = defineModel<string>({ required: true })   // Vue 3.4+ (conferir versão)
defineProps<{ rotulo: string; erro?: string; id: string }>()
</script>
<template>
  <label :for="id">{{ rotulo }}</label>
  <input :id="id" v-model="modelo" :aria-invalid="!!erro" />
  <p v-if="erro" :id="`${id}-erro`" role="alert">{{ erro }}</p>
</template>
```

Se a versão adotada for anterior ao `defineModel`, use a prop `modelValue` +
emit `update:modelValue`. Uso: `<CampoTexto v-model="form.nome" id="nome" rotulo="Nome" />`.

## Reatividade: o essencial

| Ferramenta | Uso | Observação |
|---|---|---|
| `ref` | Qualquer valor (primitivo ou objeto); acessa com `.value` no script | Padrão do projeto |
| `reactive` | Objeto com várias chaves | Só objetos; **não** pode ser reatribuído nem desestruturado sem perder reatividade |
| `computed` | Valor **derivado** de outros | Puro, sem efeito colateral; cacheado |
| `watch` | **Efeito** quando algo muda (chamar API, sincronizar storage) | Não use para derivar valor |
| `watchEffect` | Efeito que depende do que lê dentro | Cuidado com dependências ocultas |
| `shallowRef` | Objeto grande trocado por inteiro | Evita reatividade profunda |
| `toRef`/`toRefs` | Manter reatividade ao extrair de `reactive`/props | |

Convenção: **prefira `ref`** em todo lugar; `reactive` só para um objeto de formulário local.

### Armadilhas comuns

| Armadilha | Sintoma | Correção |
|---|---|---|
| Desestruturar `reactive`/props: `const { x } = props` | Valor deixa de atualizar | Usar `props.x`, `toRefs(props)`, ou computed (verificar a regra da versão) |
| Reatribuir `reactive`: `estado = {...}` | Template não muda | `Object.assign(estado, novo)` ou usar `ref` |
| Esquecer `.value` no script | Compara/usa o objeto `ref` | `.value` no script (no template é automático) |
| `watch` que escreve outro estado derivado | Estado fora de sincronia | `computed` |
| Mutar prop (`props.lista.push`) | Efeito colateral no pai, aviso | Emitir evento; ou copiar para estado local |
| `v-for` sem `key` estável, ou com índice | Estado de componente trocado ao reordenar | `:key="item.id"` |
| `v-if` e `v-for` no mesmo elemento | Precedência confusa | `computed` filtrada ou `<template v-for>` |
| Array de `ref` dentro de `ref` sem desembrulhar | `.value` inesperado | Normalizar estrutura |
| Objeto não reativo mutado (`Map`, classe) | Nada re-renderiza | `ref`/`reactive` ou substituir por novo objeto |
| Efeito sem limpar (timer, listener, requisição) | Vazamento, atualização em componente desmontado | `onUnmounted`/`onWatcherCleanup` (conferir) e cancelamento |
| Corrida em `watch` com chamada assíncrona | Resposta antiga sobrescreve a nova | Cancelamento (`AbortController`) ou ignorar resposta velha |

### Corrida em watch assíncrono

```ts
watch(() => props.salaId, async (id, _, onCleanup) => {
  const ctl = new AbortController()
  onCleanup(() => ctl.abort())
  reservas.value = await listarReservas(id, { signal: ctl.signal })
}, { immediate: true })
```

## Composables

Função `useX()` que encapsula **estado + lógica reutilizável**, devolve refs e funções.

```ts
// composables/useCarregamento.ts
export function useCarregamento<T>(fn: (signal: AbortSignal) => Promise<T>) {
  const dados = ref<T | null>(null)
  const erro = ref<string | null>(null)
  const carregando = ref(false)
  let ctl: AbortController | null = null

  async function executar() {
    ctl?.abort()
    ctl = new AbortController()
    carregando.value = true
    erro.value = null
    try {
      dados.value = await fn(ctl.signal)
    } catch (e) {
      if (!ehCancelamento(e)) erro.value = mensagemDeErro(e)
    } finally {
      carregando.value = false
    }
  }
  onUnmounted(() => ctl?.abort())
  return { dados, erro, carregando, executar }
}
```

Regras:

- Nome começa com `use`; arquivo em `composables/`.
- **Quando criar:** lógica com estado usada em 2+ componentes (regra de três vale). Uma
  vez só, deixe no componente.
- Devolva `ref`s/`computed` e funções; não devolva `reactive` desestruturável por engano.
- Chame composables só no `setup` (ou em outro composable), não em callbacks.
- Efeito do composable deve limpar após si (listener, timer, requisição).
- Não esconda I/O: quem chama sabe que há rede (nome e parâmetros mostram).

Os três estados de dado (carregando, erro, vazio) devem existir **em toda tela que carrega
dado**; um composable assim evita repetir.

## Slots e composição

- **Slot** para variar conteúdo (um `Card` com slot padrão e `#acoes`), em vez de dezenas de
  props booleanas.
- Props booleanas que trocam comportamento (`modoX`, `semY`) sinalizam componente fazendo
  coisa demais: divida.
- `provide`/`inject` para dependência de **subárvore** (tema, contexto de formulário), com
  chave tipada (`InjectionKey<T>`); não use como Pinia escondida.

## Template: boas práticas

- Lógica complexa fora do template (`computed`).
- `v-if` x `v-show`: `v-if` para o que raramente muda ou não deve existir; `v-show` para
  alternância frequente (mantém no DOM; atenção a acessibilidade, o elemento fica oculto
  por CSS).
- Eventos: `@click.prevent` para formulários; botão dentro de form **precisa** de `type`.
- `:key` em listas e em elementos que devem ser recriados (troca de usuário).
- Texto visível vem de um lugar (constantes/arquivo de mensagens) quando o Design define
  textos fixos, facilitando conferir a fidelidade.

## Erros comuns

| Erro | Correção |
|---|---|
| `any` em props/emit | Tipos literais; tipos de domínio |
| Filho altera prop | Emitir evento |
| Store para estado de um componente | `ref` local |
| `watch` para calcular | `computed` |
| Composable com efeito sem limpeza | Cancelar/remover em `onUnmounted` |
| `v-for` sem `key` ou com índice em lista mutável | `key` por id |
| `<div @click>` como botão | `<button type="button">` |
| Componente gigante com 400 linhas de template | Extrair subcomponentes por responsabilidade |
