<script setup lang="ts">
import { computed } from 'vue'
import { problemMessageKey } from './problemMessages'
import { isApiError } from '@/shared/api/problemDetails'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/shared/format/useFormat'
import CanvasIcon from './CanvasIcon.vue'

const props = defineProps<{
  error: unknown
  /** Offers "Tentar de novo" (ESP-00 section 6, load error). */
  retry?: boolean
}>()
defineEmits<{ retry: [] }>()

const problem = computed(() => (isApiError(props.error) ? props.error.problem : undefined))
const messageKey = computed(() => problemMessageKey(props.error))
const traceId = computed(() => problem.value?.trace_id)
const format = useFormat()
/** Named extensions some refusals carry (openapi `Problem`): limit, tool, skill, current version. */
const limitBytes = computed(() =>
  typeof problem.value?.limit_bytes === 'number' ? problem.value.limit_bytes : undefined,
)
const extension = (field: string) =>
  computed(() => {
    const value = problem.value?.[field]
    return typeof value === 'string' || typeof value === 'number' ? String(value) : undefined
  })
const tool = extension('tool')
const skill = extension('skill')
const currentVersion = extension('current_version')
const i18n = useI18n()
/** US-44 CA-2: the package refusal says what is wrong, by its `reason` code (as built). */
const packageReason = computed(() => {
  if (!isApiError(props.error) || props.error.slug !== 'invalid-skill-package') return undefined
  const reason = problem.value?.reason
  const key = `skillLibrary.packageReasons.${String(reason)}`
  return typeof reason === 'string' && i18n.te(key) ? i18n.t(key) : problem.value?.detail
})
const findingIds = computed(() => {
  const findings = problem.value?.findings
  return Array.isArray(findings) ? findings.filter((id) => typeof id === 'string') : []
})

async function copyTraceId(): Promise<void> {
  if (traceId.value) await navigator.clipboard.writeText(traceId.value)
}
</script>

<template>
  <!-- ESP-00 ER-00-12: message from the catalog by problem type, with the copyable trace id. -->
  <!-- Look: canvas `.note` in red tones. -->
  <div role="alert" class="note problem-alert">
    <span class="problem-icon"><CanvasIcon name="alert" :stroke-width="2" /></span>
    <div class="problem-body">
      <p>{{ $t(messageKey) }}</p>
      <!-- US-29 CA-1: the schema errors of a rejected artifact (path and message, no content). -->
      <ul v-if="problem?.schema_errors?.length" class="problem-list" data-testid="schema-errors">
        <li v-for="(item, index) in problem.schema_errors" :key="index">
          <span class="mono">{{ item.path || '/' }}</span> · {{ item.message }}
        </li>
      </ul>
      <p v-if="limitBytes" data-testid="problem-limit">
        {{ $t('problems.limit', { limit: format.bytes(limitBytes) }) }}
      </p>
      <p v-if="tool" data-testid="problem-tool">
        {{ $t('problems.tool', { tool }) }}
      </p>
      <p v-if="skill" data-testid="problem-skill">
        {{ $t('problems.skill', { skill }) }}
      </p>
      <p v-if="currentVersion" data-testid="problem-current-version">
        {{ $t('problems.currentVersion', { version: currentVersion }) }}
      </p>
      <p v-if="packageReason" data-testid="problem-detail">{{ packageReason }}</p>
      <!-- US-08 CA-2, US-52 CA-3: the findings behind a refusal (`blocking-finding`), by id. -->
      <p v-if="findingIds.length" data-testid="problem-findings">
        {{ $t('problems.findings', { findings: findingIds.join(', ') }) }}
      </p>
      <p v-if="traceId" class="t2 problem-trace">
        {{ $t('problems.traceId') }}
        <code class="mono">{{ traceId }}</code>
        <button
          type="button"
          class="ibtn"
          :aria-label="$t('problems.copyTraceId')"
          @click="copyTraceId"
        >
          <CanvasIcon name="copy" :size="18" />
        </button>
      </p>
    </div>
    <button v-if="retry" type="button" class="btn" @click="$emit('retry')">
      {{ $t('common.retry') }}
    </button>
  </div>
</template>

<style scoped>
.problem-alert {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.12);
  color: #f1f3ff;
  flex-wrap: wrap;
}

.problem-icon {
  display: flex;
  flex: none;
  color: #fca5a5;
}

.problem-body {
  flex: 1 1 240px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  overflow-wrap: anywhere;
}

.problem-body p {
  margin: 0;
}

.problem-list {
  margin: 0;
  padding-left: 18px;
}

.problem-trace {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}
</style>
