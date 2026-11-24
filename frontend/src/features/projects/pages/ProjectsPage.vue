<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import type { ProjectState } from '@/shared/api/contract'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import { useSessionStore } from '@/shared/session/sessionStore'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StageSegments from '@/shared/ui/StageSegments.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { projectsApi, type Project } from '../api/projectsApi'
import ProjectSituation from '../components/ProjectSituation.vue'
import {
  STATE_FILTERS,
  budgetUsed,
  openFindings,
  stateFilterFrom,
  type StateFilter,
} from '../composables/projectSituation'

const STREAM_END_NOTICES = new Set(['access_removed', 'project_deleted', 'not_found', 'deleted'])

const route = useRoute()
const router = useRouter()
const format = useFormat()
const { t } = useI18n()
const { message, announce } = useAnnouncer()
/** ESP-04 2: the chosen state filter lives in the address, so a link keeps it. */
const filter = computed<StateFilter>({
  get: () => stateFilterFrom(route.query),
  set: (value) => {
    void router.replace({ query: { ...route.query, state: value === 'all' ? undefined : value } })
  },
})
const session = useSessionStore()
/** ESP-26 section 2: the administrator also lists every project of the organization (US-02 CA-6). */
const scope = ref<'my' | 'organization'>('my')
const projects = ref<Project[]>([])
const counts = ref<Partial<Record<ProjectState, number>>>({})
const nextCursor = ref<string | null>(null)
const loadingMore = ref(false)
const moreFailure = ref<unknown>()

const loader = useLoader(async () => {
  const page = await projectsApi.listProjects({
    state: filter.value === 'all' ? undefined : [filter.value],
    scope: scope.value,
  })
  projects.value = page.items
  counts.value = page.count_by_state
  nextCursor.value = page.next_cursor
  // ESP-04 8: changing the filter announces the number of results.
  announce(t('dashboard.results', { n: filterCount(filter.value), shown: page.items.length }))
})

const totalCount = computed(() =>
  Object.values(counts.value).reduce((sum, value) => sum + value, 0),
)
const notice = computed(() => {
  const value = route.query.notice
  return typeof value === 'string' && STREAM_END_NOTICES.has(value) ? value : undefined
})

watch([filter, scope], () => void loader.run(), { immediate: true })

/** CA-04-8: the next page comes in without repeating or skipping a project. */
async function loadMore(): Promise<void> {
  if (!nextCursor.value) return
  loadingMore.value = true
  moreFailure.value = undefined
  try {
    const page = await projectsApi.listProjects({
      state: filter.value === 'all' ? undefined : [filter.value],
      cursor: nextCursor.value,
      scope: scope.value,
    })
    const known = new Set(projects.value.map((project) => project.id))
    projects.value = [...projects.value, ...page.items.filter((project) => !known.has(project.id))]
    nextCursor.value = page.next_cursor
  } catch (error) {
    moreFailure.value = error
  } finally {
    loadingMore.value = false
  }
}

function filterCount(value: StateFilter): number {
  return value === 'all' ? totalCount.value : (counts.value[value] ?? 0)
}

function firstLine(text: string | undefined): string {
  return text?.split('\n').find((line) => line.trim() !== '') ?? ''
}

/** ESP-04 3: the open findings by severity, or zero; the escalated ones are told apart below. */
function findingsText(project: Project): string {
  const list = openFindings(project.summary)
  if (list.length === 0) return format.integer(0)
  return list
    .map(([severity, count]) =>
      t('dashboard.severityCount', { n: count, severity: t(`findings.severity.${severity}`) }),
    )
    .join(', ')
}

function dismissNotice(): void {
  void router.replace({ name: ROUTE_NAMES.projects, query: { ...route.query, notice: undefined } })
}
</script>

<template>
  <!-- Layout: canvas "Projetos" (Projetos.dc.html). -->
  <AppBreadcrumbs :items="[{ title: $t('pages.projects.title') }]" />
  <div class="projects-head">
    <div>
      <h1>{{ $t('pages.projects.title') }}</h1>
      <p class="t2 projects-description">{{ $t('pages.projects.description') }}</p>
    </div>
    <router-link class="btn btn-p" :to="{ name: ROUTE_NAMES.newProject }">
      <CanvasIcon name="plus" :size="16" :stroke-width="2.2" />
      {{ $t('projects.list.newProject') }}
    </router-link>
  </div>

  <div v-if="notice" class="note projects-notice" role="alert">
    <span class="projects-notice-text">{{ $t(`projects.list.notices.${notice}`) }}</span>
    <button
      type="button"
      class="ibtn projects-notice-close"
      :aria-label="$t('common.close')"
      @click="dismissNotice"
    >
      <CanvasIcon name="close" :size="18" />
    </button>
  </div>

  <div
    v-if="session.isAdministrator"
    role="group"
    :aria-label="$t('projects.list.scopeLabel')"
    class="projects-filters"
  >
    <button
      v-for="value in ['my', 'organization'] as const"
      :key="value"
      type="button"
      class="tabf"
      :class="{ on: scope === value }"
      :aria-pressed="scope === value"
      @click="scope = value"
    >
      {{ $t(`projects.list.scopes.${value}`) }}
    </button>
  </div>

  <!-- ESP-04: one state filter at a time, each with its count; the active one is exposed as pressed. -->
  <div role="group" :aria-label="$t('projects.list.filterLabel')" class="projects-filters">
    <button
      v-for="value in ['all', ...STATE_FILTERS] as StateFilter[]"
      :key="value"
      type="button"
      class="tabf"
      :class="{ on: filter === value }"
      :aria-pressed="filter === value"
      @click="filter = value"
    >
      {{ $t(`projects.list.filters.${value}`) }}
      <!-- The brackets are in the name ("Todos (3)") but not on screen, as in the canvas pill. -->
      <span class="cnt"
        ><span class="paren">(</span>{{ filterCount(value) }}<span class="paren">)</span></span
      >
    </button>
  </div>

  <div role="status" class="sr-only">{{ message }}</div>
  <LoadingState v-if="loader.loading.value" type="card@3" />
  <ProblemAlert
    v-else-if="loader.error.value"
    :error="loader.error.value"
    retry
    @retry="loader.run"
  />
  <template v-else-if="projects.length === 0">
    <p v-if="filter === 'all'" class="t2 projects-empty">{{ $t('projects.list.emptyAll') }}</p>
    <p v-else class="t2 projects-empty">{{ $t('projects.list.emptyFilter') }}</p>
  </template>
  <ul v-else class="project-cards">
    <li v-for="project in projects" :key="project.id" class="card project-card">
      <div class="pc">
        <div class="pc-head">
          <div class="pc-title">
            <h2 class="pc-name">
              <router-link
                class="pc-link"
                :to="{ name: ROUTE_NAMES.project, params: { projectId: project.id } }"
              >
                {{ project.name }}
              </router-link>
            </h2>
            <span class="t2 pc-need">{{ firstLine(project.business_need) }}</span>
          </div>
          <StateLabel
            machine="project"
            :state="project.state"
            :detail="
              project.pause_reason ? $t(`projects.pauseReason.${project.pause_reason}`) : undefined
            "
          />
        </div>
        <div class="pc-progress">
          <div class="pc-now">
            <ProjectSituation :project="project" />
            <!-- CA-04-4: a created project has no stage counter. -->
            <span v-if="project.state !== 'created' && project.summary" class="t2 pc-stages">
              <span class="sr-only">{{
                $t('projects.list.stagesApproved', {
                  approved: project.summary.approved_stages,
                  total: project.summary.stages_total,
                })
              }}</span>
              <span aria-hidden="true">{{
                $t('projects.list.stagesOf', {
                  approved: project.summary.approved_stages,
                  total: project.summary.stages_total,
                })
              }}</span>
            </span>
          </div>
          <StageSegments :summary="project.summary" />
        </div>
        <div class="pc-meta">
          <div class="pc-cell">
            <span class="t2 pc-label"
              >{{ $t('projects.list.card.lead') }}<span class="sr-only">: </span></span
            >
            <span>{{ project.lead?.name ?? '-' }}</span>
          </div>
          <div class="pc-cell">
            <span class="t2 pc-label"
              >{{ $t('projects.list.card.budgetUsed') }}<span class="sr-only">: </span></span
            >
            <span
              v-if="budgetUsed(project.summary) !== undefined"
              data-testid="project-budget-used"
              >{{ format.percent(budgetUsed(project.summary)) }}</span
            >
            <span v-else>-</span>
            <span v-if="project.summary" class="t2 pc-cost">
              {{
                $t('projects.list.cost', { cost: format.money(project.summary.accumulated_cost) })
              }}
            </span>
          </div>
          <div class="pc-cell">
            <span class="t2 pc-label"
              >{{ $t('projects.list.card.openFindings') }}<span class="sr-only">: </span></span
            >
            <span v-if="project.summary" data-testid="project-open-findings">
              {{ findingsText(project) }}
              <!-- ESP-04 3: escalated in text (also counted in the severities above). -->
              <template v-if="(project.summary.open_findings?.escalated ?? 0) > 0">
                ·
                {{
                  $t('dashboard.escalated', { n: project.summary.open_findings?.escalated ?? 0 })
                }}
              </template>
            </span>
            <span v-else>-</span>
          </div>
        </div>
      </div>
    </li>
  </ul>

  <div v-if="nextCursor && !loader.loading.value" class="projects-more">
    <button
      type="button"
      class="btn"
      :disabled="loadingMore"
      :aria-busy="loadingMore"
      @click="loadMore"
    >
      {{ $t('common.loadMore') }}
    </button>
    <ProblemAlert v-if="moreFailure" :error="moreFailure" />
  </div>

  <p class="t2 projects-states">{{ $t('projects.list.statesHelp') }}</p>
</template>

<style scoped>
.projects-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
}

.projects-description {
  margin: 4px 0 0;
}

.projects-notice {
  margin: 0;
  align-items: center;
}

.projects-notice-text {
  flex: 1;
}

.projects-notice-close {
  flex: none;
  margin: -10px -8px -10px 0;
}

.projects-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.projects-empty {
  margin: 0;
}

.project-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 18px;
  list-style: none;
  margin: 0;
  padding: 0;
}

.project-card {
  overflow: hidden;
}

.pc {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 20px;
  color: #f1f3ff;
  height: 100%;
  box-sizing: border-box;
}

.pc:hover {
  background: rgba(255, 255, 255, 0.03);
}

.pc-head {
  display: flex;
  align-items: flex-start;
  gap: 14px;
}

.pc-title {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.pc-name {
  font-size: 16px;
  line-height: 20px;
  font-weight: 700;
}

/* The whole card is the link target, as on the canvas (`a.pc`); the name stays the link text. */
.pc-link {
  color: #f1f3ff;
  text-decoration: none;
}

.pc-link:hover {
  color: #f1f3ff;
}

.pc-link::after {
  content: '';
  position: absolute;
  inset: 0;
}

.pc-need {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pc-progress {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.pc-now {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 13px;
}

.pc-stages {
  white-space: nowrap;
}

.pc-meta {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  font-size: 13px;
}

.pc-cell {
  display: flex;
  flex-direction: column;
}

.pc-label,
.pc-cost {
  font-size: 12px;
}

.projects-more {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}

.projects-states {
  margin: 0;
  font-size: 13px;
}

.paren {
  font-size: 0;
}
</style>
