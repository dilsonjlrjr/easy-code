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
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import LiveConnectionStatus from '@/shared/ui/LiveConnectionStatus.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { useFormat } from '@/shared/format/useFormat'
import { moneyShare } from '@/shared/format/formatters'
import { subtractMoney } from '@/shared/format/money'
import type { Money } from '@/shared/api/contract'
import { repositoryApi } from '@/features/repository/api/repositoryApi'
import { costsApi } from '@/features/costs/api/costsApi'
import { nextStageOf } from '@/features/costs/composables/costView'
import ProjectLifecycleActions from '../components/ProjectLifecycleActions.vue'
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
const format = useFormat()
/** US-39 CA-1: the last budget alert received live; the project state does not change. */
const budgetAlert = ref<{ spend: Money; stopLimit: Money; percentage?: number }>()
/**
 * US-39 / PROJETO 10: why the project paused by budget, from the live `budget_exhausted` event:
 * the spend reached the stop limit, or the estimate of the next wave exceeds the balance (US-40).
 */
const budgetExhausted = ref<{ cause?: string; estimate?: Money; balance?: Money }>()

const projectId = computed(() => String(route.params.projectId))
const startFailure = ref<unknown>()
/** ESP-06 (CA-06-25, PC-17): the platform clone address, copyable, in the header. */
const cloneUrl = ref<string>()
/** ESP-06 3.2 (PC-19): the estimate of the first stage while the project is `created`. */
const firstStageEstimate = ref<Money>()
const starting = ref(false)
let startKey = newIdempotencyKey()
let refreshTimer: ReturnType<typeof setTimeout> | undefined

const loader = useLoader(async () => {
  store.clear()
  cloneUrl.value = undefined
  firstStageEstimate.value = undefined
  const lastSequence = await store.load(projectId.value)
  void loadHeaderExtras(projectId.value)
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

/** ER-06-11: a concluded or canceled project is read-only, with its history kept. */
const readOnly = computed(
  () => project.value?.state === 'completed' || project.value?.state === 'canceled',
)
const pausedByBudget = computed(
  () => project.value?.state === 'paused' && project.value.pause_reason === 'budget',
)
/** ESP-06 3.6: budget use = spend ÷ stop limit, in the organization's currency. */
const budgetUse = computed(() => {
  const summary = project.value?.summary
  if (!summary?.stop_limit) return undefined
  return {
    spend: summary.accumulated_cost,
    limit: summary.stop_limit,
    percentage: moneyShare(summary.accumulated_cost, summary.stop_limit),
  }
})

/**
 * US-39 CA-1: the alert to show. The live event wins; after a reload the project keeps the last
 * alert crossed (`summary.last_budget_alert_percentage`), so the warning does not vanish.
 */
const shownBudgetAlert = computed(() => {
  if (budgetAlert.value) return budgetAlert.value
  const summary = project.value?.summary
  const percentage = summary?.last_budget_alert_percentage
  if (percentage === null || percentage === undefined || !summary?.stop_limit) return undefined
  if (project.value?.state !== 'in_progress') return undefined
  return { spend: summary.accumulated_cost, stopLimit: summary.stop_limit, percentage }
})

/** ESP-06 6: paused shows who paused it and when. */
const pausedBy = computed(() => {
  const pause = project.value?.state === 'paused' ? project.value.pause : undefined
  if (!pause) return undefined
  const who =
    pause.by.type === 'human'
      ? (pause.by.user?.name ?? pause.by.id ?? '')
      : t('agents.orchestrator')
  return { who, when: pause.at }
})

/**
 * The cause of a pause by budget: the live event, else the pause record read by REST, so it
 * survives a reload (`pause.cause`, `pause.next_wave_estimate`).
 */
const exhaustedCause = computed(() => {
  if (budgetExhausted.value) return budgetExhausted.value
  const pause = project.value?.state === 'paused' ? project.value.pause : undefined
  if (!pause?.cause) return undefined
  const summary = project.value?.summary
  return {
    cause: pause.cause,
    estimate: pause.next_wave_estimate ?? undefined,
    balance: subtractMoney(summary?.stop_limit, summary?.accumulated_cost),
  }
})

/** ESP-30 2: the findings tab carries the count of open findings (`summary.open_findings`, PL-72). */
const openFindings = computed(() => {
  const open = project.value?.summary?.open_findings
  // `escalated` is also inside the severity counts: it is not added again.
  return open
    ? (['critical', 'high', 'medium', 'low'] as const).reduce(
        (sum, severity) => sum + (open[severity] ?? 0),
        0,
      )
    : 0
})
const findingsTabLabel = computed(() =>
  openFindings.value > 0
    ? t('pages.project.findingsCount', { n: openFindings.value })
    : t('pages.project.findings'),
)
/** Canvas: the count chip is red when a critical, high or escalated finding is open. */
const findingsTone = computed(() => {
  const open = project.value?.summary?.open_findings
  if (!open || openFindings.value === 0) return 'mute'
  return (open.critical ?? 0) + (open.high ?? 0) + (open.escalated ?? 0) > 0 ? 'crit' : 'warn'
})

/** ESP-27 2: the audit trail is for approvers and the lead (x-roles of listProjectAudit). */
const seesAudit = computed(() =>
  (project.value?.my_roles ?? []).some((role) => role === 'approver' || role === 'lead'),
)

const tabs = computed(() => [
  { name: ROUTE_NAMES.project, label: t('pages.project.workflow') },
  { name: ROUTE_NAMES.projectWaves, label: t('pages.project.waves') },
  { name: ROUTE_NAMES.projectArtifacts, label: t('pages.project.artifacts') },
  { name: ROUTE_NAMES.projectFindings, label: findingsTabLabel.value },
  { name: ROUTE_NAMES.traceability, label: t('pages.project.traceability') },
  { name: ROUTE_NAMES.projectCosts, label: t('pages.project.costs') },
  ...(seesAudit.value ? [{ name: ROUTE_NAMES.projectAudit, label: t('pages.project.audit') }] : []),
  { name: ROUTE_NAMES.projectConfiguration, label: t('pages.project.configuration') },
  { name: ROUTE_NAMES.projectMembers, label: t('pages.project.members') },
])

/** The stage and Release pages open inside the Workflow tab. */
const activeTab = computed(() =>
  route.name === ROUTE_NAMES.stage || route.name === ROUTE_NAMES.release
    ? ROUTE_NAMES.project
    : route.name,
)

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
  if (type.startsWith('project.budget_alerted')) {
    const spend = event.data.spend as Money | undefined
    const stopLimit = event.data.stop_limit as Money | undefined
    const alertLimit = event.data.alert_limit as Money | undefined
    if (spend && stopLimit)
      budgetAlert.value = { spend, stopLimit, percentage: moneyShare(alertLimit, stopLimit) }
  }
  if (type.startsWith('project.budget_exhausted')) {
    budgetAlert.value = undefined
    const spend = event.data.spend as Money | undefined
    const stopLimit = event.data.stop_limit as Money | undefined
    budgetExhausted.value = {
      cause: typeof event.data.cause === 'string' ? event.data.cause : undefined,
      estimate: event.data.next_wave_estimate as Money | undefined,
      balance: subtractMoney(stopLimit, spend),
    }
  }
  scheduleRefresh()
}

/**
 * The clone address and the first-stage estimate complete the header; either can be missing
 * (repository not served, estimate refused) and the rest of the page does not depend on them.
 */
async function loadHeaderExtras(id: string): Promise<void> {
  const firstStage =
    store.project?.state === 'created'
      ? nextStageOf(
          store.workflow?.stages.filter((stage) => stage.type === 'stage') ?? [],
          'created',
        )
      : undefined
  const [repository, estimate] = await Promise.all([
    repositoryApi.getRepository(id).catch(() => undefined),
    firstStage
      ? costsApi.getStageEstimate(id, firstStage.id).catch(() => undefined)
      : Promise.resolve(undefined),
  ])
  if (id !== projectId.value) return
  cloneUrl.value = repository?.clone_url
  firstStageEstimate.value = estimate?.estimated
}

async function copyCloneUrl(): Promise<void> {
  if (!cloneUrl.value) return
  try {
    await navigator.clipboard.writeText(cloneUrl.value)
    announce(t('repository.copied'))
  } catch {
    announce(t('repository.copyFailed'))
  }
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
  <!-- Layout: canvas "Projeto" (Projeto.dc.html; created, blocked and questions variants). -->
  <LoadingState v-if="loader.loading.value && !project" />
  <template v-else-if="loader.error.value && !project">
    <h1>{{ $t('pages.project.title') }}</h1>
    <!-- ER-00-13: same message for missing and without access, without the project name. -->
    <div v-if="notFound" class="note note-warn" role="alert">
      <CanvasIcon name="alert" />
      <span class="note-text">
        <span class="note-line">{{ $t('problems.not-found') }}</span>
        <router-link :to="{ name: ROUTE_NAMES.projects }">{{
          $t('common.goProjects')
        }}</router-link>
      </span>
    </div>
    <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
  </template>
  <template v-else-if="project">
    <AppBreadcrumbs :items="breadcrumbs" />
    <div class="project-head">
      <div class="project-title">
        <h1>{{ project.name }}</h1>
        <span class="project-meta">
          <StateLabel
            machine="project"
            :state="project.state"
            :detail="
              project.pause_reason ? $t(`projects.pauseReason.${project.pause_reason}`) : undefined
            "
          />
          <span v-if="project.business_need || project.lead" class="t2 meta-text">
            <template v-if="project.business_need">{{ project.business_need }}</template>
            <template v-if="project.business_need && project.lead"> · </template>
            <template v-if="project.lead">{{
              $t('projects.list.lead', { name: project.lead.name })
            }}</template>
          </span>
          <LiveConnectionStatus class="t2 meta-text" :status="live.status.value" />
        </span>
        <span v-if="cloneUrl" class="t2 meta-text clone-row" data-testid="header-clone-url">
          <span class="sr-only">{{ $t('repository.cloneUrl') }}:</span>
          <span class="mono clone-url">{{ cloneUrl }}</span>
          <button
            type="button"
            class="ibtn"
            :aria-label="$t('repository.copy')"
            :title="$t('repository.copy')"
            @click="copyCloneUrl"
          >
            <CanvasIcon name="copy" :size="16" />
          </button>
        </span>
        <span v-if="pausedBy" class="t2 meta-text" data-testid="paused-by">
          {{
            $t('projects.lifecycle.pausedBy', {
              who: pausedBy.who,
              when: format.dateTime(pausedBy.when),
            })
          }}
        </span>
        <span v-if="budgetUse" class="t2 meta-text" data-testid="budget-use">
          {{
            $t('projects.budget.use', {
              spend: format.money(budgetUse.spend),
              limit: format.money(budgetUse.limit),
              percentage: format.percent(budgetUse.percentage),
            })
          }}
        </span>
      </div>
      <div class="project-actions">
        <ProjectLifecycleActions
          :project="project"
          :workflow="store.workflow"
          @changed="store.load(projectId).catch(() => undefined)"
        />
        <!-- US-05 CA-1: the created project starts its workflow from the header (canvas). -->
        <button
          v-if="project.state === 'created' && canStart"
          type="button"
          class="btn btn-p"
          :disabled="starting"
          :aria-busy="starting ? 'true' : undefined"
          @click="startWorkflow"
        >
          <CanvasIcon name="play" :size="16" :stroke-width="2" />
          {{ $t('projects.view.start') }}
        </button>
      </div>
    </div>
    <div role="status" class="sr-only">{{ message }}</div>

    <nav :aria-label="$t('projects.view.sections')">
      <div class="tabs" role="tablist">
        <router-link
          v-for="tab in tabs"
          :key="tab.name"
          class="tab"
          :class="{ on: activeTab === tab.name }"
          role="tab"
          :aria-selected="activeTab === tab.name ? 'true' : 'false'"
          :aria-current="activeTab === tab.name ? 'page' : undefined"
          :to="{ name: tab.name, params: { projectId: project.id } }"
        >
          <template v-if="tab.name === ROUTE_NAMES.projectFindings">
            <span class="sr-only">{{ tab.label }}</span>
            <span aria-hidden="true">{{ $t('pages.project.findings') }}</span>
            <span v-if="project.summary" class="chip" :class="findingsTone" aria-hidden="true">
              {{ openFindings }}
            </span>
          </template>
          <template v-else>{{ tab.label }}</template>
        </router-link>
      </div>
    </nav>

    <div v-if="viewedAsAdministrator" class="note" data-testid="administrator-view">
      <span class="note-text">{{ $t('pages.project.asAdministrator') }}</span>
    </div>

    <!-- ER-06-11, US-52 CA-4: concluded or canceled is read-only, with history kept. -->
    <div v-if="readOnly" class="note" data-testid="read-only-notice">
      <span class="note-text">
        {{
          project.state === 'completed'
            ? $t('projects.lifecycle.completedReadOnly')
            : $t('projects.lifecycle.canceledReadOnly')
        }}
      </span>
    </div>
    <!-- ESP-06 3.2: paused by the budget comes first; it says what to do to resume (ER-06-8). -->
    <div v-if="pausedByBudget" class="note note-crit" role="alert" data-testid="budget-paused">
      <CanvasIcon name="alert" />
      <span class="note-text">
        <span class="note-line">
          {{
            budgetUse
              ? $t('projects.budget.pausedWithValues', {
                  spend: format.money(budgetUse.spend),
                  limit: format.money(budgetUse.limit),
                })
              : $t('projects.budget.paused')
          }}
        </span>
        <span
          v-if="exhaustedCause?.cause === 'estimate_exceeds_balance'"
          class="note-line"
          data-testid="budget-paused-estimate"
        >
          {{
            exhaustedCause.estimate && exhaustedCause.balance
              ? $t('projects.budget.estimateExceedsBalance', {
                  estimate: format.money(exhaustedCause.estimate),
                  balance: format.money(exhaustedCause.balance),
                })
              : $t('projects.budget.estimateExceedsBalanceShort')
          }}
        </span>
        <span class="note-line">{{ $t('projects.budget.howToResume') }}</span>
      </span>
      <router-link
        class="btn btn-p"
        :to="{ name: ROUTE_NAMES.projectConfiguration, params: { projectId: project.id } }"
      >
        {{ $t('projects.budget.raiseLimit') }}
      </router-link>
    </div>
    <div v-else-if="shownBudgetAlert" class="note note-warn" data-testid="budget-alert">
      <CanvasIcon name="alert" />
      <span class="note-text">
        {{
          $t('projects.budget.alerted', {
            percentage: format.percent(shownBudgetAlert.percentage),
            spend: format.money(shownBudgetAlert.spend),
            limit: format.money(shownBudgetAlert.stopLimit),
          })
        }}
      </span>
    </div>
    <!-- ESP-06 3.2: contextual notice, one per situation. -->
    <div v-if="openQuestionWave" class="note note-vio" data-testid="question-notice">
      <CanvasIcon name="question" />
      <span class="note-text">
        <strong>{{
          $t('projects.view.questionNotice', { agent: $t(`agents.${openQuestionWave.agent}`) })
        }}</strong>
      </span>
      <router-link
        class="btn btn-p"
        :to="{
          name: ROUTE_NAMES.wave,
          params: { projectId: project.id, waveId: openQuestionWave.wave },
        }"
      >
        {{ $t('projects.view.answer') }}
      </router-link>
    </div>
    <div v-if="project.state === 'created'" class="note">
      <CanvasIcon name="bolt" />
      <span class="note-text">
        <span class="note-line">{{ $t('projects.view.notStarted') }}</span>
        <span v-if="firstStageEstimate" class="note-line" data-testid="first-stage-estimate">
          {{ $t('projects.view.firstStageEstimate', { value: format.money(firstStageEstimate) }) }}
        </span>
      </span>
    </div>
    <StartRefusal v-if="startFailure" :error="startFailure" />

    <router-view />
  </template>
</template>

<style scoped>
.project-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: flex-end;
  gap: 16px;
}

.project-title {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.project-meta {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.meta-text {
  font-size: 13px;
}

.project-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.clone-row {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  min-width: 0;
}

.clone-url {
  overflow-wrap: anywhere;
}

.note-text {
  flex: 1 1 400px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.note > svg {
  flex: none;
}

.note-crit {
  flex-wrap: wrap;
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.1);
}

.note-warn {
  flex-wrap: wrap;
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.1);
}

.note-vio {
  flex-wrap: wrap;
  border-color: rgba(168, 85, 247, 0.5);
  background: rgba(168, 85, 247, 0.12);
}
</style>
