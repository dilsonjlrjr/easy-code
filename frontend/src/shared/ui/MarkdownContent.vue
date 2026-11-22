<script setup lang="ts">
import { computed } from 'vue'
import { renderUntrustedMarkdown } from './markdown'

const props = defineProps<{ source: string }>()

const html = computed(() => renderUntrustedMarkdown(props.source))
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- HTML comes only from renderUntrustedMarkdown: raw HTML escaped and sanitized (ER-21-4, RNF-05). -->
  <div class="markdown-content" v-html="html" />
</template>

<style scoped>
.markdown-content :deep(pre) {
  overflow-x: auto;
  padding: 0.75rem;
  background: rgb(var(--v-theme-on-surface), 0.05);
}

.markdown-content :deep(table) {
  display: block;
  overflow-x: auto;
  border-collapse: collapse;
}

.markdown-content :deep(th),
.markdown-content :deep(td) {
  border: 1px solid rgb(var(--v-theme-on-surface), 0.2);
  padding: 0.25rem 0.5rem;
}
</style>
