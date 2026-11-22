import { withQuery } from '@/shared/api/httpClient'
import { createSseParser, type SseFrame } from './sseParser'

/** CloudEvent 1.0 of the catalog (`specs/dominio/eventos/envelope.schema.json`). */
export interface CloudEvent<TData = Record<string, unknown>> {
  specversion: string
  id: string
  type: string
  source: string
  subject: string
  time: string
  data: TData
  correlationid?: string
  /** Only domain events carry the project sequence. */
  sequence?: number
  projectid?: string
  waveid?: string
}

export type EventClass = 'domain' | 'telemetry'

export type StreamStatus = 'connecting' | 'live' | 'reconnecting' | 'reloading' | 'ended'

/** `easycode.ended` reasons (sse.md 5.4), plus `not_found` when the stream answers 404. */
export type EndReason =
  'session_expired' | 'session_revoked' | 'access_removed' | 'project_deleted' | 'not_found'

export interface GapInfo {
  request: number
  first_available: number
  action: string
}

export interface Timers {
  setTimeout: (callback: () => void, milliseconds: number) => unknown
  clearTimeout: (handle: unknown) => void
}

export interface EventStreamOptions {
  /** Absolute or same-origin URL of the stream (`watchProjectEvents`), without `since`. */
  url: string
  /** Last sequence already reflected in the state read by REST (`Project.last_sequence`, sse.md 5.1). */
  since?: number
  /** Extra filters (`waveId`, `class`, `type`); with filters, sequence jumps are expected. */
  query?: Record<string, string | string[] | undefined>
  onEvent: (event: CloudEvent, eventClass: EventClass) => void
  /**
   * Called on `easycode.gap`: the client must reload the state by REST before applying more events.
   * Returns the `last_sequence` of the reloaded state, so older replayed events are discarded.
   */
  onGap?: (gap: GapInfo) => Promise<number | undefined>
  onEnded?: (reason: EndReason) => void
  onStatus?: (status: StreamStatus) => void
  onUnauthorized?: () => void
  fetchImplementation?: typeof fetch
  timers?: Timers
  /** No frame (not even a heartbeat) for this long means a dead connection (sse.md 10: 45 s). */
  silenceTimeoutMs?: number
  /** Most a reconnection waits after repeated failures. */
  maxReconnectDelayMs?: number
}

export interface EventStream {
  start: () => void
  stop: () => void
  readonly lastSequence: number | undefined
}

const SILENCE_TIMEOUT_MS = 45_000
const DEFAULT_RETRY_MS = 2_000
const MAX_RECONNECT_DELAY_MS = 30_000
const RATE_LIMIT_FALLBACK_MS = 5_000
const MAX_REMEMBERED_IDS = 2_000

const FINAL_REASONS = new Set<string>([
  'session_expired',
  'session_revoked',
  'access_removed',
  'project_deleted',
])

const defaultTimers: Timers = {
  setTimeout: (callback, milliseconds) => globalThis.setTimeout(callback, milliseconds),
  clearTimeout: (handle) => {
    globalThis.clearTimeout(handle as ReturnType<typeof globalThis.setTimeout>)
  },
}

/**
 * Client of `watchProjectEvents` (sse.md). Guarantees for the screens: domain events in `sequence`
 * order, never twice (by `sequence` and by CloudEvent `id`), resumption with `Last-Event-ID` after a
 * drop (US-09 CA-3), reload of the state on `easycode.gap` before applying more events (ER-00-16),
 * and the end reasons of `easycode.ended` (ER-00-17).
 */
export function createEventStream(options: EventStreamOptions): EventStream {
  const fetchImplementation = options.fetchImplementation ?? globalThis.fetch.bind(globalThis)
  const timers = options.timers ?? defaultTimers
  const silenceTimeoutMs = options.silenceTimeoutMs ?? SILENCE_TIMEOUT_MS
  const maxReconnectDelayMs = options.maxReconnectDelayMs ?? MAX_RECONNECT_DELAY_MS

  let lastSequence = options.since
  let lastEventId: string | undefined
  let retryMs = DEFAULT_RETRY_MS
  let failures = 0
  let stopped = true
  let controller: AbortController | undefined
  let silenceTimer: unknown
  let reconnectTimer: unknown
  let processing: Promise<void> = Promise.resolve()
  const seenIds = new Set<string>()

  function isStopped(): boolean {
    return stopped
  }

  function setStatus(status: StreamStatus): void {
    options.onStatus?.(status)
  }

  function clearSilenceTimer(): void {
    if (silenceTimer !== undefined) timers.clearTimeout(silenceTimer)
    silenceTimer = undefined
  }

  function armSilenceTimer(): void {
    clearSilenceTimer()
    silenceTimer = timers.setTimeout(() => {
      // Dead connection: drop it and resume from the last event.
      controller?.abort()
      scheduleReconnect(0)
    }, silenceTimeoutMs)
  }

  function scheduleReconnect(delayMs: number): void {
    if (stopped) return
    clearSilenceTimer()
    if (reconnectTimer !== undefined) timers.clearTimeout(reconnectTimer)
    setStatus('reconnecting')
    reconnectTimer = timers.setTimeout(() => {
      reconnectTimer = undefined
      void connect()
    }, delayMs)
  }

  function backoffDelay(): number {
    failures += 1
    return Math.min(retryMs * 2 ** (failures - 1), maxReconnectDelayMs)
  }

  function end(reason: EndReason): void {
    stopped = true
    clearSilenceTimer()
    controller?.abort()
    setStatus('ended')
    options.onEnded?.(reason)
  }

  function remember(id: string): void {
    seenIds.add(id)
    if (seenIds.size > MAX_REMEMBERED_IDS) {
      const oldest = seenIds.values().next().value
      if (oldest !== undefined) seenIds.delete(oldest)
    }
  }

  async function handleFrame(frame: SseFrame): Promise<void> {
    if (stopped) return
    if (frame.event === 'easycode.gap') {
      setStatus('reloading')
      const gap = JSON.parse(frame.data) as GapInfo
      const reloaded = await options.onGap?.(gap)
      if (reloaded !== undefined) lastSequence = reloaded
      // `stop()` may have run while the state was reloading.
      if (!isStopped()) setStatus('live')
      return
    }
    if (frame.event === 'easycode.ended') {
      const { reason } = JSON.parse(frame.data) as { reason: string }
      if (FINAL_REASONS.has(reason)) {
        end(reason as EndReason)
      } else {
        // slow_consumer reconnects at once; offboarding with the usual delay.
        controller?.abort()
        scheduleReconnect(reason === 'slow_consumer' ? 0 : retryMs)
      }
      return
    }

    let event: CloudEvent
    try {
      event = JSON.parse(frame.data) as CloudEvent
    } catch {
      return
    }

    if (frame.id === undefined) {
      options.onEvent(event, 'telemetry')
      return
    }
    const sequence = event.sequence ?? Number(frame.id)
    if (lastSequence !== undefined && sequence <= lastSequence) return
    if (seenIds.has(event.id)) return
    remember(event.id)
    lastSequence = sequence
    lastEventId = frame.id
    options.onEvent(event, 'domain')
  }

  async function connect(): Promise<void> {
    if (stopped) return
    controller = new AbortController()
    const headers: Record<string, string> = { Accept: 'text/event-stream' }
    if (lastEventId !== undefined) headers['Last-Event-ID'] = lastEventId
    const url = withQuery(options.url, {
      ...options.query,
      since: lastEventId === undefined && lastSequence !== undefined ? lastSequence : undefined,
    })

    let response: Response
    try {
      response = await fetchImplementation(url, {
        headers,
        credentials: 'include',
        signal: controller.signal,
        cache: 'no-store',
      })
    } catch {
      if (!controller.signal.aborted) scheduleReconnect(backoffDelay())
      return
    }

    if (response.status === 401) {
      end('session_expired')
      options.onUnauthorized?.()
      return
    }
    if (response.status === 404) {
      end('not_found')
      return
    }
    if (response.status === 429) {
      const retryAfter = Number(response.headers.get('Retry-After'))
      scheduleReconnect(retryAfter > 0 ? retryAfter * 1000 : RATE_LIMIT_FALLBACK_MS)
      return
    }
    if (!response.ok || !response.body) {
      scheduleReconnect(backoffDelay())
      return
    }

    failures = 0
    setStatus('live')
    armSilenceTimer()
    await readBody(response.body, controller.signal)
  }

  async function readBody(body: ReadableStream<Uint8Array>, signal: AbortSignal): Promise<void> {
    const reader = body.getReader()
    signal.addEventListener(
      'abort',
      () => {
        reader.cancel().catch(() => undefined)
      },
      { once: true },
    )
    const decoder = new TextDecoder()
    const parser = createSseParser({
      onFrame: (frame) => {
        processing = processing.then(() => handleFrame(frame))
      },
      onRetry: (milliseconds) => {
        retryMs = milliseconds
      },
    })
    try {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        armSilenceTimer()
        parser.feed(decoder.decode(value, { stream: true }))
      }
    } catch {
      // Aborted or network drop: handled below.
    }
    await processing
    if (signal.aborted || stopped) return
    // The server closed the stream without `easycode.ended`: resume like EventSource does.
    scheduleReconnect(retryMs)
  }

  return {
    start(): void {
      if (!stopped) return
      stopped = false
      setStatus('connecting')
      void connect()
    },
    stop(): void {
      stopped = true
      clearSilenceTimer()
      if (reconnectTimer !== undefined) timers.clearTimeout(reconnectTimer)
      controller?.abort()
    },
    get lastSequence() {
      return lastSequence
    },
  }
}
