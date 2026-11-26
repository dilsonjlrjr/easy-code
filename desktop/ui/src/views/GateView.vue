<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import OpenWebButton from '@/components/OpenWebButton.vue'
import { useLabel } from '@/i18n'
import { navigate, openWeb } from '@/store'
import { chip } from '@/tone'
import type { GateRound, UIError } from '@/types'

// ESP-28 Gate (resumo): situation and checks, read-only; the decision is on the web (ADR-19 step-up).
const props = defineProps<{ projectId: string; gateId: string; round: number }>()
const { t } = useI18n()
const label = useLabel()
const gate = ref<GateRound | null>(null)
const error = ref<UIError | null>(null)

const passed = computed(() => gate.value?.checks.filter((c) => c.status === 'passed').length ?? 0)

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
  <section class="view" aria-labelledby="view-title">
    <nav class="bc" :aria-label="t('common.breadcrumb')">
      <ol>
        <li>
          <button type="button" @click="navigate({ name: 'dashboard' })">
            {{ t('nav.dashboard') }}
          </button>
        </li>
        <li>
          <button type="button" @click="navigate({ name: 'project', projectId })">
            {{ t('common.project') }}
          </button>
        </li>
        <li>
          <span aria-current="page">{{ t('gate.title', { gate: gateId, round }) }}</span>
        </li>
      </ol>
    </nav>
    <div class="head">
      <div class="title-line">
        <h1 id="view-title" tabindex="-1">{{ t('gate.title', { gate: gateId, round }) }}</h1>
        <span v-if="gate" :class="chip('gate', gate.state)">{{
          t('common.state', { state: label(`states.gate.${gate.state}`, gate.state) })
        }}</span>
      </div>
      <OpenWebButton
        v-if="gate"
        primary
        :label="t('gate.decideOnWeb')"
        :open="() => openWeb({ kind: 'gate_round', project_id: projectId, gate_id: gateId, round })"
      />
    </div>
    <ErrorMessage :error="error" />
    <template v-if="gate">
      <p class="note">{{ t('gate.decideNote') }}</p>
      <div class="cols">
        <section class="card col-main" aria-labelledby="checks-title">
          <div class="card-head">
            <h2 id="checks-title">{{ t('gate.checks') }}</h2>
            <span v-if="gate.checks.length" class="t2 small">{{
              t('gate.passedCount', { passed, total: gate.checks.length })
            }}</span>
          </div>
          <p v-if="gate.checks.length === 0" class="empty">{{ t('gate.noChecks') }}</p>
          <ul v-else class="rows">
            <li v-for="check in gate.checks" :key="check.check">
              <span class="grow">{{
                t('gate.checkLine', {
                  check: check.name ?? check.check,
                  status: label(`gate.checkStatus.${check.status}`, check.status),
                })
              }}</span>
              <span :class="chip('check', check.status)" aria-hidden="true">{{
                label(`gate.checkStatus.${check.status}`, check.status)
              }}</span>
            </li>
          </ul>
        </section>
        <section class="card col-side" style="display: block" aria-labelledby="blocking-title">
          <h2 id="blocking-title">{{ t('gate.blocking') }}</h2>
          <p v-if="!gate.blocking_findings?.length" class="empty">{{ t('gate.noBlocking') }}</p>
          <ul v-else class="rows">
            <li v-for="finding in gate.blocking_findings" :key="finding.id" class="top">
              <span class="grow">
                <span>{{
                  t('gate.findingLine', {
                    id: finding.id,
                    severity: finding.severity,
                    title: finding.title,
                  })
                }}</span>
              </span>
              <OpenWebButton
                small
                :item="finding.id"
                :open="
                  () => openWeb({ kind: 'finding', project_id: projectId, finding_id: finding.id })
                "
              />
            </li>
          </ul>
        </section>
      </div>
    </template>
  </section>
</template>
