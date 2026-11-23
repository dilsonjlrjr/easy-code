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

function callTitle(call: CallRecord): string {
  if (call.type === 'tool') return t('waves.calls.tool', { tool: call.tool ?? '' })
  return t('waves.calls.model', {
    model: call.model ?? '',
    role: call.role ? t(`waves.roles.${call.role}`) : '',
  })
}
</script>

<template>
  <v-container max-width="1200">
    <LoadingState v-if="live.loading.value && !wave" />
    <template v-else-if="live.error.value && !wave">
      <h1 class="text-h4 mb-4">{{ $t('pages.wave.title') }}</h1>
      <v-alert v-if="notFound" type="warning" variant="tonal" role="alert">
        <p>{{ $t('waves.notFound') }}</p>
        <router-link :to="{ name: ROUTE_NAMES.projects }">{{
          $t('common.goProjects')
        }}</router-link>
      </v-alert>
      <ProblemAlert v-else :error="live.error.value" retry @retry="live.load" />
    </template>

    <template v-else-if="wave">
      <AppBreadcrumbs :items="breadcrumbs" />
      <div class="d-flex flex-wrap align-center ga-3 mb-1">
        <h1 class="text-h4">
          {{ $t('waves.heading') }} <span class="monospace">{{ wave.id }}</span>
        </h1>
        <StateLabel
          machine="wave"
          :state="wave.state"
          :detail="
            wave.state === 'queued' && wave.attempt > 1 ? $t('waves.control.restarted') : undefined
          "
        />
        <v-spacer />
        <LiveConnectionStatus :status="live.status.value" />
      </div>
      <!-- ESP-18 section 4: lifecycle controls, only from the web (US-12 RN-1). -->
      <div v-if="hasControls" class="d-flex flex-wrap ga-2 mb-3" data-testid="wave-actions">
        <v-btn
          v-if="canPause"
          variant="outlined"
          :loading="sendingAction === 'pause'"
          :disabled="sendingAction !== undefined"
          @click="sendWithoutDialog('pause')"
        >
          {{ $t('waves.control.pause') }}
        </v-btn>
        <v-btn
          v-if="canPauseNow"
          color="warning"
          variant="outlined"
          :disabled="sendingAction !== undefined"
          @click="pauseNowDialogOpen = true"
        >
          {{ $t('waves.control.pauseNow') }}
        </v-btn>
        <v-btn
          v-if="canResume"
          color="primary"
          variant="flat"
          :loading="sendingAction === 'resume'"
          :disabled="sendingAction !== undefined"
          @click="sendWithoutDialog('resume')"
        >
          {{ $t('waves.control.resume') }}
        </v-btn>
        <v-btn
          v-if="canRestart"
          color="primary"
          variant="flat"
          :disabled="sendingAction !== undefined"
          @click="restartDialogOpen = true"
        >
          {{ $t('waves.control.restart') }}
        </v-btn>
        <v-btn
          v-if="canCancel"
          color="error"
          variant="outlined"
          :disabled="sendingAction !== undefined"
          @click="cancelDialogOpen = true"
        >
          {{ $t('waves.cancel.action') }}
        </v-btn>
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
      <div v-if="controlNotice" role="status" class="text-body-2 mb-2" data-testid="control-notice">
        {{ controlNotice }}
      </div>
      <ProblemAlert
        v-if="controlFailure"
        :error="controlFailure"
        class="mb-3"
        data-testid="control-failure"
      />
      <v-alert
        v-if="pausingTooLong"
        type="warning"
        variant="tonal"
        role="alert"
        class="mb-4"
        data-testid="pausing-too-long"
      >
        {{ $t('waves.control.pausingTooLong') }}
      </v-alert>
      <v-alert
        v-if="wave.state === 'paused' || wave.state === 'pausing'"
        type="info"
        variant="tonal"
        class="mb-4"
        data-testid="pause-details"
      >
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
      </v-alert>
      <p class="text-body-1 mb-4">
        {{ $t(`stages.${wave.stage}`) }} · {{ $t(`agents.${wave.agent}`) }} ·
        {{ $t(`waves.purpose.${wave.purpose}`) }}
        <template v-if="wave.task"> · {{ $t('waves.task', { task: wave.task }) }}</template>
        <template v-if="wave.attempt > 1">
          · {{ $t('waves.attempt', { n: wave.attempt }) }}</template
        >
      </p>
      <p v-if="wave.git_commit" class="text-body-2 mb-4" data-testid="wave-commit">
        {{ $t('evidence.commit') }}:
        <span class="monospace" :title="wave.git_commit">{{ wave.git_commit.slice(0, 12) }}</span>
      </p>
      <!-- ESP-18 section 8: state changes announced politely, without moving the focus. -->
      <div role="status" class="sr-only">{{ live.message.value }}</div>
      <p v-if="live.status.value === 'reconnecting'" class="text-body-2 mb-2">
        {{ $t('live.staleData') }}
      </p>

      <!-- US-11: resumption from the last checkpoint after the platform restarted. -->
      <v-alert
        v-if="live.resumeNotice.value"
        type="info"
        variant="tonal"
        class="mb-4"
        data-testid="resume-notice"
      >
        {{ $t('waves.resumedAfterCrash', { checkpoint: live.resumeNotice.value.checkpoint }) }}
        <template v-if="live.resumeNotice.value.lostCost">
          {{
            $t(live.resumeNotice.value.estimated ? 'waves.lostCostEstimated' : 'waves.lostCost', {
              cost: format.money(live.resumeNotice.value.lostCost),
            })
          }}
        </template>
      </v-alert>
      <v-alert
        v-if="live.retryInSeconds.value !== undefined"
        type="info"
        variant="tonal"
        class="mb-4"
      >
        {{ $t('waves.retryIn', { seconds: live.retryInSeconds.value }) }}
      </v-alert>

      <!-- ESP-18 section 6: a canceled wave keeps its final and lost cost; partial artifacts stay drafts. -->
      <v-alert
        v-if="wave.state === 'canceled'"
        type="info"
        variant="tonal"
        class="mb-4"
        data-testid="canceled-notice"
      >
        {{ $t('waves.cancel.canceledNotice', { cost: format.money(wave.cost.lost) }) }}
      </v-alert>
      <!-- US-29 CA-2: a failed wave shows its cause and the schema errors per artifact. -->
      <v-alert
        v-if="wave.state === 'failed'"
        type="error"
        variant="tonal"
        role="alert"
        class="mb-4"
        data-testid="failure-cause"
      >
        <p class="font-weight-medium">
          {{
            $t('waves.failureCause', {
              cause: failureCauseText(wave.failure_cause),
            })
          }}
        </p>
        <template v-if="wave.artifact_errors?.length">
          <p class="mt-2">{{ $t('waves.schemaErrors') }}</p>
          <ul class="ml-4">
            <li v-for="(item, index) in wave.artifact_errors" :key="index">
              <span class="monospace">{{ item.artifact }}</span> ·
              <span class="monospace">{{ item.path }}</span> · {{ item.message }}
            </li>
          </ul>
        </template>
      </v-alert>
      <v-alert
        v-else-if="wave.artifact_errors?.length"
        type="warning"
        variant="tonal"
        class="mb-4"
        data-testid="schema-rejections"
      >
        <!-- US-29 CA-1: the platform rejected an artifact; the agent corrects it in the same wave. -->
        <p>{{ $t('waves.schemaRejected') }}</p>
        <ul class="ml-4">
          <li v-for="(item, index) in wave.artifact_errors" :key="index">
            <span class="monospace">{{ item.artifact }}</span> ·
            <span class="monospace">{{ item.path }}</span> · {{ item.message }}
          </li>
        </ul>
      </v-alert>

      <div v-if="answerNotice" role="status" class="text-body-2 mb-2">{{ answerNotice }}</div>
      <WaveQuestions
        :questions="live.questions.value"
        :can-answer="canAnswer"
        :focus-on-open="openedWaiting"
        :submit="submitAnswer"
      />

      <!-- US-36: cost and time of the wave. -->
      <section aria-labelledby="cost-heading" class="mb-8">
        <h2 id="cost-heading" class="text-h6 mb-2">{{ $t('waves.cost.title') }}</h2>
        <dl class="summary-list" data-testid="wave-cost">
          <dt>{{ $t('waves.cost.reasoningModel') }}</dt>
          <dd>
            {{ providerName(wave.models.reasoning.provider) }} ·
            <span class="monospace">{{ wave.models.reasoning.model }}</span>
          </dd>
          <dt>{{ $t('waves.cost.executionModel') }}</dt>
          <dd>
            {{ providerName(wave.models.execution.provider) }} ·
            <span class="monospace">{{ wave.models.execution.model }}</span>
          </dd>
          <dt>{{ $t('waves.cost.inputTokens') }}</dt>
          <dd>{{ format.integer(wave.tokens.input) }}</dd>
          <dt>{{ $t('waves.cost.outputTokens') }}</dt>
          <dd>{{ format.integer(wave.tokens.output) }}</dd>
          <dt>{{ $t('waves.cost.readCacheTokens') }}</dt>
          <dd>{{ format.integer(wave.tokens.read_cache) }}</dd>
          <dt>{{ $t('waves.cost.writeCacheTokens') }}</dt>
          <dd>{{ format.integer(wave.tokens.write_cache) }}</dd>
          <dt>{{ $t('waves.cost.useful') }}</dt>
          <dd>{{ format.money(wave.cost.useful) }}</dd>
          <dt>{{ $t('waves.cost.lost') }}</dt>
          <dd>
            {{ format.money(wave.cost.lost) }}
            <span v-if="hasEstimatedLoss">
              {{ $t('waves.cost.estimatedPart', { cost: format.money(wave.cost.estimated_lost) }) }}
            </span>
          </dd>
          <dt>{{ $t('waves.cost.total') }}</dt>
          <dd data-testid="wave-cost-total">{{ format.money(wave.cost.total) }}</dd>
          <template v-for="item in wave.cost.per_provider ?? []" :key="item.provider">
            <dt>{{ $t('waves.cost.perProvider', { provider: providerName(item.provider) }) }}</dt>
            <dd>{{ format.money(item.total) }}</dd>
          </template>
          <dt>{{ $t('waves.cost.executionTime') }}</dt>
          <dd>{{ format.duration(wave.time.execution_ms) }}</dd>
          <dt>{{ $t('waves.cost.cycleTime') }}</dt>
          <dd>{{ format.duration(wave.time.cycle_ms) }}</dd>
          <template v-if="isEnded && wave.result">
            <dt>{{ $t('waves.cost.result') }}</dt>
            <dd>{{ $t(`waves.results.${wave.result}`) }}</dd>
          </template>
          <template v-if="wave.versions?.price_table">
            <dt>{{ $t('waves.cost.priceTable') }}</dt>
            <dd class="monospace">{{ wave.versions.price_table }}</dd>
          </template>
        </dl>
        <p class="text-body-2 text-medium-emphasis mt-2">{{ $t('waves.cost.timeRule') }}</p>
      </section>

      <section v-if="wave.expected_outputs?.length" aria-labelledby="outputs-heading" class="mb-8">
        <h2 id="outputs-heading" class="text-h6 mb-2">{{ $t('waves.outputs.title') }}</h2>
        <ul class="plain-list">
          <li
            v-for="output in wave.expected_outputs"
            :key="`${output.type}-${output.artifact ?? ''}`"
            class="mb-1"
          >
            {{ output.type }} · {{ $t(`waves.outputs.status.${output.status}`) }}
            <router-link
              v-if="output.artifact"
              :to="{
                name: ROUTE_NAMES.artifact,
                params: { projectId, artifactId: output.artifact },
                query: output.version ? { version: output.version } : {},
              }"
              class="monospace"
            >
              {{ output.artifact }}
            </router-link>
          </li>
        </ul>
      </section>

      <SandboxEvidence
        v-if="showEvidence"
        :wave-id="waveId"
        :executions="live.executions.value"
        :error="live.executionsError.value"
        :download="downloadOutput"
        @retry="live.refreshLists"
      />

      <section aria-labelledby="calls-heading" class="mb-8">
        <div class="d-flex flex-wrap align-center ga-2 mb-2">
          <h2 id="calls-heading" class="text-h6">{{ $t('waves.calls.title') }}</h2>
          <v-spacer />
          <v-btn
            v-if="!isEnded"
            variant="outlined"
            size="small"
            :aria-pressed="live.paused.value"
            @click="live.setPaused(!live.paused.value)"
          >
            {{ live.paused.value ? $t('live.resumeUpdates') : $t('live.pauseUpdates') }}
          </v-btn>
        </div>
        <!-- US-30 CA-1, RNF-07: least privilege refused some tool calls; each one is in the record. -->
        <v-alert
          v-if="refusedCalls.length > 0"
          type="info"
          variant="tonal"
          class="mb-2"
          data-testid="refused-calls"
        >
          {{ $t('waves.calls.refusedNotice', { count: refusedCalls.length }) }}
        </v-alert>
        <p v-if="live.paused.value && live.pendingWhilePaused.value.length > 0" class="text-body-2">
          {{ $t('live.pendingCount', { count: live.pendingWhilePaused.value.length }) }}
        </p>
        <p class="text-body-2 mb-2">
          {{
            wave.last_checkpoint === null || wave.last_checkpoint === undefined
              ? $t('waves.checkpoints.none')
              : $t('waves.checkpoints.last', {
                  n: wave.last_checkpoint,
                  count: live.checkpoints.value.length,
                })
          }}
        </p>
        <p v-if="live.calls.value.length === 0" class="text-body-2">{{ $t('waves.calls.none') }}</p>
        <!-- ESP-18 section 8: the call record is a log; no prompt, answer or tool arguments (ER-18-15). -->
        <div v-else role="log" :aria-label="$t('waves.calls.title')">
          <ol class="call-log">
            <li
              v-for="call in live.calls.value"
              :key="`${call.attempt ?? 1}-${call.number}`"
              :data-testid="`call-${call.number}`"
              class="call-item"
            >
              <p class="font-weight-medium">
                {{ $t('waves.calls.number', { n: call.number }) }} · {{ callTitle(call) }} ·
                {{ $t(`waves.calls.results.${call.result}`) }}
                <template v-if="call.lost">· {{ $t('waves.calls.lostCall') }}</template>
              </p>
              <p v-if="call.summary" class="text-body-2">{{ call.summary }}</p>
              <p v-if="call.refusal_reason" class="text-body-2">
                {{ $t('waves.calls.refusal', { reason: refusalText(call.refusal_reason) }) }}
              </p>
              <p class="text-body-2 text-medium-emphasis">
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
              </p>
            </li>
          </ol>
        </div>
      </section>

      <section v-if="previousAttempts.length" aria-labelledby="attempts-heading" class="mb-8">
        <h2 id="attempts-heading" class="text-h6 mb-2">{{ $t('waves.control.attempts') }}</h2>
        <ul class="plain-list" data-testid="wave-attempts">
          <li v-for="attempt in previousAttempts" :key="attempt.number" class="text-body-2 mb-1">
            {{ $t('waves.attempt', { n: attempt.number }) }} ·
            {{
              $te(`states.wave.${attempt.final_state}`)
                ? $t(`states.wave.${attempt.final_state}`)
                : attempt.final_state
            }}
            <template v-if="attempt.failure_cause">
              · {{ $t('waves.failureCause', { cause: failureCauseText(attempt.failure_cause) }) }}
            </template>
          </li>
        </ul>
      </section>

      <section aria-labelledby="history-heading" class="mb-8">
        <h2 id="history-heading" class="text-h6 mb-2">{{ $t('waves.history.title') }}</h2>
        <p v-if="recentHistory.length === 0" class="text-body-2">{{ $t('waves.history.none') }}</p>
        <ol v-else class="plain-list" data-testid="wave-history">
          <li v-for="event in recentHistory" :key="event.id" class="text-body-2 mb-1">
            <span class="text-medium-emphasis">{{ format.dateTime(event.time) }}</span>
            · {{ describe(event) }}
          </li>
        </ol>
      </section>
    </template>
  </v-container>
</template>

<style scoped>
.call-log {
  list-style: none;
  padding: 0;
}

.call-item {
  border-left: 3px solid rgb(var(--v-theme-on-surface), 0.2);
  padding: 0.25rem 0 0.25rem 0.75rem;
  margin-bottom: 0.75rem;
}
</style>
