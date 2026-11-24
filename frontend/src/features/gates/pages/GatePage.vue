<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { projectsApi, type Project } from '@/features/projects/api/projectsApi'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { hasProblemSlug, isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import type { CloudEvent, EventClass } from '@/shared/events/eventStream'
import { useProjectEventStream } from '@/shared/events/useProjectEventStream'
import { useFormat } from '@/shared/format/useFormat'
import { redirectToSso } from '@/shared/session/sessionApi'
import { useSessionStore } from '@/shared/session/sessionStore'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import { stateTone, type ChipTone } from '@/shared/ui/stateFamilies'
import LiveConnectionStatus from '@/shared/ui/LiveConnectionStatus.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import {
  gatesApi,
  type ApprovalDecision,
  type CheckResult,
  type DecisionRecord,
  type GateRound,
  type GateRoundSummary,
} from '../api/gatesApi'
import { savePendingApproval, takePendingApproval } from '../composables/pendingApproval'

/**
 * ESP-12: the report of a gate round and, for the approver only, the decision (US-06). Approving is a
 * critical decision: it carries the anti-CSRF token and, when the api asks for a recent
 * authentication, the page goes to the SSO step-up and sends the confirmed approval once back
 * (CA-5). Rejecting needs a reason and no step-up (RN-2).
 */
const REFRESH_DELAY_MS = 250
const REFRESHING_GROUPS = ['gate.', 'stage.', 'finding.']

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const format = useFormat()
const session = useSessionStore()
const live = useProjectEventStream()
const { message, announce } = useAnnouncer()

const project = ref<Project>()
const round = ref<GateRound>()
const history = ref<GateRoundSummary[]>([])
const noRound = ref(false)
const deciding = ref(false)
const reauthenticating = ref(false)
const decisionFailure = ref<unknown>()
let refreshTimer: ReturnType<typeof setTimeout> | undefined
let approveKey = newIdempotencyKey()
let rejectKey = newIdempotencyKey()

const projectId = computed(() => String(route.params.projectId))
const gateId = computed(() => String(route.params.gateId))
const requestedRound = computed(() =>
  typeof route.params.round === 'string' && route.params.round !== ''
    ? Number(route.params.round)
    : undefined,
)

/** Only the approver decides (US-02 CA-2, ER-12-10); the api's actions say what the state allows. */
const isApprover = computed(() => project.value?.my_roles.includes('approver') ?? false)
const canApprove = computed(
  () => isApprover.value && (round.value?.available_actions.includes('approve') ?? false),
)
const canReject = computed(
  () => isApprover.value && (round.value?.available_actions.includes('reject') ?? false),
)
const awaitingHuman = computed(() => round.value?.state === 'awaiting_human')
const humanChecks = computed(
  () => round.value?.checks.filter((check) => check.type === 'human') ?? [],
)
const counts = computed(() => {
  const checks = round.value?.checks ?? []
  return {
    passed: checks.filter((check) => check.status === 'passed').length,
    alerts: checks.filter(
      (check) => check.status === 'failed' && check.failure_severity === 'alert',
    ).length,
    blocking: checks.filter(
      (check) => check.status === 'failed' && check.failure_severity === 'blocks',
    ).length,
  }
})
const stageName = computed(() => {
  const stage = round.value?.stage
  if (!stage) return ''
  return i18n.te(`stages.${stage}`) ? t(`stages.${stage}`) : stage
})
const breadcrumbs = computed(() => [
  { title: t('pages.projects.title'), to: { name: ROUTE_NAMES.projects } },
  {
    title: project.value?.name ?? '',
    to: { name: ROUTE_NAMES.project, params: { projectId: projectId.value } },
  },
  { title: t('gates.heading', { stage: stageName.value }) },
])
const notFound = computed(() => isApiError(loader.error.value) && loader.error.value.status === 404)
/** ER-12-8 / ER-12-9: the refusal says which separation rule applied (openapi `Problem.reason`). */
const separationOfDuties = computed(() => {
  if (!hasProblemSlug(decisionFailure.value, 'separation-of-duties')) return undefined
  const reason = decisionFailure.value.problem?.reason
  return reason === 'risk_acceptance_same_cycle' || reason === 'approver_grace_period'
    ? `gates.separation.${reason}`
    : 'gates.separation.generic'
})
/** US-31 CA-2 / ER-12-1: the failed blocking checks and the blocking findings, by name. */
const failedBlockingChecks = computed(
  () =>
    round.value?.checks.filter(
      (check) =>
        check.failure_severity === 'blocks' &&
        (check.status === 'failed' || check.status === 'error'),
    ) ?? [],
)
const blockingFindings = computed(() => round.value?.blocking_findings ?? [])
/** Why approving is not possible now (no "approve anyway", US-31 RN-1). */
const approvalBlocked = computed(() => {
  const state = round.value?.state
  if (state !== 'awaiting_human' && state !== 'evaluating') return false
  return failedBlockingChecks.value.length > 0 || blockingFindings.value.length > 0
})
const failedCheckNames = computed(() => {
  const names = new Map(round.value?.checks.map((check) => [check.check, check.name]) ?? [])
  return (round.value?.decision?.failed_checks ?? []).map((id) => names.get(id) ?? id)
})

async function loadRound(): Promise<void> {
  const rounds = await gatesApi.listRounds(projectId.value, gateId.value)
  history.value = rounds.items
  const number = requestedRound.value ?? rounds.items[0]?.round
  noRound.value = number === undefined
  round.value =
    number === undefined
      ? undefined
      : await gatesApi.getRound(projectId.value, gateId.value, number)
}

const loader = useLoader(async () => {
  project.value = await projectsApi.getProject(projectId.value)
  await loadRound()
  live.open({
    projectId: projectId.value,
    since: project.value.last_sequence,
    query: { class: 'domain' },
    onEvent: handleEvent,
    reload: async () => {
      await loadRound()
      return undefined
    },
  })
  await resumeApprovalAfterStepUp()
})

watch([projectId, gateId, requestedRound], () => void loader.run(), { immediate: true })

/** ESP-12 section 6: round, check and finding changes show live and are announced (RNF-08). */
function handleEvent(event: CloudEvent, eventClass: EventClass): void {
  if (eventClass !== 'domain') return
  const type = event.type.replace(/^br\.easycode\./, '')
  if (!REFRESHING_GROUPS.some((group) => type.startsWith(group))) return
  if (refreshTimer !== undefined) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    refreshTimer = undefined
    const before = round.value?.state
    void loadRound()
      .then(() => {
        if (round.value && round.value.state !== before) {
          announce(t('gates.stateChanged', { state: t(`states.gate.${round.value.state}`) }))
        }
      })
      .catch(() => undefined)
  }, REFRESH_DELAY_MS)
}

onBeforeUnmount(() => {
  if (refreshTimer !== undefined) clearTimeout(refreshTimer)
})

// ------------------------------------------------------------------ approve

const approveDialog = ref(false)
const confirmedChecks = ref<string[]>([])
const comment = ref('')
const allChecksConfirmed = computed(() =>
  humanChecks.value.every((check) => confirmedChecks.value.includes(check.check)),
)

function openApprove(): void {
  decisionFailure.value = undefined
  confirmedChecks.value = []
  comment.value = ''
  approveDialog.value = true
}

function approvalDecision(): ApprovalDecision {
  const text = comment.value.trim()
  return {
    confirmed_human_checks: [...confirmedChecks.value],
    ...(text ? { comment: text } : {}),
  }
}

async function approve(decision: ApprovalDecision, afterStepUp = false): Promise<void> {
  const current = round.value
  if (!current) return
  deciding.value = true
  decisionFailure.value = undefined
  try {
    round.value = await gatesApi.approve(projectId.value, gateId.value, current.round, decision, {
      idempotencyKey: approveKey,
      csrfToken: session.csrfToken,
    })
    approveDialog.value = false
    announce(t('gates.approved'))
    void loadRound().catch(() => undefined)
  } catch (error) {
    if (hasProblemSlug(error, 'reauthentication-required') && !afterStepUp) {
      // CA-5 / ER-02-8: straight to the SSO; nothing is recorded until the return.
      savePendingApproval({
        projectId: projectId.value,
        gateId: gateId.value,
        round: current.round,
        decision,
      })
      reauthenticating.value = true
      redirectToSso(
        router.resolve({
          name: ROUTE_NAMES.gate,
          params: { projectId: projectId.value, gateId: gateId.value, round: current.round },
        }).fullPath,
        { reauthenticate: true },
      )
      return
    }
    decisionFailure.value = error
    // ER-12-8: the refusal by separation of duties is explained on the page itself.
    if (hasProblemSlug(error, 'separation-of-duties')) approveDialog.value = false
    // ER-12-11 / ER-12-16: a refused decision shows the real state of the round.
    if (isApiError(error) && error.status === 409) void loadRound().catch(() => undefined)
  } finally {
    deciding.value = false
    approveKey = newIdempotencyKey()
  }
}

/** Back from the step-up on the same round: the approval already confirmed is sent once. */
async function resumeApprovalAfterStepUp(): Promise<void> {
  const current = round.value
  if (!current) return
  const pending = takePendingApproval(projectId.value, gateId.value, current.round)
  if (!pending || !canApprove.value) return
  announce(t('gates.sendingAfterStepUp'))
  await approve(pending.decision, true)
}

// ------------------------------------------------------------------ reject

const rejectDialog = ref(false)
const reason = ref('')
const reasonError = ref(false)
const unconfirmedChecks = ref<string[]>([])
const reasonField = ref<HTMLTextAreaElement>()

function openReject(): void {
  decisionFailure.value = undefined
  reason.value = ''
  reasonError.value = false
  unconfirmedChecks.value = []
  rejectDialog.value = true
  // ESP-12 section 8: the focus goes to the reason field when the rejection opens.
  void nextTick(() => setTimeout(() => reasonField.value?.focus(), 50))
}

async function reject(): Promise<void> {
  const current = round.value
  if (!current) return
  // CA-2 / ER-12-4: an empty reason (only spaces included) is refused before sending.
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
      gateId.value,
      current.round,
      {
        reason: reason.value.trim(),
        ...(unconfirmedChecks.value.length
          ? { unconfirmed_human_checks: [...unconfirmedChecks.value] }
          : {}),
      },
      { idempotencyKey: rejectKey, csrfToken: session.csrfToken },
    )
    rejectDialog.value = false
    announce(t('gates.rejected'))
    void loadRound().catch(() => undefined)
  } catch (error) {
    decisionFailure.value = error
    if (hasProblemSlug(error, 'separation-of-duties')) rejectDialog.value = false
    if (isApiError(error) && error.status === 409) void loadRound().catch(() => undefined)
  } finally {
    deciding.value = false
    rejectKey = newIdempotencyKey()
  }
}

// ------------------------------------------------------------------ display helpers

function who(record: DecisionRecord): string {
  if (record.per.type === 'human') return record.per.user?.name ?? record.per.id ?? ''
  return t('agents.orchestrator')
}

function origin(record: DecisionRecord): string {
  return record.origin ? t(`waves.origin.${record.origin}`) : t('agents.orchestrator')
}

/** openapi `WaveId`: evidence that points at a wave links to its sandbox evidence (US-41). */
const WAVE_REFERENCE = /^(REQ|ARQ|DBA|DSG|VAL|DEV|QA|SEG|REL)-[0-9]{2,}$/

function isWaveReference(reference: string): boolean {
  return WAVE_REFERENCE.test(reference)
}

/** ESP-15 3.2: a failed alert reads "alert", a failed blocking check "failed (blocks)". */
function checkStatus(check: CheckResult): string {
  if (check.status === 'failed')
    return t(
      check.failure_severity === 'alert'
        ? 'gates.checkStatus.alert'
        : 'gates.checkStatus.failedBlocks',
    )
  return t(`gates.checkStatus.${check.status}`)
}

/** Canvas "Gate": the round marker of a check; the status itself is always in the text. */
function checkMark(check: CheckResult): { symbol: string; tone: string } {
  if (check.status === 'passed') return { symbol: '✓', tone: 'mark-ok' }
  if (check.status === 'pending') return { symbol: '…', tone: 'mark-mute' }
  if (check.status === 'failed' && check.failure_severity === 'blocks')
    return { symbol: '!', tone: 'mark-crit' }
  return { symbol: '!', tone: 'mark-warn' }
}

function severityTone(severity: string): ChipTone {
  if (severity === 'critical' || severity === 'high') return 'crit'
  return severity === 'medium' ? 'warn' : 'mute'
}

const DECISION_TONES: Record<string, ChipTone> = {
  approved: 'ok',
  rejected: 'crit',
  invalidated: 'mute',
}
</script>

<template>
  <!-- Layout: canvas "Gate" (Gate.dc.html). -->
  <LoadingState v-if="loader.loading.value && !round && !noRound" />
  <template v-else-if="loader.error.value && !project">
    <h1>{{ $t('pages.gate.title') }}</h1>
    <!-- ER-00-13 / RNF-04: same message for missing and without access. -->
    <div v-if="notFound" class="note n-warn" role="alert">
      <span class="note-icon fg-warn"><CanvasIcon name="alert" :stroke-width="2" /></span>
      <span class="note-body">{{ $t('problems.not-found') }}</span>
    </div>
    <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
  </template>
  <template v-else-if="project">
    <AppBreadcrumbs :items="breadcrumbs" />
    <div role="status" class="sr-only">{{ message }}</div>

    <template v-if="noRound">
      <div class="page-head">
        <h1>{{ $t('pages.gate.title') }}</h1>
        <p class="t2 head-text">{{ $t('gates.noRound') }}</p>
      </div>
      <div>
        <router-link class="btn" :to="{ name: ROUTE_NAMES.project, params: { projectId } }">
          {{ $t('projects.stage.back') }}
        </router-link>
      </div>
    </template>

    <template v-else-if="round">
      <div class="page-head">
        <h1>{{ $t('gates.heading', { stage: stageName }) }}</h1>
        <span class="head-row">
          <StateLabel machine="gate" :state="round.state" />
          <span class="t2 head-text">
            {{ project.name }} · {{ $t('gates.round', { n: round.round }) }} ·
            <span :title="format.dateTime(round.opened_at)">
              {{ $t('gates.openedAge', { age: format.age(round.opened_at) }) }}
            </span>
            ·
            {{
              round.approval_mode === 'automatic'
                ? $t('gates.modeAutomatic')
                : $t('gates.modeHuman')
            }}
          </span>
          <LiveConnectionStatus :status="live.status.value" />
        </span>
        <span class="t2 head-text">
          {{ $t('gates.explanation') }}
          <router-link
            :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: round.stage } }"
          >
            {{ $t('gates.openStage', { stage: stageName }) }}
          </router-link>
        </span>
      </div>

      <!-- ESP-12 section 6: who waits for whom, in text. -->
      <div
        v-if="awaitingHuman"
        class="note"
        :class="isApprover ? 'n-warn' : undefined"
        data-testid="gate-waiting"
        role="alert"
      >
        <span class="note-icon" :class="isApprover ? 'fg-warn' : 'fg-info'">
          <CanvasIcon name="gate" />
        </span>
        <span class="note-body">
          {{ isApprover ? $t('gates.waitingForYou') : $t('gates.waitingForApproval') }}
        </span>
      </div>
      <div v-if="reauthenticating" class="note" role="status">
        <span class="note-icon fg-info"><CanvasIcon name="bolt" /></span>
        <span class="note-body">{{ $t('gates.reauthenticating') }}</span>
      </div>
      <div
        v-if="round.state === 'evaluating'"
        class="note"
        role="alert"
        data-testid="gate-evaluating"
      >
        <span class="note-icon fg-info"><CanvasIcon name="bolt" /></span>
        <span class="note-body">
          {{ $t('gates.evaluating') }}
          <template v-if="round.reevaluation?.finding">
            {{ $t('gates.reevaluationBy') }}
            <router-link
              :to="{
                name: ROUTE_NAMES.finding,
                params: { projectId, findingId: round.reevaluation.finding },
              }"
              class="mono"
              data-testid="gate-reevaluation-finding"
              >{{ round.reevaluation.finding }}</router-link
            >
          </template>
        </span>
      </div>
      <!-- US-31 CA-2, CA-3 / ER-12-1: why approval is unavailable, by name and in text. -->
      <div v-if="approvalBlocked" class="note n-crit" role="alert" data-testid="gate-blocked">
        <span class="note-icon fg-crit"><CanvasIcon name="alert" :stroke-width="2" /></span>
        <div class="note-body">
          <p class="note-title">{{ $t('gates.blocked.title') }}</p>
          <p v-if="failedBlockingChecks.length > 0">
            {{
              $t('gates.blocked.checks', {
                checks: failedBlockingChecks.map((check) => check.name).join(', '),
              })
            }}
          </p>
          <p v-if="blockingFindings.length > 0">
            {{ $t('gates.blocked.findings') }}
            <template v-for="(finding, index) in blockingFindings" :key="finding.id">
              <router-link
                :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
                class="mono"
                >{{ finding.id }}</router-link><span v-if="index < blockingFindings.length - 1"
                >,
              </span>
            </template>
          </p>
          <p>{{ $t('gates.blocked.noOverride') }}</p>
        </div>
      </div>
      <div
        v-if="separationOfDuties"
        class="note n-crit"
        role="alert"
        data-testid="decision-failure"
      >
        <span class="note-icon fg-crit"><CanvasIcon name="alert" :stroke-width="2" /></span>
        <span class="note-body">{{ $t(separationOfDuties) }}</span>
      </div>
      <ProblemAlert
        v-else-if="decisionFailure && !approveDialog && !rejectDialog"
        :error="decisionFailure"
        data-testid="decision-failure"
      />

      <div class="gate-layout">
        <div class="gate-main">
          <section class="card main-card" aria-labelledby="checks-heading">
            <div class="card-head">
              <h2 id="checks-heading">{{ $t('gates.checks') }}</h2>
              <span class="count-chips" data-testid="check-counts">
                <span class="chip ok">{{
                  $t('gates.countChips.passed', { n: counts.passed })
                }}</span>{{ ' '
                }}<span class="chip" :class="counts.alerts > 0 ? 'warn' : 'mute'">{{
                  $t('gates.countChips.alerts', { n: counts.alerts })
                }}</span
                >{{ ' '
                }}<span class="chip" :class="counts.blocking > 0 ? 'crit' : 'mute'">{{
                  $t('gates.countChips.blocking', { n: counts.blocking })
                }}</span>
              </span>
            </div>
            <p v-if="round.checks.length === 0" class="t2 card-text">{{ $t('gates.noChecks') }}</p>
            <ul v-else class="list check-list">
              <li
                v-for="check in round.checks"
                :key="check.check"
                class="li"
                :data-testid="`check-${check.check}`"
              >
                <span class="mark" :class="checkMark(check).tone" aria-hidden="true">{{
                  checkMark(check).symbol
                }}</span>
                <span class="item-text">
                  <span>{{ check.name }}</span>
                  <span class="t2 item-detail">
                    {{ $t('gates.checkType') }}: {{ $t(`gates.checkTypes.${check.type}`) }} ·
                    {{ $t('gates.status') }}: {{ checkStatus(check) }}
                  </span>
                  <ul v-if="check.evidence?.length" class="list">
                    <li v-for="(item, index) in check.evidence" :key="index" class="t2 item-detail">
                      <!-- PT-48: a wave reference opens the build and test evidence of that wave. -->
                      <router-link
                        v-if="item.reference && isWaveReference(item.reference)"
                        :to="{
                          name: ROUTE_NAMES.wave,
                          params: { projectId, waveId: item.reference },
                          hash: '#evidence',
                        }"
                        class="mono"
                        :aria-label="$t('evidence.openOf', { wave: item.reference })"
                      >
                        {{ item.reference }}
                      </router-link>
                      <span v-else-if="item.reference" class="mono">{{ item.reference }}</span>
                      <span v-if="item.path" class="mono"> {{ item.path }}</span>
                      {{ item.message }}
                    </li>
                  </ul>
                </span>
                <span class="chip" :class="check.failure_severity === 'blocks' ? 'mute' : 'info'">
                  <span class="sr-only">{{ $t('gates.severity') }}:</span>
                  {{ $t(`gates.severities.${check.failure_severity}`) }}
                </span>
              </li>
            </ul>
          </section>

          <section class="card main-card" aria-labelledby="outputs-heading">
            <h2 id="outputs-heading" class="card-title">{{ $t('gates.outputs') }}</h2>
            <p v-if="round.outputs.length === 0" class="t2 card-text">
              {{ $t('artifacts.empty') }}
            </p>
            <ul v-else class="list">
              <li v-for="output in round.outputs" :key="output.artifact" class="li">
                <span class="item-text">
                  <router-link
                    :to="{
                      name: ROUTE_NAMES.artifact,
                      params: { projectId, artifactId: output.artifact },
                    }"
                    class="mono item-link"
                  >
                    {{ output.artifact }}
                  </router-link>
                </span>
                <span
                  class="chip"
                  :class="output.state ? stateTone('artifact', output.state) : 'mute'"
                >
                  <template v-if="output.state">
                    {{ $t(`states.artifact.${output.state}`) }} ·
                  </template>
                  {{ $t('artifacts.versionNumber', { n: output.version }) }}
                </span>
              </li>
            </ul>
          </section>

          <section class="card main-card" aria-labelledby="findings-heading">
            <h2 id="findings-heading" class="card-title">{{ $t('projects.stage.findings') }}</h2>
            <p
              v-if="!round.blocking_findings?.length && !round.non_blocking_findings?.length"
              class="t2 card-text"
            >
              {{ $t('gates.noFindings') }}
            </p>
            <ul v-else class="list">
              <li
                v-for="finding in [
                  ...(round.blocking_findings ?? []),
                  ...(round.non_blocking_findings ?? []),
                ]"
                :key="finding.id"
                class="li wrap-row"
              >
                <router-link
                  :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
                  class="mono"
                >
                  {{ finding.id }}
                </router-link>
                <span class="item-text">{{ finding.title }}</span>
                <span class="chip" :class="severityTone(finding.severity)">
                  {{ $t(`findings.severity.${finding.severity}`) }}
                </span>
                <span class="chip" :class="stateTone('finding', finding.status)">
                  {{ $t(`states.finding.${finding.status}`) }}
                </span>
                <span v-if="finding.blocking" class="chip crit">
                  {{ $t('projects.stage.blocking') }}
                </span>
              </li>
            </ul>
          </section>
        </div>

        <div
          v-if="canApprove || canReject || round.decision || history.length > 1"
          class="gate-side"
        >
          <section
            v-if="canApprove || canReject"
            class="card side-card"
            aria-labelledby="your-decision-heading"
          >
            <h2 id="your-decision-heading">{{ $t('gates.yourDecision') }}</h2>
            <div class="decision-buttons">
              <button
                v-if="canApprove"
                type="button"
                class="btn btn-p"
                :disabled="deciding || reauthenticating"
                @click="openApprove"
              >
                {{ $t('gates.approve') }}
              </button>
              <button
                v-if="canReject"
                type="button"
                class="btn"
                :disabled="deciding || reauthenticating"
                @click="openReject"
              >
                {{ $t('gates.reject') }}
              </button>
            </div>
            <p class="t2 fine-print">{{ $t('gates.decisionNote') }}</p>
          </section>

          <section v-if="round.decision" class="card side-card" aria-labelledby="decision-heading">
            <h2 id="decision-heading">{{ $t('gates.decision') }}</h2>
            <dl class="kv-list" data-testid="gate-decision">
              <div class="kv">
                <dt class="t2">{{ $t('gates.result') }}</dt>
                <dd>
                  <span class="chip" :class="DECISION_TONES[round.decision.result] ?? 'mute'">
                    {{ $t(`gates.results.${round.decision.result}`) }}
                  </span>
                </dd>
              </div>
              <div class="kv">
                <dt class="t2">{{ $t('gates.by') }}</dt>
                <dd>{{ who(round.decision.record) }}</dd>
              </div>
              <div class="kv">
                <dt class="t2">{{ $t('gates.when') }}</dt>
                <dd>{{ format.dateTime(round.decision.record.when) }}</dd>
              </div>
              <div class="kv">
                <dt class="t2">{{ $t('gates.from') }}</dt>
                <dd>{{ origin(round.decision.record) }}</dd>
              </div>
              <div v-if="round.decision.reason" class="kv kv-stack">
                <dt class="t2">{{ $t('gates.reason') }}</dt>
                <dd class="pre-wrap">{{ round.decision.reason }}</dd>
              </div>
              <div v-if="failedCheckNames.length" class="kv kv-stack">
                <dt class="t2">{{ $t('gates.failedChecks') }}</dt>
                <dd data-testid="decision-failed-checks">{{ failedCheckNames.join(', ') }}</dd>
              </div>
              <div v-if="round.decision.blocking_findings?.length" class="kv kv-stack">
                <dt class="t2">{{ $t('gates.blockingFindings') }}</dt>
                <dd>
                  <template v-for="(id, index) in round.decision.blocking_findings" :key="id">
                    <router-link
                      :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: id } }"
                      class="mono"
                      >{{ id }}</router-link><span
                      v-if="index < round.decision.blocking_findings.length - 1"
                      >,
                    </span>
                  </template>
                </dd>
              </div>
              <div v-if="round.decision.comment" class="kv kv-stack">
                <dt class="t2">{{ $t('gates.comment') }}</dt>
                <dd class="pre-wrap">{{ round.decision.comment }}</dd>
              </div>
            </dl>
            <p v-if="round.decision.result === 'rejected'" class="t2 fine-print">
              {{ $t('gates.reworkNotice') }}
            </p>
          </section>

          <section
            v-if="history.length > 1"
            class="card side-card"
            aria-labelledby="history-heading"
          >
            <h2 id="history-heading">{{ $t('gates.history') }}</h2>
            <ul class="list">
              <li v-for="item in history" :key="item.round" class="li">
                <span class="item-text">
                  <router-link
                    :to="{
                      name: ROUTE_NAMES.gate,
                      params: { projectId, gateId, round: item.round },
                    }"
                    :aria-current="item.round === round.round ? 'page' : undefined"
                    class="item-link"
                  >
                    {{ $t('gates.round', { n: item.round }) }}
                  </router-link>
                </span>
                <span class="chip" :class="stateTone('gate', item.state)">
                  {{ $t(`states.gate.${item.state}`) }}
                </span>
              </li>
            </ul>
          </section>
        </div>
      </div>
    </template>
  </template>

  <!-- PT-31: one approval dialog, one box per human check; Approve waits until all are checked. -->
  <v-dialog
    v-model="approveDialog"
    max-width="460"
    :persistent="deciding"
    aria-labelledby="approve-dialog-title"
  >
    <v-card class="dlg-card">
      <h2 id="approve-dialog-title">{{ $t('gates.approveTitle', { stage: stageName }) }}</h2>
      <form class="dlg-form" @submit.prevent="approve(approvalDecision())">
        <fieldset
          v-if="humanChecks.length > 0"
          class="plain-fieldset"
          aria-describedby="human-checks-help"
        >
          <legend class="field-label">{{ $t('gates.humanChecks') }}</legend>
          <p id="human-checks-help" class="t2 field-help">{{ $t('gates.humanChecksHelp') }}</p>
          <label
            v-for="check in humanChecks"
            :key="check.check"
            :for="`approve-check-${check.check}`"
            class="check-option"
          >
            <input
              :id="`approve-check-${check.check}`"
              v-model="confirmedChecks"
              type="checkbox"
              :value="check.check"
            />
            <span>{{ check.name }}</span>
          </label>
        </fieldset>
        <p v-else class="dlg-text">{{ $t('gates.noHumanChecks') }}</p>
        <div class="field">
          <label for="gate-comment" class="field">
            <span class="field-label">{{ $t('gates.commentOptional') }}</span>
            <textarea id="gate-comment" v-model="comment" class="fld" rows="3" />
          </label>
          <span class="t2 field-counter">{{ `${comment.length} / 5000` }}</span>
        </div>
        <ProblemAlert v-if="decisionFailure" :error="decisionFailure" />
        <div class="dlg-actions">
          <button type="button" class="btn" :disabled="deciding" @click="approveDialog = false">
            {{ $t('common.cancel') }}
          </button>
          <button
            type="submit"
            class="btn btn-p"
            :disabled="deciding || !allChecksConfirmed"
            :aria-busy="deciding || undefined"
          >
            <v-progress-circular v-if="deciding" indeterminate size="16" width="2" />
            {{ $t('gates.approve') }}
          </button>
        </div>
      </form>
    </v-card>
  </v-dialog>

  <v-dialog
    v-model="rejectDialog"
    max-width="460"
    :persistent="deciding"
    aria-labelledby="reject-dialog-title"
  >
    <v-card class="dlg-card">
      <h2 id="reject-dialog-title">{{ $t('gates.rejectTitle', { stage: stageName }) }}</h2>
      <form class="dlg-form" @submit.prevent="reject">
        <div class="field">
          <label for="gate-reason" class="field">
            <span class="field-label">{{ $t('gates.reasonRequired') }}</span>
            <textarea
              id="gate-reason"
              ref="reasonField"
              v-model="reason"
              class="fld"
              rows="4"
              aria-required="true"
              :aria-invalid="reasonError || undefined"
              :aria-describedby="reasonError ? 'gate-reason-error' : undefined"
              @input="reasonError = false"
            />
          </label>
          <span v-if="reasonError" id="gate-reason-error" class="field-error" role="alert">
            {{ $t('gates.reasonMissing') }}
          </span>
          <span class="t2 field-counter">{{ `${reason.length} / 10000` }}</span>
        </div>
        <fieldset v-if="humanChecks.length > 0" class="plain-fieldset">
          <legend class="field-label">{{ $t('gates.unconfirmedChecks') }}</legend>
          <label
            v-for="check in humanChecks"
            :key="check.check"
            :for="`reject-check-${check.check}`"
            class="check-option"
          >
            <input
              :id="`reject-check-${check.check}`"
              v-model="unconfirmedChecks"
              type="checkbox"
              :value="check.check"
            />
            <span>{{ check.name }}</span>
          </label>
        </fieldset>
        <ProblemAlert v-if="decisionFailure" :error="decisionFailure" />
        <div class="dlg-actions">
          <button type="button" class="btn" :disabled="deciding" @click="rejectDialog = false">
            {{ $t('common.cancel') }}
          </button>
          <button
            type="submit"
            class="btn btn-p"
            :disabled="deciding"
            :aria-busy="deciding || undefined"
          >
            <v-progress-circular v-if="deciding" indeterminate size="16" width="2" />
            {{ $t('gates.reject') }}
          </button>
        </div>
      </form>
    </v-card>
  </v-dialog>
</template>

<style scoped>
/* Values copied from the canvas "Gate" artboard. */
.page-head {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.head-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.head-text {
  margin: 0;
  font-size: 13px;
}

.note p {
  margin: 0;
}

.note-body {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.note-title {
  font-weight: 700;
}

.note-icon {
  display: flex;
  flex: none;
}

.n-ok {
  border-color: rgba(34, 197, 94, 0.45);
  background: rgba(34, 197, 94, 0.12);
}

.n-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.12);
}

.n-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.12);
}

.fg-info {
  color: #c7d2fe;
}

.fg-warn {
  color: #fcd34d;
}

.fg-crit {
  color: #fca5a5;
}

.gate-layout {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.gate-main {
  flex: 3 1 520px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.gate-side {
  flex: 2 1 300px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.main-card {
  padding: 18px 22px;
}

.side-card {
  padding: 22px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.card-title {
  margin-bottom: 4px;
}

.card-text {
  margin: 0;
}

.count-chips {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.check-list {
  margin-top: 8px;
}

.wrap-row {
  flex-wrap: wrap;
}

.mark {
  width: 22px;
  height: 22px;
  border-radius: 11px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
}

.mark-ok {
  background: rgba(34, 197, 94, 0.18);
  color: #86efac;
}

.mark-warn {
  background: rgba(234, 179, 8, 0.18);
  color: #fcd34d;
}

.mark-crit {
  background: rgba(239, 68, 68, 0.18);
  color: #fca5a5;
}

.mark-mute {
  background: rgba(255, 255, 255, 0.1);
  color: #e5e7f5;
}

.item-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow-wrap: anywhere;
}

.item-detail {
  font-size: 12px;
}

.item-link {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  font-weight: 500;
}

.decision-buttons {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.decision-buttons .btn {
  width: 100%;
}

.fine-print {
  margin: 0;
  font-size: 12px;
  line-height: 16px;
}

.kv-list {
  margin: 0;
}

.kv dt,
.kv dd {
  margin: 0;
}

.kv dd {
  font-weight: 600;
  text-align: right;
  overflow-wrap: anywhere;
}

.kv-stack {
  flex-direction: column;
  gap: 4px;
}

.kv-stack dd {
  text-align: left;
  font-weight: 400;
}

.pre-wrap {
  white-space: pre-wrap;
}

/* Dialog: canvas `.dlg` (Onda.dc.html). */
.dlg-card {
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  background: #151b45;
}

.dlg-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.dlg-text {
  margin: 0;
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.plain-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.field-label {
  font-weight: 600;
  padding: 0;
}

.field-help {
  margin: 4px 0 4px;
  font-size: 13px;
}

.field-counter {
  align-self: flex-end;
  font-size: 12px;
}

.field-error {
  color: #fca5a5;
  font-size: 12px;
}

.fld[aria-invalid='true'] {
  border-color: #fca5a5;
}

.check-option {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  cursor: pointer;
}

.check-option input {
  width: 18px;
  height: 18px;
  margin: 0;
  flex: none;
  accent-color: #7c3aed;
  cursor: pointer;
}
</style>
