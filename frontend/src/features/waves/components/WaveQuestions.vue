<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { useFormat } from '@/shared/format/useFormat'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Question } from '../api/wavesApi'

const props = defineProps<{
  questions: Question[]
  canAnswer: boolean
  /** Focus the first answer only when the page opens waiting, never on a live change (ESP-18 §8). */
  focusOnOpen: boolean
  submit: (
    question: Question,
    answers: Array<{ number: number; text: string }>,
    key: string,
  ) => Promise<void>
}>()

const { t } = useI18n()
const format = useFormat()
const drafts = reactive<Record<string, Record<number, string>>>({})
const missing = ref<Record<string, number[]>>({})
const sending = ref<string>()
const failures = ref<Record<string, unknown>>({})
const keys = new Map<string, string>()

const openQuestions = computed(() => props.questions.filter((question) => !question.answer))
const answeredQuestions = computed(() => props.questions.filter((question) => question.answer))

watch(
  openQuestions,
  (questions) => {
    for (const question of questions) {
      drafts[question.id] ??= Object.fromEntries(question.items.map((item) => [item.number, '']))
    }
  },
  { immediate: true },
)

onMounted(async () => {
  if (!props.focusOnOpen || !props.canAnswer) return
  await nextTick()
  const field = document.querySelector<HTMLElement>('textarea[data-answer-field]')
  field?.focus()
})

function keyFor(question: Question): string {
  let key = keys.get(question.id)
  if (!key) {
    key = newIdempotencyKey()
    keys.set(question.id, key)
  }
  return key
}

/** CA-18-7: sending is blocked while any numbered question has no answer; the numbers are shown. */
async function send(question: Question): Promise<void> {
  const draft = drafts[question.id] ?? {}
  const empty = question.items.filter((item) => (draft[item.number] ?? '').trim() === '')
  missing.value = { ...missing.value, [question.id]: empty.map((item) => item.number) }
  if (empty.length > 0) return
  sending.value = question.id
  failures.value = { ...failures.value, [question.id]: undefined }
  try {
    await props.submit(
      question,
      question.items.map((item) => ({
        number: item.number,
        text: (draft[item.number] ?? '').trim(),
      })),
      keyFor(question),
    )
  } catch (error) {
    failures.value = { ...failures.value, [question.id]: error }
  } finally {
    sending.value = undefined
  }
}

function answerOf(question: Question, number: number): string {
  return question.answer?.answers?.find((answer) => answer.number === number)?.text ?? ''
}

function answeredBy(question: Question): string {
  const record = question.answer?.record
  return t('waves.questions.answeredBy', {
    who: record?.per?.name ?? record?.per?.id ?? '',
    when: format.dateTime(record?.when),
    origin: record?.origin ? t(`waves.origin.${record.origin}`) : t('waves.origin.web'),
  })
}
</script>

<template>
  <!-- Look: a canvas `.card` (Onda.dc.html), with `.li` rows and `.fld` answers. -->
  <section aria-labelledby="questions-heading" class="card questions-card">
    <h2 id="questions-heading" class="card-title">{{ $t('waves.questions.title') }}</h2>
    <p v-if="questions.length === 0" class="t2 card-text">{{ $t('waves.questions.none') }}</p>

    <div
      v-for="question in openQuestions"
      :key="question.id"
      class="question open-question"
      :data-testid="`question-${question.id}`"
    >
      <div class="question-head">
        <h3>{{ $t('waves.questions.openTitle', { id: question.id }) }}</h3>
        <span class="t2 question-meta">
          {{ $t('waves.questions.waitingSince', { age: format.age(question.opened_at) }) }}
        </span>
      </div>
      <form class="question-form" @submit.prevent="send(question)">
        <ol class="question-items">
          <li v-for="item in question.items" :key="item.number" class="question-item">
            <span class="item-number" aria-hidden="true">{{ item.number }}</span>
            <div class="item-body">
              <!-- Agent text is untrusted: shown as plain text, never as HTML (RNF-05). -->
              <p :id="`${question.id}-item-${item.number}`" class="question-text">
                {{ item.text }}
              </p>
              <p v-if="item.suggestion" class="t2 item-note">
                {{ $t('waves.questions.suggestion', { text: item.suggestion }) }}
              </p>
              <p v-if="item.origin?.source" class="t2 item-note">
                {{ $t('waves.questions.origin', { source: item.origin.source }) }}
              </p>
              <template v-if="canAnswer && drafts[question.id]">
                <label :for="`${question.id}-answer-${item.number}`" class="field">
                  <span class="field-label">
                    {{ $t('waves.questions.answerLabel', { n: item.number }) }}
                  </span>
                  <textarea
                    :id="`${question.id}-answer-${item.number}`"
                    v-model="drafts[question.id]![item.number]"
                    class="fld"
                    rows="2"
                    :aria-invalid="missing[question.id]?.includes(item.number) || undefined"
                    :aria-describedby="
                      missing[question.id]?.includes(item.number)
                        ? `${question.id}-item-${item.number} ${question.id}-error-${item.number}`
                        : `${question.id}-item-${item.number}`
                    "
                    data-answer-field
                  />
                </label>
                <span
                  v-if="missing[question.id]?.includes(item.number)"
                  :id="`${question.id}-error-${item.number}`"
                  class="field-error"
                >
                  {{ $t('waves.questions.required') }}
                </span>
              </template>
            </div>
          </li>
        </ol>
        <div v-if="(missing[question.id]?.length ?? 0) > 0" class="note n-crit" role="alert">
          {{
            $t('waves.questions.missingNumbers', {
              numbers: missing[question.id]?.join(', ') ?? '',
            })
          }}
        </div>
        <ProblemAlert v-if="failures[question.id]" :error="failures[question.id]" />
        <div v-if="canAnswer">
          <button
            type="submit"
            class="btn btn-p"
            :disabled="sending === question.id"
            :aria-busy="sending === question.id || undefined"
          >
            <v-progress-circular v-if="sending === question.id" indeterminate size="16" width="2" />
            {{ $t('waves.questions.send') }}
          </button>
        </div>
      </form>
    </div>

    <div v-for="question in answeredQuestions" :key="question.id" class="question">
      <div class="question-head">
        <h3>{{ $t('waves.questions.answeredTitle', { id: question.id }) }}</h3>
        <span class="t2 question-meta">{{ answeredBy(question) }}</span>
      </div>
      <ol class="question-items">
        <li v-for="item in question.items" :key="item.number" class="question-item">
          <span class="item-number item-number-done" aria-hidden="true">{{ item.number }}</span>
          <div class="item-body">
            <p class="question-text">{{ item.text }}</p>
            <p class="t2 item-note">
              {{ $t('waves.questions.answerText', { text: answerOf(question, item.number) }) }}
            </p>
          </div>
        </li>
      </ol>
    </div>
  </section>
</template>

<style scoped>
.questions-card {
  padding: 18px 22px;
}

.card-title {
  margin-bottom: 4px;
}

.card-text {
  margin: 0;
}

.question {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.question-head {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

h3 {
  margin: 0;
  font-size: 15px;
  line-height: 20px;
  font-weight: 600;
}

.question-meta {
  font-size: 13px;
}

.question-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.question-items {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.question-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.item-number {
  width: 24px;
  height: 24px;
  border-radius: 12px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
  background: #6366f1;
  color: #ffffff;
}

.item-number-done {
  background: #15803d;
}

.item-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.item-body p {
  margin: 0;
}

.question-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.item-note {
  font-size: 13px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 4px;
}

.field-label {
  font-weight: 600;
}

.field-error {
  color: #fca5a5;
  font-size: 12px;
}

.fld[aria-invalid='true'] {
  border-color: #fca5a5;
}

.note {
  margin: 0;
}

.n-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.12);
}
</style>
