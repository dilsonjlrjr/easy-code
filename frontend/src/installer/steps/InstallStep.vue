<script setup lang="ts">
import { mdiAlertCircle, mdiCheckCircle, mdiClockOutline, mdiMinusCircleOutline } from '@mdi/js'
import { computed } from 'vue'
import { useInstallerStore } from '../installerStore'
import { useReason } from '../useReason'
import type { StepStatus } from '../types'

const store = useInstallerStore()
const { reasonText } = useReason()
const steps = computed(() => store.status?.steps ?? [])
const failed = computed(() => steps.value.find((s) => s.status === 'failed'))
const icons: Record<Exclude<StepStatus, 'running'>, string> = {
  pending: mdiClockOutline,
  done: mdiCheckCircle,
  failed: mdiAlertCircle,
  skipped: mdiMinusCircleOutline,
}
const colors: Partial<Record<StepStatus, string>> = { done: 'success', failed: 'error' }

/** A reason is "field: code" for a field problem, or a message of the adapter. */
function reason(text: string | undefined): string {
  if (!text) return ''
  const code = text.includes(': ') ? (text.split(': ').pop() ?? text) : text
  return /^[a-z_]+(:.+)?$/.test(code) ? reasonText(code) : text
}
</script>

<template>
  <section>
    <p class="text-body-1 mb-4">{{ $t('installer.progress.intro') }}</p>
    <ol class="plain-list" aria-live="polite">
      <li v-for="step in steps" :key="step.name" class="d-flex align-center ga-3 py-2">
        <v-progress-circular
          v-if="step.status === 'running'"
          indeterminate
          size="20"
          width="2"
          color="primary"
          aria-hidden="true"
        />
        <v-icon v-else :icon="icons[step.status]" :color="colors[step.status]" aria-hidden="true" />
        <span class="flex-grow-1">{{ $t(`installer.steps.${step.name}`) }}</span>
        <span
          class="text-body-2"
          :class="step.status === 'failed' ? 'text-error' : 'text-medium-emphasis'"
        >
          {{ $t(`installer.progress.statuses.${step.status}`) }}
        </span>
      </li>
    </ol>
    <v-alert
      v-if="failed && !store.status?.running"
      type="error"
      variant="tonal"
      role="alert"
      class="mt-4"
    >
      <p>
        {{
          $t('installer.progress.failed', {
            step: $t(`installer.steps.${failed.name}`),
            reason: reason(failed.reason),
          })
        }}
      </p>
      <v-btn
        class="mt-3"
        color="primary"
        variant="flat"
        :loading="store.busy"
        data-testid="retry"
        @click="store.retry()"
      >
        {{ $t('installer.actions.retry') }}
      </v-btn>
    </v-alert>
  </section>
</template>
