<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import type { ProjectState } from '@/shared/api/contract'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import { useSessionStore } from '@/shared/session/sessionStore'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { projectsApi, type Project } from '../api/projectsApi'

const STATE_FILTERS = ['in_progress', 'created', 'paused', 'completed', 'canceled'] as const
type StateFilter = 'all' | (typeof STATE_FILTERS)[number]
const STREAM_END_NOTICES = new Set(['access_removed', 'project_deleted', 'not_found'])

const route = useRoute()
const router = useRouter()
const format = useFormat()
const filter = ref<StateFilter>('all')
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

function dismissNotice(): void {
  void router.replace({ name: ROUTE_NAMES.projects })
}
</script>

<template>
  <v-container max-width="1200">
    <div class="d-flex flex-wrap align-center ga-2 mb-2">
      <h1 class="text-h4">{{ $t('pages.projects.title') }}</h1>
      <v-spacer />
      <v-btn color="primary" variant="flat" :to="{ name: ROUTE_NAMES.newProject }">
        {{ $t('projects.list.newProject') }}
      </v-btn>
    </div>
    <p class="text-body-1 text-medium-emphasis mb-4">{{ $t('pages.projects.description') }}</p>

    <v-alert
      v-if="notice"
      type="info"
      variant="tonal"
      role="alert"
      closable
      class="mb-4"
      @click:close="dismissNotice"
    >
      {{ $t(`projects.list.notices.${notice}`) }}
    </v-alert>

    <div
      v-if="session.isAdministrator"
      role="group"
      :aria-label="$t('projects.list.scopeLabel')"
      class="d-flex flex-wrap ga-2 mb-2"
    >
      <v-btn
        v-for="value in ['my', 'organization'] as const"
        :key="value"
        :variant="scope === value ? 'flat' : 'outlined'"
        :color="scope === value ? 'primary' : undefined"
        :aria-pressed="scope === value"
        size="small"
        @click="scope = value"
      >
        {{ $t(`projects.list.scopes.${value}`) }}
      </v-btn>
    </div>

    <!-- ESP-04: one state filter at a time, each with its count; the active one is exposed as pressed. -->
    <div
      role="group"
      :aria-label="$t('projects.list.filterLabel')"
      class="d-flex flex-wrap ga-2 mb-4"
    >
      <v-btn
        v-for="value in ['all', ...STATE_FILTERS] as StateFilter[]"
        :key="value"
        :variant="filter === value ? 'flat' : 'outlined'"
        :color="filter === value ? 'primary' : undefined"
        :aria-pressed="filter === value"
        size="small"
        @click="filter = value"
      >
        {{ $t(`projects.list.filters.${value}`) }} ({{ filterCount(value) }})
      </v-btn>
    </div>

    <LoadingState v-if="loader.loading.value" type="card@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="projects.length === 0">
      <p v-if="filter === 'all'" class="mb-4">{{ $t('projects.list.emptyAll') }}</p>
      <p v-else>{{ $t('projects.list.emptyFilter') }}</p>
    </template>
    <ul v-else class="project-cards">
      <li v-for="project in projects" :key="project.id">
        <v-card variant="outlined" class="pa-4 h-100">
          <h2 class="text-h6">
            <router-link :to="{ name: ROUTE_NAMES.project, params: { projectId: project.id } }">
              {{ project.name }}
            </router-link>
          </h2>
          <p class="text-body-2 text-medium-emphasis project-need">
            {{ firstLine(project.business_need) }}
          </p>
          <div class="mt-2">
            <StateLabel
              machine="project"
              :state="project.state"
              :detail="
                project.pause_reason
                  ? $t(`projects.pauseReason.${project.pause_reason}`)
                  : undefined
              "
            />
          </div>
          <p class="text-body-2 mt-2">
            <template v-if="project.state === 'created'">{{
              $t('projects.list.notStarted')
            }}</template>
            <template v-else-if="project.summary">
              {{
                $t('projects.list.stagesApproved', {
                  approved: project.summary.approved_stages,
                  total: project.summary.stages_total,
                })
              }}
            </template>
          </p>
          <p v-if="project.summary && project.summary.open_questions > 0" class="text-body-2">
            {{ $t('projects.list.openQuestions', { count: project.summary.open_questions }) }}
          </p>
          <p v-if="project.lead" class="text-body-2">
            {{ $t('projects.list.lead', { name: project.lead.name }) }}
          </p>
          <p v-if="project.summary" class="text-body-2">
            {{ $t('projects.list.cost', { cost: format.money(project.summary.accumulated_cost) }) }}
          </p>
        </v-card>
      </li>
    </ul>

    <div v-if="nextCursor && !loader.loading.value" class="mt-4">
      <v-btn variant="outlined" :loading="loadingMore" @click="loadMore">
        {{ $t('common.loadMore') }}
      </v-btn>
      <ProblemAlert v-if="moreFailure" :error="moreFailure" class="mt-2" />
    </div>
  </v-container>
</template>

<style scoped>
.project-cards {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  list-style: none;
  padding: 0;
}

.project-need {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
