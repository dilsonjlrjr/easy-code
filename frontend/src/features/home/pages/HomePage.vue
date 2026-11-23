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

void projectsLoader.run()
void pendingLoader.run()
void costsLoader.run()
</script>

<template>
  <v-container max-width="1200">
    <div class="d-flex flex-wrap align-center ga-2 mb-2">
      <h1 class="text-h4">{{ $t('pages.home.title') }}</h1>
      <v-spacer />
      <v-btn color="primary" variant="flat" :to="{ name: ROUTE_NAMES.newProject }">
        {{ $t('projects.list.newProject') }}
      </v-btn>
    </div>
    <p v-if="session.user" class="text-body-1 mb-1" data-testid="home-greeting">
      {{ $t('home.greeting', { name: session.user.name }) }}
    </p>
    <p class="text-body-1 text-medium-emphasis mb-6">{{ $t('pages.home.description') }}</p>
    <!-- S-19 (RK-16): every new desktop app session is flagged until the person reviews it. -->
    <v-alert
      v-if="(session.user?.local_client_sessions_to_review ?? 0) > 0"
      type="warning"
      variant="tonal"
      class="mb-6"
      data-testid="local-sessions-to-review"
    >
      {{ $t('localClient.toReview', session.user?.local_client_sessions_to_review ?? 0) }}
      <router-link :to="{ name: ROUTE_NAMES.localClientSessions }" class="ml-1">
        {{ $t('localClient.review') }}
      </router-link>
    </v-alert>

    <!-- ESP-03 3: indicators. -->
    <section aria-labelledby="home-indicators-heading" class="mb-8">
      <h2 id="home-indicators-heading" class="sr-only">{{ $t('home.indicators') }}</h2>
      <ul class="indicators" data-testid="home-indicators">
        <li>
          <v-card variant="outlined" class="pa-4 h-100">
            <p class="text-body-2 text-medium-emphasis">{{ $t('home.inProgress') }}</p>
            <p class="text-h4" data-testid="home-in-progress">
              {{ format.integer(counts.in_progress ?? 0) }}
            </p>
            <p v-if="(counts.created ?? 0) > 0" class="text-body-2">
              {{ $t('home.created', { n: counts.created ?? 0 }) }}
            </p>
          </v-card>
        </li>
        <li>
          <v-card variant="outlined" class="pa-4 h-100">
            <p class="text-body-2 text-medium-emphasis">{{ $t('home.gatesAwaiting') }}</p>
            <p class="text-h4" data-testid="home-gates">
              {{ format.integer(pendingTotals.gate) }}
            </p>
          </v-card>
        </li>
        <li>
          <v-card variant="outlined" class="pa-4 h-100">
            <p class="text-body-2 text-medium-emphasis">{{ $t('home.questions') }}</p>
            <p class="text-h4" data-testid="home-questions">
              {{ format.integer(pendingTotals.question) }}
            </p>
          </v-card>
        </li>
        <li>
          <v-card variant="outlined" class="pa-4 h-100">
            <p class="text-body-2 text-medium-emphasis">{{ $t('home.severeFindings') }}</p>
            <p class="text-h4" data-testid="home-findings">{{ format.integer(severeFindings) }}</p>
            <p v-if="pendingTotals.finding_escalated > 0" class="text-body-2">
              {{ $t('home.escalated', { n: pendingTotals.finding_escalated }) }}
            </p>
          </v-card>
        </li>
      </ul>
    </section>

    <div class="home-layout">
      <div>
        <!-- ESP-03 3: the projects table, newest update first. -->
        <section aria-labelledby="home-projects-heading" class="mb-8">
          <div class="d-flex flex-wrap align-center ga-2 mb-2">
            <h2 id="home-projects-heading" class="text-h6">{{ $t('pages.projects.title') }}</h2>
            <v-spacer />
            <router-link :to="{ name: ROUTE_NAMES.projects }" class="text-body-2">
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
          <p v-else-if="projects.length === 0" data-testid="home-no-projects">
            {{ $t('projects.list.emptyAll') }}
          </p>
          <div v-else class="table-scroll">
            <v-table density="comfortable" data-testid="home-projects">
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
                      :to="{ name: ROUTE_NAMES.project, params: { projectId: project.id } }"
                    >
                      {{ project.name }}
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
                  <td>
                    <template v-if="project.state !== 'created' && project.summary">
                      {{
                        $t('home.stagesValue', {
                          approved: project.summary.approved_stages,
                          total: project.summary.stages_total,
                        })
                      }}
                    </template>
                    <template v-else>—</template>
                  </td>
                  <td><ProjectSituation :project="project" /></td>
                  <td>{{ format.money(project.summary?.accumulated_cost) || '—' }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
        </section>

        <!-- ESP-03 3: the non-final waves of the projects. -->
        <section aria-labelledby="home-agents-heading" class="mb-8">
          <h2 id="home-agents-heading" class="text-h6 mb-2">{{ $t('home.agentsRunning') }}</h2>
          <p v-if="!projectsLoader.loading.value && agents.length === 0" class="text-body-2">
            {{ $t('home.noAgents') }}
          </p>
          <ul v-else class="plain-list" data-testid="home-agents">
            <li v-for="item in agents" :key="item.wave" class="text-body-2 mb-1">
              <router-link
                :to="{
                  name: ROUTE_NAMES.wave,
                  params: { projectId: item.project.id, waveId: item.wave },
                }"
                class="monospace"
              >
                {{ item.wave }}
              </router-link>
              · {{ $t(`agents.${item.agent}`) }} · {{ item.project.name }} ·
              <StateLabel machine="wave" :state="item.state" />
            </li>
          </ul>
        </section>
      </div>

      <aside>
        <!-- ESP-03 3: what waits, oldest first; the box itself is Pendências (US-48). -->
        <section aria-labelledby="home-pending-heading" class="mb-8">
          <div class="d-flex flex-wrap align-center ga-2 mb-2">
            <h2 id="home-pending-heading" class="text-h6">{{ $t('pages.pendingItems.title') }}</h2>
            <v-spacer />
            <router-link :to="{ name: ROUTE_NAMES.pendingItems }" class="text-body-2">
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
          <p v-else-if="pending.length === 0" class="text-body-2">{{ $t('pendingItems.empty') }}</p>
          <ul v-else class="plain-list" data-testid="home-pending">
            <li v-for="item in pending" :key="`${item.type}:${item.target.id}`" class="mb-2">
              <router-link :to="pendingDestination(item)" class="text-body-2">
                {{ $t(`pendingItems.types.${item.type}`) }} · {{ item.target.id }}
              </router-link>
              <p class="text-body-2 text-medium-emphasis">
                {{ item.project.name }} ·
                <time :datetime="item.opened_at" :title="format.dateTime(item.opened_at)">{{
                  format.age(item.opened_at)
                }}</time>
              </p>
            </li>
          </ul>
        </section>

        <!-- ESP-03 3 (PL-02, PC-32, PD-11): the month of the user's projects; no monthly budget. -->
        <section aria-labelledby="home-costs-heading" data-testid="home-costs">
          <h2 id="home-costs-heading" class="text-h6 mb-2">{{ $t('home.monthCost') }}</h2>
          <LoadingState v-if="costsLoader.loading.value" type="paragraph" />
          <ProblemAlert
            v-else-if="costsLoader.error.value"
            :error="costsLoader.error.value"
            retry
            @retry="costsLoader.run"
          />
          <template v-else-if="monthCosts">
            <p class="text-h5" data-testid="home-month-total">
              {{ format.money(monthCosts.total.total) }}
            </p>
            <p class="text-body-2">{{ $t('home.ofYourProjects') }}</p>
            <p v-if="!isZeroMoney(monthCosts.total.estimated_lost)" class="text-body-2">
              {{
                $t('costs.estimatedPart', { value: format.money(monthCosts.total.estimated_lost) })
              }}
            </p>
            <p class="text-body-2" data-testid="home-month-rework">
              {{
                $t('home.reworkShare', { percentage: format.percent(monthCosts.rework_percentage) })
              }}
            </p>
            <p v-if="largest" class="text-body-2" data-testid="home-month-largest">
              {{
                $t('home.largestSpend', { name: largest.name, cost: format.money(largest.total) })
              }}
            </p>
          </template>
        </section>
      </aside>
    </div>

    <!-- ESP-03 3 / ER-03-11: the last 24 h by replay, then live. -->
    <RecentActivity :projects="projects" />
  </v-container>
</template>

<style scoped>
.indicators {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  list-style: none;
  padding: 0;
}

.home-layout {
  display: grid;
  gap: 2rem;
  grid-template-columns: minmax(0, 1fr);
}

@media (min-width: 960px) {
  .home-layout {
    grid-template-columns: minmax(0, 2fr) minmax(260px, 1fr);
  }
}
</style>
