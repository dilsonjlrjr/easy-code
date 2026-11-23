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
const reasonField = ref<{ focus: () => void }>()

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
</script>

<template>
  <v-container max-width="1200">
    <LoadingState v-if="loader.loading.value && !round && !noRound" />
    <template v-else-if="loader.error.value && !project">
      <h1 class="text-h4 mb-4">{{ $t('pages.gate.title') }}</h1>
      <!-- ER-00-13 / RNF-04: same message for missing and without access. -->
      <v-alert v-if="notFound" type="warning" variant="tonal" role="alert">
        {{ $t('problems.not-found') }}
      </v-alert>
      <ProblemAlert v-else :error="loader.error.value" retry @retry="loader.run" />
    </template>
    <template v-else-if="project">
      <AppBreadcrumbs :items="breadcrumbs" />
      <div role="status" class="sr-only">{{ message }}</div>

      <template v-if="noRound">
        <h1 class="text-h4 mb-2">{{ $t('pages.gate.title') }}</h1>
        <p class="mb-2">{{ $t('gates.noRound') }}</p>
        <router-link :to="{ name: ROUTE_NAMES.project, params: { projectId } }">
          {{ $t('projects.stage.back') }}
        </router-link>
      </template>

      <template v-else-if="round">
        <div class="d-flex flex-wrap align-center ga-3 mb-2">
          <h1 class="text-h4">{{ $t('gates.heading', { stage: stageName }) }}</h1>
          <StateLabel machine="gate" :state="round.state" />
          <v-spacer />
          <LiveConnectionStatus :status="live.status.value" />
        </div>
        <p class="text-body-2 mb-1">
          {{ $t('gates.round', { n: round.round }) }} ·
          <span :title="format.dateTime(round.opened_at)">
            {{ $t('gates.openedAge', { age: format.age(round.opened_at) }) }}
          </span>
          ·
          {{
            round.approval_mode === 'automatic' ? $t('gates.modeAutomatic') : $t('gates.modeHuman')
          }}
        </p>
        <router-link
          :to="{ name: ROUTE_NAMES.stage, params: { projectId, stageId: round.stage } }"
          class="text-body-2"
        >
          {{ $t('gates.openStage', { stage: stageName }) }}
        </router-link>

        <!-- ESP-12 section 6: who waits for whom, in text. -->
        <v-alert
          v-if="awaitingHuman"
          :type="isApprover ? 'warning' : 'info'"
          variant="tonal"
          class="my-4"
          data-testid="gate-waiting"
        >
          {{ isApprover ? $t('gates.waitingForYou') : $t('gates.waitingForApproval') }}
        </v-alert>
        <v-alert v-if="reauthenticating" type="info" variant="tonal" role="status" class="my-4">
          {{ $t('gates.reauthenticating') }}
        </v-alert>
        <v-alert
          v-if="round.state === 'evaluating'"
          type="info"
          variant="tonal"
          class="my-4"
          data-testid="gate-evaluating"
        >
          {{ $t('gates.evaluating') }}
          <template v-if="round.reevaluation?.finding">
            {{ $t('gates.reevaluationBy') }}
            <router-link
              :to="{
                name: ROUTE_NAMES.finding,
                params: { projectId, findingId: round.reevaluation.finding },
              }"
              class="monospace"
              data-testid="gate-reevaluation-finding"
              >{{ round.reevaluation.finding }}</router-link
            >
          </template>
        </v-alert>
        <!-- US-31 CA-2, CA-3 / ER-12-1: why approval is unavailable, by name and in text. -->
        <v-alert
          v-if="approvalBlocked"
          type="error"
          variant="tonal"
          class="my-4"
          data-testid="gate-blocked"
        >
          <p class="font-weight-medium">{{ $t('gates.blocked.title') }}</p>
          <p v-if="failedBlockingChecks.length > 0" class="mt-1">
            {{
              $t('gates.blocked.checks', {
                checks: failedBlockingChecks.map((check) => check.name).join(', '),
              })
            }}
          </p>
          <p v-if="blockingFindings.length > 0" class="mt-1">
            {{ $t('gates.blocked.findings') }}
            <template v-for="(finding, index) in blockingFindings" :key="finding.id">
              <router-link
                :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
                class="monospace"
                >{{ finding.id }}</router-link><span v-if="index < blockingFindings.length - 1"
                >,
              </span>
            </template>
          </p>
          <p class="mt-1">{{ $t('gates.blocked.noOverride') }}</p>
        </v-alert>
        <v-alert
          v-if="separationOfDuties"
          type="error"
          variant="tonal"
          role="alert"
          class="my-4"
          data-testid="decision-failure"
        >
          {{ $t(separationOfDuties) }}
        </v-alert>
        <ProblemAlert
          v-else-if="decisionFailure && !approveDialog && !rejectDialog"
          :error="decisionFailure"
          class="my-4"
          data-testid="decision-failure"
        />

        <div v-if="canApprove || canReject" class="d-flex flex-wrap ga-2 my-4">
          <v-btn
            v-if="canApprove"
            color="primary"
            variant="flat"
            :disabled="deciding || reauthenticating"
            @click="openApprove"
          >
            {{ $t('gates.approve') }}
          </v-btn>
          <v-btn
            v-if="canReject"
            variant="outlined"
            :disabled="deciding || reauthenticating"
            @click="openReject"
          >
            {{ $t('gates.reject') }}
          </v-btn>
        </div>

        <section v-if="round.decision" aria-labelledby="decision-heading" class="my-6">
          <h2 id="decision-heading" class="text-h6 mb-2">{{ $t('gates.decision') }}</h2>
          <dl class="summary-list" data-testid="gate-decision">
            <dt>{{ $t('gates.result') }}</dt>
            <dd>{{ $t(`gates.results.${round.decision.result}`) }}</dd>
            <dt>{{ $t('gates.by') }}</dt>
            <dd>{{ who(round.decision.record) }}</dd>
            <dt>{{ $t('gates.when') }}</dt>
            <dd>{{ format.dateTime(round.decision.record.when) }}</dd>
            <dt>{{ $t('gates.from') }}</dt>
            <dd>{{ origin(round.decision.record) }}</dd>
            <template v-if="round.decision.reason">
              <dt>{{ $t('gates.reason') }}</dt>
              <dd class="pre-wrap">{{ round.decision.reason }}</dd>
            </template>
            <template v-if="failedCheckNames.length">
              <dt>{{ $t('gates.failedChecks') }}</dt>
              <dd data-testid="decision-failed-checks">{{ failedCheckNames.join(', ') }}</dd>
            </template>
            <template v-if="round.decision.blocking_findings?.length">
              <dt>{{ $t('gates.blockingFindings') }}</dt>
              <dd>
                <template v-for="(id, index) in round.decision.blocking_findings" :key="id">
                  <router-link
                    :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: id } }"
                    class="monospace"
                    >{{ id }}</router-link><span
                    v-if="index < round.decision.blocking_findings.length - 1"
                    >,
                  </span>
                </template>
              </dd>
            </template>
            <template v-if="round.decision.comment">
              <dt>{{ $t('gates.comment') }}</dt>
              <dd class="pre-wrap">{{ round.decision.comment }}</dd>
            </template>
          </dl>
          <p v-if="round.decision.result === 'rejected'" class="text-body-2 mt-2">
            {{ $t('gates.reworkNotice') }}
          </p>
        </section>

        <section aria-labelledby="checks-heading" class="my-6">
          <h2 id="checks-heading" class="text-h6 mb-2">{{ $t('gates.checks') }}</h2>
          <p class="text-body-2 mb-2" data-testid="check-counts">
            {{ $t('gates.counts', counts) }}
          </p>
          <p v-if="round.checks.length === 0" class="text-body-2">{{ $t('gates.noChecks') }}</p>
          <v-table v-else density="compact">
            <caption class="sr-only">
              {{
                $t('gates.checks')
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col">{{ $t('gates.check') }}</th>
                <th scope="col">{{ $t('gates.checkType') }}</th>
                <th scope="col">{{ $t('gates.severity') }}</th>
                <th scope="col">{{ $t('gates.status') }}</th>
                <th scope="col">{{ $t('gates.evidence') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="check in round.checks"
                :key="check.check"
                :data-testid="`check-${check.check}`"
              >
                <th scope="row" class="font-weight-regular">{{ check.name }}</th>
                <td>{{ $t(`gates.checkTypes.${check.type}`) }}</td>
                <td>{{ $t(`gates.severities.${check.failure_severity}`) }}</td>
                <td>{{ checkStatus(check) }}</td>
                <td>
                  <ul v-if="check.evidence?.length" class="plain-list">
                    <li v-for="(item, index) in check.evidence" :key="index" class="text-body-2">
                      <!-- PT-48: a wave reference opens the build and test evidence of that wave. -->
                      <router-link
                        v-if="item.reference && isWaveReference(item.reference)"
                        :to="{
                          name: ROUTE_NAMES.wave,
                          params: { projectId, waveId: item.reference },
                          hash: '#evidence',
                        }"
                        class="monospace"
                        :aria-label="$t('evidence.openOf', { wave: item.reference })"
                      >
                        {{ item.reference }}
                      </router-link>
                      <span v-else-if="item.reference" class="monospace">{{ item.reference }}</span>
                      <span v-if="item.path" class="monospace"> {{ item.path }}</span>
                      {{ item.message }}
                    </li>
                  </ul>
                </td>
              </tr>
            </tbody>
          </v-table>
        </section>

        <section aria-labelledby="outputs-heading" class="my-6">
          <h2 id="outputs-heading" class="text-h6 mb-2">{{ $t('gates.outputs') }}</h2>
          <p v-if="round.outputs.length === 0" class="text-body-2">{{ $t('artifacts.empty') }}</p>
          <ul v-else class="plain-list">
            <li v-for="output in round.outputs" :key="output.artifact" class="text-body-2 mb-1">
              <router-link
                :to="{
                  name: ROUTE_NAMES.artifact,
                  params: { projectId, artifactId: output.artifact },
                }"
                class="monospace"
              >
                {{ output.artifact }}
              </router-link>
              · {{ $t('artifacts.versionNumber', { n: output.version }) }}
              <template v-if="output.state">
                · {{ $t(`states.artifact.${output.state}`) }}</template
              >
            </li>
          </ul>
        </section>

        <section aria-labelledby="findings-heading" class="my-6">
          <h2 id="findings-heading" class="text-h6 mb-2">{{ $t('projects.stage.findings') }}</h2>
          <p
            v-if="!round.blocking_findings?.length && !round.non_blocking_findings?.length"
            class="text-body-2"
          >
            {{ $t('gates.noFindings') }}
          </p>
          <ul v-else class="plain-list">
            <li
              v-for="finding in [
                ...(round.blocking_findings ?? []),
                ...(round.non_blocking_findings ?? []),
              ]"
              :key="finding.id"
              class="text-body-2 mb-1"
            >
              <router-link
                :to="{ name: ROUTE_NAMES.finding, params: { projectId, findingId: finding.id } }"
                class="monospace"
              >
                {{ finding.id }}
              </router-link>
              · {{ finding.title }} · {{ $t(`findings.severity.${finding.severity}`) }} ·
              {{ $t(`states.finding.${finding.status}`) }}
              <template v-if="finding.blocking"> · {{ $t('projects.stage.blocking') }}</template>
            </li>
          </ul>
        </section>

        <section v-if="history.length > 1" aria-labelledby="history-heading" class="my-6">
          <h2 id="history-heading" class="text-h6 mb-2">{{ $t('gates.history') }}</h2>
          <ul class="plain-list">
            <li v-for="item in history" :key="item.round" class="text-body-2 mb-1">
              <router-link
                :to="{ name: ROUTE_NAMES.gate, params: { projectId, gateId, round: item.round } }"
                :aria-current="item.round === round.round ? 'page' : undefined"
              >
                {{ $t('gates.round', { n: item.round }) }}
              </router-link>
              · {{ $t(`states.gate.${item.state}`) }}
            </li>
          </ul>
        </section>
      </template>
    </template>

    <!-- PT-31: one approval dialog, one box per human check; Approve waits until all are checked. -->
    <v-dialog v-model="approveDialog" max-width="560" :persistent="deciding">
      <v-card>
        <v-card-title>
          <h2 class="text-h6">{{ $t('gates.approveTitle', { stage: stageName }) }}</h2>
        </v-card-title>
        <v-form @submit.prevent="approve(approvalDecision())">
          <v-card-text>
            <fieldset
              v-if="humanChecks.length > 0"
              class="plain-fieldset"
              aria-describedby="human-checks-help"
            >
              <legend class="text-body-1 mb-1">{{ $t('gates.humanChecks') }}</legend>
              <p id="human-checks-help" class="text-body-2 text-medium-emphasis mb-2">
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
            <p v-else class="text-body-2 mb-2">{{ $t('gates.noHumanChecks') }}</p>
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
              :disabled="!allChecksConfirmed"
            >
              {{ $t('gates.approve') }}
            </v-btn>
          </v-card-actions>
        </v-form>
      </v-card>
    </v-dialog>

    <v-dialog v-model="rejectDialog" max-width="560" :persistent="deciding">
      <v-card>
        <v-card-title>
          <h2 class="text-h6">{{ $t('gates.rejectTitle', { stage: stageName }) }}</h2>
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
            <fieldset v-if="humanChecks.length > 0" class="plain-fieldset mt-2">
              <legend class="text-body-2 mb-1">{{ $t('gates.unconfirmedChecks') }}</legend>
              <v-checkbox
                v-for="check in humanChecks"
                :key="check.check"
                v-model="unconfirmedChecks"
                :value="check.check"
                :label="check.name"
                density="compact"
                hide-details
              />
            </fieldset>
            <ProblemAlert v-if="decisionFailure" :error="decisionFailure" class="mt-2" />
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" :disabled="deciding" @click="rejectDialog = false">
              {{ $t('common.cancel') }}
            </v-btn>
            <v-btn type="submit" color="error" variant="flat" :loading="deciding">
              {{ $t('gates.reject') }}
            </v-btn>
          </v-card-actions>
        </v-form>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<style scoped>
.plain-fieldset {
  border: 0;
  margin: 0;
  padding: 0;
}

.pre-wrap {
  white-space: pre-wrap;
}
</style>
