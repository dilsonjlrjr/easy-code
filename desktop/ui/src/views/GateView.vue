<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import OpenWebButton from '@/components/OpenWebButton.vue'
import { useLabel } from '@/i18n'
import { navigate, openWeb } from '@/store'
import type { GateRound, UIError } from '@/types'

// ESP-28 Gate (resumo): situation and checks, read-only; the decision is on the web (ADR-19 step-up).
const props = defineProps<{ projectId: string; gateId: string; round: number }>()
const { t } = useI18n()
const label = useLabel()
const gate = ref<GateRound | null>(null)
const error = ref<UIError | null>(null)

onMounted(async () => {
  try {
    gate.value = await bridge().call<GateRound>(
      'GateRound',
      props.projectId,
      props.gateId,
      props.round,
    )
  } catch (e) {
    error.value = toUIError(e)
  }
})
</script>

<template>
  <section aria-labelledby="view-title">
    <button type="button" class="link-button" @click="navigate({ name: 'project', projectId })">
      {{ t('common.back') }}
    </button>
    <h1 id="view-title" tabindex="-1">{{ t('gate.title', { gate: gateId, round }) }}</h1>
    <ErrorMessage :error="error" />
    <template v-if="gate">
      <p>
        {{ t('common.state', { state: label(`states.gate.${gate.state}`, gate.state) }) }}
      </p>
      <OpenWebButton
        :label="t('gate.decideOnWeb')"
        :open="() => openWeb({ kind: 'gate_round', project_id: projectId, gate_id: gateId, round })"
      />
      <h2>{{ t('gate.checks') }}</h2>
      <p v-if="gate.checks.length === 0">{{ t('gate.noChecks') }}</p>
      <ul v-else class="rows">
        <li v-for="check in gate.checks" :key="check.check">
          {{
            t('gate.checkLine', {
              check: check.name ?? check.check,
              status: label(`gate.checkStatus.${check.status}`, check.status),
            })
          }}
        </li>
      </ul>
      <h2>{{ t('gate.blocking') }}</h2>
      <p v-if="!gate.blocking_findings?.length">{{ t('gate.noBlocking') }}</p>
      <ul v-else class="rows">
        <li v-for="finding in gate.blocking_findings" :key="finding.id">
          {{
            t('gate.findingLine', {
              id: finding.id,
              severity: finding.severity,
              title: finding.title,
            })
          }}
          <OpenWebButton
            :item="finding.id"
            :open="
              () => openWeb({ kind: 'finding', project_id: projectId, finding_id: finding.id })
            "
          />
        </li>
      </ul>
    </template>
  </section>
</template>
