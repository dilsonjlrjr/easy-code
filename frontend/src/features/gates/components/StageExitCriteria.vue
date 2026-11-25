<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Stage } from '@/features/projects/api/projectsApi'
import { useLoader } from '@/shared/composables/useLoader'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { gatesApi, type DecisionRecord, type GateRound } from '../api/gatesApi'
import { criteriaCounts, criterionMark, exitCriteria } from '../exitCriteria'

/**
 * Canvas "Critério de saída" strip of every Etapa artboard (ESP-07 to ESP-16, ESP-12; PENDENCIAS PC-20,
 * PL-46): the checks of the stage gate, one per line with the catalog name, the severity and the result
 * of the current round in text; before any round, "not evaluated" (no preview). With a decided round it
 * says who approved or rejected it and in which round (`decision.record`).
 */
const props = defineProps<{
  projectId: string
  stage: Stage
  /** Bumps when a live event changes the project, so the round is read again. */
  revision: number
}>()

const { t } = useI18n()
const round = ref<GateRound>()

const roundNumber = computed(() => props.stage.current_round ?? undefined)
const gate = computed(() => props.stage.gate ?? undefined)

const loader = useLoader(async () => {
  round.value =
    gate.value && roundNumber.value
      ? await gatesApi.getRound(props.projectId, gate.value, roundNumber.value)
      : undefined
})

const criteria = computed(() => exitCriteria(props.stage.gate_checks, round.value))
const counts = computed(() => criteriaCounts(criteria.value))

function who(record: DecisionRecord): string {
  if (record.per.type === 'human') return record.per.user?.name ?? record.per.id ?? ''
  return t('agents.orchestrator')
}

/** "Gate aprovado por … (rodada N)" and the other outcomes of the round, in text. */
const outcome = computed(() => {
  const current = round.value
  if (!current) return undefined
  const decision = current.decision
  if (decision?.result === 'approved')
    return t('gates.exit.approvedBy', { who: who(decision.record), n: current.round })
  if (decision?.result === 'rejected')
    return t('gates.exit.rejectedBy', { who: who(decision.record), n: current.round })
  if (current.state === 'invalidated') return t('gates.exit.invalidated', { n: current.round })
  return undefined
})

const summary = computed(() => {
  if (!round.value) return t('gates.exit.notEvaluated')
  if (round.value.checks.length === 0)
    return t('gates.exit.awaitingEvaluation', { n: round.value.round })
  return t('gates.exit.fromRound', {
    n: round.value.round,
    passed: counts.value.passed,
    total: counts.value.total,
  })
})

watch([() => props.projectId, gate, roundNumber, () => props.revision], () => void loader.run(), {
  immediate: true,
})
</script>

<template>
  <section
    v-if="gate"
    class="card exit"
    aria-labelledby="exit-criteria-heading"
    data-testid="stage-exit-criteria"
  >
    <div class="exit-head">
      <h3 id="exit-criteria-heading" class="lb exit-label">{{ $t('gates.exit.label') }}</h3>
      <span class="t2 exit-summary" data-testid="exit-criteria-summary">{{ summary }}</span>
      <span v-if="outcome" class="exit-outcome" data-testid="exit-criteria-outcome">
        {{ outcome }}
      </span>
    </div>
    <ProblemAlert
      v-if="loader.error.value && !round"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="criteria.length === 0" class="t2 exit-empty">{{ $t('gates.exit.noChecks') }}</p>
    <ul v-else class="exit-list" data-testid="exit-criteria-list">
      <li
        v-for="item in criteria"
        :key="item.check"
        class="exit-item"
        :data-testid="`exit-criterion-${item.check}`"
      >
        <span
          class="exit-mark"
          :class="`mark-${criterionMark(item.status).tone}`"
          aria-hidden="true"
        >
          {{ criterionMark(item.status).symbol }}
        </span>
        <span class="exit-text">
          {{ item.name }}
          <span class="t2">
            · {{ $t(`gates.exit.status.${item.status}`)
            }}<template v-if="item.severity === 'alert' && item.status !== 'alert'">
              ({{ $t('gates.exit.alertSuffix') }})</template
            >
          </span>
        </span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.exit {
  padding: 14px 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.exit-head {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 16px;
  align-items: baseline;
}

.exit-label {
  margin: 0;
  line-height: 18px;
}

.exit-summary,
.exit-outcome {
  font-size: 13px;
}

.exit-outcome {
  font-weight: 600;
}

.exit-empty {
  margin: 0;
  font-size: 13px;
}

.exit-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 6px 24px;
}

.exit-item {
  display: flex;
  gap: 6px;
  align-items: baseline;
  font-size: 13px;
  line-height: 18px;
  min-width: 0;
}

.exit-mark {
  flex: none;
  width: 14px;
  font-weight: 700;
  text-align: center;
}

.exit-text {
  min-width: 0;
  overflow-wrap: anywhere;
}

.mark-ok {
  color: #86efac;
}

.mark-crit {
  color: #fca5a5;
}

.mark-warn {
  color: #fcd34d;
}

.mark-info {
  color: #a5b4fc;
}

.mark-mute {
  color: #a9b0d6;
}
</style>
