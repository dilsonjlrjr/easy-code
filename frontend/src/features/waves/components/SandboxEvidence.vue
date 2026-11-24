<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useFormat } from '@/shared/format/useFormat'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { ChipTone } from '@/shared/ui/stateFamilies'
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

/** Canvas chip tone of the outcome; the outcome is always written next to it. */
function outcomeTone(execution: SandboxExecution): ChipTone {
  const outcome = executionOutcome(execution)
  if (outcome === 'succeeded') return 'ok'
  if (outcome === 'limit') return 'warn'
  return outcome === 'aborted' ? 'mute' : 'crit'
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
  <!-- Look: a canvas `.card` (Onda.dc.html) with `.li` rows and `.kv` pairs. -->
  <section id="evidence" aria-labelledby="evidence-heading" class="card evidence-card">
    <h2 id="evidence-heading" class="card-title" tabindex="-1">
      {{ $t('evidence.title') }}
    </h2>
    <ProblemAlert v-if="error" :error="error" retry @retry="$emit('retry')" />
    <p v-else-if="executions.length === 0" class="t2 card-text" data-testid="evidence-empty">
      {{ $t('evidence.none') }}
    </p>
    <template v-else>
      <!-- RNF-03: the sandbox stopped some commands at a resource limit. -->
      <div v-if="limitCount > 0" class="note n-warn" role="alert" data-testid="evidence-limits">
        {{ $t('evidence.limitsNotice', { count: limitCount }) }}
      </div>
      <div role="status" class="sr-only">{{ message }}</div>
      <ProblemAlert v-if="downloadFailure" :error="downloadFailure" />
      <ol class="execution-list" data-testid="evidence-list">
        <li
          v-for="(execution, index) in executions"
          :key="execution.id"
          :data-testid="`execution-${index + 1}`"
          class="execution-item"
        >
          <h3 class="execution-heading">
            <span>
              {{
                $t('evidence.commandHeading', {
                  n: index + 1,
                  category: $t(`evidence.categories.${execution.category}`),
                })
              }}
              ·
            </span>
            <span class="chip" :class="outcomeTone(execution)" data-testid="execution-outcome">{{
              outcomeText(execution)
            }}</span>
          </h3>
          <p class="mono command-line">{{ execution.command }}</p>
          <dl class="kv-list">
            <div class="kv">
              <dt class="t2">{{ $t('evidence.exitCode') }}</dt>
              <dd>{{ execution.exit_code ?? $t('evidence.noExitCode') }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('evidence.duration') }}</dt>
              <dd>{{ format.duration(execution.duration_ms) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('evidence.endReason') }}</dt>
              <dd>{{ $t(`evidence.endReasons.${execution.end_reason}`) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('evidence.started') }}</dt>
              <dd>{{ format.dateTime(execution.start) }}</dd>
            </div>
            <div v-if="execution.tests" class="kv">
              <dt class="t2">{{ $t('evidence.tests') }}</dt>
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
            </div>
            <div v-if="execution.image_digest" class="kv">
              <dt class="t2">{{ $t('evidence.image') }}</dt>
              <dd class="mono digest">{{ execution.image_digest }}</dd>
            </div>
          </dl>
          <details v-if="execution.output_start" class="output-details">
            <summary>
              {{ $t('evidence.showOutput', { size: format.bytes(execution.output_bytes) }) }}
            </summary>
            <p v-if="outputIsPartial(execution)" class="t2 partial">
              {{ $t('evidence.partialOutput') }}
            </p>
            <!-- Scrollable plain text; focusable so it scrolls by keyboard (WCAG 2.1.1). -->
            <pre
              class="ev mono command-output"
              role="region"
              tabindex="0"
              :aria-label="$t('evidence.outputLabel', { n: index + 1 })"
              >{{ execution.output_start }}</pre>
          </details>
          <div v-if="execution.output_bytes > 0">
            <button
              type="button"
              class="btn"
              :disabled="downloading === execution.id"
              :aria-busy="downloading === execution.id || undefined"
              :aria-label="$t('evidence.downloadLabel', { n: index + 1 })"
              :data-testid="`download-${index + 1}`"
              @click="downloadOutput(execution, index + 1)"
            >
              <v-progress-circular
                v-if="downloading === execution.id"
                indeterminate
                size="14"
                width="2"
              />
              {{ $t('evidence.download') }}
            </button>
          </div>
        </li>
      </ol>
    </template>
  </section>
</template>

<style scoped>
.evidence-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.card-text {
  margin: 0;
}

.note {
  margin: 0;
}

.n-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.12);
}

.execution-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.execution-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.execution-heading {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  line-height: 20px;
  font-weight: 600;
}

.command-line {
  margin: 0;
  overflow-wrap: anywhere;
}

.kv-list {
  margin: 0;
}

.kv dt,
.kv dd {
  margin: 0;
}

.kv dd {
  font-weight: 600;
  text-align: right;
  overflow-wrap: anywhere;
}

.digest {
  overflow-wrap: anywhere;
}

.output-details summary {
  min-height: 44px;
  display: flex;
  align-items: center;
  cursor: pointer;
  color: #a78bfa;
  font-weight: 500;
}

.output-details summary:hover {
  color: #c4b5fd;
}

.partial {
  margin: 0 0 6px;
  font-size: 13px;
}

.command-output {
  max-height: 20rem;
  overflow: auto;
  overflow-wrap: anywhere;
}
</style>
