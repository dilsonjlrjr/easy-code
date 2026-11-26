<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toUIError } from '@/bridge'
import ErrorMessage from './ErrorMessage.vue'
import type { UIError } from '@/types'

// ER-28-5: the only "action" of the app: open the web screen of the item in the default browser.
const props = defineProps<{
  open: () => Promise<unknown>
  item?: string
  label?: string
  primary?: boolean
  small?: boolean
}>()
const { t } = useI18n()
const error = ref<UIError | null>(null)

async function onClick(): Promise<void> {
  error.value = null
  try {
    await props.open()
  } catch (e) {
    error.value = toUIError(e)
  }
}
</script>

<template>
  <span class="open-web">
    <button
      type="button"
      class="btn"
      :class="{ primary, small }"
      :aria-label="item ? t('common.openWebLabel', { item }) : undefined"
      @click="onClick"
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        aria-hidden="true"
      >
        <path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
      </svg>
      {{ label ?? t('common.openWeb') }}
    </button>
    <ErrorMessage :error="error" />
  </span>
</template>
