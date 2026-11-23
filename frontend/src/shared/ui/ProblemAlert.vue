<script setup lang="ts">
import { mdiContentCopy } from '@mdi/js'
import { computed } from 'vue'
import { problemMessageKey } from './problemMessages'
import { isApiError } from '@/shared/api/problemDetails'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/shared/format/useFormat'

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
  <v-alert type="error" variant="tonal" role="alert" class="problem-alert">
    <p>{{ $t(messageKey) }}</p>
    <!-- US-29 CA-1: the schema errors of a rejected artifact (path and message, no content). -->
    <ul v-if="problem?.schema_errors?.length" class="ml-4 mt-1" data-testid="schema-errors">
      <li v-for="(item, index) in problem.schema_errors" :key="index">
        <span class="monospace">{{ item.path || '/' }}</span> · {{ item.message }}
      </li>
    </ul>
    <p v-if="limitBytes" class="mt-1" data-testid="problem-limit">
      {{ $t('problems.limit', { limit: format.bytes(limitBytes) }) }}
    </p>
    <p v-if="tool" class="mt-1" data-testid="problem-tool">
      {{ $t('problems.tool', { tool }) }}
    </p>
    <p v-if="skill" class="mt-1" data-testid="problem-skill">
      {{ $t('problems.skill', { skill }) }}
    </p>
    <p v-if="currentVersion" class="mt-1" data-testid="problem-current-version">
      {{ $t('problems.currentVersion', { version: currentVersion }) }}
    </p>
    <p v-if="packageReason" class="mt-1" data-testid="problem-detail">{{ packageReason }}</p>
    <!-- US-08 CA-2, US-52 CA-3: the findings behind a refusal (`blocking-finding`), by id. -->
    <p v-if="findingIds.length" class="mt-1" data-testid="problem-findings">
      {{ $t('problems.findings', { findings: findingIds.join(', ') }) }}
    </p>
    <p v-if="traceId" class="text-body-2 mt-2">
      {{ $t('problems.traceId') }}
      <code class="monospace">{{ traceId }}</code>
      <v-btn
        :icon="mdiContentCopy"
        size="small"
        variant="text"
        :aria-label="$t('problems.copyTraceId')"
        @click="copyTraceId"
      />
    </p>
    <template v-if="retry" #append>
      <v-btn variant="outlined" @click="$emit('retry')">{{ $t('common.retry') }}</v-btn>
    </template>
  </v-alert>
</template>
