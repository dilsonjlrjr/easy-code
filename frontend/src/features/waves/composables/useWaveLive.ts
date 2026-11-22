import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { projectsApi, type Project, type ProjectsApi } from '@/features/projects/api/projectsApi'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { describeEvent, shortEventType } from '@/shared/events/eventDescriptions'
import type { CloudEvent, EventClass } from '@/shared/events/eventStream'
import { useProjectEventStream } from '@/shared/events/useProjectEventStream'
import {
  wavesApi,
  type CallRecord,
  type Checkpoint,
  type Question,
  type SandboxExecution,
  type Wave,
  type WavesApi,
} from '../api/wavesApi'
import {
  belongsToWave,
  checkpointTotals,
  lastResumeNotice,
  mergeEvents,
  resumeNoticeFrom,
  waveStateFrom,
  type ResumeNotice,
} from './waveEvents'

let api: WavesApi = wavesApi
let projectApi: Pick<ProjectsApi, 'getProject'> = projectsApi

/** Tests replace the APIs; the application uses the default ones. */
export function setWaveLiveApis(waves: WavesApi, projects: Pick<ProjectsApi, 'getProject'>): void {
  api = waves
  projectApi = projects
}

export function currentWavesApi(): WavesApi {
  return api
}

const REFRESH_DELAY_MS = 200

/**
 * Live state of one wave (ESP-18, US-09, US-11, US-36): read by REST, then kept current by the
 * project stream filtered by the wave. Lists the stream changes are re-read (calls, checkpoints,
 * questions) so a resumption never shows a call twice (US-11 CA-2); state and running cost change at
 * once from the event itself.
 */
export function useWaveLive(projectId: string, waveId: string) {
  const { t } = useI18n()
  const live = useProjectEventStream()
  const { message, announce } = useAnnouncer()

  const project = ref<Project>()
  const wave = ref<Wave>()
  const calls = ref<CallRecord[]>([])
  const checkpoints = ref<Checkpoint[]>([])
  const questions = ref<Question[]>([])
  const executions = ref<SandboxExecution[]>([])
  const executionsError = ref<unknown>()
  const history = ref<CloudEvent[]>([])
  const resumeNotice = ref<ResumeNotice>()
  const retryInSeconds = ref<number>()
  const loading = ref(false)
  const error = ref<unknown>()
  const paused = ref(false)
  const pendingWhilePaused = ref<CloudEvent[]>([])
  let refreshTimer: ReturnType<typeof setTimeout> | undefined

  /**
   * US-41: the sandbox evidence is read on its own, so a failure there shows in its block and never
   * hides the rest of the wave.
   */
  async function readExecutions(): Promise<void> {
    try {
      executions.value = await api.listSandboxExecutions(projectId, waveId)
      executionsError.value = undefined
    } catch (caught) {
      executionsError.value = caught
    }
  }

  async function readState(): Promise<number | undefined> {
    const loadedProject = await projectApi.getProject(projectId)
    const [loadedWave, loadedCalls, loadedCheckpoints, loadedQuestions, events] = await Promise.all(
      [
        api.getWave(projectId, waveId),
        api.listCalls(projectId, waveId),
        api.listCheckpoints(projectId, waveId),
        api.listQuestions(projectId, waveId),
        api.listEvents(projectId, waveId),
        readExecutions(),
      ],
    )
    project.value = loadedProject
    wave.value = loadedWave
    calls.value = loadedCalls
    checkpoints.value = loadedCheckpoints
    questions.value = loadedQuestions
    history.value = mergeEvents([], events.items)
    resumeNotice.value = lastResumeNotice(history.value)
    // sse.md 5.1: the stream starts after the last sequence the project read reflects; events read
    // meanwhile come again and are dropped by their id.
    return loadedProject.last_sequence
  }

  async function refreshLists(): Promise<void> {
    const [loadedWave, loadedCalls, loadedCheckpoints, loadedQuestions] = await Promise.all([
      api.getWave(projectId, waveId),
      api.listCalls(projectId, waveId),
      api.listCheckpoints(projectId, waveId),
      api.listQuestions(projectId, waveId),
      readExecutions(),
    ])
    wave.value = loadedWave
    calls.value = loadedCalls
    checkpoints.value = loadedCheckpoints
    questions.value = loadedQuestions
  }

  function scheduleRefresh(): void {
    if (refreshTimer !== undefined) clearTimeout(refreshTimer)
    refreshTimer = setTimeout(() => {
      refreshTimer = undefined
      void refreshLists().catch(() => undefined)
    }, REFRESH_DELAY_MS)
  }

  function applyDomainEvent(event: CloudEvent): void {
    if (history.value.some((known) => known.id === event.id)) return
    history.value = mergeEvents(history.value, [event])
    const state = waveStateFrom(event)
    if (state && wave.value) {
      wave.value = { ...wave.value, state }
      announce(describeEvent(t, event))
    }
    const totals = checkpointTotals(event)
    if (totals && wave.value) {
      wave.value = {
        ...wave.value,
        tokens: totals.tokens,
        cost: { ...wave.value.cost, total: totals.cost },
        last_checkpoint: totals.checkpoint,
      }
    }
    const notice = resumeNoticeFrom(event)
    if (notice) {
      resumeNotice.value = notice
      announce(describeEvent(t, event))
    }
    const type = shortEventType(event.type)
    if (type === 'wave.retry_scheduled' && typeof event.data.delay_ms === 'number') {
      retryInSeconds.value = Math.round(event.data.delay_ms / 1000)
    } else if (state === 'running') {
      retryInSeconds.value = undefined
    }
    if (type === 'wave.question_opened' || type === 'wave.task_failed') {
      announce(describeEvent(t, event))
    }
    // Every domain event re-reads the lists; `wave.tool_called` fires when each sandbox command ends,
    // so the evidence block follows the commands as they finish (US-41, ESP-14 "em_execucao").
    scheduleRefresh()
  }

  function handleEvent(event: CloudEvent, eventClass: EventClass): void {
    if (!belongsToWave(event, waveId)) return
    if (eventClass === 'telemetry') {
      // `cost.model_call_recorded`: a model call ended; the cost and the call list follow it.
      if (shortEventType(event.type) === 'cost.model_call_recorded') scheduleRefresh()
      return
    }
    if (paused.value) {
      pendingWhilePaused.value = [...pendingWhilePaused.value, event]
      return
    }
    applyDomainEvent(event)
  }

  /** WCAG 2.2.2: the live list can be frozen; events gathered meanwhile come in on resume. */
  function setPaused(value: boolean): void {
    paused.value = value
    if (!value) {
      const pending = pendingWhilePaused.value
      pendingWhilePaused.value = []
      for (const event of pending) applyDomainEvent(event)
    }
  }

  async function load(): Promise<void> {
    loading.value = true
    error.value = undefined
    try {
      const since = await readState()
      live.open({
        projectId,
        since,
        query: { waveId },
        onEvent: handleEvent,
        reload: readState,
      })
    } catch (caught) {
      error.value = caught
    } finally {
      loading.value = false
    }
  }

  onBeforeUnmount(() => {
    if (refreshTimer !== undefined) clearTimeout(refreshTimer)
  })

  return {
    project,
    wave,
    calls,
    checkpoints,
    questions,
    executions,
    executionsError,
    history,
    resumeNotice,
    retryInSeconds,
    loading,
    error,
    paused,
    pendingWhilePaused,
    status: live.status,
    message,
    load,
    refreshLists,
    setPaused,
  }
}
