import { describe, expect, it } from 'vitest'
import { createSseParser, type SseFrame } from '../sseParser'

function parse(chunks: string[]) {
  const frames: SseFrame[] = []
  const comments: string[] = []
  const retries: number[] = []
  const parser = createSseParser({
    onFrame: (frame) => frames.push(frame),
    onComment: (comment) => comments.push(comment),
    onRetry: (milliseconds) => retries.push(milliseconds),
  })
  for (const chunk of chunks) parser.feed(chunk)
  return { frames, comments, retries }
}

describe('createSseParser', () => {
  it('reads the frames of sse.md section 2: retry, domain with id, telemetry, heartbeat, control', () => {
    const { frames, comments, retries } = parse([
      'retry: 2000\n\n',
      'id: 128\nevent: br.easycode.wave.started.v1\ndata: {"id":"a","sequence":128}\n\n',
      'event: br.easycode.cost.model_call_recorded.v1\ndata: {"id":"t"}\n\n',
      ': hb 2026-11-19T14:00:15-03:00\n\n',
      'event: easycode.gap\ndata: {"request":40,"first_available":401,"action":"reload_state"}\n\n',
    ])

    expect(retries).toEqual([2000])
    expect(comments).toEqual(['hb 2026-11-19T14:00:15-03:00'])
    expect(frames).toEqual([
      { event: 'br.easycode.wave.started.v1', data: '{"id":"a","sequence":128}', id: '128' },
      { event: 'br.easycode.cost.model_call_recorded.v1', data: '{"id":"t"}' },
      {
        event: 'easycode.gap',
        data: '{"request":40,"first_available":401,"action":"reload_state"}',
      },
    ])
  })

  it('joins a frame split across network chunks', () => {
    const { frames } = parse(['id: 7\nevent: x\nda', 'ta: {"a"', ':1}\n', '\n'])
    expect(frames).toEqual([{ event: 'x', data: '{"a":1}', id: '7' }])
  })

  it('accepts CRLF line ends and frames without event name', () => {
    const { frames } = parse(['data: hello\r\n\r\n'])
    expect(frames).toEqual([{ event: 'message', data: 'hello' }])
  })
})
