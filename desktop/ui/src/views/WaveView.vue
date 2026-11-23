<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bridge, EVENTS, toUIError } from '@/bridge'
import ErrorMessage from '@/components/ErrorMessage.vue'
import OpenWebButton from '@/components/OpenWebButton.vue'
import { formatMoney, shortType } from '@/format'
import { fieldText, useLabel } from '@/i18n'
import { announce, navigate, openWeb } from '@/store'
import type { CallRecord, CloudEvent, ConnectionState, Project, UIError, Wave } from '@/types'

// ESP-28 Onda ao vivo (US-46 CA-2, CA-5): state, agent, models, call log and accumulated cost, live. The Go
// process holds the stream (watchProjectEvents filtered by the wave, from last_sequence); domain events
// update the state at once and the view reads the wave again from the server, which is the source of truth.
const props = defineProps<{ projectId: string; waveId: string }>()
const { t, locale } = useI18n()
const label = useLabel()

interface LiveEntry {
  key: string
  text: string
}

const project = ref<Project | null>(null)
const wave = ref<Wave | null>(null)
const calls = ref<CallRecord[]>([])
const live = ref<LiveEntry[]>([])
const following = ref(false)
const error = ref<UIError | null>(null)
const closedReason = ref('')
const seen = new Set<string>()
const offs: (() => void)[] = []
let reloadTimer: ReturnType<typeof setTimeout> | undefined
const MAX_LIVE = 50

interface Snapshot {
  project: Project
  wave: Wave
  calls: { items: CallRecord[] }
}

function apply(view: Snapshot): void {
  project.value = view.project
  wave.value = view.wave
  calls.value = view.calls.items
}

async function reload(): Promise<void> {
  try {
    apply(await bridge().call<Snapshot>('WaveSnapshot', props.projectId, props.waveId))
  } catch (e) {
    error.value = toUIError(e)
  }
}

function scheduleReload(): void {
  clearTimeout(reloadTimer)
  reloadTimer = setTimeout(() => void reload(), 300)
}

function addLive(key: string, text: string): void {
  live.value.unshift({ key, text })
  live.value.splice(MAX_LIVE)
}

function waveStateLabel(state: string): string {
  return label(`states.wave.${state}`, state)
}

// Idempotent by CloudEvent id: an event replayed after a reconnection is applied once (sse.md 5.1).
function onWaveEvent(data: unknown): void {
  const event = data as CloudEvent
  if (!event.id || seen.has(event.id)) return
  seen.add(event.id)
  const type = shortType(event.type)
  const payload = event.data ?? {}
  if (type === 'agent.progress_reported') {
    addLive(event.id, t('wave.progress', { step: fieldText(payload.step) }))
    return
  }
  if (type === 'cost.model_call_recorded') {
    addLive(
      event.id,
      t('wave.modelCall', {
        model: fieldText(payload.model),
        result: label(`wave.results.${fieldText(payload.result)}`, fieldText(payload.result)),
      }),
    )
    scheduleReload()
    return
  }
  const transition = payload.transition as { machine?: string; to_state?: string } | undefined
  if (transition?.machine === 'wave' && transition.to_state && wave.value) {
    wave.value = { ...wave.value, state: transition.to_state }
    announce(t('common.state', { state: waveStateLabel(transition.to_state) }))
  }
  addLive(event.id, label(`notices.${type}`, t('notices.other', { type })))
  scheduleReload()
}

onMounted(async () => {
  offs.push(
    bridge().on(EVENTS.wave, onWaveEvent),
    bridge().on(EVENTS.gap, (data) => {
      if ((data as { stream: string }).stream !== 'wave') return
      announce(t('connection.reloaded'))
      void reload()
    }),
    bridge().on(EVENTS.connection, (data) => {
      const state = data as ConnectionState
      if (state.stream === 'wave' && state.reconnected) {
        announce(t('connection.reconnected'))
        void reload()
      }
    }),
    bridge().on(EVENTS.waveClosed, (data) => {
      closedReason.value = (data as { reason?: string }).reason ?? 'not_found'
    }),
  )
  try {
    apply(await bridge().call<Snapshot>('OpenWave', props.projectId, props.waveId))
    following.value = await bridge().call<boolean>('Following', props.projectId, props.waveId)
  } catch (e) {
    error.value = toUIError(e)
  }
})

onBeforeUnmount(() => {
  clearTimeout(reloadTimer)
  offs.forEach((off) => {
    off()
  })
  void bridge().call('CloseWave')
})

async function toggleFollow(): Promise<void> {
  await bridge().call('Follow', props.projectId, props.waveId, following.value)
}

const stateText = computed(() => {
  const current = wave.value
  if (!current) return ''
  const label = waveStateLabel(current.state)
  let hint = ''
  if (current.state === 'queued') hint = t('wave.hints.queued')
  if (current.state === 'pausing') hint = t('wave.hints.pausing')
  if (current.state === 'paused' && current.pause?.reason === 'budget')
    hint = t('wave.hints.paused_budget')
  else if (current.state === 'paused') hint = t('wave.hints.paused_manual')
  if (current.state === 'failed' && current.failure_cause) hint = current.failure_cause
  return hint ? `${label} (${hint})` : label
})

const cost = computed(() =>
  formatMoney(wave.value?.cost?.total ?? wave.value?.cost_total, locale.value),
)

function model(ref?: { provider?: string; model?: string }): string {
  if (!ref?.model) return t('common.none')
  return ref.provider ? `${ref.provider} / ${ref.model}` : ref.model
}
</script>

<template>
  <section aria-labelledby="view-title">
    <button type="button" class="link-button" @click="navigate({ name: 'project', projectId })">
      {{ project?.name ?? t('common.back') }}
    </button>
    <h1 id="view-title" tabindex="-1">{{ t('wave.title', { id: waveId }) }}</h1>
    <ErrorMessage :error="error" />
    <p v-if="closedReason" class="notice" role="alert">{{ t(`wave.closed.${closedReason}`) }}</p>

    <template v-if="wave">
      <dl class="facts">
        <dt>{{ t('wave.state') }}</dt>
        <dd>{{ stateText }}</dd>
        <dt>{{ t('wave.stage') }}</dt>
        <dd>{{ label(`stages.${wave.stage}`, wave.stage) }}</dd>
        <dt>{{ t('wave.agent') }}</dt>
        <dd>{{ label(`agents.${wave.agent}`, wave.agent) }}</dd>
        <dt>{{ t('wave.model') }}</dt>
        <dd>
          <span>{{
            t('wave.reasoningModel', { model: model(wave.models?.reasoning) })
          }}</span><br />
          <span>{{ t('wave.executionModel', { model: model(wave.models?.execution) }) }}</span>
        </dd>
        <dt>{{ t('wave.cost') }}</dt>
        <dd>{{ cost ?? t('common.none') }}</dd>
        <dt>{{ t('wave.attempt') }}</dt>
        <dd>{{ wave.attempt }}</dd>
      </dl>

      <div class="actions">
        <OpenWebButton
          :label="t('wave.actOnWeb')"
          :open="() => openWeb({ kind: 'wave', project_id: projectId, wave_id: waveId })"
        />
        <label class="check" for="follow-wave">
          <input id="follow-wave" v-model="following" type="checkbox" @change="toggleFollow" />
          {{ t('wave.follow') }}
        </label>
      </div>

      <h2 id="live-title">{{ t('wave.live') }}</h2>
      <p v-if="live.length === 0">{{ t('wave.noLive') }}</p>
      <ol v-else class="rows" role="log" aria-live="off" aria-labelledby="live-title">
        <li v-for="entry in live" :key="entry.key">{{ entry.text }}</li>
      </ol>

      <h2 id="calls-title">{{ t('wave.calls') }}</h2>
      <p v-if="calls.length === 0">{{ t('wave.noCalls') }}</p>
      <div v-else class="table-wrap">
        <table aria-labelledby="calls-title">
          <thead>
            <tr>
              <th scope="col">{{ t('wave.callColumns.number') }}</th>
              <th scope="col">{{ t('wave.callColumns.type') }}</th>
              <th scope="col">{{ t('wave.callColumns.what') }}</th>
              <th scope="col">{{ t('wave.callColumns.duration') }}</th>
              <th scope="col">{{ t('wave.callColumns.result') }}</th>
              <th scope="col">{{ t('wave.callColumns.summary') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="call in calls" :key="call.number">
              <td>{{ call.number }}</td>
              <td>{{ t(`wave.callTypes.${call.type}`) }}</td>
              <td>
                {{
                  call.type === 'model'
                    ? (call.model ?? t('common.none'))
                    : (call.tool ?? t('common.none'))
                }}
              </td>
              <td>{{ t('wave.durationMs', { ms: call.duration_ms }) }}</td>
              <td>{{ label(`wave.results.${call.result}`, call.result) }}</td>
              <td>{{ call.summary ?? t('common.none') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </section>
</template>
