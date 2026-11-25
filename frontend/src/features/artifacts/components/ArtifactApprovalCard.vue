<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { projectsApi } from '@/features/projects/api/projectsApi'
import { gatesApi, type GateRound } from '@/features/gates/api/gatesApi'
import type { Artifact } from '../api/artifactsApi'
import { approvedRounds, roundCarries } from '../composables/artifactLinks'

/**
 * ESP-21 3 "Aprovação" (CA-21-4, ER-21-3): an artifact is approved or returned at the gate of its owner
 * stage, never one by one. Approved, the card names the gate and the round that approved this version
 * (`listGateRounds`, then the round outputs) and who decided; in review, it links the open round.
 */
const props = defineProps<{ projectId: string; artifact: Artifact }>()
const { t } = useI18n()
const format = useFormat()

const gate = ref<string>()
const approving = ref<GateRound>()
const openRound = ref<number>()

const loader = useLoader(async () => {
  approving.value = undefined
  openRound.value = undefined
  const stage = await projectsApi.getStage(props.projectId, props.artifact.stage)
  gate.value = stage.approved_in_gate ?? stage.gate ?? undefined
  if (!gate.value) return
  if (props.artifact.state === 'in_review') openRound.value = stage.current_round ?? undefined
  if (props.artifact.state !== 'approved') return
  const { items } = await gatesApi.listRounds(props.projectId, gate.value)
  for (const summary of approvedRounds(items)) {
    const round = await gatesApi.getRound(props.projectId, gate.value, summary.round)
    if (roundCarries(round, props.artifact.id, props.artifact.version)) {
      approving.value = round
      return
    }
  }
})

watch(
  () => [props.artifact.id, props.artifact.version, props.artifact.state],
  () => void loader.run(),
  {
    immediate: true,
  },
)

const gateName = computed(() => {
  const stage = gate.value?.replace(/^gate\./, '') ?? ''
  return stage ? t('artifactLinks.gateOf', { stage: t(`stages.${stage}`, stage) }) : ''
})
const decidedBy = computed(() => {
  const record = approving.value?.decision?.record
  if (!record) return ''
  const who =
    record.per.user?.name ??
    (record.per.type === 'orchestrator' ? t('agents.orchestrator') : (record.per.id ?? ''))
  return t('artifactLinks.decidedBy', { person: who, when: format.dateTime(record.when) })
})
</script>

<template>
  <section
    class="card side-card"
    aria-labelledby="artifact-approval-heading"
    data-testid="artifact-approval"
  >
    <h2 id="artifact-approval-heading">{{ $t('artifacts.approval') }}</h2>
    <p class="side-text">{{ $t('artifactLinks.approvalRule') }}</p>
    <ProblemAlert v-if="loader.error.value" :error="loader.error.value" />
    <p v-if="approving && gate" class="side-text" data-testid="artifact-approved-in">
      {{ $t('artifactLinks.approvedIn', { gate: gateName, round: approving.round }) }}
      <template v-if="decidedBy"> {{ decidedBy }}</template>
    </p>
    <p v-else-if="artifact.state === 'approved' && !loader.loading.value" class="t2 side-text">
      {{ $t('artifactLinks.approvalRoundUnknown') }}
    </p>
    <router-link
      v-if="gate && (approving || openRound)"
      :to="{
        name: ROUTE_NAMES.gate,
        params: { projectId, gateId: gate, round: approving?.round ?? openRound },
      }"
      class="side-link"
    >
      {{
        approving
          ? $t('artifactLinks.openApprovalRound', { round: approving.round })
          : $t('artifactLinks.openReviewRound', { round: openRound ?? '' })
      }}
    </router-link>
    <router-link
      :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: artifact.stage } }"
      class="side-link"
    >
      {{ $t('artifactLinks.stageAndGate') }}
    </router-link>
  </section>
</template>

<style scoped>
.side-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
}

.side-card h2 {
  margin-bottom: 4px;
}

.side-text {
  margin: 0 0 6px;
  font-size: 13px;
}

.side-link {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  text-decoration: none;
  font-weight: 500;
}
</style>
