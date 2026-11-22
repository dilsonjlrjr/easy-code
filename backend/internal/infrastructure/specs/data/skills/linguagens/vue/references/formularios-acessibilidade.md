# Formulários, validação e acessibilidade em componentes Vue

Complementa `ux-acessibilidade` (Design define textos, estados e regras de WCAG). Aqui: como
implementar **fiel ao protótipo**.

## Formulário acessível

```vue
<script setup lang="ts">
import { reactive, ref, nextTick } from 'vue'
import { ErroApi } from '@/api/erros'
import { criarReserva } from '@/api/reservas'

const form = reactive({ salaId: '', inicio: '', fim: '' })
const erros = ref<Record<string, string>>({})
const erroGeral = ref('')
const enviando = ref(false)
const resumo = ref<HTMLElement | null>(null)

function validar(): boolean {
  const e: Record<string, string> = {}
  if (!form.salaId) e.salaId = 'Escolha uma sala.'
  if (!form.inicio) e.inicio = 'Informe o início.'
  if (form.inicio && form.fim && form.fim <= form.inicio) e.fim = 'O fim deve ser depois do início.'
  erros.value = e
  return Object.keys(e).length === 0
}

async function enviar() {
  erroGeral.value = ''
  if (!validar()) return await focarResumo()
  enviando.value = true
  try {
    await criarReserva({ ...form })
    /* sucesso: navegar e anunciar na tela de destino */
  } catch (e) {
    if (e instanceof ErroApi) { erros.value = e.campos; erroGeral.value = e.message }
    else erroGeral.value = 'Não foi possível concluir. Tente de novo.'
    await focarResumo()
  } finally { enviando.value = false }
}
async function focarResumo() { await nextTick(); resumo.value?.focus() }
</script>

<template>
  <form novalidate @submit.prevent="enviar">
    <div v-if="erroGeral || Object.keys(erros).length" ref="resumo" tabindex="-1" role="alert">
      <p v-if="erroGeral">{{ erroGeral }}</p>
      <ul>
        <li v-for="(msg, campo) in erros" :key="campo"><a :href="`#${campo}`">{{ msg }}</a></li>
      </ul>
    </div>

    <label for="salaId">Sala</label>
    <select id="salaId" v-model="form.salaId" required
            :aria-invalid="!!erros.salaId" :aria-describedby="erros.salaId ? 'salaId-erro' : undefined">
      <option value="">Selecione</option>
      <!-- opções -->
    </select>
    <p v-if="erros.salaId" id="salaId-erro">{{ erros.salaId }}</p>

    <!-- inicio, fim: mesmo padrão -->
    <button type="submit" :aria-busy="enviando">{{ enviando ? 'Reservando...' : 'Reservar' }}</button>
  </form>
</template>
```

### Regras

| Regra | Como |
|---|---|
| **Todo campo tem `label`** visível ligado (`for`/`id`) | Placeholder **não** substitui label |
| `novalidate` no form + validação própria | Mensagens do Design, não as do navegador |
| Erro **por campo** ligado ao campo | `aria-invalid` + `aria-describedby` apontando para a mensagem |
| Erro com **texto**, não só cor/ícone | Contraste e texto explícito (WCAG) |
| **Resumo de erros** no topo ao enviar com falha | `role="alert"`, recebe foco; links para os campos |
| Foco vai para o resumo (ou o 1º campo inválido) | Teclado e leitor de tela percebem o problema |
| Tipos de input corretos | `type="email"`, `inputmode`, `autocomplete` (`autocomplete="email"`) |
| Obrigatório indicado em **texto** | "(obrigatório)" ou convenção do Design, não só asterisco colorido |
| Botão de envio **não** fica desabilitado sem explicar | Prefira habilitado e validar; durante envio, `aria-busy` e texto "Reservando..." |
| Impedir envio duplo | Ignorar novo envio enquanto `enviando` |
| Preservar o digitado após erro | Não limpar o formulário |
| Grupos relacionados | `fieldset` + `legend` (rádios, datas início/fim) |

### Validação: onde e quando

- **No front: conforto** (feedback rápido). **No backend: verdade.** Toda regra do front
  existe também no backend; o erro do backend (`ErroApi.campos`) mostra no mesmo lugar.
- **Quando mostrar:** ao **enviar** e, depois do primeiro erro, ao sair do campo (blur) e ao
  corrigir. Não gritar enquanto a pessoa ainda digita.
- Mensagens **do Design** (texto único, orientadas à ação: "Informe o início", não "Campo
  inválido"). Centralize em um arquivo para conferir fidelidade.
- Mesma regra em lugar só: função de validação pura testada (`validarReserva(form)`).
- Bibliotecas de validação por esquema (Zod, Valibot, VeeValidate; conferir) são opcionais
  em aplicação pequena; só se reduzirem código. Um `validar()` simples e testado costuma bastar.

### Mapear erro do backend para campos

Problem Details com lista de erros por campo vira `Record<campo, mensagem>`; campo
desconhecido vai para o resumo. Nomes de campo da API ≠ nomes do formulário? Faça o
mapeamento em um lugar (`api/reservas.ts`), não em cada tela.

## Acessibilidade em componentes

### Semântica primeiro

| Quer | Use | Não use |
|---|---|---|
| Ação | `<button type="button">` | `<div @click>` / `<span @click>` |
| Navegação | `<a>`/`<RouterLink>` | botão que faz `push` |
| Lista | `<ul><li>` | `div`s empilhadas |
| Tabela de dados | `<table>`, `<th scope>`, `<caption>` | grade de `div`s |
| Título | `<h1>`..`<h6>` em ordem | negrito grande em `div` |
| Regiões | `<header> <nav> <main> <footer>` | `div` genérico |

ARIA só quando o HTML nativo não resolve. Primeira regra do ARIA: não use ARIA se existe
elemento nativo. `role`/`aria-*` errados pioram a experiência.

### Foco

- **Foco visível sempre** (`:focus-visible` com contraste); nunca `outline: none` sem
  substituto.
- **Mudança de rota:** foco no `<h1>` da nova tela + título atualizado (ver
  `roteamento-estado.md`).
- **Ordem do foco = ordem visual.** Sem `tabindex` positivo. `tabindex="-1"` só para alvos de
  foco programático (títulos, resumos de erro).
- **Após ação que remove o elemento** (excluir item da lista), mover o foco para um lugar
  lógico (próximo item, ou o título da lista), não perder para o `body`.

### Modal / diálogo

Preferir o elemento nativo `<dialog>` com `showModal()` (foco preso e `Esc` fecham
nativamente; conferir suporte nos navegadores-alvo), ou implementar:

```vue
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
const emit = defineEmits<{ fechar: [] }>()
const dialogo = ref<HTMLDialogElement | null>(null)
let anterior: HTMLElement | null = null

onMounted(() => {
  anterior = document.activeElement as HTMLElement | null
  dialogo.value?.showModal()                       // foco entra no diálogo
})
onUnmounted(() => anterior?.focus())               // devolve o foco a quem abriu
</script>
<template>
  <dialog ref="dialogo" aria-labelledby="titulo-dialogo" @close="emit('fechar')">
    <h2 id="titulo-dialogo"><slot name="titulo" /></h2>
    <slot />
    <button type="button" @click="dialogo?.close()">Fechar</button>
  </dialog>
</template>
```

Checklist do modal: título (`aria-labelledby`), foco entra, **fica preso**, `Esc` fecha,
foco **volta** ao botão que abriu, fundo inerte, botão de fechar com texto acessível.

### Anúncios (regiões vivas)

Mudança que não move o foco (sucesso, "salvo", carregou N resultados) precisa de **anúncio**.

```vue
<!-- App.vue: uma região viva global, presente desde o carregamento -->
<div class="sr-only" role="status" aria-live="polite">{{ mensagemAnuncio }}</div>
```

```ts
// composables/useAnuncio.ts (estado simples, singleton em módulo)
export const mensagemAnuncio = ref('')
export function anunciar(texto: string) {
  mensagemAnuncio.value = ''
  setTimeout(() => (mensagemAnuncio.value = texto), 50)   // força releitura de texto igual
}
```

- `role="status"`/`aria-live="polite"` para informação; `role="alert"` (assertivo) só para
  erro que exige ação imediata.
- A região **existe antes** de receber o texto; não crie-e-preencha ao mesmo tempo.
- Estado de carregamento: texto ("Carregando reservas...") em região viva e `aria-busy` na
  área; não apenas um spinner decorativo.
- **Estados da tela** (carregando, vazio, erro, sucesso) com texto, iguais ao protótipo.

### Outros pontos

- Imagens informativas com `alt`; decorativas com `alt=""`.
- Alvos de toque com tamanho do Design (mínimo definido pelo WCAG 2.2 / tokens).
- Não dependa de hover; tudo por teclado.
- Contraste vem dos tokens do Design; não sobrescreva cor "no olho".
- Respeitar `prefers-reduced-motion` em animações.
- Rótulo do botão de ícone: `aria-label` ou texto oculto visualmente.
- Datas/horas: use `<input type="date|time">` ou o componente do Design com teclado
  completo; formatação no padrão pt-BR via `Intl.DateTimeFormat`.

### Testar acessibilidade

- Teste de componente: consultar por **papel e nome acessível** (`getByRole('button',
  { name: 'Reservar' })`) já valida a semântica. Ver `testes-vitest.md`.
- Varredura automática (axe-core, open source) no e2e do QA (`playwright-e2e`,
  `acessibilidade-axe`); cobre uma parte, não substitui teste de teclado.
- Teste manual curto: percorrer a tela só com `Tab`/`Shift+Tab`/`Enter`/`Esc`.

## Erros comuns

| Erro | Efeito | Correção |
|---|---|---|
| Placeholder no lugar de label | Some ao digitar; leitor de tela perde | `<label>` visível |
| Erro só em vermelho | Não perceptível | Texto + ligação `aria-describedby` |
| Desabilitar o botão sem explicar | Usuário preso sem saber o porquê | Habilitado + mensagens |
| `div` clicável | Sem teclado/leitor de tela | `button`/`a` |
| Modal sem foco preso/retorno | Perde-se na tela | `<dialog>`/gestão de foco |
| Sucesso silencioso | Leitor de tela não sabe | Região viva `status` |
| Validar só no front | Burlável | Backend valida; mostrar seu erro por campo |
| Limpar o formulário após erro | Retrabalho | Preservar valores |
| `tabindex` positivo | Ordem de foco caótica | Ordem do DOM |
| Foco perdido ao trocar de rota | Leitor de tela desorientado | Foco no `h1` + título |
