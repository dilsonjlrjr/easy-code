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
  <section aria-labelledby="cost-summary-heading" class="mt-6" data-testid="cost-summary">
    <div class="d-flex flex-wrap align-center ga-2 mb-2">
      <h2 id="cost-summary-heading" class="text-h6">{{ $t('costs.summary.title') }}</h2>
      <v-spacer />
      <router-link
        :to="{ name: ROUTE_NAMES.projectCosts, params: { projectId } }"
        class="text-body-2"
        data-testid="cost-summary-details"
      >
        {{ $t('costs.summary.details') }}
      </router-link>
    </div>
    <LoadingState v-if="loader.loading.value && !costs" type="paragraph" />
    <ProblemAlert
      v-else-if="loader.error.value && !costs"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="costs">
      <dl class="summary-list mb-4">
        <dt>{{ $t('costs.summary.budgetUse') }}</dt>
        <dd data-testid="cost-summary-budget">
          <template v-if="costs.budget?.stop_limit">
            {{
              $t('costs.summary.budgetUseValue', {
                spend: format.money(costs.total.total),
                limit: format.money(costs.budget.stop_limit),
                percentage: format.percent(costs.budget.used_percentage),
              })
            }}
          </template>
          <template v-else>{{ $t('costs.budget.none') }}</template>
        </dd>
        <template v-if="budget">
          <dt>{{ $t('costs.summary.alertsAndStop') }}</dt>
          <dd>
            {{
              $t('costs.summary.alertsAndStopValue', {
                alerts: (budget.percentage_alert_limits ?? [50, 80])
                  .map((value) => format.percent(value))
                  .join(', '),
                limit: format.money(budget.stop_limit),
              })
            }}
          </dd>
        </template>
        <dt>{{ $t('costs.columns.rework') }}</dt>
        <dd data-testid="cost-summary-rework">
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
        </dd>
        <dt>{{ $t('costs.columns.cycle') }}</dt>
        <dd data-testid="cost-summary-cycle">{{ format.longDuration(costs.time.cycle_ms) }}</dd>
        <dt>{{ $t('costs.columns.execution') }}</dt>
        <dd data-testid="cost-summary-execution">
          {{ format.longDuration(costs.time.execution_ms) }}
        </dd>
      </dl>
    </template>

    <!-- ESP-06 3.6: the next stage with its estimate; "First stage" while the project is created. -->
    <div v-if="nextStage" data-testid="next-stage">
      <p class="text-body-1 mb-1">
        {{
          store.project?.state === 'created'
            ? $t('costs.summary.firstStage', { stage: stageName(nextStage.id) })
            : $t('costs.summary.nextStage', { stage: stageName(nextStage.id) })
        }}
      </p>
      <p v-if="nextStage.state === 'blocked'" class="text-body-2 mb-2">
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
  </section>
</template>
