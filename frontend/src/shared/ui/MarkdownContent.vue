<script setup lang="ts">
import { computed } from 'vue'
import { renderUntrustedMarkdown } from './markdown'

const props = defineProps<{ source: string }>()

const html = computed(() => renderUntrustedMarkdown(props.source))
</script>

<template>
  <!-- Typography: canvas "Artefato" article (headings, `.mono` code, `.tbl` tables, links). -->
  <!-- eslint-disable-next-line vue/no-v-html -- HTML comes only from renderUntrustedMarkdown: raw HTML escaped and sanitized (ER-21-4, RNF-05). -->
  <div class="markdown-content" v-html="html" />
</template>

<style scoped>
.markdown-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  line-height: 22px;
  overflow-wrap: anywhere;
}

.markdown-content :deep(*) {
  margin: 0;
}

.markdown-content :deep(:is(h1, h2, h3, h4, h5, h6)) {
  font-weight: 700;
  color: #f1f3ff;
}

.markdown-content :deep(:is(h1, h2, h3, h4, h5, h6):not(:first-child)) {
  margin-top: 6px;
}

.markdown-content :deep(h1) {
  font-size: 22px;
  line-height: 28px;
}

.markdown-content :deep(h2) {
  font-size: 18px;
  line-height: 24px;
}

.markdown-content :deep(h3) {
  font-size: 16px;
  line-height: 22px;
}

.markdown-content :deep(:is(h4, h5, h6)) {
  font-size: 14px;
  line-height: 20px;
}

.markdown-content :deep(:is(ul, ol)) {
  padding-left: 22px;
}

.markdown-content :deep(li + li) {
  margin-top: 4px;
}

.markdown-content :deep(a) {
  color: #a78bfa;
}

.markdown-content :deep(a:hover) {
  color: #c4b5fd;
}

.markdown-content :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  padding: 1px 6px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.08);
}

.markdown-content :deep(pre) {
  overflow-x: auto;
  padding: 12px 14px;
  border: 1px solid rgba(140, 150, 255, 0.16);
  border-radius: 10px;
  background: rgba(10, 13, 40, 0.8);
  line-height: 20px;
}

.markdown-content :deep(pre code) {
  padding: 0;
  background: transparent;
}

.markdown-content :deep(blockquote) {
  padding: 2px 0 2px 14px;
  border-left: 3px solid rgba(129, 140, 248, 0.45);
  color: #a9b0d6;
}

.markdown-content :deep(hr) {
  border: 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

/* Tables: canvas `.tbl`. */
.markdown-content :deep(table) {
  display: block;
  max-width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
  font-size: 13px;
}

.markdown-content :deep(th) {
  text-align: left;
  font-weight: 500;
  color: #c7cceb;
  padding: 8px 10px 8px 0;
  border-bottom: 1px solid rgba(140, 150, 255, 0.14);
}

.markdown-content :deep(td) {
  padding: 10px 10px 10px 0;
  border-bottom: 1px solid rgba(140, 150, 255, 0.08);
  vertical-align: top;
}

.markdown-content :deep(img) {
  max-width: 100%;
}
</style>
