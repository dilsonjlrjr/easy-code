import { onBeforeUnmount, ref, type Ref } from 'vue'
import { useRouter } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { apiClient } from '@/shared/api/apiClient'
import { joinUrl } from '@/shared/api/httpClient'
import { useSessionStore } from '@/shared/session/sessionStore'
import {
  createEventStream,
  type CloudEvent,
  type EndReason,
  type EventClass,
  type EventStream,
  type EventStreamOptions,
  type GapInfo,
  type StreamStatus,
} from './eventStream'

export interface ProjectEventStreamOptions {
  projectId: string
  since: number | undefined
  query?: EventStreamOptions['query']
  onEvent: (event: CloudEvent, eventClass: EventClass) => void
  /** Reloads the screen state by REST and returns its `last_sequence` (ER-00-16). */
  reload: (gap: GapInfo) => Promise<number | undefined>
}

let streamFactory: (options: EventStreamOptions) => EventStream = createEventStream

/** Tests replace the stream; the application uses the real client. */
export function setEventStreamFactory(factory: (options: EventStreamOptions) => EventStream): void {
  streamFactory = factory
}

export function projectEventsUrl(projectId: string): string {
  return joinUrl(apiClient.baseUrl, `/projects/${encodeURIComponent(projectId)}/events`)
}

/**
 * One `watchProjectEvents` stream for the page (ER-00-14: at most one per tab), closed when the page
 * goes away. End reasons follow ER-00-17: expired or revoked session → login keeping the address;
 * access removed or project deleted → project list with a notice.
 */
export function useProjectEventStream(): {
  status: Ref<StreamStatus | undefined>
  open: (options: ProjectEventStreamOptions) => void
  close: () => void
} {
  const router = useRouter()
  const session = useSessionStore()
  const status = ref<StreamStatus | undefined>()
  let stream: EventStream | undefined

  function handleEnded(reason: EndReason): void {
    if (reason === 'session_expired' || reason === 'session_revoked') {
      session.markSignedOut()
      void router.push({
        name: ROUTE_NAMES.login,
        query: { return: router.currentRoute.value.fullPath },
      })
      return
    }
    void router.push({ name: ROUTE_NAMES.projects, query: { notice: reason } })
  }

  function close(): void {
    stream?.stop()
    stream = undefined
  }

  function open(options: ProjectEventStreamOptions): void {
    close()
    stream = streamFactory({
      url: projectEventsUrl(options.projectId),
      since: options.since,
      query: options.query,
      onEvent: options.onEvent,
      onGap: options.reload,
      onEnded: handleEnded,
      onStatus: (next) => {
        status.value = next
      },
    })
    stream.start()
  }

  onBeforeUnmount(close)

  return { status, open, close }
}
