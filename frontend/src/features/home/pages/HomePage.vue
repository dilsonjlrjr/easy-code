<script setup lang="ts">
import { computed, ref } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import type { ProjectState } from '@/shared/api/contract'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import { isZeroMoney } from '@/shared/format/money'
import { useSessionStore } from '@/shared/session/sessionStore'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import StageSegments from '@/shared/ui/StageSegments.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { costsApi, type UserCosts } from '@/features/costs/api/costsApi'
import {
  oldestFirst,
  pendingDestination,
  pendingItemsApi,
  type PendingItem,
} from '@/features/pending-items/api/pendingItemsApi'
import { projectsApi, type Project } from '@/features/projects/api/projectsApi'
import ProjectSituation from '@/features/projects/components/ProjectSituation.vue'
import RecentActivity from '../components/RecentActivity.vue'
import { criticalOrHigh, runningAgents } from '@/features/projects/composables/projectSituation'

/**
 * ESP-03 (US-50, US-48, US-38): one view of what the agents are doing, what waits for the user and
 * how much it cost, in the projects where the user has a role. Each block loads on its own and
 * fails on its own; the page decides nothing, it only leads to the screen that does.
 */
const PENDING_SHOWN = 5

const format = useFormat()
const session = useSessionStore()

const projects = ref<Project[]>([])
const counts = ref<Partial<Record<ProjectState, number>>>({})
const projectsLoader = useLoader(async () => {
  const page = await projectsApi.listProjects({})
  projects.value = page.items
  counts.value = page.count_by_state
})

const pending = ref<PendingItem[]>([])
const pendingTotals = ref({ all: 0, gate: 0, question: 0, finding_escalated: 0 })
const pendingLoader = useLoader(async () => {
  const [all, gates, questions, escalated] = await Promise.all([
    pendingItemsApi.list({ limit: PENDING_SHOWN }),
    pendingItemsApi.list({ type: 'gate', limit: 1 }),
    pendingItemsApi.list({ type: 'question', limit: 1 }),
    pendingItemsApi.list({ type: 'finding_escalated', limit: 1 }),
  ])
  pending.value = oldestFirst(all.items).slice(0, PENDING_SHOWN)
  pendingTotals.value = {
    all: all.total,
    gate: gates.total,
    question: questions.total,
    finding_escalated: escalated.total,
  }
})

const monthCosts = ref<UserCosts>()
const costsLoader = useLoader(async () => {
  monthCosts.value = await costsApi.getUserCosts()
})

const agents = computed(() => runningAgents(projects.value))
const severeFindings = computed(() => criticalOrHigh(projects.value))
const largest = computed(() => monthCosts.value?.largest_spend[0])

/** Canvas "Pendências" card: the chip of each kind of item. */
const PENDING_TONES: Record<PendingItem['type'], string> = {
  gate: 'warn',
  question: 'vio',
  finding_escalated: 'crit',
  budget: 'warn',
}

void projectsLoader.run()
void pendingLoader.run()
void costsLoader.run()
</script>

<template>
  <!-- Layout: canvas "Desktop · Início". -->
  <div class="home-head">
    <div>
      <h1 v-if="session.user" data-testid="home-greeting">
        {{ $t('home.greeting', { name: session.user.name }) }}
      </h1>
      <h1 v-else>{{ $t('pages.home.title') }}</h1>
      <p class="t2 home-greeting">{{ $t('pages.home.description') }}</p>
    </div>
    <router-link class="btn btn-p" :to="{ name: ROUTE_NAMES.newProject }">
      <CanvasIcon name="plus" :size="16" :stroke-width="2.2" />
      {{ $t('projects.list.newProject') }}
    </router-link>
  </div>

  <!-- S-19 (RK-16): every new desktop app session is flagged until the person reviews it. -->
  <p
    v-if="(session.user?.local_client_sessions_to_review ?? 0) > 0"
    class="note"
    data-testid="local-sessions-to-review"
  >
    <span>
      {{ $t('localClient.toReview', session.user?.local_client_sessions_to_review ?? 0) }}
      <router-link :to="{ name: ROUTE_NAMES.localClientSessions }">
        {{ $t('localClient.review') }}
      </router-link>
    </span>
  </p>

  <!-- ESP-03 3: indicators. -->
  <section aria-labelledby="home-indicators-heading">
    <h2 id="home-indicators-heading" class="sr-only">{{ $t('home.indicators') }}</h2>
    <ul class="kpis" data-testid="home-indicators">
      <li>
        <router-link class="card kpi" :to="{ name: ROUTE_NAMES.projects }">
          <span class="ki ki-blue"><CanvasIcon name="folder" :size="24" /></span>
          <span class="kpi-text">
            <span class="t2 kpi-label">{{ $t('home.inProgress') }}</span>
            <span class="kpi-value" data-testid="home-in-progress">
              {{ format.integer(counts.in_progress ?? 0) }}
            </span>
            <span v-if="(counts.created ?? 0) > 0" class="t2 kpi-note">
              {{ $t('home.created', { n: counts.created ?? 0 }) }}
            </span>
          </span>
        </router-link>
      </li>
      <li>
        <router-link
          class="card kpi"
          :to="{ name: ROUTE_NAMES.pendingItems, query: { type: 'gate' } }"
        >
          <span class="ki ki-amber"><CanvasIcon name="gate" :size="24" /></span>
          <span class="kpi-text">
            <span class="t2 kpi-label">{{ $t('home.gatesAwaiting') }}</span>
            <span class="kpi-value" data-testid="home-gates">
              {{ format.integer(pendingTotals.gate) }}
            </span>
          </span>
        </router-link>
      </li>
      <li>
        <router-link
          class="card kpi"
          :to="{ name: ROUTE_NAMES.pendingItems, query: { type: 'question' } }"
        >
          <span class="ki ki-violet"><CanvasIcon name="question" :size="24" /></span>
          <span class="kpi-text">
            <span class="t2 kpi-label">{{ $t('home.questions') }}</span>
            <span class="kpi-value" data-testid="home-questions">
              {{ format.integer(pendingTotals.question) }}
            </span>
          </span>
        </router-link>
      </li>
      <li>
        <router-link
          class="card kpi"
          :to="{ name: ROUTE_NAMES.pendingItems, query: { type: 'finding_escalated' } }"
        >
          <span class="ki ki-red"><CanvasIcon name="alert" :size="24" /></span>
          <span class="kpi-text">
            <span class="t2 kpi-label">{{ $t('home.severeFindings') }}</span>
            <span class="kpi-value" data-testid="home-findings">
              {{ format.integer(severeFindings) }}
            </span>
            <span v-if="pendingTotals.finding_escalated > 0" class="t2 kpi-note">
              {{ $t('home.escalated', { n: pendingTotals.finding_escalated }) }}
            </span>
          </span>
        </router-link>
      </li>
    </ul>
  </section>

  <div class="home-layout">
    <div class="home-main">
      <!-- ESP-03 3: the projects table, newest update first. -->
      <section class="card home-card home-projects" aria-labelledby="home-projects-heading">
        <div class="card-head">
          <h2 id="home-projects-heading">{{ $t('pages.projects.title') }}</h2>
          <router-link :to="{ name: ROUTE_NAMES.projects }" class="card-link">
            {{ $t('home.seeAll') }}
          </router-link>
        </div>
        <LoadingState v-if="projectsLoader.loading.value" type="table" />
        <ProblemAlert
          v-else-if="projectsLoader.error.value"
          :error="projectsLoader.error.value"
          retry
          @retry="projectsLoader.run"
        />
        <p v-else-if="projects.length === 0" class="t2 card-empty" data-testid="home-no-projects">
          {{ $t('projects.list.emptyAll') }}
        </p>
        <div v-else class="table-scroll">
          <table class="tbl home-table" data-testid="home-projects">
            <thead>
              <tr>
                <th scope="col">{{ $t('home.columns.name') }}</th>
                <th scope="col">{{ $t('home.columns.state') }}</th>
                <th scope="col">{{ $t('home.columns.stages') }}</th>
                <th scope="col">{{ $t('home.columns.situation') }}</th>
                <th scope="col">{{ $t('home.columns.cost') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="project in projects" :key="project.id">
                <th scope="row">
                  <router-link
                    class="project-link"
                    :to="{ name: ROUTE_NAMES.project, params: { projectId: project.id } }"
                  >
                    <span class="project-name">{{ project.name }}</span>
                    <span v-if="project.business_need" class="t2 project-need">
                      {{ project.business_need }}
                    </span>
                  </router-link>
                </th>
                <td>
                  <StateLabel
                    machine="project"
                    :state="project.state"
                    :detail="
                      project.pause_reason
                        ? $t(`projects.pauseReason.${project.pause_reason}`)
                        : undefined
                    "
                  />
                </td>
                <td class="stages-cell">
                  <span class="stages">
                    <StageSegments :summary="project.summary" />
                    <span class="t2 stages-text">
                      {{
                        $t('home.stagesValue', {
                          approved: project.summary?.approved_stages ?? 0,
                          total: project.summary?.stages_total ?? 0,
                        })
                      }}
                    </span>
                  </span>
                </td>
                <td class="small-cell"><ProjectSituation :project="project" /></td>
                <td class="small-cell">
                  {{ format.money(project.summary?.accumulated_cost) || '-' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- ESP-03 3: the non-final waves of the projects. -->
      <section class="card home-card" aria-labelledby="home-agents-heading">
        <h2 id="home-agents-heading">{{ $t('home.agentsRunning') }}</h2>
        <p v-if="!projectsLoader.loading.value && agents.length === 0" class="t2 card-empty">
          {{ $t('home.noAgents') }}
        </p>
        <ul v-else class="plain-list" data-testid="home-agents">
          <li v-for="item in agents" :key="item.wave">
            <router-link
              class="li"
              :to="{
                name: ROUTE_NAMES.wave,
                params: { projectId: item.project.id, waveId: item.wave },
              }"
            >
              <span class="chip mute mono wave-id">{{ item.wave }}</span>
              <span class="agent-text">
                <span class="agent-title"
                  >{{ $t(`agents.${item.agent}`) }} · {{ item.project.name }}</span
                >
              </span>
              <StateLabel machine="wave" :state="item.state" />
            </router-link>
          </li>
        </ul>
      </section>
    </div>

    <aside class="home-side">
      <!-- ESP-03 3: what waits, oldest first; the box itself is Pendências (US-48). -->
      <section class="card home-card" aria-labelledby="home-pending-heading">
        <div class="card-head">
          <h2 id="home-pending-heading">{{ $t('pages.pendingItems.title') }}</h2>
          <router-link :to="{ name: ROUTE_NAMES.pendingItems }" class="card-link">
            {{ $t('home.openPending', { n: pendingTotals.all }) }}
          </router-link>
        </div>
        <LoadingState v-if="pendingLoader.loading.value" type="list-item-two-line@2" />
        <ProblemAlert
          v-else-if="pendingLoader.error.value"
          :error="pendingLoader.error.value"
          retry
          @retry="pendingLoader.run"
        />
        <p v-else-if="pending.length === 0" class="t2 card-empty">{{ $t('pendingItems.empty') }}</p>
        <ul v-else class="plain-list" data-testid="home-pending">
          <li v-for="item in pending" :key="`${item.type}:${item.target.id}`">
            <router-link class="li" :to="pendingDestination(item)">
              <span class="chip" :class="PENDING_TONES[item.type]">
                {{ $t(`pendingItems.types.${item.type}`) }}
              </span>
              <span class="pending-text">
                <span>{{ item.title ?? item.target.id }} · {{ item.project.name }}</span>
                <time
                  class="t2 pending-age"
                  :datetime="item.opened_at"
                  :title="format.dateTime(item.opened_at)"
                  >{{ format.age(item.opened_at) }}</time
                >
              </span>
            </router-link>
          </li>
        </ul>
      </section>

      <!-- ESP-03 3 (PL-02, PC-32, PD-11): the month of the user's projects; no monthly budget. -->
      <section class="card home-card" aria-labelledby="home-costs-heading" data-testid="home-costs">
        <h2 id="home-costs-heading">{{ $t('home.monthCost') }}</h2>
        <p class="t2 cost-hint">{{ $t('home.ofYourProjects') }}</p>
        <LoadingState v-if="costsLoader.loading.value" type="paragraph" />
        <ProblemAlert
          v-else-if="costsLoader.error.value"
          :error="costsLoader.error.value"
          retry
          @retry="costsLoader.run"
        />
        <template v-else-if="monthCosts">
          <p class="cost-total" data-testid="home-month-total">
            {{ format.money(monthCosts.total.total) }}
          </p>
          <p v-if="!isZeroMoney(monthCosts.total.estimated_lost)" class="t2 cost-estimated">
            {{
              $t('costs.estimatedPart', { value: format.money(monthCosts.total.estimated_lost) })
            }}
          </p>
          <div class="kv cost-first" data-testid="home-month-rework">
            <span class="t2">{{ $t('home.rework') }}</span>
            <span class="kv-value">
              {{
                $t('home.reworkShare', { percentage: format.percent(monthCosts.rework_percentage) })
              }}
            </span>
          </div>
          <div v-if="largest" class="kv" data-testid="home-month-largest">
            <span class="t2">{{ $t('home.largest') }}</span>
            <span class="kv-value">
              {{
                $t('home.largestSpend', { name: largest.name, cost: format.money(largest.total) })
              }}
            </span>
          </div>
        </template>
      </section>

      <!-- ESP-03 3 / ER-03-11: the last 24 h by replay, then live. -->
      <RecentActivity :projects="projects" />
    </aside>
  </div>
</template>

<style scoped>
.home-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
}

.home-greeting {
  margin: 4px 0 0;
}

.note {
  margin: 0;
}

.kpis {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
  gap: 16px;
  list-style: none;
  margin: 0;
  padding: 0;
}

.kpis > li {
  display: flex;
}

.kpi {
  flex: 1;
  display: flex;
  gap: 16px;
  padding: 20px;
  text-decoration: none;
  color: #f1f3ff;
}

.kpi:hover {
  color: #ffffff;
  border-color: rgba(167, 139, 250, 0.5);
}

.ki {
  width: 46px;
  height: 46px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}

.ki-blue {
  background: rgba(59, 130, 246, 0.22);
  color: #93c5fd;
}

.ki-amber {
  background: rgba(234, 179, 8, 0.18);
  color: #fcd34d;
}

.ki-violet {
  background: rgba(168, 85, 247, 0.22);
  color: #e9d5ff;
}

.ki-red {
  background: rgba(239, 68, 68, 0.18);
  color: #fca5a5;
}

.kpi-text {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.kpi-label {
  font-size: 13px;
}

.kpi-value {
  font-size: 26px;
  line-height: 32px;
  font-weight: 700;
}

.kpi-note {
  font-size: 12px;
}

.home-layout {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.home-main {
  flex: 999 1 600px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.home-side {
  flex: 1 1 320px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.home-card {
  padding: 18px 22px;
}

.home-projects {
  padding: 18px 22px 6px;
}

.card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.card-link {
  min-height: 44px;
  display: inline-flex;
  align-items: center;
  text-decoration: none;
  font-weight: 500;
}

.card-empty {
  margin: 12px 0;
}

.home-table {
  min-width: 760px;
  font-size: 14px;
}

.home-table th[scope='col'] {
  padding: 10px 12px 10px 0;
  font-size: 13px;
}

.home-table td,
.home-table th[scope='row'] {
  padding: 12px 12px 12px 0;
  border-bottom: 1px solid rgba(140, 150, 255, 0.1);
  vertical-align: middle;
  text-align: left;
  font-weight: 400;
}

.home-table tr:last-child td,
.home-table tr:last-child th {
  border-bottom: 0;
}

.project-link {
  color: #f1f3ff;
  text-decoration: none;
  display: flex;
  flex-direction: column;
}

.project-name {
  font-weight: 700;
}

.project-need {
  font-size: 13px;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.stages-cell {
  min-width: 150px;
}

.stages {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.stages-text {
  font-size: 12px;
}

.small-cell {
  font-size: 13px;
}

.wave-id {
  flex: none;
}

.agent-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.agent-title {
  font-weight: 600;
}

.pending-text {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.pending-age {
  font-size: 12px;
}

.cost-hint {
  margin: 4px 0 12px;
  font-size: 13px;
}

.cost-total {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
}

.cost-estimated {
  margin: 4px 0 0;
  font-size: 13px;
}

.cost-first {
  margin-top: 12px;
}

.kv-value {
  font-weight: 600;
  text-align: right;
}
</style>
