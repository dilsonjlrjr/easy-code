<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import { subtractMoney } from '@/shared/format/money'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import type { Budget, ProjectCosts } from '@/features/projects/api/projectsApi'
import {
  currentProjectApi,
  useCurrentProjectStore,
} from '@/features/projects/stores/currentProjectStore'
import { costsApi } from '../api/costsApi'
import { nextStageOf } from '../composables/costView'
import StageEstimate from './StageEstimate.vue'

/**
 * ESP-06 3.6 (US-38, US-39, US-40): the "Cost and time" block of the project page. Budget use,
 * alerts and stop, rework apart from the original waves, cycle and execution time side by side,
 * and the estimate of the next stage. "See details" opens the costs view (ESP-25).
 */
const i18n = useI18n()
const { t } = i18n
const format = useFormat()
const store = useCurrentProjectStore()
const costs = ref<ProjectCosts>()
const budget = ref<Budget | null>(null)

const projectId = computed(() => store.project?.id ?? '')
const loader = useLoader(async () => {
  const [loaded, configuration] = await Promise.all([
    costsApi.getProjectCosts(projectId.value, 'stage'),
    currentProjectApi()
      .getConfiguration(projectId.value)
      .catch(() => undefined),
  ])
  costs.value = loaded
  budget.value = configuration?.configuration.budget ?? null
})

const reworkWaves = computed(() => store.waves.filter((wave) => wave.purpose === 'rework'))
const reworkFindings = computed(() => [
  ...new Set(reworkWaves.value.flatMap((wave) => (wave.finding ? [wave.finding] : []))),
])
const nextStage = computed(() =>
  nextStageOf(
    store.workflow?.stages.filter((stage) => stage.type === 'stage') ?? [],
    store.project?.state,
  ),
)
const balance = computed(
  () =>
    costs.value?.budget?.balance ??
    subtractMoney(store.project?.summary?.stop_limit, store.project?.summary?.accumulated_cost),
)

/** Canvas "Custo e tempo": the bar fills up to the stop limit; each alert is a mark on it. */
const usedWidth = computed(
  () => `${String(Math.min(Math.max(costs.value?.budget?.used_percentage ?? 0, 0), 100))}%`,
)
const alertMarks = computed(() =>
  budget.value ? (budget.value.percentage_alert_limits ?? [50, 80]) : [],
)

function stageName(id: string): string {
  const key = `stages.${id}`
  return i18n.te(key) ? t(key) : id
}

watch(
  () => [projectId.value, store.revision],
  () => {
    if (projectId.value) void loader.run()
  },
  { immediate: true },
)
</script>

<template>
  <!-- Layout: "Custo e tempo" card of canvas "Projeto" (Projeto.dc.html). -->
  <section aria-labelledby="cost-summary-heading" class="card cost-card" data-testid="cost-summary">
    <h2 id="cost-summary-heading">{{ $t('costs.summary.title') }}</h2>
    <LoadingState v-if="loader.loading.value && !costs" type="paragraph" />
    <ProblemAlert
      v-else-if="loader.error.value && !costs"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="costs">
      <div class="budget-use" data-testid="cost-summary-budget">
        <template v-if="costs.budget?.stop_limit">
          <span class="budget-percentage">{{ format.percent(costs.budget.used_percentage) }}</span>
          <span class="t2">
            {{
              $t('costs.summary.ofBudget', {
                spend: format.money(costs.total.total),
                limit: format.money(costs.budget.stop_limit),
              })
            }}
          </span>
        </template>
        <span v-else class="t2">{{ $t('costs.budget.none') }}</span>
      </div>
      <template v-if="costs.budget?.stop_limit">
        <!-- The text above and below carries the values; the bar only repeats them. -->
        <div class="budget-bar" aria-hidden="true">
          <div class="budget-fill" :style="{ width: usedWidth }" />
          <span
            v-for="mark in alertMarks"
            :key="mark"
            class="budget-mark"
            :style="{ left: `${String(mark)}%` }"
          />
        </div>
      </template>
      <span v-if="budget" class="t2 budget-rule">
        {{
          $t('costs.summary.alertsAndStopValue', {
            alerts: (budget.percentage_alert_limits ?? [50, 80])
              .map((value) => format.percent(value))
              .join(', '),
            limit: format.money(budget.stop_limit),
          })
        }}
      </span>
      <div class="cost-rows">
        <div class="kv">
          <span class="t2">{{ $t('costs.columns.rework') }}</span>
          <span class="kv-value" data-testid="cost-summary-rework">
            {{
              $t('costs.summary.reworkValue', {
                n: reworkWaves.length,
                cost:
                  format.money(costs.total.rework) ||
                  format.money({ value: '0', currency: costs.total.total.currency }),
              })
            }}
            <template v-if="reworkFindings.length">
              · {{ $t('costs.summary.reworkFindings', { findings: reworkFindings.join(', ') }) }}
            </template>
          </span>
        </div>
        <div class="kv">
          <span class="t2">{{ $t('costs.columns.cycle') }}</span>
          <span class="kv-value" data-testid="cost-summary-cycle">
            {{ format.longDuration(costs.time.cycle_ms) }}
          </span>
        </div>
        <div class="kv">
          <span class="t2">{{ $t('costs.columns.execution') }}</span>
          <span class="kv-value" data-testid="cost-summary-execution">
            {{ format.longDuration(costs.time.execution_ms) }}
          </span>
        </div>
      </div>
    </template>

    <!-- ESP-06 3.6: the next stage with its estimate; "First stage" while the project is created. -->
    <div v-if="nextStage" class="next-stage" data-testid="next-stage">
      <p class="next-stage-title">
        {{
          store.project?.state === 'created'
            ? $t('costs.summary.firstStage', { stage: stageName(nextStage.id) })
            : $t('costs.summary.nextStage', { stage: stageName(nextStage.id) })
        }}
      </p>
      <p v-if="nextStage.state === 'blocked'" class="t2 next-stage-wait">
        {{
          $t('projects.workflow.waitingFor', {
            stages: nextStage.depends_on.map((id) => stageName(id)).join(', '),
          })
        }}
      </p>
      <StageEstimate
        :project-id="projectId"
        :stage="nextStage.id"
        :balance="balance"
        :revision="store.revision"
      />
    </div>
    <router-link
      :to="{ name: ROUTE_NAMES.projectCosts, params: { projectId } }"
      class="card-link"
      data-testid="cost-summary-details"
    >
      {{ $t('costs.summary.details') }}
    </router-link>
  </section>
</template>

<style scoped>
.cost-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
}

.budget-use {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-top: 10px;
}

.budget-percentage {
  font-size: 22px;
  font-weight: 700;
}

.budget-bar {
  position: relative;
  height: 10px;
  border-radius: 5px;
  background: rgba(255, 255, 255, 0.12);
  margin: 10px 0 4px;
}

.budget-fill {
  height: 10px;
  border-radius: 5px;
  background: linear-gradient(90deg, #3b82f6, #8b5cf6);
}

.budget-mark {
  position: absolute;
  top: -3px;
  width: 2px;
  height: 16px;
  background: #f59e0b;
}

.budget-rule {
  font-size: 12px;
}

.cost-rows {
  margin-top: 10px;
}

.kv-value {
  font-weight: 600;
  text-align: right;
}

.next-stage {
  padding-top: 10px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.next-stage-title {
  margin: 0;
  font-weight: 600;
}

.next-stage-wait {
  margin: 4px 0 0;
  font-size: 13px;
}

.next-stage :deep([data-testid='stage-estimate']) {
  margin: 10px 0 0;
}

.card-link {
  display: inline-flex;
  align-self: flex-start;
  min-height: 44px;
  align-items: center;
  font-weight: 500;
  text-decoration: none;
}
</style>
