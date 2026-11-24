<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import ProblemAlert from './ProblemAlert.vue'

/**
 * Confirmation of a lifecycle action (ESP-18 PC-27, ESP-06 section 4): it says the effect before
 * anything is sent. The confirm button is never the initial focus (ESP-18 section 8); Esc closes it
 * and the focus goes back to the button that opened it. One Idempotency-Key per opening.
 */
const props = withDefaults(
  defineProps<{
    title: string
    /** Paragraphs that describe the effect; they are the dialog's accessible description. */
    lines: string[]
    keepLabel: string
    confirmLabel: string
    confirmColor?: string
    /** Extra condition of the confirm button (a typed name, for example). */
    confirmDisabled?: boolean
    testId?: string
    /** Sends the command and waits for it; throws the refusal, which the dialog shows. */
    submit: (idempotencyKey: string) => Promise<void>
  }>(),
  { confirmColor: 'error', confirmDisabled: false, testId: undefined },
)

const open = defineModel<boolean>({ required: true })
const id = useId()
const sending = ref(false)
const failure = ref<unknown>()
const keepButton = ref<HTMLButtonElement>()
/** Canvas buttons: a destructive confirmation reads in red (`.btn-d`); the others are primary. */
const confirmClass = computed(() => (props.confirmColor === 'error' ? 'btn btn-d' : 'btn btn-p'))
let key = newIdempotencyKey()
let opener: HTMLElement | undefined

watch(open, (value) => {
  if (value) {
    failure.value = undefined
    key = newIdempotencyKey()
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : undefined
  }
})

/** The focus goes back to the button that opened the dialog, when it is still on the page. */
function restoreFocus(): void {
  if (opener?.isConnected) opener.focus()
  opener = undefined
}

async function focusKeep(): Promise<void> {
  await nextTick()
  keepButton.value?.focus()
}

async function confirm(): Promise<void> {
  if (props.confirmDisabled) return
  sending.value = true
  failure.value = undefined
  try {
    await props.submit(key)
    open.value = false
  } catch (error) {
    failure.value = error
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <!-- Look: canvas `.dlg` (Onda.dc.html). -->
  <v-dialog
    v-model="open"
    max-width="460"
    :persistent="sending"
    :aria-labelledby="`${id}-title`"
    :aria-describedby="`${id}-description`"
    @after-enter="focusKeep"
    @after-leave="restoreFocus"
  >
    <v-card class="dlg-card" :data-testid="props.testId">
      <h2 :id="`${id}-title`">{{ props.title }}</h2>
      <form class="dlg-form" @submit.prevent="confirm">
        <div :id="`${id}-description`" class="dlg-description">
          <p v-for="(line, index) in props.lines" :key="index">{{ line }}</p>
        </div>
        <slot :failure="failure" />
        <ProblemAlert v-if="failure" :error="failure" />
        <div class="dlg-actions">
          <button
            ref="keepButton"
            type="button"
            class="btn"
            :disabled="sending"
            @click="open = false"
          >
            {{ props.keepLabel }}
          </button>
          <button
            type="submit"
            :class="confirmClass"
            :disabled="sending || props.confirmDisabled"
            :aria-busy="sending || undefined"
          >
            <v-progress-circular v-if="sending" indeterminate size="16" width="2" />
            {{ props.confirmLabel }}
          </button>
        </div>
      </form>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.dlg-card {
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  background: #151b45;
}

.dlg-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.dlg-description {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dlg-description p {
  margin: 0;
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
