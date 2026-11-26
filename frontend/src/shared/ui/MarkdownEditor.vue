<script setup lang="ts">
import {
  mdiCodeTags,
  mdiFormatBold,
  mdiFormatHeaderPound,
  mdiFormatItalic,
  mdiFormatListBulleted,
  mdiFormatListNumbered,
  mdiFormatQuoteClose,
  mdiLinkVariant,
} from '@mdi/js'
import { computed, nextTick, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import MarkdownContent from './MarkdownContent.vue'

/**
 * Small Markdown editor for the artifact file (US-53): a toolbar for the common marks, keyboard shortcuts
 * (Ctrl/Cmd+B, Ctrl/Cmd+I, Tab indents) and a preview of the body through the sanitizer (RNF-05). The YAML
 * front matter stays in the text and is shown apart in the preview. The textarea keeps the attributes
 * the parent passes (id, aria-*), so its label still names it.
 */
defineOptions({ inheritAttrs: false })
const model = defineModel<string>({ required: true })
defineProps<{ rows?: number }>()

const { t } = useI18n()
const id = useId()
const editor = ref<HTMLTextAreaElement>()
const mode = ref<'write' | 'preview'>('write')
/** Esc right before Tab lets the focus leave the textarea (keyboard users). */
const escaped = ref(false)

const FRONT_MATTER = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/

const parts = computed(() => {
  const match = FRONT_MATTER.exec(model.value)
  if (!match) return { header: '', body: model.value }
  return { header: match[1] ?? '', body: model.value.slice(match[0].length) }
})

type Action = 'bold' | 'italic' | 'heading' | 'bullets' | 'numbers' | 'code' | 'link' | 'quote'

const actions: Array<{ action: Action; icon: string }> = [
  { action: 'bold', icon: mdiFormatBold },
  { action: 'italic', icon: mdiFormatItalic },
  { action: 'heading', icon: mdiFormatHeaderPound },
  { action: 'bullets', icon: mdiFormatListBulleted },
  { action: 'numbers', icon: mdiFormatListNumbered },
  { action: 'quote', icon: mdiFormatQuoteClose },
  { action: 'code', icon: mdiCodeTags },
  { action: 'link', icon: mdiLinkVariant },
]

async function replace(start: number, end: number, text: string, select: [number, number]) {
  const area = editor.value
  if (!area) return
  model.value = model.value.slice(0, start) + text + model.value.slice(end)
  await nextTick()
  area.focus()
  area.setSelectionRange(start + select[0], start + select[1])
}

function wrap(before: string, after: string, placeholder: string) {
  const area = editor.value
  if (!area) return
  const { selectionStart: start, selectionEnd: end } = area
  const selected = model.value.slice(start, end) || placeholder
  void replace(start, end, before + selected + after, [
    before.length,
    before.length + selected.length,
  ])
}

/** Prefixes every selected line (or the line of the cursor). */
function prefixLines(prefix: (index: number) => string) {
  const area = editor.value
  if (!area) return
  const start = model.value.lastIndexOf('\n', area.selectionStart - 1) + 1
  const lineEnd = model.value.indexOf('\n', area.selectionEnd)
  const end = lineEnd < 0 ? model.value.length : lineEnd
  const text = model.value
    .slice(start, end)
    .split('\n')
    .map((line, index) => prefix(index) + line)
    .join('\n')
  void replace(start, end, text, [text.length, text.length])
}

function apply(action: Action) {
  mode.value = 'write'
  switch (action) {
    case 'bold':
      wrap('**', '**', t('markdownEditor.sample.bold'))
      break
    case 'italic':
      wrap('_', '_', t('markdownEditor.sample.italic'))
      break
    case 'code':
      wrap('`', '`', t('markdownEditor.sample.code'))
      break
    case 'link':
      wrap('[', '](https://)', t('markdownEditor.sample.link'))
      break
    case 'heading':
      prefixLines(() => '## ')
      break
    case 'bullets':
      prefixLines(() => '- ')
      break
    case 'numbers':
      prefixLines((index) => `${String(index + 1)}. `)
      break
    case 'quote':
      prefixLines(() => '> ')
      break
  }
}

function onKeydown(event: KeyboardEvent) {
  const shortcut = event.ctrlKey || event.metaKey
  if (shortcut && event.key.toLowerCase() === 'b') {
    event.preventDefault()
    apply('bold')
  } else if (shortcut && event.key.toLowerCase() === 'i') {
    event.preventDefault()
    apply('italic')
  } else if (event.key === 'Tab' && !event.shiftKey && !shortcut && !event.altKey) {
    // Tab indents inside the editor; Esc then Tab (or Shift+Tab) still leaves it (keyboard users).
    if (escaped.value) return
    event.preventDefault()
    const area = event.target as HTMLTextAreaElement
    void replace(area.selectionStart, area.selectionEnd, '  ', [2, 2])
  }
  escaped.value = event.key === 'Escape'
}
</script>

<template>
  <div class="mde">
    <div class="mde-bar">
      <div class="mde-tabs" role="tablist">
        <button
          :id="`${id}-write`"
          type="button"
          role="tab"
          class="mde-tab"
          :aria-selected="mode === 'write'"
          :aria-controls="`${id}-panel`"
          @click="mode = 'write'"
        >
          {{ t('markdownEditor.write') }}
        </button>
        <button
          :id="`${id}-preview`"
          type="button"
          role="tab"
          class="mde-tab"
          :aria-selected="mode === 'preview'"
          :aria-controls="`${id}-panel`"
          data-testid="markdown-editor-preview"
          @click="mode = 'preview'"
        >
          {{ t('markdownEditor.preview') }}
        </button>
      </div>
      <div class="mde-actions" role="toolbar" :aria-label="t('markdownEditor.toolbar')">
        <button
          v-for="item in actions"
          :key="item.action"
          type="button"
          class="mde-btn"
          :title="t(`markdownEditor.action.${item.action}`)"
          :aria-label="t(`markdownEditor.action.${item.action}`)"
          :disabled="mode === 'preview'"
          @click="apply(item.action)"
        >
          <v-icon :icon="item.icon" size="18" />
        </button>
      </div>
    </div>
    <div :id="`${id}-panel`" role="tabpanel" :aria-labelledby="`${id}-${mode}`">
      <!-- eslint-disable-next-line vuejs-accessibility/form-control-has-label -- the parent names it: its label `for` this id and aria-labelledby come through $attrs. -->
      <textarea
        v-show="mode === 'write'"
        ref="editor"
        v-bind="$attrs"
        v-model="model"
        class="fld mono mde-text"
        :rows="rows ?? 14"
        spellcheck="false"
        @keydown="onKeydown"
      />
      <div v-if="mode === 'preview'" class="mde-preview" data-testid="markdown-editor-rendered">
        <details v-if="parts.header" class="mde-header">
          <summary>{{ t('markdownEditor.header') }}</summary>
          <pre class="mono">{{ parts.header }}</pre>
        </details>
        <MarkdownContent v-if="parts.body.trim()" :source="parts.body" />
        <p v-else class="t2">{{ t('markdownEditor.empty') }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mde {
  display: flex;
  flex-direction: column;
  border: 1px solid rgba(140, 150, 255, 0.24);
  border-radius: 10px;
  overflow: hidden;
}

.mde-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  padding: 4px 6px;
  background: rgba(140, 150, 255, 0.08);
  border-bottom: 1px solid rgba(140, 150, 255, 0.24);
}

.mde-tabs,
.mde-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
}

.mde-tab,
.mde-btn {
  min-height: 32px;
  padding: 0 10px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: inherit;
  font: inherit;
  font-size: 13px;
  cursor: pointer;
}

.mde-btn {
  min-width: 32px;
  padding: 0 6px;
}

.mde-tab[aria-selected='true'] {
  background: rgba(140, 150, 255, 0.22);
  font-weight: 600;
}

.mde-tab:hover,
.mde-btn:hover:not(:disabled) {
  background: rgba(140, 150, 255, 0.16);
}

.mde-tab:focus-visible,
.mde-btn:focus-visible {
  outline: 2px solid #8c96ff;
  outline-offset: 1px;
}

.mde-btn:disabled {
  opacity: 0.4;
  cursor: default;
}

.mde-text {
  display: block;
  width: 100%;
  border: 0;
  border-radius: 0;
  resize: vertical;
}

.mde-preview {
  min-height: 280px;
  max-height: 480px;
  overflow-y: auto;
  padding: 12px 16px;
}

.mde-header {
  margin-bottom: 12px;
  font-size: 13px;
}

.mde-header summary {
  cursor: pointer;
}

.mde-header pre {
  margin: 8px 0 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
