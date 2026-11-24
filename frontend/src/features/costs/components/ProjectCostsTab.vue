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
  <!-- Layout: canvas primitives of Projeto.dc.html ("Custo e tempo" card, "Ondas" table); no own artboard. -->
  <section class="costs" aria-labelledby="costs-heading" data-testid="project-costs">
    <div class="costs-head">
      <h2 id="costs-heading">{{ $t('costs.title') }}</h2>
      <button
        type="button"
        class="btn"
        :disabled="loader.loading.value"
        :aria-busy="loader.loading.value ? 'true' : undefined"
        @click="loader.run"
      >
        {{ $t('costs.refresh') }}
      </button>
    </div>
    <div role="status" class="sr-only">{{ message }}</div>

    <div role="group" :aria-label="$t('costs.groupingLabel')" class="groupings">
      <button
        v-for="item in COST_GROUPINGS"
        :key="item"
        type="button"
        class="tabf"
        :class="{ on: grouping === item }"
        :aria-pressed="grouping === item"
        :disabled="loader.loading.value"
        @click="choose(item)"
      >
        {{ $t(`costs.groupings.${item}`) }}
      </button>
    </div>

    <LoadingState v-if="loader.loading.value && !costs" type="table" />
    <p v-else-if="notFound" class="note note-warn" role="alert">
      {{ $t('problems.not-found') }}
    </p>
    <ProblemAlert
      v-else-if="loader.error.value && !invalidGrouping"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="costs">
      <p v-if="noWaves" class="note" data-testid="costs-no-waves">
        {{ $t('costs.noWaves') }}
      </p>

      <div class="costs-summary">
        <!-- ER-25-6: percentage, balance and the alert or stop state, in text and bar. -->
        <section
          class="card costs-card"
          aria-labelledby="costs-budget-heading"
          data-testid="costs-budget"
        >
          <h3 id="costs-budget-heading">{{ $t('costs.budget.title') }}</h3>
          <p v-if="budget.kind === 'none'" class="t2 card-text">{{ $t('costs.budget.none') }}</p>
          <template v-else-if="costs.budget">
            <div class="budget-figure">
              <span class="budget-percentage" aria-hidden="true">
                {{ format.percent(budget.percentage) }}
              </span>
              <span class="t2" data-testid="costs-budget-use">
                {{
                  $t('costs.budget.use', {
                    percentage: format.percent(budget.percentage),
                    limit: format.money(costs.budget.stop_limit),
                    balance: format.money(costs.budget.balance),
                  })
                }}
              </span>
            </div>
            <div
              class="budget-bar"
              role="progressbar"
              aria-valuemin="0"
              aria-valuemax="100"
              :aria-valuenow="Math.min(100, budget.percentage)"
              :aria-label="$t('costs.budget.barLabel')"
              :aria-valuetext="format.percent(budget.percentage)"
            >
              <div
                class="budget-fill"
                :class="`budget-fill-${budget.kind}`"
                :style="{ width: `${Math.min(100, budget.percentage)}%` }"
              />
              <span
                v-for="limit in alertLimits"
                :key="limit"
                class="budget-mark"
                :style="{ left: `${limit}%` }"
                aria-hidden="true"
              />
            </div>
            <p v-if="pausedByBudget" class="note note-crit" data-testid="costs-budget-paused">
              <span>
                {{ $t('projects.budget.paused') }}
                <router-link
                  v-if="isLead"
                  :to="{ name: ROUTE_NAMES.projectConfiguration, params: { projectId } }"
                >
                  {{ $t('projects.budget.raiseLimit') }}
                </router-link>
              </span>
            </p>
            <p
              v-else-if="budget.kind === 'alert'"
              class="note note-warn"
              data-testid="costs-budget-alert"
            >
              {{
                $t('costs.budget.alert', {
                  reached: format.percent(budget.reached),
                  percentage: format.percent(budget.percentage),
                })
              }}
            </p>
            <p
              v-else-if="budget.kind === 'exhausted'"
              class="note note-crit"
              data-testid="costs-budget-exhausted"
            >
              {{ $t('costs.budget.exhausted') }}
            </p>
          </template>
          <router-link
            v-if="isLead"
            :to="{ name: ROUTE_NAMES.projectConfiguration, params: { projectId } }"
            class="card-link"
          >
            {{ $t('costs.budget.configure') }}
          </router-link>
        </section>

        <section class="card costs-card" aria-labelledby="costs-total-heading">
          <h3 id="costs-total-heading">{{ $t('costs.total.title') }}</h3>
          <dl class="kv-list" data-testid="costs-total">
            <div class="kv">
              <dt class="t2">{{ $t('costs.columns.useful') }}</dt>
              <dd>{{ format.money(costs.total.useful) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('costs.columns.rework') }}</dt>
              <dd data-testid="costs-total-rework">
                {{ format.money(costs.total.rework) || '—' }}
              </dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('costs.columns.lost') }}</dt>
              <dd data-testid="costs-total-lost">
                {{ format.money(costs.total.lost) }}
                <span v-if="hasEstimatedLoss(costs.total)" data-testid="costs-estimated-lost">
                  ({{
                    $t('costs.estimatedPart', { value: format.money(costs.total.estimated_lost) })
                  }})
                </span>
              </dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('costs.columns.total') }}</dt>
              <dd class="kv-total">{{ format.money(costs.total.total) }}</dd>
            </div>
          </dl>
          <template v-if="providers.length > 0">
            <h4 class="lb providers-heading">{{ $t('costs.total.byProvider') }}</h4>
            <ul class="plain-list kv-list" data-testid="costs-providers">
              <li v-for="item in providers" :key="item.provider" class="kv">
                <span class="t2">{{ item.provider }}:</span>
                <span class="kv-value">{{ format.money(item.total) }}</span>
              </li>
            </ul>
          </template>
        </section>

        <section class="card costs-card" aria-labelledby="costs-tokens-heading">
          <h3 id="costs-tokens-heading">{{ $t('costs.tokens.title') }}</h3>
          <dl class="kv-list" data-testid="costs-tokens">
            <div class="kv">
              <dt class="t2">{{ $t('costs.tokens.input') }}</dt>
              <dd>{{ format.integer(costs.tokens.input) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('costs.tokens.output') }}</dt>
              <dd>{{ format.integer(costs.tokens.output) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('costs.tokens.readCache') }}</dt>
              <dd>{{ format.integer(costs.tokens.read_cache) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('costs.tokens.writeCache') }}</dt>
              <dd>{{ format.integer(costs.tokens.write_cache) }}</dd>
            </div>
          </dl>
        </section>

        <!-- US-38 CA-3: cycle time (with human wait) and execution time side by side. -->
        <section class="card costs-card" aria-labelledby="costs-time-heading">
          <h3 id="costs-time-heading">{{ $t('costs.time.title') }}</h3>
          <dl class="kv-list" data-testid="costs-time">
            <div class="kv">
              <dt class="t2">{{ $t('costs.columns.cycle') }}</dt>
              <dd>{{ format.longDuration(costs.time.cycle_ms) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('costs.columns.execution') }}</dt>
              <dd>{{ format.longDuration(costs.time.execution_ms) }}</dd>
            </div>
          </dl>
        </section>
      </div>

      <!-- ER-25-1, ER-25-2: one row per group, rework and lost cost apart, the total row last. -->
      <div class="card costs-table-card">
        <div class="table-scroll">
          <table class="tbl costs-table" data-testid="costs-groups">
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
                <th scope="row" class="row-head">
                  <button
                    v-if="waveFilterOf(grouping, group.key)"
                    type="button"
                    class="group-toggle"
                    :aria-expanded="openGroup === group.key"
                    aria-controls="costs-group-waves"
                    @click="toggleGroup(group.key)"
                  >
                    {{ label(group.key) }}
                  </button>
                  <span v-else :class="{ mono: grouping === 'model' }">{{ label(group.key) }}</span>
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
                <th scope="row" class="row-head">{{ $t('costs.columns.total') }}</th>
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
          </table>
        </div>
      </div>

      <!-- CA-25-7: the waves of the chosen stage or agent, each to its wave page (US-36). -->
      <section
        v-if="openGroup"
        id="costs-group-waves"
        class="card costs-card"
        aria-labelledby="costs-group-waves-heading"
        data-testid="costs-group-waves"
      >
        <h3 id="costs-group-waves-heading">
          {{ $t('costs.wavesOfGroup', { group: label(openGroup) }) }}
        </h3>
        <p v-if="groupWaves.length === 0" class="t2 card-text">
          {{ $t('projects.waves.empty') }}
        </p>
        <ul v-else class="plain-list wave-list">
          <li v-for="wave in groupWaves" :key="wave.id" class="li">
            <router-link
              :to="{ name: ROUTE_NAMES.wave, params: { projectId, waveId: wave.id } }"
              class="mono"
            >
              {{ wave.id }}
            </router-link>
            <span class="t2 wave-purpose">· {{ $t(`waves.purpose.${wave.purpose}`) }} ·</span>
            <StateLabel machine="wave" :state="wave.state" />
            <span v-if="wave.cost_total" class="wave-cost">
              · {{ format.money(wave.cost_total) }}
            </span>
          </li>
        </ul>
      </section>
    </template>
  </section>
</template>

<style scoped>
.costs {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.costs-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.groupings {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tabf {
  min-height: 44px;
}

.tabf[disabled] {
  opacity: 0.45;
  cursor: not-allowed;
}

.note {
  margin: 0;
}

.note-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.note-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.1);
}

.costs-summary {
  display: grid;
  gap: 20px;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  align-items: start;
}

.costs-card {
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

h3 {
  margin: 0;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.card-text {
  margin: 0;
}

.budget-figure {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 8px;
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
  margin: 0 0 4px;
}

.budget-fill {
  height: 10px;
  border-radius: 5px;
  background: linear-gradient(90deg, #3b82f6, #8b5cf6);
}

.budget-fill-alert {
  background: linear-gradient(90deg, #f59e0b, #fcd34d);
}

.budget-fill-exhausted {
  background: linear-gradient(90deg, #ef4444, #fca5a5);
}

.budget-mark {
  position: absolute;
  top: -3px;
  width: 2px;
  height: 16px;
  background: #f59e0b;
}

.card-link {
  display: inline-flex;
  align-self: flex-start;
  min-height: 44px;
  align-items: center;
  font-weight: 500;
  text-decoration: none;
}

.kv-list {
  margin: 0;
  padding: 0;
}

.kv dt {
  margin: 0;
}

.kv dd,
.kv-value {
  margin: 0;
  font-weight: 600;
  text-align: right;
}

.kv-total {
  font-weight: 700;
}

.providers-heading {
  margin: 6px 0 0;
}

.costs-table-card {
  padding: 18px 22px 8px;
}

.costs-table {
  min-width: 960px;
}

.costs-table tfoot td,
.costs-table tfoot th {
  border-bottom: 0;
  font-weight: 600;
}

.row-head {
  font-weight: 600;
  color: #f1f3ff;
  padding: 10px 10px 10px 0;
  border-bottom: 1px solid rgba(140, 150, 255, 0.08);
  vertical-align: top;
}

.group-toggle {
  min-height: 44px;
  margin: -12px 0;
  padding: 0;
  border: 0;
  background: transparent;
  color: #a78bfa;
  font: inherit;
  font-weight: 600;
  cursor: pointer;
  text-align: left;
}

.group-toggle:hover {
  color: #c4b5fd;
  text-decoration: underline;
}

.group-toggle[aria-expanded='true'] {
  color: #ffffff;
}

.wave-list {
  margin: 0;
}

.wave-list .li {
  flex-wrap: wrap;
  gap: 8px;
  font-size: 13px;
}

.wave-cost {
  font-weight: 600;
}
</style>
