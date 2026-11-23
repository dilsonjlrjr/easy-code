// State of the interface. It holds only what is on screen; the server is the source of truth and nothing
// about waves is kept by the app (US-46 RN-1).
import { reactive } from 'vue'
import { bridge, EVENTS } from './bridge'
import type { CloudEvent, ConnectionState, Status, WebTarget } from './types'

export type View =
  | { name: 'dashboard' }
  | { name: 'project'; projectId: string }
  | { name: 'wave'; projectId: string; waveId: string }
  | { name: 'gate'; projectId: string; gateId: string; round: number }
  | { name: 'account' }

export interface RecentNotice {
  key: number
  type: string
  projectId?: string
}

interface State {
  status: Status | null
  view: View
  connection: { notices?: ConnectionState['state']; wave?: ConnectionState['state'] }
  announcement: string
  recent: RecentNotice[]
  /** Bumped when the dashboard should read again (notice, gap, reconnection). */
  dashboardVersion: number
}

export const store = reactive<State>({
  status: null,
  view: { name: 'dashboard' },
  connection: {},
  announcement: '',
  recent: [],
  dashboardVersion: 0,
})

const MAX_RECENT = 20
let recentKey = 0

export function navigate(view: View): void {
  store.view = view
}

/** Polite announcement for screen readers, without moving the focus (RNF-19). */
export function announce(text: string): void {
  store.announcement = ''
  queueMicrotask(() => {
    store.announcement = text
  })
}

export async function openWeb(target: WebTarget): Promise<void> {
  await bridge().call('OpenWeb', target)
}

/** Subscribes to the events of the Go service; returns the unsubscribe function. */
export function listen(onReloaded: () => void): () => void {
  const offs = [
    bridge().on(EVENTS.session, (data) => {
      store.status = data as Status
      if (store.status.phase !== 'signed_in') {
        store.view = { name: 'dashboard' }
        store.connection = {}
        store.recent = []
      }
    }),
    bridge().on(EVENTS.connection, (data) => {
      const state = data as ConnectionState
      store.connection[state.stream] = state.state
      if (state.stream === 'notices' && state.reconnected) {
        store.dashboardVersion++
        onReloaded()
      }
    }),
    bridge().on(EVENTS.notice, (data) => {
      const signal = data as { type: string; project_id?: string }
      store.recent.unshift({ key: ++recentKey, type: signal.type, projectId: signal.project_id })
      store.recent.splice(MAX_RECENT)
      store.dashboardVersion++
    }),
    bridge().on(EVENTS.gap, (data) => {
      if ((data as { stream: string }).stream === 'notices') {
        store.dashboardVersion++
        onReloaded()
      }
    }),
    bridge().on(EVENTS.projectsChanged, () => {
      store.dashboardVersion++
    }),
    bridge().on(EVENTS.navigate, (data) => {
      const target = data as WebTarget
      if (target.kind === 'wave' && target.project_id && target.wave_id) {
        store.view = { name: 'wave', projectId: target.project_id, waveId: target.wave_id }
      }
    }),
  ]
  return () => {
    offs.forEach((off) => {
      off()
    })
  }
}

export type { CloudEvent }
