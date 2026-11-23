// The interface never talks to the api: it calls the Go service bound by Wails and listens to the events it
// emits (cliente-local.md section 4, ADR-20). Tests replace the bridge with a fake (setBridge).
import { Call, Events } from '@wailsio/runtime'
import type { UIError } from './types'

const SERVICE = 'easycode-desktop/internal/app.Service'

export interface Bridge {
  call<T>(method: string, ...args: unknown[]): Promise<T>
  on(event: string, handler: (data: unknown) => void): () => void
}

const wailsBridge: Bridge = {
  // A failed call rejects with an Error whose `cause` is the Go app.UIError; views read it with toUIError.
  async call<T>(method: string, ...args: unknown[]): Promise<T> {
    return (await Call.ByName(`${SERVICE}.${method}`, ...args)) as T
  },
  on(event, handler) {
    return Events.On(event, (ev: { data: unknown }) => {
      handler(ev.data)
    })
  },
}

let current: Bridge = wailsBridge

export function bridge(): Bridge {
  return current
}

export function setBridge(next: Bridge): void {
  current = next
}

/** Wails rejects with the Go error marshalled as `cause` (app.UIError). */
export function toUIError(error: unknown): UIError {
  if (isUIError(error)) return error
  if (typeof error === 'object' && error !== null && 'cause' in error) {
    if (isUIError(error.cause)) return error.cause
  }
  return { kind: 'network' }
}

function isUIError(value: unknown): value is UIError {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { kind?: unknown }).kind === 'string'
  )
}

/** Names of the events emitted by internal/app (service.go). */
export const EVENTS = {
  session: 'easycode:session',
  connection: 'easycode:connection',
  notice: 'easycode:notice',
  wave: 'easycode:wave-event',
  gap: 'easycode:gap',
  waveClosed: 'easycode:wave-closed',
  navigate: 'easycode:navigate',
  projectsChanged: 'easycode:projects-changed',
} as const
