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
  const field = document.querySelector<HTMLElement>(
    '[data-answer-field] textarea:not(.v-textarea__sizer)',
  )
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
  <section aria-labelledby="questions-heading" class="mb-8">
    <h2 id="questions-heading" class="text-h6 mb-2">{{ $t('waves.questions.title') }}</h2>
    <p v-if="questions.length === 0" class="text-body-2">{{ $t('waves.questions.none') }}</p>

    <v-card
      v-for="question in openQuestions"
      :key="question.id"
      variant="outlined"
      class="pa-4 mb-4 open-question"
      :data-testid="`question-${question.id}`"
    >
      <h3 class="text-subtitle-1 font-weight-medium">
        {{ $t('waves.questions.openTitle', { id: question.id }) }}
      </h3>
      <p class="text-body-2 mb-3">
        {{ $t('waves.questions.waitingSince', { age: format.age(question.opened_at) }) }}
      </p>
      <v-form @submit.prevent="send(question)">
        <ol class="question-items">
          <li v-for="item in question.items" :key="item.number" class="mb-4">
            <!-- Agent text is untrusted: shown as plain text, never as HTML (RNF-05). -->
            <p :id="`${question.id}-item-${item.number}`" class="question-text">{{ item.text }}</p>
            <p v-if="item.suggestion" class="text-body-2 text-medium-emphasis">
              {{ $t('waves.questions.suggestion', { text: item.suggestion }) }}
            </p>
            <p v-if="item.origin?.source" class="text-body-2 text-medium-emphasis">
              {{ $t('waves.questions.origin', { source: item.origin.source }) }}
            </p>
            <v-textarea
              v-if="canAnswer && drafts[question.id]"
              v-model="drafts[question.id]![item.number]"
              :label="$t('waves.questions.answerLabel', { n: item.number })"
              :aria-describedby="`${question.id}-item-${item.number}`"
              :error-messages="
                missing[question.id]?.includes(item.number) ? [$t('waves.questions.required')] : []
              "
              rows="2"
              auto-grow
              data-answer-field
            />
          </li>
        </ol>
        <v-alert
          v-if="(missing[question.id]?.length ?? 0) > 0"
          type="error"
          variant="tonal"
          role="alert"
          class="mb-2"
        >
          {{
            $t('waves.questions.missingNumbers', {
              numbers: missing[question.id]?.join(', ') ?? '',
            })
          }}
        </v-alert>
        <ProblemAlert v-if="failures[question.id]" :error="failures[question.id]" class="mb-2" />
        <v-btn
          v-if="canAnswer"
          type="submit"
          color="primary"
          variant="flat"
          :loading="sending === question.id"
        >
          {{ $t('waves.questions.send') }}
        </v-btn>
      </v-form>
    </v-card>

    <v-card
      v-for="question in answeredQuestions"
      :key="question.id"
      variant="outlined"
      class="pa-4 mb-4"
    >
      <h3 class="text-subtitle-1 font-weight-medium">
        {{ $t('waves.questions.answeredTitle', { id: question.id }) }}
      </h3>
      <p class="text-body-2 mb-2">{{ answeredBy(question) }}</p>
      <ol class="question-items">
        <li v-for="item in question.items" :key="item.number" class="mb-2">
          <p class="question-text">{{ item.text }}</p>
          <p class="text-body-2">
            {{ $t('waves.questions.answerText', { text: answerOf(question, item.number) }) }}
          </p>
        </li>
      </ol>
    </v-card>
  </section>
</template>

<style scoped>
.question-items {
  padding-left: 1.25rem;
}

.question-text {
  white-space: pre-wrap;
}
</style>
