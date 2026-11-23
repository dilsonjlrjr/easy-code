<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UIError } from '@/types'

// ER-28-9: decided by kind and Problem Details type, never by text; the api title and detail are shown
// when present (they come in the interface language, Accept-Language).
const props = defineProps<{ error: UIError | null }>()
const { t } = useI18n()

const message = computed(() => {
  const error = props.error
  if (!error) return ''
  if (error.kind === 'problem') {
    if (error.problem_type === 'urn:easycode:problem:not-found') return t('errors.notFound')
    if (error.problem_type === 'urn:easycode:problem:forbidden') return t('errors.forbidden')
    return [error.title, error.detail].filter(Boolean).join(' — ') || t('errors.problem')
  }
  const key = `errors.${error.kind}`
  return t(key) === key ? t('errors.network') : t(key)
})
</script>

<template>
  <p v-if="error" class="error" role="alert">{{ message }}</p>
</template>
