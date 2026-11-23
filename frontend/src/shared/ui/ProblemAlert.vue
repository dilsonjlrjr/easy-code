<script setup lang="ts">
import { mdiContentCopy } from '@mdi/js'
import { computed } from 'vue'
import { problemMessageKey } from './problemMessages'
import { isApiError } from '@/shared/api/problemDetails'

const props = defineProps<{
  error: unknown
  /** Offers "Tentar de novo" (ESP-00 section 6, load error). */
  retry?: boolean
}>()
defineEmits<{ retry: [] }>()

const problem = computed(() => (isApiError(props.error) ? props.error.problem : undefined))
const messageKey = computed(() => problemMessageKey(props.error))
const traceId = computed(() => problem.value?.trace_id)
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
