import { describe, expect, it, vi } from 'vitest'
import { createEventStream, type CloudEvent, type StreamStatus, type Timers } from '../eventStream'

const encoder = new TextEncoder()

function controllableResponse(status = 200, headers: Record<string, string> = {}) {
  let controller: ReadableStreamDefaultController<Uint8Array> | undefined
  const body = new ReadableStream<Uint8Array>({
    start(streamController) {
      controller = streamController
    },
  })
  return {
    response: new Response(status === 200 ? body : null, {
      status,
      headers: { 'Content-Type': 'text/event-stream', ...headers },
    }),
    push: (text: string) => controller?.enqueue(encoder.encode(text)),
    close: () => controller?.close(),
  }
}

function manualTimers() {
  const tasks = new Map<number, { callback: () => void; milliseconds: number }>()
  let nextId = 0
  const timers: Timers = {
    setTimeout: (callback, milliseconds) => {
      nextId += 1
      tasks.set(nextId, { callback, milliseconds })
      return nextId
    },
    clearTimeout: (handle) => {
      tasks.delete(handle as number)
    },
  }
  return {
    timers,
    /** Runs the pending timers with the given delay (or all). */
    fire(milliseconds?: number) {
      for (const [id, task] of [...tasks]) {
        if (milliseconds === undefined || task.milliseconds === milliseconds) {
          tasks.delete(id)
          task.callback()
        }
      }
    },
    delays: () => [...tasks.values()].map((task) => task.milliseconds),
  }
}

function domainFrame(sequence: number, id = `event-${String(sequence)}`): string {
  const event = { id, type: 'br.easycode.wave.started.v1', sequence, data: { wave: 'REQ-01' } }
  return `id: ${String(sequence)}\nevent: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`
}

function last<TItem>(items: TItem[]): TItem | undefined {
  return items[items.length - 1]
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

function headerOf(fetchMock: ReturnType<typeof vi.fn<typeof fetch>>, call: number, name: string) {
  const init = fetchMock.mock.calls[call]?.[1]
  return new Headers(init?.headers).get(name)
}

describe('createEventStream', () => {
  it('starts after the sequence read by REST and applies domain events once, in order (sse.md 5.1)', async () => {
    const first = controllableResponse()
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValueOnce(first.response)
    const received: Array<{ event: CloudEvent; eventClass: string }> = []
    const stream = createEventStream({
      url: '/api/v1/projects/p1/events',
      since: 10,
      query: { waveId: 'REQ-01' },
      fetchImplementation: fetchMock,
      timers: manualTimers().timers,
      onEvent: (event, eventClass) => received.push({ event, eventClass }),
    })

    stream.start()
    await flush()
    first.push(domainFrame(10)) // already reflected by REST
    first.push(domainFrame(11))
    first.push(domainFrame(11)) // duplicate
    first.push('event: br.easycode.cost.model_call_recorded.v1\ndata: {"id":"t1","data":{}}\n\n')
    first.push(domainFrame(12))
    await flush()

    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/projects/p1/events?waveId=REQ-01&since=10')
    expect(
      received.map(({ event, eventClass }) => [event.sequence ?? event.id, eventClass]),
    ).toEqual([
      [11, 'domain'],
      ['t1', 'telemetry'],
      [12, 'domain'],
    ])
    expect(stream.lastSequence).toBe(12)
    stream.stop()
  })

  it('watchNotices orders by the outbox position, so events of other projects are not dropped (sse.md 3)', async () => {
    const first = controllableResponse()
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValueOnce(first.response)
    const received: string[] = []
    const stream = createEventStream({
      url: '/api/v1/me/events',
      order: 'position',
      fetchImplementation: fetchMock,
      timers: manualTimers().timers,
      onEvent: (event) => received.push(event.id),
    })
    const frame = (position: number, sequence: number, id: string) =>
      `id: ${String(position)}\nevent: e\ndata: ${JSON.stringify({ id, type: 'e', sequence, data: {} })}\n\n`

    stream.start()
    await flush()
    first.push(frame(100, 50, 'project-a'))
    first.push(frame(101, 3, 'project-b'))
    first.push(frame(101, 3, 'project-b'))
    await flush()

    expect(received).toEqual(['project-a', 'project-b'])
    stream.stop()
  })

  it('US-09 CA-3: after a drop it reconnects with Last-Event-ID and skips what it already applied', async () => {
    const first = controllableResponse()
    const second = controllableResponse()
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(first.response)
      .mockResolvedValueOnce(second.response)
    const clock = manualTimers()
    const statuses: StreamStatus[] = []
    const sequences: number[] = []
    const stream = createEventStream({
      url: '/events',
      since: 0,
      fetchImplementation: fetchMock,
      timers: clock.timers,
      onEvent: (event) => sequences.push(event.sequence ?? -1),
      onStatus: (status) => statuses.push(status),
    })

    stream.start()
    await flush()
    first.push('retry: 2000\n\n')
    first.push(domainFrame(1))
    first.push(domainFrame(2))
    await flush()
    first.close()
    await flush()

    expect(last(statuses)).toBe('reconnecting')
    clock.fire(2000)
    await flush()
    expect(headerOf(fetchMock, 1, 'Last-Event-ID')).toBe('2')

    second.push(domainFrame(2)) // replayed by the server: dropped
    second.push(domainFrame(3))
    await flush()
    expect(sequences).toEqual([1, 2, 3])
    expect(last(statuses)).toBe('live')
    stream.stop()
  })

  it('ER-00-16 / CA-00-10: 45 s without any frame means a dead connection and a reconnection', async () => {
    const first = controllableResponse()
    const second = controllableResponse()
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(first.response)
      .mockResolvedValueOnce(second.response)
    const clock = manualTimers()
    const statuses: StreamStatus[] = []
    const stream = createEventStream({
      url: '/events',
      fetchImplementation: fetchMock,
      timers: clock.timers,
      onEvent: () => undefined,
      onStatus: (status) => statuses.push(status),
    })

    stream.start()
    await flush()
    first.push(domainFrame(5))
    await flush()
    expect(clock.delays()).toContain(45_000)

    clock.fire(45_000)
    expect(last(statuses)).toBe('reconnecting')
    clock.fire(0)
    await flush()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(headerOf(fetchMock, 1, 'Last-Event-ID')).toBe('5')
    stream.stop()
  })

  it('CA-00-11: on easycode.gap it reloads the state before applying the next events', async () => {
    const first = controllableResponse()
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValueOnce(first.response)
    let releaseReload: (value: number) => void = () => undefined
    const order: string[] = []
    const statuses: StreamStatus[] = []
    const stream = createEventStream({
      url: '/events',
      since: 40,
      fetchImplementation: fetchMock,
      timers: manualTimers().timers,
      onGap: () => {
        order.push('reload')
        return new Promise<number>((resolve) => {
          releaseReload = resolve
        })
      },
      onEvent: (event) => order.push(`event ${String(event.sequence)}`),
      onStatus: (status) => statuses.push(status),
    })

    stream.start()
    await flush()
    first.push(
      'event: easycode.gap\ndata: {"request":40,"first_available":401,"action":"reload_state"}\n\n',
    )
    first.push(domainFrame(401))
    first.push(domainFrame(402))
    await flush()
    expect(order).toEqual(['reload'])
    expect(last(statuses)).toBe('reloading')

    releaseReload(401) // the state read by REST already reflects 401
    await flush()
    expect(order).toEqual(['reload', 'event 402'])
    expect(last(statuses)).toBe('live')
    stream.stop()
  })

  it('ER-00-17: session_expired ends the stream for good; slow_consumer reconnects at once', async () => {
    const first = controllableResponse()
    const second = controllableResponse()
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(first.response)
      .mockResolvedValueOnce(second.response)
    const clock = manualTimers()
    const ended: string[] = []
    const stream = createEventStream({
      url: '/events',
      fetchImplementation: fetchMock,
      timers: clock.timers,
      onEvent: () => undefined,
      onEnded: (reason) => ended.push(reason),
    })

    stream.start()
    await flush()
    first.push('event: easycode.ended\ndata: {"reason":"slow_consumer"}\n\n')
    await flush()
    clock.fire(0)
    await flush()
    expect(fetchMock).toHaveBeenCalledTimes(2)

    second.push('event: easycode.ended\ndata: {"reason":"session_expired"}\n\n')
    await flush()
    clock.fire()
    await flush()
    expect(ended).toEqual(['session_expired'])
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('ER-00-18: 429 waits for Retry-After before trying again; 401 ends and reports it', async () => {
    const limited = controllableResponse(429, { 'Retry-After': '7' })
    const unauthorized = controllableResponse(401)
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(limited.response)
      .mockResolvedValueOnce(unauthorized.response)
    const clock = manualTimers()
    const onUnauthorized = vi.fn()
    const stream = createEventStream({
      url: '/events',
      fetchImplementation: fetchMock,
      timers: clock.timers,
      onEvent: () => undefined,
      onUnauthorized,
    })

    stream.start()
    await flush()
    expect(clock.delays()).toEqual([7000])
    clock.fire(7000)
    await flush()
    expect(onUnauthorized).toHaveBeenCalledOnce()
    expect(clock.delays()).toEqual([])
  })
})
