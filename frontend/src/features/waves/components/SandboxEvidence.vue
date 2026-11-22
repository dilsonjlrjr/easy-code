<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useFormat } from '@/shared/format/useFormat'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { SandboxExecution } from '../api/wavesApi'
import {
  endedByLimit,
  executionOutcome,
  outputFileName,
  outputIsPartial,
} from '../composables/sandboxEvidence'

/**
 * US-41, ESP-18 "Evidência", ESP-14 ER-14-9..11: each command run in the sandbox with exit code,
 * duration, end reason, test counts and the start of its output; the full output is downloaded as a
 * file. Output is untrusted content (RNF-05): plain text in a <pre>, never HTML.
 */
const props = defineProps<{
  waveId: string
  executions: SandboxExecution[]
  error?: unknown
  download: (execution: SandboxExecution) => Promise<string>
}>()
defineEmits<{ retry: [] }>()

const { t } = useI18n()
const format = useFormat()
const { message, announce } = useAnnouncer()
const downloadFailure = ref<unknown>()
const downloading = ref<string>()

const limitCount = computed(() => props.executions.filter(endedByLimit).length)

function outcomeText(execution: SandboxExecution): string {
  const outcome = executionOutcome(execution)
  if (outcome === 'limit') return t(`evidence.endReasons.${execution.end_reason}`)
  if (outcome === 'failed') {
    return t('evidence.outcome.failed', {
      code: execution.exit_code ?? t('evidence.noExitCode'),
    })
  }
  return t(`evidence.outcome.${outcome}`)
}

async function downloadOutput(execution: SandboxExecution, position: number): Promise<void> {
  downloadFailure.value = undefined
  downloading.value = execution.id
  try {
    const text = await props.download(execution)
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }))
    const link = document.createElement('a')
    link.href = url
    link.download = outputFileName(props.waveId, position)
    link.click()
    URL.revokeObjectURL(url)
    announce(t('evidence.downloaded', { n: position }))
  } catch (caught) {
    downloadFailure.value = caught
  } finally {
    downloading.value = undefined
  }
}
</script>

<template>
  <section id="evidence" aria-labelledby="evidence-heading" class="mb-8">
    <h2 id="evidence-heading" class="text-h6 mb-2" tabindex="-1">
      {{ $t('evidence.title') }}
    </h2>
    <ProblemAlert v-if="error" :error="error" retry @retry="$emit('retry')" />
    <p v-else-if="executions.length === 0" class="text-body-2" data-testid="evidence-empty">
      {{ $t('evidence.none') }}
    </p>
    <template v-else>
      <!-- RNF-03: the sandbox stopped some commands at a resource limit. -->
      <v-alert
        v-if="limitCount > 0"
        type="warning"
        variant="tonal"
        class="mb-4"
        data-testid="evidence-limits"
      >
        {{ $t('evidence.limitsNotice', { count: limitCount }) }}
      </v-alert>
      <div role="status" class="sr-only">{{ message }}</div>
      <ProblemAlert v-if="downloadFailure" :error="downloadFailure" class="mb-2" />
      <ol class="execution-list" data-testid="evidence-list">
        <li
          v-for="(execution, index) in executions"
          :key="execution.id"
          :data-testid="`execution-${index + 1}`"
          class="execution-item"
        >
          <h3 class="text-subtitle-1 font-weight-medium">
            {{
              $t('evidence.commandHeading', {
                n: index + 1,
                category: $t(`evidence.categories.${execution.category}`),
              })
            }}
            · <span data-testid="execution-outcome">{{ outcomeText(execution) }}</span>
          </h3>
          <p class="monospace command-line">{{ execution.command }}</p>
          <dl class="summary-list mt-2">
            <dt>{{ $t('evidence.exitCode') }}</dt>
            <dd>{{ execution.exit_code ?? $t('evidence.noExitCode') }}</dd>
            <dt>{{ $t('evidence.duration') }}</dt>
            <dd>{{ format.duration(execution.duration_ms) }}</dd>
            <dt>{{ $t('evidence.endReason') }}</dt>
            <dd>{{ $t(`evidence.endReasons.${execution.end_reason}`) }}</dd>
            <dt>{{ $t('evidence.started') }}</dt>
            <dd>{{ format.dateTime(execution.start) }}</dd>
            <template v-if="execution.tests">
              <dt>{{ $t('evidence.tests') }}</dt>
              <dd data-testid="execution-tests">
                {{
                  $t('evidence.testCounts', {
                    total: format.integer(execution.tests.total),
                    passed: format.integer(execution.tests.passed),
                    failed: format.integer(execution.tests.failed),
                    ignored: format.integer(execution.tests.ignored ?? 0),
                  })
                }}
              </dd>
            </template>
            <template v-if="execution.image_digest">
              <dt>{{ $t('evidence.image') }}</dt>
              <dd class="monospace digest">{{ execution.image_digest }}</dd>
            </template>
          </dl>
          <details v-if="execution.output_start" class="mt-2">
            <summary>
              {{ $t('evidence.showOutput', { size: format.bytes(execution.output_bytes) }) }}
            </summary>
            <p v-if="outputIsPartial(execution)" class="text-body-2 mt-1">
              {{ $t('evidence.partialOutput') }}
            </p>
            <!-- Scrollable plain text; focusable so it scrolls by keyboard (WCAG 2.1.1). -->
            <pre
              class="command-output mt-1"
              role="region"
              tabindex="0"
              :aria-label="$t('evidence.outputLabel', { n: index + 1 })"
              >{{ execution.output_start }}</pre>
          </details>
          <v-btn
            v-if="execution.output_bytes > 0"
            variant="outlined"
            size="small"
            class="mt-2"
            :loading="downloading === execution.id"
            :aria-label="$t('evidence.downloadLabel', { n: index + 1 })"
            :data-testid="`download-${index + 1}`"
            @click="downloadOutput(execution, index + 1)"
          >
            {{ $t('evidence.download') }}
          </v-btn>
        </li>
      </ol>
    </template>
  </section>
</template>

<style scoped>
.execution-list {
  list-style: none;
  padding: 0;
}

.execution-item {
  border-left: 3px solid rgb(var(--v-theme-on-surface), 0.2);
  padding: 0.25rem 0 0.25rem 0.75rem;
  margin-bottom: 1rem;
}

.command-line,
.digest {
  overflow-wrap: anywhere;
}

.command-output {
  max-height: 20rem;
  overflow: auto;
  padding: 0.5rem;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: rgb(var(--v-theme-on-surface), 0.05);
  border-radius: 4px;
}

.command-output:focus-visible {
  outline: 2px solid rgb(var(--v-theme-primary));
}
</style>
