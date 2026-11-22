<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'

/**
 * US-14, ESP-18 section 4 (PC-27): the strong confirmation of "cancel": it says the action is final
 * and that the call in progress is lost and charged. The confirm button is never the initial focus
 * (ESP-18 section 8); Esc closes it and the focus goes back to the button that opened it.
 */
const props = defineProps<{
  waveId: string
  /** Sends the command and waits for it; throws the refusal (409 shows the real state). */
  submit: (idempotencyKey: string) => Promise<void>
}>()

const open = defineModel<boolean>({ required: true })
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
    aria-labelledby="cancel-wave-title"
    aria-describedby="cancel-wave-description"
    @after-enter="focusKeep"
    @after-leave="restoreFocus"
  >
    <v-card data-testid="cancel-wave-dialog">
      <v-card-title>
        <h2 id="cancel-wave-title" class="text-h6">
          {{ $t('waves.cancel.title', { wave: props.waveId }) }}
        </h2>
      </v-card-title>
      <v-card-text>
        <div id="cancel-wave-description">
          <p class="mb-2">{{ $t('waves.cancel.final') }}</p>
          <p>{{ $t('waves.cancel.lostCall') }}</p>
        </div>
        <ProblemAlert v-if="failure" :error="failure" class="mt-3" />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn ref="keepButton" variant="text" :disabled="sending" @click="open = false">
          {{ $t('waves.cancel.keep') }}
        </v-btn>
        <v-btn color="error" variant="flat" :loading="sending" @click="confirm">
          {{ $t('waves.cancel.confirm') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
