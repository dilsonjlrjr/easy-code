<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError, hasProblemSlug, isApiError } from '@/shared/api/problemDetails'
import { waitForCommand } from '@/shared/commands/waitForCommand'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import { redirectToSso } from '@/shared/session/sessionApi'
import { useSessionStore } from '@/shared/session/sessionStore'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import type { ProjectCosts, Stage } from '@/features/projects/api/projectsApi'
import {
  currentProjectApi,
  useCurrentProjectStore,
} from '@/features/projects/stores/currentProjectStore'
import {
  gatesApi,
  type ApprovalDecision,
  type DecisionRecord,
  type GateRound,
} from '@/features/gates/api/gatesApi'
import {
  savePendingApproval,
  takePendingApproval,
} from '@/features/gates/composables/pendingApproval'
import { repositoryApi, type Repository } from '@/features/repository/api/repositoryApi'
import {
  releaseApi,
  splitOpenFindings,
  type ReleaseBlock,
  type ReleaseSummary,
} from '../api/releaseApi'
import ReleaseConsolidation from '../components/ReleaseConsolidation.vue'

/**
 * ESP-17: the Release consolidates gates, tests, findings, accepted risks and cost, and the approver
 * decides the `gate.release`, always human (US-08 RN-1). Approving needs no block, every human check
 * confirmed and an SSO authentication at most 10 minutes old: the page goes to the step-up and sends
 * the confirmed approval once back (CA-4). After approval the repository shows the version mark
 * (CA-3) and the lead or an approver concludes the project (US-52). The page follows the project
 * stream through the project store (ER-17-20).
 */
const GATE = 'gate.release'
const TABS = ['consolidation', 'notes', 'delivery'] as const
type Tab = (typeof TABS)[number]

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const format = useFormat()
const session = useSessionStore()
const store = useCurrentProjectStore()
const { message, announce } = useAnnouncer()

const summary = ref<ReleaseSummary>()
const stage = ref<Stage>()
const round = ref<GateRound>()
const repository = ref<Repository>()
const costs = ref<ProjectCosts>()
const deciding = ref(false)
const reauthenticating = ref(false)
const decisionFailure = ref<unknown>()
let approveKey = newIdempotencyKey()
let rejectKey = newIdempotencyKey()
let firstLoad = true

const project = computed(() => store.project)
const projectId = computed(() => String(route.params.projectId))

async function loadRound(): Promise<void> {
  const rounds = await gatesApi.listRounds(projectId.value, GATE)
  const latest = rounds.items.reduce<number | undefined>(
    (max, item) => (max === undefined || item.round > max ? item.round : max),
    undefined,
  )
  round.value =
    latest === undefined ? undefined : await gatesApi.getRound(projectId.value, GATE, latest)
}

const loader = useLoader(async () => {
  const id = projectId.value
  const [loadedSummary, loadedStage] = await Promise.all([
    releaseApi.getReleaseSummary(id),
    currentProjectApi().getStage(id, 'release'),
    loadRound(),
    repositoryApi
      .getRepository(id)
      .then((value) => (repository.value = value))
      .catch(() => undefined),
    currentProjectApi()
      .getCosts(id)
      .then((value) => (costs.value = value))
      .catch(() => undefined),
  ])
  summary.value = loadedSummary
  stage.value = loadedStage
  if (firstLoad) {
    firstLoad = false
    await resumeApprovalAfterStepUp()
  }
})

// ER-17-20: the project page reloads the store on gate, finding, stage and project events.
watch([projectId, () => store.revision], () => void loader.run(), { immediate: true })

// ------------------------------------------------------------------ tabs (?tab=)

const tab = computed<Tab>({
  get: () => {
    const value = route.query.tab
    return typeof value === 'string' && (TABS as readonly string[]).includes(value)
      ? (value as Tab)
      : 'consolidation'
  },
  set: (value) => void router.replace({ query: { ...route.query, tab: value } }),
})

// ------------------------------------------------------------------ state of the screen

const isApprover = computed(() => project.value?.my_roles.includes('approver') ?? false)
const blocks = computed(() => summary.value?.blocks ?? [])
const awaitingHuman = computed(() => round.value?.state === 'awaiting_human')
const myApproval = computed(() =>
  (round.value?.partial_approvals ?? []).some(
    (record) => record.per.user?.id === session.user?.id || record.per.id === session.user?.id,
  ),
)
/** ESP-17 section 6: the approver sees "approve" while the round waits for a person. */
const showApprove = computed(() => isApprover.value && awaitingHuman.value && !myApproval.value)
const approveOffered = computed(() => round.value?.available_actions.includes('approve') ?? false)
/** Approving is unavailable with any block (ER-17-4, ER-17-6); the list is the reason (8). */
const approveUnavailable = computed(() => blocks.value.length > 0 || !approveOffered.value)
const canReject = computed(
  () => isApprover.value && (round.value?.available_actions.includes('reject') ?? false),
)
const approvals = computed(() => ({
  done: round.value?.partial_approvals?.length ?? 0,
  needed: round.value?.min_approvals ?? 1,
}))
const humanChecks = computed(
  () => round.value?.checks.filter((check) => check.type === 'human') ?? [],
)
const separationOfDuties = computed(() => {
  if (!hasProblemSlug(decisionFailure.value, 'separation-of-duties')) return undefined
  const reason = decisionFailure.value.problem?.reason
  return reason === 'risk_acceptance_same_cycle' || reason === 'approver_grace_period'
    ? `gates.separation.${reason}`
    : 'gates.separation.generic'
})
const releaseApproved = computed(
  () => (stage.value?.state ?? summary.value?.stage_state) === 'approved',
)
const releaseArtifact = computed(() =>
  stage.value?.artifacts.find((artifact) => artifact.type === 'RELEASE'),
)

function stageLabel(id: string): string {
  return i18n.te(`stages.${id}`) ? t(`stages.${id}`) : id
}

/** The dependency the blocked release waits for (ESP-17 section 6: "espera a Segurança"). */
const waitingFor = computed(() => {
  const dependencies = stage.value?.depends_on ?? []
  const pending = dependencies.find(
    (id) => store.workflow?.stages.find((item) => item.id === id)?.state !== 'approved',
  )
  return pending ? stageLabel(pending) : undefined
})

const statusText = computed(() => {
  const stageState = stage.value?.state
  const roundState = round.value?.state
  if (stageState === 'blocked' || stageState === null || stageState === undefined)
    return waitingFor.value
      ? t('release.status.blockedBy', { stage: waitingFor.value })
      : t('release.status.blocked')
  if (stageState === 'ready' || stageState === 'running') return t('release.status.preparing')
  if (stageState === 'approved') return t('release.status.approved')
  if (stageState === 'returned' || roundState === 'rejected') return t('release.status.returned')
  if (roundState === 'invalidated') return t('release.status.invalidated')
  if (roundState === 'evaluating') return t('release.status.evaluating')
  if (roundState === 'awaiting_human') {
    if (blocks.value.length > 0) return t('release.status.blockedByFindings')
    if (myApproval.value) return t('release.status.youApproved')
    return isApprover.value ? t('release.status.readyForYou') : t('release.status.waitingApprover')
  }
  return t('release.status.preparing')
})

function who(record: DecisionRecord): string {
  if (record.per.type === 'human') return record.per.user?.name ?? record.per.id ?? ''
  return t('agents.orchestrator')
}

function blockRoute(block: ReleaseBlock) {
  if (block.type === 'active_wave')
    return {
      name: ROUTE_NAMES.wave,
      params: { projectId: projectId.value, waveId: block.reference },
    }
  if (block.type === 'gate_not_approved')
    return {
      name: ROUTE_NAMES.gate,
      params: { projectId: projectId.value, gateId: block.reference },
    }
  return {
    name: ROUTE_NAMES.finding,
    params: { projectId: projectId.value, findingId: block.reference },
  }
}

function blockReference(block: ReleaseBlock): string {
  if (block.type === 'gate_not_approved') return stageLabel(block.reference.replace(/^gate\./, ''))
  return block.reference
}

// ------------------------------------------------------------------ approve (3.7)

const approveDialog = ref(false)
const confirmedChecks = ref<string[]>([])
const comment = ref('')
const missingChecks = computed(
  () => humanChecks.value.filter((check) => !confirmedChecks.value.includes(check.check)).length,
)

async function openApprove(): Promise<void> {
  if (approveUnavailable.value) {
    // aria-disabled keeps the button focusable; the reason is the list of blocks.
    await nextTick()
    document.getElementById('release-blocks')?.focus()
    return
  }
  decisionFailure.value = undefined
  confirmedChecks.value = []
  comment.value = ''
  approveDialog.value = true
}

function approvalDecision(): ApprovalDecision {
  const text = comment.value.trim()
  return { confirmed_human_checks: [...confirmedChecks.value], ...(text ? { comment: text } : {}) }
}

async function approve(decision: ApprovalDecision, afterStepUp = false): Promise<void> {
  const current = round.value
  if (!current) return
  deciding.value = true
  decisionFailure.value = undefined
  try {
    round.value = await gatesApi.approve(projectId.value, GATE, current.round, decision, {
      idempotencyKey: approveKey,
      csrfToken: session.csrfToken,
    })
    approveDialog.value = false
    announce(t('release.approve.done'))
    await store.load(projectId.value).catch(() => undefined)
  } catch (error) {
    if (hasProblemSlug(error, 'reauthentication-required') && !afterStepUp) {
      // CA-4 / ER-17-7: to the SSO; nothing is recorded until the return.
      savePendingApproval({
        projectId: projectId.value,
        gateId: GATE,
        round: current.round,
        decision,
      })
      reauthenticating.value = true
      redirectToSso(
        router.resolve({ name: ROUTE_NAMES.release, params: { projectId: projectId.value } })
          .fullPath,
        { reauthenticate: true },
      )
      return
    }
    decisionFailure.value = error
    if (hasProblemSlug(error, 'separation-of-duties')) approveDialog.value = false
    if (isApiError(error) && error.status === 409) void loader.run()
  } finally {
    deciding.value = false
    approveKey = newIdempotencyKey()
  }
}

/** Back from the step-up: the approval already confirmed is sent once (ESP-17 section 2). */
async function resumeApprovalAfterStepUp(): Promise<void> {
  const current = round.value
  if (!current) return
  const pending = takePendingApproval(projectId.value, GATE, current.round)
  if (!pending || !isApprover.value || !approveOffered.value) return
  announce(t('gates.sendingAfterStepUp'))
  await approve(pending.decision, true)
}

// ------------------------------------------------------------------ reject (no step-up, ER-17-7)

const rejectDialog = ref(false)
const reason = ref('')
const reasonError = ref(false)
const reasonField = ref<{ focus: () => void }>()

function openReject(): void {
  decisionFailure.value = undefined
  reason.value = ''
  reasonError.value = false
  rejectDialog.value = true
  void nextTick(() => setTimeout(() => reasonField.value?.focus(), 50))
}

async function reject(): Promise<void> {
  const current = round.value
  if (!current) return
  // CA-17-11: no reason, no rejection.
  if (reason.value.trim() === '') {
    reasonError.value = true
    reasonField.value?.focus()
    return
  }
  deciding.value = true
  decisionFailure.value = undefined
  try {
    round.value = await gatesApi.reject(
      projectId.value,
      GATE,
      current.round,
      { reason: reason.value.trim() },
      { idempotencyKey: rejectKey, csrfToken: session.csrfToken },
    )
    rejectDialog.value = false
    announce(t('release.reject.done'))
    await store.load(projectId.value).catch(() => undefined)
  } catch (error) {
    decisionFailure.value = error
    if (hasProblemSlug(error, 'separation-of-duties')) rejectDialog.value = false
    if (isApiError(error) && error.status === 409) void loader.run()
  } finally {
    deciding.value = false
    rejectKey = newIdempotencyKey()
  }
}

// ------------------------------------------------------------------ conclude (3.8, US-52)

const completeDialog = ref(false)
const canComplete = computed(
  () => project.value?.available_actions?.includes('record_publication') ?? false,
)
const conditions = computed(() => {
  const activeWave =
    blocks.value.some((block) => block.type === 'active_wave') ||
    (project.value?.summary?.active_waves?.length ?? 0) > 0
  const blockingFinding = splitOpenFindings(summary.value?.open_findings ?? []).blocking.length > 0
  return [
    { key: 'releaseApproved', met: releaseApproved.value },
    { key: 'noActiveWave', met: !activeWave },
    { key: 'noBlockingFinding', met: !blockingFinding },
  ]
})

/** ESP-17 3.8: the effect and each condition with its state, in text. */
const completeLines = computed(() => [
  t('projects.lifecycle.complete.effect'),
  ...conditions.value.map((condition) =>
    t('release.complete.conditionLine', {
      condition: t(`release.complete.conditions.${condition.key}`),
      state: condition.met ? t('release.complete.met') : t('release.complete.pending'),
    }),
  ),
])

/** US-52 CA-2, CA-3: the refusal by its `reason` (`release_not_approved`, `active_wave`). */
function completionReason(failure: unknown): string | undefined {
  if (!hasProblemSlug(failure, 'state-conflict')) return undefined
  const problem = failure.problem
  const reason = problem?.reason ?? (!releaseApproved.value ? 'release_not_approved' : undefined)
  if (reason === 'release_not_approved') return t('projects.lifecycle.complete.releaseNotApproved')
  if (reason === 'active_wave') {
    const waves = Array.isArray(problem?.waves) ? problem.waves.join(', ') : ''
    return t('projects.lifecycle.complete.activeWaves', { waves })
  }
  return undefined
}

async function submitComplete(key: string): Promise<void> {
  const api = currentProjectApi()
  const accepted = await api.completeProject(projectId.value, key)
  const finished = await waitForCommand(accepted, (id) => api.getCommand(projectId.value, id))
  if (finished.state === 'refused') {
    await store.load(projectId.value).catch(() => undefined)
    throw new ApiError(finished.refusal?.status ?? 409, finished.refusal)
  }
  announce(t('projects.lifecycle.complete.applied'))
  await store.load(projectId.value).catch(() => undefined)
}
</script>

<template>
  <section aria-labelledby="release-heading" data-testid="release-page">
    <div role="status" class="sr-only">{{ message }}</div>
    <LoadingState v-if="loader.loading.value && !summary" />
    <ProblemAlert
      v-else-if="loader.error.value && !summary"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="summary">
      <div class="d-flex flex-wrap align-center ga-3 mb-1">
        <h2 id="release-heading" class="text-h5">{{ $t('release.title') }}</h2>
        <StateLabel v-if="stage?.state" machine="stage" :state="stage.state" />
        <StateLabel v-if="round" machine="gate" :state="round.state" />
      </div>
      <p class="text-body-2 mb-1">{{ $t('release.alwaysHuman') }}</p>
      <p v-if="round" class="text-body-2 mb-1" data-testid="release-approvals">
        {{ $t('gates.round', { n: round.round }) }} ·
        {{ $t('release.approvals', { done: approvals.done, needed: approvals.needed }) }}
        <template v-if="round.partial_approvals?.length">
          ({{ round.partial_approvals.map(who).join(', ') }})
        </template>
      </p>
      <v-alert type="info" variant="tonal" class="my-3" data-testid="release-status">
        {{ statusText }}
        <template v-if="releaseApproved && round?.decision">
          {{
            $t('release.status.approvedBy', {
              who: who(round.decision.record),
              when: format.dateTime(round.decision.record.when),
            })
          }}
        </template>
        <template v-if="round?.decision?.result === 'rejected' && round.decision.reason">
          {{ $t('release.status.rejectedReason', { reason: round.decision.reason }) }}
        </template>
      </v-alert>
      <v-alert v-if="reauthenticating" type="info" variant="tonal" role="status" class="my-3">
        {{ $t('gates.reauthenticating') }}
      </v-alert>

      <!-- ESP-17 3.2: every block, linked to its object; with any of them there is no approval. -->
      <section
        v-if="blocks.length"
        id="release-blocks"
        tabindex="-1"
        aria-labelledby="release-blocks-heading"
        class="blocks my-3"
        data-testid="release-blocks"
      >
        <h3 id="release-blocks-heading" class="text-subtitle-1 font-weight-medium mb-1">
          {{ $t('release.blocks.title') }}
        </h3>
        <ul class="plain-list">
          <li
            v-for="block in blocks"
            :key="`${block.type}-${block.reference}`"
            class="text-body-2 mb-1"
            :data-testid="`release-block-${block.reference}`"
          >
            {{ $t(`release.blocks.types.${block.type}`) }}:
            <router-link :to="blockRoute(block)" class="monospace">{{
              blockReference(block)
            }}</router-link>
            <template v-if="block.type === 'acceptance_expired'">
              <template v-if="block.valid_until">
                · {{ $t('release.blocks.expiredOn', { date: format.date(block.valid_until) }) }}
              </template>
              ·
              <router-link
                :to="{
                  name: ROUTE_NAMES.finding,
                  params: { projectId, findingId: block.reference },
                  hash: '#decision',
                }"
                >{{ $t('release.blocks.renew') }}</router-link
              >
            </template>
          </li>
        </ul>
        <p class="text-body-2 mt-1">{{ $t('gates.blocked.noOverride') }}</p>
      </section>

      <v-alert
        v-if="separationOfDuties"
        type="error"
        variant="tonal"
        role="alert"
        class="my-3"
        data-testid="release-decision-failure"
      >
        {{ $t(separationOfDuties) }}
      </v-alert>
      <ProblemAlert
        v-else-if="decisionFailure && !approveDialog && !rejectDialog"
        :error="decisionFailure"
        class="my-3"
        data-testid="release-decision-failure"
      />

      <div v-if="showApprove || canReject" class="d-flex flex-wrap ga-2 my-3">
        <v-btn
          v-if="showApprove"
          color="primary"
          variant="flat"
          :class="{ 'unavailable-action': approveUnavailable }"
          :aria-disabled="approveUnavailable ? 'true' : undefined"
          :aria-describedby="blocks.length ? 'release-blocks' : undefined"
          :disabled="deciding || reauthenticating"
          data-testid="release-approve"
          @click="openApprove"
        >
          {{ $t('release.approve.action') }}
        </v-btn>
        <v-btn
          v-if="canReject"
          variant="outlined"
          :disabled="deciding || reauthenticating"
          @click="openReject"
        >
          {{ $t('release.reject.action') }}
        </v-btn>
      </div>

      <!-- ESP-17 3.3: exit criteria of the current round; with no round, "not evaluated". -->
      <section aria-labelledby="release-checks-heading" class="my-4">
        <h3 id="release-checks-heading" class="text-subtitle-1 font-weight-medium mb-1">
          {{ $t('release.checks') }}
        </h3>
        <p v-if="!round" class="text-body-2">{{ $t('release.consolidation.notEvaluated') }}</p>
        <ul v-else class="plain-list" data-testid="release-checks">
          <li v-for="check in round.checks" :key="check.check" class="text-body-2 mb-1">
            {{ check.name }} · {{ $t(`gates.checkTypes.${check.type}`) }} ·
            {{
              $t(`gates.checkStatus.${check.status === 'failed' ? 'failedBlocks' : check.status}`)
            }}
          </li>
        </ul>
      </section>

      <nav :aria-label="$t('release.tabsLabel')" class="mb-4">
        <v-tabs v-model="tab" color="primary" density="compact">
          <v-tab v-for="item in TABS" :key="item" :value="item">
            {{ $t(`release.tabs.${item}`) }}
          </v-tab>
        </v-tabs>
      </nav>

      <ReleaseConsolidation
        v-if="tab === 'consolidation'"
        :project-id="projectId"
        :summary="summary"
        :costs="costs"
      />

      <section v-else-if="tab === 'notes'" aria-labelledby="release-notes-heading">
        <h3 id="release-notes-heading" class="text-subtitle-1 font-weight-medium mb-2">
          {{ $t('release.tabs.notes') }}
        </h3>
        <p v-if="releaseArtifact" class="text-body-2" data-testid="release-artifact">
          <router-link
            :to="{
              name: ROUTE_NAMES.artifact,
              params: { projectId, artifactId: releaseArtifact.id },
            }"
            class="monospace"
            >{{ releaseArtifact.id }}</router-link
          >
          · {{ releaseArtifact.title }} ·
          {{ $t('artifacts.versionNumber', { n: releaseArtifact.version }) }} ·
          {{ $t(`states.artifact.${releaseArtifact.state}`) }}
        </p>
        <p v-else class="text-body-2">{{ $t('release.notes.none') }}</p>
        <p class="text-body-2 mt-2">{{ $t('release.notes.docs') }}</p>
      </section>

      <section v-else aria-labelledby="release-delivery-heading" data-testid="release-delivery">
        <h3 id="release-delivery-heading" class="text-subtitle-1 font-weight-medium mb-2">
          {{ $t('release.tabs.delivery') }}
        </h3>
        <template v-if="releaseApproved">
          <ul v-if="repository?.marks?.length" class="plain-list mb-2" data-testid="release-marks">
            <li v-for="mark in repository.marks" :key="mark.name" class="text-body-2 mb-1">
              {{ $t('release.delivery.mark') }}: <span class="monospace">{{ mark.name }}</span> ·
              {{ $t('evidence.commit') }}:
              <span class="monospace" :title="mark.commit">{{ mark.commit.slice(0, 12) }}</span>
              <template v-if="mark.when"> · {{ format.dateTime(mark.when) }}</template>
            </li>
          </ul>
          <p v-else class="text-body-2 mb-2">{{ $t('release.delivery.noMark') }}</p>
          <p class="text-body-2 mb-2">{{ $t('release.delivery.releaseInDocs') }}</p>
        </template>
        <template v-else>
          <p class="text-body-2 mb-2" data-testid="release-to-mark">
            {{ $t('release.delivery.willMark') }}
            <template v-if="repository?.clone_url">
              <span class="monospace">{{ repository.clone_url }}</span>
            </template>
            <template v-if="repository?.last_commit?.sha">
              · {{ $t('evidence.commit') }}:
              <span class="monospace">{{ repository.last_commit.sha.slice(0, 12) }}</span>
            </template>
          </p>
        </template>
        <!-- CA-17-17: Phases 1 and 2 end at the repository; nothing is published from here. -->
        <p class="text-body-2 mb-4" data-testid="release-phase-note">
          {{ $t('release.delivery.phaseNote') }}
        </p>

        <section aria-labelledby="release-complete-heading" data-testid="release-complete">
          <h4 id="release-complete-heading" class="text-subtitle-2 mb-1">
            {{ $t('release.complete.title') }}
          </h4>
          <ul class="plain-list mb-2">
            <li
              v-for="condition in conditions"
              :key="condition.key"
              class="text-body-2 mb-1"
              :data-testid="`condition-${condition.key}`"
            >
              {{ $t(`release.complete.conditions.${condition.key}`) }}:
              {{ condition.met ? $t('release.complete.met') : $t('release.complete.pending') }}
            </li>
          </ul>
          <v-btn
            v-if="canComplete"
            color="primary"
            variant="flat"
            data-testid="release-complete-action"
            @click="completeDialog = true"
          >
            {{ $t('projects.lifecycle.complete.action') }}
          </v-btn>
        </section>
      </section>
    </template>

    <!-- ESP-17 3.7: blocks (empty), each human check confirmed, optional comment. -->
    <v-dialog
      v-model="approveDialog"
      max-width="560"
      :persistent="deciding"
      aria-labelledby="release-approve-title"
    >
      <v-card>
        <v-card-title>
          <h2 id="release-approve-title" class="text-h6 text-wrap">
            {{ $t('release.approve.title') }}
          </h2>
        </v-card-title>
        <v-form @submit.prevent="approve(approvalDecision())">
          <v-card-text>
            <p class="text-body-2 mb-2">{{ $t('release.approve.noBlocks') }}</p>
            <fieldset
              v-if="humanChecks.length > 0"
              class="plain-fieldset"
              aria-describedby="release-human-checks-help"
            >
              <legend class="text-body-1 mb-1">{{ $t('gates.humanChecks') }}</legend>
              <p id="release-human-checks-help" class="text-body-2 text-medium-emphasis mb-2">
                {{ $t('gates.humanChecksHelp') }}
              </p>
              <v-checkbox
                v-for="check in humanChecks"
                :key="check.check"
                v-model="confirmedChecks"
                :value="check.check"
                :label="check.name"
                density="compact"
                hide-details
              />
            </fieldset>
            <v-textarea
              v-model="comment"
              :label="$t('gates.commentOptional')"
              rows="2"
              auto-grow
              class="mt-4"
              :counter="5000"
            />
            <ProblemAlert v-if="decisionFailure" :error="decisionFailure" class="mt-2" />
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" :disabled="deciding" @click="approveDialog = false">
              {{ $t('common.cancel') }}
            </v-btn>
            <v-btn
              type="submit"
              color="primary"
              variant="flat"
              :loading="deciding"
              :disabled="missingChecks > 0"
            >
              {{
                missingChecks > 0
                  ? $t('release.approve.missing', { n: missingChecks })
                  : $t('release.approve.action')
              }}
            </v-btn>
          </v-card-actions>
        </v-form>
      </v-card>
    </v-dialog>

    <v-dialog
      v-model="rejectDialog"
      max-width="560"
      :persistent="deciding"
      aria-labelledby="release-reject-title"
    >
      <v-card>
        <v-card-title>
          <h2 id="release-reject-title" class="text-h6 text-wrap">
            {{ $t('release.reject.title') }}
          </h2>
        </v-card-title>
        <v-form @submit.prevent="reject">
          <v-card-text>
            <v-textarea
              ref="reasonField"
              v-model="reason"
              :label="$t('gates.reasonRequired')"
              :error-messages="reasonError ? [$t('gates.reasonMissing')] : []"
              rows="3"
              auto-grow
              :counter="10000"
              @update:model-value="reasonError = false"
            />
            <ProblemAlert v-if="decisionFailure" :error="decisionFailure" class="mt-2" />
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" :disabled="deciding" @click="rejectDialog = false">
              {{ $t('common.cancel') }}
            </v-btn>
            <v-btn type="submit" color="error" variant="flat" :loading="deciding">
              {{ $t('release.reject.action') }}
            </v-btn>
          </v-card-actions>
        </v-form>
      </v-card>
    </v-dialog>

    <ConfirmActionDialog
      v-if="project"
      v-model="completeDialog"
      :title="$t('projects.lifecycle.complete.title', { name: project.name })"
      :lines="completeLines"
      :keep-label="$t('projects.lifecycle.keep')"
      :confirm-label="$t('projects.lifecycle.complete.confirm')"
      confirm-color="primary"
      test-id="release-complete-dialog"
      :submit="submitComplete"
    >
      <template #default="{ failure }">
        <p
          v-if="completionReason(failure)"
          class="mt-2 font-weight-medium"
          data-testid="completion-reason"
        >
          {{ completionReason(failure) }}
        </p>
      </template>
    </ConfirmActionDialog>
  </section>
</template>

<style scoped>
.blocks {
  border-left: 3px solid rgb(var(--v-theme-error));
  padding-left: 0.75rem;
}

.blocks:focus-visible {
  outline: 2px solid rgb(var(--v-theme-primary));
}

.plain-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
}

.unavailable-action {
  opacity: 0.6;
}
</style>
