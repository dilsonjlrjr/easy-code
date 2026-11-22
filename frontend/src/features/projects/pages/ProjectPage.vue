<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError, isApiError } from '@/shared/api/problemDetails'
import { waitForCommand } from '@/shared/commands/waitForCommand'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { describeEvent } from '@/shared/events/eventDescriptions'
import { useSessionStore } from '@/shared/session/sessionStore'
import type { CloudEvent, EventClass } from '@/shared/events/eventStream'
import { useProjectEventStream } from '@/shared/events/useProjectEventStream'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import LiveConnectionStatus from '@/shared/ui/LiveConnectionStatus.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import StartRefusal from '../components/StartRefusal.vue'
import { currentProjectApi, useCurrentProjectStore } from '../stores/currentProjectStore'
import { useProjectNoticesStore } from '../stores/projectNotices'

const REFRESH_DELAY_MS = 250
const REFRESHING_GROUPS = ['project.', 'stage.', 'wave.', 'gate.', 'finding.']

const { t } = useI18n()
const route = useRoute()
const store = useCurrentProjectStore()
const notices = useProjectNoticesStore()
const { message, announce } = useAnnouncer()
const live = useProjectEventStream()
const session = useSessionStore()

const projectId = computed(() => String(route.params.projectId))
const startFailure = ref<unknown>()
const starting = ref(false)
let startKey = newIdempotencyKey()
let refreshTimer: ReturnType<typeof setTimeout> | undefined

const loader = useLoader(async () => {
  store.clear()
  const lastSequence = await store.load(projectId.value)
  live.open({
    projectId: projectId.value,
    since: lastSequence,
    query: { class: 'domain' },
    onEvent: handleEvent,
    reload: () => store.load(projectId.value),
  })
})

const project = computed(() => store.project)
const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
const canStart = computed(() => project.value?.available_actions?.includes('start') ?? false)
/** ESP-26 section 6: the administrator without a role sees the project as such, and it is audited. */
const viewedAsAdministrator = computed(
  () => session.isAdministrator && (project.value?.my_roles.length ?? 0) === 0,
)
const openQuestionWave = computed(() =>
  project.value?.summary?.active_waves?.find((wave) => wave.state === 'awaiting_user'),
)

/** ESP-30 2: the findings tab carries the count of open findings (`summary.open_findings`, PL-72). */
const findingsTabLabel = computed(() => {
  const open = project.value?.summary?.open_findings
  const total = open ? Object.values(open).reduce((sum, value) => sum + value, 0) : 0
  return total > 0 ? t('pages.project.findingsCount', { n: total }) : t('pages.project.findings')
})

const tabs = computed(() => [
  { name: ROUTE_NAMES.project, label: t('pages.project.workflow') },
  { name: ROUTE_NAMES.projectWaves, label: t('pages.project.waves') },
  { name: ROUTE_NAMES.projectArtifacts, label: t('pages.project.artifacts') },
  { name: ROUTE_NAMES.projectFindings, label: findingsTabLabel.value },
  { name: ROUTE_NAMES.projectConfiguration, label: t('pages.project.configuration') },
  { name: ROUTE_NAMES.projectMembers, label: t('pages.project.members') },
])

const breadcrumbs = computed(() => [
  { title: t('pages.projects.title'), to: { name: ROUTE_NAMES.projects } },
  { title: project.value?.name ?? '' },
])

function scheduleRefresh(): void {
  if (refreshTimer !== undefined) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    refreshTimer = undefined
    void store.load(projectId.value).catch(() => undefined)
  }, REFRESH_DELAY_MS)
}

/** ER-06-4: state changes arrive on the stream and refresh the page without reloading it. */
function handleEvent(event: CloudEvent, eventClass: EventClass): void {
  if (eventClass !== 'domain') return
  const type = event.type.replace(/^br\.easycode\./, '')
  if (!REFRESHING_GROUPS.some((group) => type.startsWith(group))) return
  if (!type.startsWith('wave.checkpoint_written')) announce(describeEvent(t, event))
  scheduleRefresh()
}

/** US-05 CA-1 / CA-06-4: start the workflow; the command is followed until applied or refused. */
async function startWorkflow(): Promise<void> {
  const api = currentProjectApi()
  starting.value = true
  startFailure.value = undefined
  try {
    const accepted = await api.startWorkflow(projectId.value, startKey)
    announce(t('projects.start.pending'))
    const finished = await waitForCommand(accepted, (id) => api.getCommand(projectId.value, id))
    if (finished.state === 'refused') {
      startFailure.value = new ApiError(finished.refusal?.status ?? 409, finished.refusal)
    } else {
      announce(t('projects.start.applied'))
      await store.load(projectId.value)
    }
  } catch (error) {
    startFailure.value = error
  } finally {
    starting.value = false
    startKey = newIdempotencyKey()
  }
}

watch(
  projectId,
  (id) => {
    startFailure.value = notices.takeStartFailure(id)
    void loader.run()
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (refreshTimer !== undefined) clearTimeout(refreshTimer)
  store.clear()
})
</script>

<template>
  <v-container max-width="1200">
    <LoadingState v-if="loader.loading.value && !project" />
    <template v-else-if="loader.error.value && !project">
      <h1 class="text-h4 mb-4">{{ $t('pages.project.title') }}</h1>
      <!-- ER-00-13: same message for missing and without access, without the project name. -->
      <v-alert v-if="notFound" type="warning" variant="tonal" role="alert">
        <p>{{ $t('problems.not-found') }}</p>
        <router-link :to="{ name: ROUTE_NAMES.projects }">{{
          $t('common.goProjects')
        }}</router-link>
      </v-alert>
      <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
    </template>
    <template v-else-if="project">
      <AppBreadcrumbs :items="breadcrumbs" />
      <div class="d-flex flex-wrap align-center ga-3 mb-2">
        <h1 class="text-h4">{{ project.name }}</h1>
        <StateLabel
          machine="project"
          :state="project.state"
          :detail="
            project.pause_reason ? $t(`projects.pauseReason.${project.pause_reason}`) : undefined
          "
        />
        <v-spacer />
        <LiveConnectionStatus :status="live.status.value" />
      </div>
      <p v-if="project.lead" class="text-body-2 mb-4">
        {{ $t('projects.list.lead', { name: project.lead.name }) }}
      </p>
      <div role="status" class="sr-only">{{ message }}</div>
      <v-alert
        v-if="viewedAsAdministrator"
        type="info"
        variant="tonal"
        class="mb-4"
        data-testid="administrator-view"
      >
        {{ $t('pages.project.asAdministrator') }}
      </v-alert>

      <!-- ESP-06 3.2: contextual notice, one per situation. -->
      <v-alert
        v-if="openQuestionWave"
        type="warning"
        variant="tonal"
        class="mb-4"
        data-testid="question-notice"
      >
        <p>
          {{
            $t('projects.view.questionNotice', { agent: $t(`agents.${openQuestionWave.agent}`) })
          }}
        </p>
        <router-link
          :to="{
            name: ROUTE_NAMES.wave,
            params: { projectId: project.id, waveId: openQuestionWave.wave },
          }"
        >
          {{ $t('projects.view.answer') }}
        </router-link>
      </v-alert>
      <v-alert v-if="project.state === 'created'" type="info" variant="tonal" class="mb-4">
        <p>{{ $t('projects.view.notStarted') }}</p>
        <v-btn
          v-if="canStart"
          color="primary"
          variant="flat"
          class="mt-3"
          :loading="starting"
          @click="startWorkflow"
        >
          {{ $t('projects.view.start') }}
        </v-btn>
      </v-alert>
      <StartRefusal v-if="startFailure" :error="startFailure" class="mb-4" />

      <nav :aria-label="$t('projects.view.sections')" class="mb-6">
        <v-tabs
          color="primary"
          :model-value="route.name === ROUTE_NAMES.stage ? ROUTE_NAMES.project : route.name"
          show-arrows
        >
          <v-tab
            v-for="tab in tabs"
            :key="tab.name"
            :value="tab.name"
            :to="{ name: tab.name, params: { projectId: project.id } }"
          >
            {{ tab.label }}
          </v-tab>
        </v-tabs>
      </nav>
      <router-view />
    </template>
  </v-container>
</template>
