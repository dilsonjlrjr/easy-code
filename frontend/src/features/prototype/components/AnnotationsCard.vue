<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RecordedPrototypeAnnotation } from '../api/prototypeApi'
import { MAX_ANNOTATIONS, MAX_COMMENT, type DraftAnnotation } from '../composables/annotationDrafts'

/**
 * US-57: the annotations of the prototype. The approver of an open gate.design round marks pins and writes
 * a comment for each one; they stay only in this tab (CA-4) until "Enviar ao Designer", which is the
 * rejection of the gate with them (PD-25 option a). The annotations of decided rounds are read only (CA-3).
 * Every comment is shown as text (RN-2).
 */
const props = defineProps<{
  canAnnotate: boolean
  annotating: boolean
  /** The point marked on the frame waiting for its comment; null = an annotation without a point. */
  pending: { x: number; y: number } | null | undefined
  drafts: DraftAnnotation[]
  recorded: Array<{ round: number; items: RecordedPrototypeAnnotation[] }>
  screenName: (id: string) => string
}>()

const emit = defineEmits<{
  save: [comment: string]
  cancel: []
  withoutPoint: []
  update: [id: number, comment: string]
  remove: [id: number]
  send: []
}>()

const { t } = useI18n()
const comment = ref('')
const field = ref<HTMLTextAreaElement>()
const full = computed(() => props.drafts.length >= MAX_ANNOTATIONS)

watch(
  () => props.pending,
  (value) => {
    if (value === undefined) return
    comment.value = ''
    void nextTick(() => field.value?.focus())
  },
)

function save(): void {
  if (comment.value.trim() === '') {
    field.value?.focus()
    return
  }
  emit('save', comment.value)
  comment.value = ''
}

function where(item: { screen: string; state: string | null; x: number | null }): string {
  const parts = [`${item.screen} ${props.screenName(item.screen)}`.trim()]
  if (item.state) parts.push(item.state)
  if (item.x === null) parts.push(t('prototype.annotations.noPoint'))
  return parts.join(' · ')
}
</script>

<template>
  <section class="card side-card notes-card" aria-labelledby="prototype-annotations-heading">
    <h2 id="prototype-annotations-heading" class="side-title">
      {{ $t('prototype.annotations.title') }}
    </h2>

    <template v-if="canAnnotate">
      <p v-if="drafts.length === 0 && pending === undefined" class="t2 fine">
        {{ $t('prototype.annotations.empty') }}
      </p>
      <form v-if="pending !== undefined" class="note-form" @submit.prevent="save">
        <label for="prototype-annotation-comment" class="field">
          <span class="lb">
            {{
              pending === null
                ? $t('prototype.annotations.commentWithoutPoint')
                : $t('prototype.annotations.comment', { n: drafts.length + 1 })
            }}
          </span>
          <textarea
            id="prototype-annotation-comment"
            ref="field"
            v-model="comment"
            class="fld note-text"
            rows="3"
            :maxlength="MAX_COMMENT"
            :placeholder="$t('prototype.annotations.placeholder')"
            data-testid="annotation-comment"
          ></textarea>
        </label>
        <span class="note-actions">
          <button type="submit" class="btn btn-p" data-testid="annotation-save">
            {{ $t('prototype.annotations.save') }}
          </button>
          <button type="button" class="btn" @click="emit('cancel')">
            {{ $t('common.cancel') }}
          </button>
        </span>
      </form>
      <button
        v-else-if="annotating && !full"
        type="button"
        class="ib"
        data-testid="annotation-without-point"
        @click="emit('withoutPoint')"
      >
        {{ $t('prototype.annotations.withoutPoint') }}
      </button>
      <p v-if="full" class="t2 fine">
        {{ $t('prototype.annotations.full', { n: MAX_ANNOTATIONS }) }}
      </p>

      <ol v-if="drafts.length > 0" class="plain-list note-list" data-testid="annotation-drafts">
        <li v-for="(draft, index) in drafts" :key="draft.id" class="note-item">
          <span class="pin-static" aria-hidden="true">{{ index + 1 }}</span>
          <span class="note-body">
            <span class="t2 note-where">
              {{ where(draft) }}
            </span>
            <label :for="`prototype-draft-${draft.id}`" class="field">
              <span class="sr-only">{{
                $t('prototype.annotations.editComment', { n: index + 1 })
              }}</span>
              <textarea
                :id="`prototype-draft-${draft.id}`"
                class="fld note-text"
                rows="2"
                :maxlength="MAX_COMMENT"
                :value="draft.comment"
                @input="emit('update', draft.id, ($event.target as HTMLTextAreaElement).value)"
              ></textarea>
            </label>
            <button
              type="button"
              class="ib note-remove"
              :aria-label="$t('prototype.annotations.removeNumber', { n: index + 1 })"
              @click="emit('remove', draft.id)"
            >
              {{ $t('prototype.annotations.remove') }}
            </button>
          </span>
        </li>
      </ol>
      <p class="t2 fine">{{ $t('prototype.annotations.onlyYou') }}</p>
      <button
        type="button"
        class="btn btn-p"
        :disabled="drafts.length === 0"
        data-testid="annotation-send"
        @click="emit('send')"
      >
        {{ $t('prototype.annotations.send') }}
      </button>
    </template>
    <p v-else-if="recorded.length === 0" class="t2 fine" data-testid="annotations-none">
      {{ $t('prototype.annotations.none') }}
    </p>

    <div
      v-for="group in recorded"
      :key="group.round"
      class="recorded"
      data-testid="annotations-recorded"
    >
      <h3 class="lb">{{ $t('prototype.annotations.ofRound', { round: group.round }) }}</h3>
      <ol class="plain-list note-list">
        <li v-for="item in group.items" :key="item.number" class="note-item">
          <span class="pin-static pin-recorded" aria-hidden="true">{{ item.number }}</span>
          <span class="note-body">
            <span class="t2 note-where">
              {{ $t('prototype.annotations.number', { n: item.number }) }} ·
              {{ where(item) }}
            </span>
            <span class="note-comment">{{ item.comment }}</span>
          </span>
        </li>
      </ol>
    </div>
  </section>
</template>

<style scoped>
.side-card {
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.side-title {
  margin: 0 0 4px;
  font-size: 16px;
  line-height: 24px;
  font-weight: 700;
}

.fine {
  margin: 0;
  font-size: 13px;
}

.note-form,
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.note-text {
  min-height: 64px;
  padding: 8px 12px;
  resize: vertical;
}

.note-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.note-list {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.note-item {
  display: flex;
  gap: 8px;
  font-size: 13px;
  align-items: flex-start;
}

.note-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.note-where {
  font-size: 12px;
}

.note-comment {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.note-remove {
  align-self: flex-start;
  min-height: 44px;
}

.pin-static {
  width: 22px;
  height: 22px;
  flex: none;
  border-radius: 11px;
  background: #a855f7;
  color: #ffffff;
  font-size: 12px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
}

.pin-recorded {
  background: #6366f1;
}

.recorded {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.recorded h3 {
  margin: 0;
}
</style>
