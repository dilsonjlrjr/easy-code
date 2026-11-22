/**
 * Incremental parser of the `text/event-stream` format (WHATWG HTML, server-sent events), used by
 * the event stream client instead of `EventSource` because the client must see heartbeat comments
 * (sse.md section 10), read the HTTP status (401, 404, 429 with `Retry-After`, section 11) and send
 * `Last-Event-ID` itself.
 */
export interface SseFrame {
  /** `event:` field; `message` when absent. */
  event: string
  data: string
  /** `id:` field; absent on telemetry and control frames. */
  id?: string
}

export interface SseParserHandlers {
  onFrame: (frame: SseFrame) => void
  onComment?: (comment: string) => void
  onRetry?: (milliseconds: number) => void
}

export interface SseParser {
  feed: (chunk: string) => void
}

export function createSseParser(handlers: SseParserHandlers): SseParser {
  let buffer = ''
  let eventName = ''
  let dataLines: string[] = []
  let frameId: string | undefined
  let hasField = false

  function dispatch(): void {
    if (hasField && dataLines.length > 0) {
      handlers.onFrame({
        event: eventName === '' ? 'message' : eventName,
        data: dataLines.join('\n'),
        ...(frameId === undefined ? {} : { id: frameId }),
      })
    }
    eventName = ''
    dataLines = []
    frameId = undefined
    hasField = false
  }

  function processLine(line: string): void {
    if (line === '') {
      dispatch()
      return
    }
    if (line.startsWith(':')) {
      handlers.onComment?.(line.slice(1).trimStart())
      return
    }
    const colon = line.indexOf(':')
    const field = colon === -1 ? line : line.slice(0, colon)
    let value = colon === -1 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)

    switch (field) {
      case 'event':
        eventName = value
        hasField = true
        break
      case 'data':
        dataLines.push(value)
        hasField = true
        break
      case 'id':
        if (!value.includes('\0')) frameId = value
        hasField = true
        break
      case 'retry':
        if (/^\d+$/.test(value)) handlers.onRetry?.(Number(value))
        break
      default:
        break
    }
  }

  return {
    feed(chunk: string): void {
      buffer += chunk
      const lines = buffer.split(/\r\n|\r|\n/)
      // The last piece may be an incomplete line; keep it for the next chunk.
      buffer = lines.pop() ?? ''
      for (const line of lines) processLine(line)
    },
  }
}
