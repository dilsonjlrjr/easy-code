<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { ApiError, isApiError } from '@/shared/api/problemDetails'
import { waitForCommand } from '@/shared/commands/waitForCommand'
import { describeEvent } from '@/shared/events/eventDescriptions'
import type { CloudEvent } from '@/shared/events/eventStream'
import { useFormat } from '@/shared/format/useFormat'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import { stateTone, type ChipTone } from '@/shared/ui/stateFamilies'
import LiveConnectionStatus from '@/shared/ui/LiveConnectionStatus.vue'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { projectsApi } from '@/features/projects/api/projectsApi'
import type { CallRecord, Question, SandboxExecution } from '../api/wavesApi'
import SandboxEvidence from '../components/SandboxEvidence.vue'
import ConfirmActionDialog from '@/shared/ui/ConfirmActionDialog.vue'
import type { AvailableAction, Command } from '@/shared/api/contract'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import WaveQuestions from '../components/WaveQuestions.vue'
import { refusedToolCalls } from '../composables/sandboxEvidence'
import { currentWavesApi, useWaveLive } from '../composables/useWaveLive'

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const format = useFormat()
const projectId = String(route.params.projectId)
const waveId = String(route.params.waveId)
const live = useWaveLive(projectId, waveId)
const openedWaiting = ref(false)
const answerNotice = ref<string>()
const cancelDialogOpen = ref(false)
const pauseNowDialogOpen = ref(false)
const restartDialogOpen = ref(false)
/** The pending / applied notice of the last control action (ESP-18 section 6, "comando pendente"). */
const controlNotice = ref<string>()
/** Failure of an action sent without a dialog (pause, resume); a 409 shows the real state. */
const controlFailure = ref<unknown>()
const sendingAction = ref<AvailableAction>()

const wave = computed(() => live.wave.value)
const ENDED_STATES = new Set(['completed', 'failed', 'canceled'])
const isEnded = computed(() => (wave.value ? ENDED_STATES.has(wave.value.state) : false))
const notFound = computed(() => isApiError(live.error.value) && live.error.value.status === 404)
const canAnswer = computed(() => wave.value?.available_actions.includes('answer') ?? false)
/**
 * US-14 CA-3, ER-18-1: "cancel" shows exactly when the api offers it (never for a completed or
 * canceled wave) and only to a project role that controls waves (member, approver, lead).
 */
const CONTROL_ROLES = new Set(['member', 'approver', 'lead'])
const canControl = computed(
  () => live.project.value?.my_roles.some((role) => CONTROL_ROLES.has(role)) ?? false,
)
/** ER-18-1: the controls are exactly `available_actions`; the page derives none by itself. */
function offers(action: AvailableAction): boolean {
  return canControl.value && (wave.value?.available_actions.includes(action) ?? false)
}
const canCancel = computed(() => offers('cancel'))
const canPause = computed(() => offers('pause'))
const canPauseNow = computed(() => offers('pause_now'))
const canResume = computed(() => offers('resume'))
const canRestart = computed(() => offers('restart'))
const hasControls = computed(
  () =>
    canPause.value || canPauseNow.value || canResume.value || canRestart.value || canCancel.value,
)
/**
 * ER-18-8 / CA-18-15: a wave paused by a policy shows the cause; with the project paused (by the
 * budget or by hand) "resume" is not offered, and the page says why and what to do.
 */
const resumeBlockedByProject = computed(
  () =>
    wave.value?.state === 'paused' && !canResume.value && live.project.value?.state === 'paused',
)
const pauseCause = computed(() => {
  const cause = wave.value?.pause?.cause
  if (!cause) return undefined
  const key = `waves.control.pauseCauses.${cause}`
  return i18n.te(key) ? t(key) : cause
})
/** US-12 CA-4 / ER-18-9: pausing for longer than the limit suggests "pause now"; nothing aborts. */
const pausingTooLong = computed(
  () => wave.value?.state === 'pausing' && wave.value.pause?.pausing_limit_exceeded === true,
)
/** US-13 CA-1: the earlier attempts stay readable, with their final state and failure cause. */
const previousAttempts = computed(() =>
  (wave.value?.attempts ?? []).filter((attempt) => attempt.number < (wave.value?.attempt ?? 1)),
)
const recentHistory = computed(() => [...live.history.value].reverse())
const refusedCalls = computed(() => refusedToolCalls(live.calls.value))
/** ESP-18: the evidence block shows when the wave ran commands; the developer's waves always show it. */
const showEvidence = computed(
  () =>
    live.executions.value.length > 0 ||
    live.executionsError.value !== undefined ||
    wave.value?.agent === 'developer-agent',
)
const hasEstimatedLoss = computed(() => Number(wave.value?.cost.estimated_lost?.value ?? 0) > 0)

const breadcrumbs = computed(() => [
  { title: t('pages.projects.title'), to: { name: ROUTE_NAMES.projects } },
  {
    title: live.project.value?.name ?? '',
    to: { name: ROUTE_NAMES.project, params: { projectId } },
  },
  {
    title: t('pages.project.waves'),
    to: { name: ROUTE_NAMES.projectWaves, params: { projectId } },
  },
  { title: waveId },
])

onMounted(async () => {
  await live.load()
  openedWaiting.value = live.wave.value?.state === 'awaiting_user'
  // PT-48: the stage, the task and the gate link straight to the evidence block.
  if (route.hash === '#evidence') {
    await nextTick()
    const heading = document.getElementById('evidence-heading')
    heading?.scrollIntoView()
    heading?.focus()
  }
})

function downloadOutput(execution: SandboxExecution): Promise<string> {
  return currentWavesApi().downloadExecutionOutput(projectId, waveId, execution.id)
}

/** US-10 CA-1: the answer is a command; the wave goes back to running from where it stopped. */
async function submitAnswer(
  question: Question,
  answers: Array<{ number: number; text: string }>,
  key: string,
): Promise<void> {
  const api = currentWavesApi()
  const accepted = await api.answerQuestion(projectId, waveId, question.id, answers, key)
  answerNotice.value = t('waves.questions.pending')
  const finished = await waitForCommand(accepted, (id) => projectsApi.getCommand(projectId, id))
  if (finished.state === 'refused') {
    answerNotice.value = undefined
    throw new ApiError(finished.refusal?.status ?? 409, finished.refusal)
  }
  answerNotice.value = t('waves.questions.applied')
  await live.refreshLists()
}

/**
 * Every control is a command followed until applied or refused (ER-06-7 pattern). The wave changes
 * live by the stream and is re-read here, so the screen never depends on the stream alone. A
 * refusal (409 `state-conflict`, `budget-exhausted`) re-reads the wave so the real state and its
 * actions show (ESP-18 section 6).
 */
async function runControl(
  send: () => Promise<Command>,
  texts: { pending: string; applied: string },
): Promise<void> {
  controlNotice.value = texts.pending
  try {
    const accepted = await send()
    const finished = await waitForCommand(accepted, (id) => projectsApi.getCommand(projectId, id))
    if (finished.state === 'refused') {
      throw new ApiError(finished.refusal?.status ?? 409, finished.refusal)
    }
    controlNotice.value = texts.applied
  } catch (error) {
    controlNotice.value = undefined
    await live.refreshLists().catch(() => undefined)
    throw error
  }
  await live.refreshLists()
}

/** US-14 CA-1, CA-2, CA-4: cancel, after the strong confirmation. */
function submitCancel(key: string): Promise<void> {
  return runControl(() => currentWavesApi().cancelWave(projectId, waveId, key), {
    pending: t('waves.cancel.pending'),
    applied: t('waves.cancel.applied'),
  })
}

/** US-12 CA-2: pause now, after the dialog that says the call in progress is lost and charged. */
function submitPauseNow(key: string): Promise<void> {
  return runControl(() => currentWavesApi().pauseWave(projectId, waveId, 'immediate', key), {
    pending: t('waves.control.pauseNowPending'),
    applied: t('waves.control.pauseNowApplied'),
  })
}

/** US-13 CA-1: restart a failed wave, after its own dialog (PC-27). */
function submitRestart(key: string): Promise<void> {
  return runControl(() => currentWavesApi().restartWave(projectId, waveId, key), {
    pending: t('waves.control.restartPending'),
    applied: t('waves.control.restartApplied'),
  })
}

/** US-12 CA-1 (cooperative pause) and CA-3 (resume): no confirmation, nothing is lost. */
async function sendWithoutDialog(action: 'pause' | 'resume'): Promise<void> {
  const api = currentWavesApi()
  const key = newIdempotencyKey()
  sendingAction.value = action
  controlFailure.value = undefined
  try {
    await runControl(
      () =>
        action === 'pause'
          ? api.pauseWave(projectId, waveId, 'cooperative', key)
          : api.resumeWave(projectId, waveId, key),
      {
        pending: t(`waves.control.${action}Pending`),
        applied: t(`waves.control.${action}Applied`),
      },
    )
  } catch (error) {
    controlFailure.value = error
  } finally {
    sendingAction.value = undefined
  }
}

function describe(event: CloudEvent): string {
  return describeEvent(t, event)
}

/** Unknown causes are shown as the API sends them (an enumeration, never agent text). */
function failureCauseText(cause: string | null | undefined): string {
  if (!cause) return t('waves.failureCauses.unknown')
  const key = `waves.failureCauses.${cause}`
  return i18n.te(key) ? t(key) : cause
}

/** US-30: the refusal reason from the catalog; an unknown code is shown as sent (an enumeration). */
function refusalText(reason: string): string {
  const key = `waves.calls.refusalReasons.${reason}`
  return i18n.te(key) ? t(key) : reason
}

/** US-24: the provider by its catalog name; an unknown id is shown as sent. */
function providerName(id: string): string {
  const key = `providers.${id}`
  return i18n.te(key) ? t(key) : id
}

const OUTPUT_TONES: Record<string, ChipTone> = { written: 'ok', writing: 'info', pending: 'mute' }

function callTitle(call: CallRecord): string {
  if (call.type === 'tool') return t('waves.calls.tool', { tool: call.tool ?? '' })
  return t('waves.calls.model', {
    model: call.model ?? '',
    role: call.role ? t(`waves.roles.${call.role}`) : '',
  })
}
</script>

<template>
  <!-- Layout: canvas "Onda" (Onda.dc.html). -->
  <LoadingState v-if="live.loading.value && !wave" />
  <template v-else-if="live.error.value && !wave">
    <h1>{{ $t('pages.wave.title') }}</h1>
    <div v-if="notFound" class="note n-warn" role="alert">
      <span class="note-icon fg-warn"><CanvasIcon name="alert" :stroke-width="2" /></span>
      <div class="note-body">
        <p>{{ $t('waves.notFound') }}</p>
        <router-link :to="{ name: ROUTE_NAMES.projects }">{{
          $t('common.goProjects')
        }}</router-link>
      </div>
    </div>
    <ProblemAlert v-else :error="live.error.value" retry @retry="live.load" />
  </template>

  <template v-else-if="wave">
    <AppBreadcrumbs :items="breadcrumbs" />
    <div class="wave-head">
      <div class="page-head">
        <h1>{{ $t('waves.heading') }} {{ wave.id }} · {{ $t(`stages.${wave.stage}`) }}</h1>
        <span class="head-row">
          <StateLabel
            machine="wave"
            :state="wave.state"
            :detail="
              wave.state === 'queued' && wave.attempt > 1
                ? $t('waves.control.restarted')
                : undefined
            "
          />
          <span class="t2 head-text">
            {{ $t(`agents.${wave.agent}`) }} · {{ $t(`waves.purpose.${wave.purpose}`) }}
            <template v-if="wave.task"> · {{ $t('waves.task', { task: wave.task }) }}</template>
            <template v-if="wave.attempt > 1">
              · {{ $t('waves.attempt', { n: wave.attempt }) }}</template
            >
          </span>
          <LiveConnectionStatus :status="live.status.value" />
        </span>
        <span v-if="wave.git_commit" class="t2 head-text" data-testid="wave-commit">
          {{ $t('evidence.commit') }}:
          <span class="mono" :title="wave.git_commit">{{ wave.git_commit.slice(0, 12) }}</span>
        </span>
      </div>
      <!-- ESP-18 section 4: lifecycle controls, only from the web (US-12 RN-1). -->
      <div v-if="hasControls" class="head-actions" data-testid="wave-actions">
        <button
          v-if="canPause"
          type="button"
          class="btn"
          :disabled="sendingAction !== undefined"
          :aria-busy="sendingAction === 'pause' || undefined"
          @click="sendWithoutDialog('pause')"
        >
          <v-progress-circular v-if="sendingAction === 'pause'" indeterminate size="16" width="2" />
          <CanvasIcon v-else name="pause" :size="16" :stroke-width="2" />
          {{ $t('waves.control.pause') }}
        </button>
        <button
          v-if="canPauseNow"
          type="button"
          class="btn"
          :disabled="sendingAction !== undefined"
          @click="pauseNowDialogOpen = true"
        >
          {{ $t('waves.control.pauseNow') }}
        </button>
        <button
          v-if="canResume"
          type="button"
          class="btn btn-p"
          :disabled="sendingAction !== undefined"
          :aria-busy="sendingAction === 'resume' || undefined"
          @click="sendWithoutDialog('resume')"
        >
          <v-progress-circular
            v-if="sendingAction === 'resume'"
            indeterminate
            size="16"
            width="2"
          />
          <CanvasIcon v-else name="play" :size="16" :stroke-width="2" />
          {{ $t('waves.control.resume') }}
        </button>
        <button
          v-if="canRestart"
          type="button"
          class="btn"
          :disabled="sendingAction !== undefined"
          @click="restartDialogOpen = true"
        >
          {{ $t('waves.control.restart') }}
        </button>
        <button
          v-if="canCancel"
          type="button"
          class="btn btn-d"
          :disabled="sendingAction !== undefined"
          @click="cancelDialogOpen = true"
        >
          {{ $t('waves.cancel.action') }}
        </button>
      </div>
    </div>
    <ConfirmActionDialog
      v-model="cancelDialogOpen"
      :title="$t('waves.cancel.title', { wave: wave.id })"
      :lines="[$t('waves.cancel.final'), $t('waves.cancel.lostCall')]"
      :keep-label="$t('waves.cancel.keep')"
      :confirm-label="$t('waves.cancel.confirm')"
      test-id="cancel-wave-dialog"
      :submit="submitCancel"
    />
    <ConfirmActionDialog
      v-model="pauseNowDialogOpen"
      :title="$t('waves.control.pauseNowTitle', { wave: wave.id })"
      :lines="[$t('waves.control.pauseNowLost'), $t('waves.control.pauseNowResume')]"
      :keep-label="$t('waves.control.back')"
      :confirm-label="$t('waves.control.pauseNowConfirm')"
      confirm-color="warning"
      test-id="pause-now-dialog"
      :submit="submitPauseNow"
    />
    <ConfirmActionDialog
      v-model="restartDialogOpen"
      :title="$t('waves.control.restartTitle', { wave: wave.id })"
      :lines="[$t('waves.control.restartEffect'), $t('waves.control.restartHistory')]"
      :keep-label="$t('waves.control.back')"
      :confirm-label="$t('waves.control.restartConfirm')"
      confirm-color="primary"
      test-id="restart-dialog"
      :submit="submitRestart"
    />
    <div v-if="controlNotice" role="status" class="note" data-testid="control-notice">
      <span class="note-icon"><CanvasIcon name="bolt" /></span>
      <span class="note-body">{{ controlNotice }}</span>
    </div>
    <ProblemAlert v-if="controlFailure" :error="controlFailure" data-testid="control-failure" />
    <div v-if="pausingTooLong" class="note n-warn" role="alert" data-testid="pausing-too-long">
      <span class="note-icon fg-warn"><CanvasIcon name="alert" :stroke-width="2" /></span>
      <span class="note-body">{{ $t('waves.control.pausingTooLong') }}</span>
    </div>
    <div
      v-if="wave.state === 'paused' || wave.state === 'pausing'"
      class="note"
      role="alert"
      data-testid="pause-details"
    >
      <span class="note-icon"><CanvasIcon name="pause" :stroke-width="2" /></span>
      <div class="note-body">
        <p>
          {{
            wave.state === 'pausing'
              ? $t('waves.control.pausingNotice')
              : $t('waves.control.pausedNotice')
          }}
          <template v-if="pauseCause">
            {{ $t('waves.control.pauseCause', { cause: pauseCause }) }}
          </template>
        </p>
        <p v-if="wave.pause?.mode === 'immediate' && Number(wave.cost.lost.value) > 0">
          {{ $t('waves.lostCost', { cost: format.money(wave.cost.lost) }) }}
        </p>
        <p v-if="resumeBlockedByProject" data-testid="resume-blocked">
          {{
            live.project.value?.pause_reason === 'budget'
              ? $t('waves.control.resumeBlockedBudget')
              : $t('waves.control.resumeBlockedProject')
          }}
          <router-link
            :to="{
              name:
                live.project.value?.pause_reason === 'budget'
                  ? ROUTE_NAMES.projectConfiguration
                  : ROUTE_NAMES.project,
              params: { projectId },
            }"
          >
            {{
              live.project.value?.pause_reason === 'budget'
                ? $t('waves.control.goBudget')
                : $t('waves.control.goProject')
            }}
          </router-link>
        </p>
      </div>
    </div>
    <!-- ESP-18 section 8: state changes announced politely, without moving the focus. -->
    <div role="status" class="sr-only">{{ live.message.value }}</div>
    <p v-if="live.status.value === 'reconnecting'" class="t2 head-text">
      {{ $t('live.staleData') }}
    </p>

    <!-- US-11: resumption from the last checkpoint after the platform restarted. -->
    <div v-if="live.resumeNotice.value" class="note" role="alert" data-testid="resume-notice">
      <span class="note-icon"><CanvasIcon name="play" :stroke-width="2" /></span>
      <span class="note-body">
        {{ $t('waves.resumedAfterCrash', { checkpoint: live.resumeNotice.value.checkpoint }) }}
        <template v-if="live.resumeNotice.value.lostCost">
          {{
            $t(live.resumeNotice.value.estimated ? 'waves.lostCostEstimated' : 'waves.lostCost', {
              cost: format.money(live.resumeNotice.value.lostCost),
            })
          }}
        </template>
      </span>
    </div>
    <div v-if="live.retryInSeconds.value !== undefined" class="note" role="alert">
      <span class="note-icon"><CanvasIcon name="bolt" /></span>
      <span class="note-body">
        {{ $t('waves.retryIn', { seconds: live.retryInSeconds.value }) }}
      </span>
    </div>

    <!-- ESP-18 section 6: a canceled wave keeps its final and lost cost; partial artifacts stay drafts. -->
    <div v-if="wave.state === 'canceled'" class="note" role="alert" data-testid="canceled-notice">
      <span class="note-icon"><CanvasIcon name="bolt" /></span>
      <span class="note-body">
        {{ $t('waves.cancel.canceledNotice', { cost: format.money(wave.cost.lost) }) }}
      </span>
    </div>
    <!-- US-29 CA-2: a failed wave shows its cause and the schema errors per artifact. -->
    <div
      v-if="wave.state === 'failed'"
      class="note n-crit"
      role="alert"
      data-testid="failure-cause"
    >
      <span class="note-icon fg-crit"><CanvasIcon name="alert" :stroke-width="2" /></span>
      <div class="note-body">
        <p class="note-title">
          {{
            $t('waves.failureCause', {
              cause: failureCauseText(wave.failure_cause),
            })
          }}
        </p>
        <template v-if="wave.artifact_errors?.length">
          <p>{{ $t('waves.schemaErrors') }}</p>
          <ul class="note-list">
            <li v-for="(item, index) in wave.artifact_errors" :key="index">
              <span class="mono">{{ item.artifact }}</span> ·
              <span class="mono">{{ item.path }}</span> · {{ item.message }}
            </li>
          </ul>
        </template>
      </div>
    </div>
    <div
      v-else-if="wave.artifact_errors?.length"
      class="note n-warn"
      role="alert"
      data-testid="schema-rejections"
    >
      <span class="note-icon fg-warn"><CanvasIcon name="alert" :stroke-width="2" /></span>
      <!-- US-29 CA-1: the platform rejected an artifact; the agent corrects it in the same wave. -->
      <div class="note-body">
        <p>{{ $t('waves.schemaRejected') }}</p>
        <ul class="note-list">
          <li v-for="(item, index) in wave.artifact_errors" :key="index">
            <span class="mono">{{ item.artifact }}</span> ·
            <span class="mono">{{ item.path }}</span> · {{ item.message }}
          </li>
        </ul>
      </div>
    </div>

    <div v-if="answerNotice" role="status" class="note">
      <span class="note-icon"><CanvasIcon name="bolt" /></span>
      <span class="note-body">{{ answerNotice }}</span>
    </div>

    <div class="wave-layout">
      <div class="wave-main">
        <WaveQuestions
          :questions="live.questions.value"
          :can-answer="canAnswer"
          :focus-on-open="openedWaiting"
          :submit="submitAnswer"
        />

        <section aria-labelledby="calls-heading" class="card main-card">
          <div class="card-head">
            <div class="card-head-text">
              <h2 id="calls-heading">{{ $t('waves.calls.title') }}</h2>
              <p class="t2 card-subtitle">{{ $t('waves.calls.subtitle') }}</p>
            </div>
            <button
              v-if="!isEnded"
              type="button"
              class="btn"
              :aria-pressed="live.paused.value"
              @click="live.setPaused(!live.paused.value)"
            >
              {{ live.paused.value ? $t('live.resumeUpdates') : $t('live.pauseUpdates') }}
            </button>
          </div>
          <!-- US-30 CA-1, RNF-07: least privilege refused some tool calls; each one is in the record. -->
          <div v-if="refusedCalls.length > 0" class="note" role="alert" data-testid="refused-calls">
            <span class="note-icon"><CanvasIcon name="alert" :stroke-width="2" /></span>
            <span class="note-body">
              {{ $t('waves.calls.refusedNotice', { count: refusedCalls.length }) }}
            </span>
          </div>
          <p
            v-if="live.paused.value && live.pendingWhilePaused.value.length > 0"
            class="t2 card-line"
          >
            {{ $t('live.pendingCount', { count: live.pendingWhilePaused.value.length }) }}
          </p>
          <p class="t2 card-line">
            {{
              wave.last_checkpoint === null || wave.last_checkpoint === undefined
                ? $t('waves.checkpoints.none')
                : $t('waves.checkpoints.last', {
                    n: wave.last_checkpoint,
                    count: live.checkpoints.value.length,
                  })
            }}
          </p>
          <p v-if="live.calls.value.length === 0" class="t2 card-line">
            {{ $t('waves.calls.none') }}
          </p>
          <!-- ESP-18 section 8: the call record is a log; no prompt, answer or tool arguments (ER-18-15). -->
          <div v-else role="log" :aria-label="$t('waves.calls.title')">
            <ol class="list">
              <li
                v-for="call in live.calls.value"
                :key="`${call.attempt ?? 1}-${call.number}`"
                :data-testid="`call-${call.number}`"
                class="li call-item"
              >
                <span class="chip call-number" :class="call.type === 'model' ? 'info' : 'mute'">
                  {{ $t('waves.calls.number', { n: call.number }) }}
                </span>
                <span class="call-body">
                  <span class="call-title">
                    {{ callTitle(call) }} · {{ $t(`waves.calls.results.${call.result}`) }}
                    <template v-if="call.lost">· {{ $t('waves.calls.lostCall') }}</template>
                  </span>
                  <span v-if="call.summary">{{ call.summary }}</span>
                  <span v-if="call.refusal_reason">
                    {{ $t('waves.calls.refusal', { reason: refusalText(call.refusal_reason) }) }}
                  </span>
                  <span class="t2 call-meta">
                    {{ format.dateTime(call.start) }} · {{ format.duration(call.duration_ms) }}
                    <template v-if="call.tokens">
                      ·
                      {{
                        $t('waves.calls.tokens', {
                          input: format.integer(call.tokens.input),
                          output: format.integer(call.tokens.output),
                          cache: format.integer(call.tokens.read_cache + call.tokens.write_cache),
                        })
                      }}
                    </template>
                    <template v-if="call.cost">
                      · {{ format.money(call.cost) }}
                      <template v-if="call.estimated_cost"
                        >({{ $t('waves.calls.estimated') }})</template
                      >
                      <template v-if="call.lost_cost">({{ $t('waves.calls.lostCost') }})</template>
                    </template>
                  </span>
                </span>
              </li>
            </ol>
          </div>
        </section>

        <SandboxEvidence
          v-if="showEvidence"
          :wave-id="waveId"
          :executions="live.executions.value"
          :error="live.executionsError.value"
          :download="downloadOutput"
          @retry="live.refreshLists"
        />

        <section
          v-if="previousAttempts.length"
          aria-labelledby="attempts-heading"
          class="card main-card"
        >
          <h2 id="attempts-heading" class="card-title">{{ $t('waves.control.attempts') }}</h2>
          <ul class="list" data-testid="wave-attempts">
            <li v-for="attempt in previousAttempts" :key="attempt.number" class="li wrap-row">
              <span>{{ $t('waves.attempt', { n: attempt.number }) }}</span>
              <span class="chip" :class="stateTone('wave', attempt.final_state)">
                {{
                  $te(`states.wave.${attempt.final_state}`)
                    ? $t(`states.wave.${attempt.final_state}`)
                    : attempt.final_state
                }}
              </span>
              <span v-if="attempt.failure_cause" class="t2 call-meta">
                {{ $t('waves.failureCause', { cause: failureCauseText(attempt.failure_cause) }) }}
              </span>
            </li>
          </ul>
        </section>

        <section aria-labelledby="history-heading" class="card main-card">
          <h2 id="history-heading" class="card-title">{{ $t('waves.history.title') }}</h2>
          <p v-if="recentHistory.length === 0" class="t2 card-line">
            {{ $t('waves.history.none') }}
          </p>
          <ol v-else class="list" data-testid="wave-history">
            <li v-for="event in recentHistory" :key="event.id" class="li history-item">
              <span class="t2 history-time">{{ format.dateTime(event.time) }}</span>
              <span class="history-text">· {{ describe(event) }}</span>
            </li>
          </ol>
        </section>
      </div>

      <div class="wave-side">
        <!-- US-36: cost and time of the wave. -->
        <section aria-labelledby="cost-heading" class="card main-card">
          <h2 id="cost-heading" class="card-title">{{ $t('waves.cost.title') }}</h2>
          <dl class="kv-list" data-testid="wave-cost">
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.reasoningModel') }}</dt>
              <dd>
                {{ providerName(wave.models.reasoning.provider) }} ·
                <span class="mono">{{ wave.models.reasoning.model }}</span>
              </dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.executionModel') }}</dt>
              <dd>
                {{ providerName(wave.models.execution.provider) }} ·
                <span class="mono">{{ wave.models.execution.model }}</span>
              </dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.inputTokens') }}</dt>
              <dd>{{ format.integer(wave.tokens.input) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.outputTokens') }}</dt>
              <dd>{{ format.integer(wave.tokens.output) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.readCacheTokens') }}</dt>
              <dd>{{ format.integer(wave.tokens.read_cache) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.writeCacheTokens') }}</dt>
              <dd>{{ format.integer(wave.tokens.write_cache) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.useful') }}</dt>
              <dd>{{ format.money(wave.cost.useful) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.lost') }}</dt>
              <dd>
                {{ format.money(wave.cost.lost) }}
                <span v-if="hasEstimatedLoss">
                  {{
                    $t('waves.cost.estimatedPart', { cost: format.money(wave.cost.estimated_lost) })
                  }}
                </span>
              </dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.total') }}</dt>
              <dd data-testid="wave-cost-total">{{ format.money(wave.cost.total) }}</dd>
            </div>
            <div v-for="item in wave.cost.per_provider ?? []" :key="item.provider" class="kv">
              <dt class="t2">
                {{ $t('waves.cost.perProvider', { provider: providerName(item.provider) }) }}
              </dt>
              <dd>{{ format.money(item.total) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.executionTime') }}</dt>
              <dd>{{ format.duration(wave.time.execution_ms) }}</dd>
            </div>
            <div class="kv">
              <dt class="t2">{{ $t('waves.cost.cycleTime') }}</dt>
              <dd>{{ format.duration(wave.time.cycle_ms) }}</dd>
            </div>
            <div v-if="isEnded && wave.result" class="kv">
              <dt class="t2">{{ $t('waves.cost.result') }}</dt>
              <dd>{{ $t(`waves.results.${wave.result}`) }}</dd>
            </div>
            <div v-if="wave.versions?.price_table" class="kv">
              <dt class="t2">{{ $t('waves.cost.priceTable') }}</dt>
              <dd class="mono">{{ wave.versions.price_table }}</dd>
            </div>
          </dl>
          <p class="t2 fine-print">{{ $t('waves.cost.timeRule') }}</p>
        </section>

        <section
          v-if="wave.expected_outputs?.length"
          aria-labelledby="outputs-heading"
          class="card main-card"
        >
          <h2 id="outputs-heading" class="card-title">{{ $t('waves.outputs.title') }}</h2>
          <ul class="list">
            <li
              v-for="output in wave.expected_outputs"
              :key="`${output.type}-${output.artifact ?? ''}`"
              class="kv"
            >
              <span class="output-name">
                {{ output.type }}
                <router-link
                  v-if="output.artifact"
                  :to="{
                    name: ROUTE_NAMES.artifact,
                    params: { projectId, artifactId: output.artifact },
                    query: output.version ? { version: output.version } : {},
                  }"
                  class="mono"
                >
                  {{ output.artifact }}
                </router-link>
              </span>
              <span class="chip" :class="OUTPUT_TONES[output.status] ?? 'mute'">
                {{ $t(`waves.outputs.status.${output.status}`) }}
              </span>
            </li>
          </ul>
        </section>

        <section
          v-if="wave.versions?.skills?.length"
          aria-labelledby="skills-heading"
          class="card main-card"
        >
          <h2 id="skills-heading" class="card-title">{{ $t('waves.skillsTitle') }}</h2>
          <ul class="list">
            <li v-for="item in wave.versions.skills" :key="item.skill" class="kv">
              <span class="mono">{{ item.skill }}</span>
              <span class="chip mute">{{
                $t('artifacts.versionNumber', { n: item.version })
              }}</span>
            </li>
          </ul>
        </section>
      </div>
    </div>
  </template>
</template>

<style scoped>
/* Values copied from the canvas "Onda" artboard. */
.wave-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: flex-end;
  gap: 16px;
}

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

.head-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.note p {
  margin: 0;
}

.note-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  overflow-wrap: anywhere;
}

.note-title {
  font-weight: 700;
}

.note-icon {
  display: flex;
  flex: none;
}

.note-list {
  margin: 0;
  padding-left: 18px;
}

.n-warn {
  border-color: rgba(234, 179, 8, 0.45);
  background: rgba(234, 179, 8, 0.12);
}

.n-crit {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.12);
}

.fg-warn {
  color: #fcd34d;
}

.fg-crit {
  color: #fca5a5;
}

.wave-layout {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}

.wave-main {
  flex: 3 1 520px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.wave-side {
  flex: 2 1 300px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.main-card {
  padding: 18px 22px;
}

.card-title {
  margin-bottom: 4px;
}

.card-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 6px;
}

.card-subtitle {
  margin: 4px 0 0;
  font-size: 13px;
}

.card-line {
  margin: 0 0 6px;
  font-size: 13px;
}

.main-card > .note {
  margin-bottom: 8px;
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.wrap-row {
  flex-wrap: wrap;
}

.call-item {
  align-items: flex-start;
}

.call-number {
  flex: none;
}

.call-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.call-title {
  font-weight: 600;
}

.call-meta {
  font-size: 12px;
}

.history-item {
  align-items: baseline;
}

.history-time {
  flex: none;
  font-size: 12px;
}

.history-text {
  flex: 1;
  min-width: 0;
  font-size: 13px;
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

.output-name {
  min-width: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  overflow-wrap: anywhere;
}

.fine-print {
  margin: 8px 0 0;
  font-size: 12px;
  line-height: 16px;
}
</style>
