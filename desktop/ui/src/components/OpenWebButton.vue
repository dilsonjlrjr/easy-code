<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toUIError } from '@/bridge'
import ErrorMessage from './ErrorMessage.vue'
import type { UIError } from '@/types'

// ER-28-5: the only "action" of the app: open the web screen of the item in the default browser.
const props = defineProps<{ open: () => Promise<unknown>; item?: string; label?: string }>()
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
      class="link-button external"
      :aria-label="item ? t('common.openWebLabel', { item }) : undefined"
      @click="onClick"
    >
      {{ label ?? t('common.openWeb') }}
    </button>
    <ErrorMessage :error="error" />
  </span>
</template>
