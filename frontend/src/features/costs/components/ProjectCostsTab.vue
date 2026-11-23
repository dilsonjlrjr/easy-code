<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { ProjectCosts } from '@/features/projects/api/projectsApi'
import {
  currentProjectApi,
  useCurrentProjectStore,
} from '@/features/projects/stores/currentProjectStore'
import { COST_GROUPINGS, costsApi, type CostGrouping } from '../api/costsApi'
import {
  budgetSituation,
  groupLabel,
  groupingFrom,
  hasEstimatedLoss,
  providerTotals,
  waveFilterOf,
} from '../composables/costView'

/**
 * ESP-25 (US-38, US-39, US-36, US-24 CA-3): where the project's spend and time concentrate. Totals
 * by stage, agent, model or provider, rework and lost cost in columns of their own, cycle and
 * execution time side by side and the budget. The screen only reads.
 */
const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const format = useFormat()
const store = useCurrentProjectStore()
const { message, announce } = useAnnouncer()

const projectId = computed(() => String(route.params.projectId))
const grouping = computed(() => groupingFrom(route.query.group))
const costs = ref<ProjectCosts>()
const alertLimits = ref<readonly number[]>([50, 80])
/** CA-25-7: the group whose waves are listed under the table. */
const openGroup = ref<string>()

const loader = useLoader(async () => {
  const [loaded, configuration] = await Promise.all([
    costsApi.getProjectCosts(projectId.value, grouping.value),
    // The alert thresholds come from the configuration; without it the defaults (50, 80) stay.
    currentProjectApi()
      .getConfiguration(projectId.value)
      .catch(() => undefined),
  ])
  costs.value = loaded
  const limits = configuration?.configuration.budget?.percentage_alert_limits
  if (limits?.length) alertLimits.value = limits
  announce(t('costs.loaded', { grouping: t(`costs.groupings.${grouping.value}`) }))
})

const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
const invalidGrouping = computed(
  () => isApiError(loader.error.value) && loader.error.value.status === 400,
)
const budget = computed(() => budgetSituation(costs.value?.budget, alertLimits.value))
const pausedByBudget = computed(
  () => store.project?.state === 'paused' && store.project.pause_reason === 'budget',
)
const isLead = computed(() => store.project?.my_roles.includes('lead') ?? false)
const providers = computed(() => (costs.value ? providerTotals(costs.value.total) : []))
const noWaves = computed(
  () => !!costs.value && costs.value.groups.every((group) => group.waves === 0),
)
const groupWaves = computed(() => {
  const filter = openGroup.value ? waveFilterOf(grouping.value, openGroup.value) : undefined
  if (!filter) return []
  return store.waves.filter(
    (wave) =>
      (!filter.stage || wave.stage === filter.stage) &&
      (!filter.agent || wave.agent === filter.agent),
  )
})

function label(key: string): string {
  return groupLabel(grouping.value, key, t, (catalogKey) => i18n.te(catalogKey))
}

function choose(next: CostGrouping): void {
  openGroup.value = undefined
  void router.replace({ query: { ...route.query, group: next === 'stage' ? undefined : next } })
}

function toggleGroup(key: string): void {
  openGroup.value = openGroup.value === key ? undefined : key
  if (openGroup.value) announce(t('costs.wavesOfGroupShown', { group: label(key) }))
}

watch([projectId, grouping], () => void loader.run(), { immediate: true })
/** ESP-25 6: an invalid grouping goes back to the default. */
watch(invalidGrouping, (invalid) => {
  if (invalid && grouping.value !== 'stage') choose('stage')
})
</script>

<template>
  <section aria-labelledby="costs-heading" data-testid="project-costs">
    <div class="d-flex flex-wrap align-center ga-2 mb-2">
      <h2 id="costs-heading" class="text-h6">{{ $t('costs.title') }}</h2>
      <v-spacer />
      <v-btn variant="outlined" size="small" :loading="loader.loading.value" @click="loader.run">
        {{ $t('costs.refresh') }}
      </v-btn>
    </div>
    <div role="status" class="sr-only">{{ message }}</div>

    <div role="group" :aria-label="$t('costs.groupingLabel')" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-for="item in COST_GROUPINGS"
        :key="item"
        :variant="grouping === item ? 'flat' : 'outlined'"
        :color="grouping === item ? 'primary' : undefined"
        :aria-pressed="grouping === item"
        :disabled="loader.loading.value"
        size="small"
        @click="choose(item)"
      >
        {{ $t(`costs.groupings.${item}`) }}
      </v-btn>
    </div>

    <LoadingState v-if="loader.loading.value && !costs" type="table" />
    <v-alert v-else-if="notFound" type="warning" variant="tonal" role="alert">
      {{ $t('problems.not-found') }}
    </v-alert>
    <ProblemAlert
      v-else-if="loader.error.value && !invalidGrouping"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="costs">
      <p v-if="noWaves" class="text-body-1 mb-4" data-testid="costs-no-waves">
        {{ $t('costs.noWaves') }}
      </p>

      <!-- ER-25-6: percentage, balance and the alert or stop state, in text and bar. -->
      <section aria-labelledby="costs-budget-heading" class="mb-6" data-testid="costs-budget">
        <h3 id="costs-budget-heading" class="text-subtitle-1 font-weight-medium mb-1">
          {{ $t('costs.budget.title') }}
        </h3>
        <p v-if="budget.kind === 'none'" class="text-body-2">{{ $t('costs.budget.none') }}</p>
        <template v-else-if="costs.budget">
          <p class="text-body-2" data-testid="costs-budget-use">
            {{
              $t('costs.budget.use', {
                percentage: format.percent(budget.percentage),
                limit: format.money(costs.budget.stop_limit),
                balance: format.money(costs.budget.balance),
              })
            }}
          </p>
          <v-progress-linear
            :model-value="Math.min(100, budget.percentage)"
            :color="
              budget.kind === 'normal' ? 'primary' : budget.kind === 'alert' ? 'warning' : 'error'
            "
            height="8"
            rounded
            class="my-2 budget-bar"
            :aria-label="$t('costs.budget.barLabel')"
            :aria-valuetext="format.percent(budget.percentage)"
          />
          <v-alert
            v-if="pausedByBudget"
            type="error"
            variant="tonal"
            class="mt-2"
            data-testid="costs-budget-paused"
          >
            <p>{{ $t('projects.budget.paused') }}</p>
            <router-link
              v-if="isLead"
              :to="{ name: ROUTE_NAMES.projectConfiguration, params: { projectId } }"
            >
              {{ $t('projects.budget.raiseLimit') }}
            </router-link>
          </v-alert>
          <v-alert
            v-else-if="budget.kind === 'alert'"
            type="warning"
            variant="tonal"
            class="mt-2"
            data-testid="costs-budget-alert"
          >
            {{
              $t('costs.budget.alert', {
                reached: format.percent(budget.reached),
                percentage: format.percent(budget.percentage),
              })
            }}
          </v-alert>
          <v-alert
            v-else-if="budget.kind === 'exhausted'"
            type="error"
            variant="tonal"
            class="mt-2"
            data-testid="costs-budget-exhausted"
          >
            {{ $t('costs.budget.exhausted') }}
          </v-alert>
        </template>
        <router-link
          v-if="isLead"
          :to="{ name: ROUTE_NAMES.projectConfiguration, params: { projectId } }"
          class="d-inline-block text-body-2 mt-1"
        >
          {{ $t('costs.budget.configure') }}
        </router-link>
      </section>

      <div class="costs-summary mb-6">
        <section aria-labelledby="costs-total-heading">
          <h3 id="costs-total-heading" class="text-subtitle-1 font-weight-medium mb-1">
            {{ $t('costs.total.title') }}
          </h3>
          <dl class="summary-list" data-testid="costs-total">
            <dt>{{ $t('costs.columns.useful') }}</dt>
            <dd>{{ format.money(costs.total.useful) }}</dd>
            <dt>{{ $t('costs.columns.rework') }}</dt>
            <dd data-testid="costs-total-rework">{{ format.money(costs.total.rework) || '—' }}</dd>
            <dt>{{ $t('costs.columns.lost') }}</dt>
            <dd data-testid="costs-total-lost">
              {{ format.money(costs.total.lost) }}
              <span v-if="hasEstimatedLoss(costs.total)" data-testid="costs-estimated-lost">
                ({{
                  $t('costs.estimatedPart', { value: format.money(costs.total.estimated_lost) })
                }})
              </span>
            </dd>
            <dt>{{ $t('costs.columns.total') }}</dt>
            <dd>
              <strong>{{ format.money(costs.total.total) }}</strong>
            </dd>
          </dl>
          <template v-if="providers.length > 0">
            <h4 class="text-body-1 font-weight-medium mt-3 mb-1">
              {{ $t('costs.total.byProvider') }}
            </h4>
            <ul class="plain-list" data-testid="costs-providers">
              <li v-for="item in providers" :key="item.provider" class="text-body-2">
                {{ item.provider }}: {{ format.money(item.total) }}
              </li>
            </ul>
          </template>
        </section>

        <section aria-labelledby="costs-tokens-heading">
          <h3 id="costs-tokens-heading" class="text-subtitle-1 font-weight-medium mb-1">
            {{ $t('costs.tokens.title') }}
          </h3>
          <dl class="summary-list" data-testid="costs-tokens">
            <dt>{{ $t('costs.tokens.input') }}</dt>
            <dd>{{ format.integer(costs.tokens.input) }}</dd>
            <dt>{{ $t('costs.tokens.output') }}</dt>
            <dd>{{ format.integer(costs.tokens.output) }}</dd>
            <dt>{{ $t('costs.tokens.readCache') }}</dt>
            <dd>{{ format.integer(costs.tokens.read_cache) }}</dd>
            <dt>{{ $t('costs.tokens.writeCache') }}</dt>
            <dd>{{ format.integer(costs.tokens.write_cache) }}</dd>
          </dl>
        </section>

        <!-- US-38 CA-3: cycle time (with human wait) and execution time side by side. -->
        <section aria-labelledby="costs-time-heading">
          <h3 id="costs-time-heading" class="text-subtitle-1 font-weight-medium mb-1">
            {{ $t('costs.time.title') }}
          </h3>
          <dl class="summary-list" data-testid="costs-time">
            <dt>{{ $t('costs.columns.cycle') }}</dt>
            <dd>{{ format.longDuration(costs.time.cycle_ms) }}</dd>
            <dt>{{ $t('costs.columns.execution') }}</dt>
            <dd>{{ format.longDuration(costs.time.execution_ms) }}</dd>
          </dl>
        </section>
      </div>

      <!-- ER-25-1, ER-25-2: one row per group, rework and lost cost apart, the total row last. -->
      <div class="table-scroll mb-4">
        <v-table density="comfortable" data-testid="costs-groups">
          <caption class="sr-only">
            {{
              $t('costs.tableCaption', { grouping: $t(`costs.groupings.${grouping}`) })
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t(`costs.groupings.${grouping}`) }}</th>
              <th scope="col">{{ $t('costs.columns.useful') }}</th>
              <th scope="col">{{ $t('costs.columns.rework') }}</th>
              <th scope="col">{{ $t('costs.columns.lost') }}</th>
              <th scope="col">{{ $t('costs.columns.total') }}</th>
              <th scope="col">{{ $t('costs.columns.tokens') }}</th>
              <th scope="col">{{ $t('costs.columns.cycle') }}</th>
              <th scope="col">{{ $t('costs.columns.execution') }}</th>
              <th scope="col">{{ $t('costs.columns.waves') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="group in costs.groups"
              :key="group.key"
              :data-testid="`costs-group-${group.key}`"
            >
              <th scope="row">
                <v-btn
                  v-if="waveFilterOf(grouping, group.key)"
                  variant="text"
                  size="small"
                  class="px-1 text-none"
                  :aria-expanded="openGroup === group.key"
                  aria-controls="costs-group-waves"
                  @click="toggleGroup(group.key)"
                >
                  {{ label(group.key) }}
                </v-btn>
                <span v-else :class="{ monospace: grouping === 'model' }">{{
                  label(group.key)
                }}</span>
              </th>
              <td>{{ format.money(group.cost.useful) }}</td>
              <td>{{ format.money(group.cost.rework) || '—' }}</td>
              <td>
                {{ format.money(group.cost.lost) }}
                <span v-if="hasEstimatedLoss(group.cost)">
                  ({{
                    $t('costs.estimatedPart', { value: format.money(group.cost.estimated_lost) })
                  }})
                </span>
              </td>
              <td>{{ format.money(group.cost.total) }}</td>
              <td>
                {{
                  $t('costs.tokensValue', {
                    input: format.integer(group.tokens.input),
                    output: format.integer(group.tokens.output),
                  })
                }}
              </td>
              <td>{{ format.longDuration(group.time.cycle_ms) }}</td>
              <td>{{ format.longDuration(group.time.execution_ms) }}</td>
              <td>{{ format.integer(group.waves) }}</td>
            </tr>
          </tbody>
          <tfoot>
            <tr data-testid="costs-total-row">
              <th scope="row">{{ $t('costs.columns.total') }}</th>
              <td>{{ format.money(costs.total.useful) }}</td>
              <td>{{ format.money(costs.total.rework) || '—' }}</td>
              <td>{{ format.money(costs.total.lost) }}</td>
              <td>
                <strong>{{ format.money(costs.total.total) }}</strong>
              </td>
              <td>
                {{
                  $t('costs.tokensValue', {
                    input: format.integer(costs.tokens.input),
                    output: format.integer(costs.tokens.output),
                  })
                }}
              </td>
              <td>{{ format.longDuration(costs.time.cycle_ms) }}</td>
              <td>{{ format.longDuration(costs.time.execution_ms) }}</td>
              <td>
                {{ format.integer(costs.groups.reduce((sum, group) => sum + group.waves, 0)) }}
              </td>
            </tr>
          </tfoot>
        </v-table>
      </div>

      <!-- CA-25-7: the waves of the chosen stage or agent, each to its wave page (US-36). -->
      <section
        v-if="openGroup"
        id="costs-group-waves"
        aria-labelledby="costs-group-waves-heading"
        data-testid="costs-group-waves"
      >
        <h3 id="costs-group-waves-heading" class="text-subtitle-1 font-weight-medium mb-2">
          {{ $t('costs.wavesOfGroup', { group: label(openGroup) }) }}
        </h3>
        <p v-if="groupWaves.length === 0" class="text-body-2">{{ $t('projects.waves.empty') }}</p>
        <ul v-else class="plain-list">
          <li v-for="wave in groupWaves" :key="wave.id" class="text-body-2 mb-1">
            <router-link
              :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: wave.id } }"
              class="monospace"
            >
              {{ wave.id }}
            </router-link>
            · {{ $t(`waves.purpose.${wave.purpose}`) }} ·
            <StateLabel machine="wave" :state="wave.state" />
            <template v-if="wave.cost_total"> · {{ format.money(wave.cost_total) }}</template>
          </li>
        </ul>
      </section>
    </template>
  </section>
</template>

<style scoped>
.costs-summary {
  display: grid;
  gap: 1.5rem;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
}

.budget-bar {
  max-width: 480px;
}
</style>
