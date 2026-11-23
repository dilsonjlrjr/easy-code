<script setup lang="ts">
import { nextTick, ref, useId, watch } from 'vue'
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
const keepButton = ref<{ $el: HTMLElement }>()
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
  keepButton.value?.$el.focus()
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
  <v-dialog
    v-model="open"
    max-width="520"
    :persistent="sending"
    :aria-labelledby="`${id}-title`"
    :aria-describedby="`${id}-description`"
    @after-enter="focusKeep"
    @after-leave="restoreFocus"
  >
    <v-card :data-testid="props.testId">
      <v-card-title>
        <h2 :id="`${id}-title`" class="text-h6 text-wrap">{{ props.title }}</h2>
      </v-card-title>
      <v-form @submit.prevent="confirm">
        <v-card-text>
          <div :id="`${id}-description`">
            <p v-for="(line, index) in props.lines" :key="index" class="mb-2">{{ line }}</p>
          </div>
          <slot :failure="failure" />
          <ProblemAlert v-if="failure" :error="failure" class="mt-3" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn ref="keepButton" variant="text" :disabled="sending" @click="open = false">
            {{ props.keepLabel }}
          </v-btn>
          <v-btn
            type="submit"
            :color="props.confirmColor"
            variant="flat"
            :loading="sending"
            :disabled="props.confirmDisabled"
          >
            {{ props.confirmLabel }}
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
