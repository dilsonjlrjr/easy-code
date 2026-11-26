import { describe, expect, it } from 'vitest'
import type { CallRecord, Checkpoint } from '../api/wavesApi'
import {
  callCounts,
  filterTimeline,
  latestCheckpoint,
  latestEntries,
  TIMELINE_PAGE,
  waveTimeline,
} from '../composables/waveTimeline'

const ZERO = { input: 0, output: 0, read_cache: 0, write_cache: 0 }
const COST = {
  useful: { value: '0', currency: 'BRL' },
  lost: { value: '0', currency: 'BRL' },
  total: { value: '0', currency: 'BRL' },
}

function call(number: number, start: string, type: CallRecord['type'], attempt = 1): CallRecord {
  return { number, type, start, duration_ms: 1000, result: 'success', attempt }
}

function checkpoint(number: number, writtenAt: string, attempt = 1): Checkpoint {
  return {
    number,
    written_at: writtenAt,
    after: 'model_call',
    attempt,
    accumulated_tokens: ZERO,
    accumulated_cost: COST,
  }
}

describe('waveTimeline (ESP-18, PD-20)', () => {
  it('puts calls and checkpoints in one list, oldest first, the call before its checkpoint', () => {
    const entries = waveTimeline(
      [call(2, '2026-11-24T10:02:00Z', 'tool'), call(1, '2026-11-24T10:00:00Z', 'model')],
      [checkpoint(1, '2026-11-24T10:01:00Z'), checkpoint(2, '2026-11-24T10:02:00Z')],
    )
    expect(entries.map((entry) => entry.key)).toEqual([
      'call-1-1',
      'checkpoint-1-1',
      'call-1-2',
      'checkpoint-1-2',
    ])
  })

  it('keeps the earlier attempt before the restarted one', () => {
    const entries = waveTimeline(
      [call(1, '2026-11-24T09:00:00Z', 'model', 2), call(1, '2026-11-24T10:00:00Z', 'model', 1)],
      [],
    )
    expect(entries.map((entry) => entry.attempt)).toEqual([1, 2])
  })
})

describe('latestCheckpoint', () => {
  it('is undefined without checkpoints', () => {
    expect(latestCheckpoint([])).toBeUndefined()
  })

  it('picks the latest attempt, then the latest time', () => {
    const latest = latestCheckpoint([
      checkpoint(3, '2026-11-24T10:05:00Z', 1),
      checkpoint(1, '2026-11-24T10:06:00Z', 2),
      checkpoint(2, '2026-11-24T10:04:00Z', 1),
    ])
    expect(latest?.attempt).toBe(2)
    expect(latest?.number).toBe(1)
  })
})

describe('callCounts', () => {
  it('counts model and tool calls', () => {
    expect(
      callCounts([
        call(1, '2026-11-24T10:00:00Z', 'model'),
        call(2, '2026-11-24T10:01:00Z', 'tool'),
        call(3, '2026-11-24T10:02:00Z', 'tool'),
      ]),
    ).toEqual({ model: 1, tool: 2 })
  })
})

describe('activity window and filters (long waves)', () => {
  const calls = Array.from({ length: 25 }, (_, i) =>
    call(i + 1, `2026-11-24T10:${String(i).padStart(2, '0')}:00Z`, i % 2 === 0 ? 'model' : 'tool'),
  )
  calls[3] = { ...calls[3], result: 'refused', refusal_reason: 'path_not_allowed' } as CallRecord
  const entries = waveTimeline(calls, [checkpoint(1, '2026-11-24T10:00:30Z')])

  it('shows only the latest entries and counts the hidden ones', () => {
    const { shown, hidden } = latestEntries(entries, TIMELINE_PAGE)
    expect(shown).toHaveLength(TIMELINE_PAGE)
    expect(hidden).toBe(entries.length - TIMELINE_PAGE)
    expect(shown[shown.length - 1]).toBe(entries[entries.length - 1])
    expect(latestEntries(entries, undefined).hidden).toBe(0)
  })

  it('filters by type and by problem; checkpoints only in "all"', () => {
    expect(filterTimeline(entries, 'all')).toHaveLength(26)
    expect(
      filterTimeline(entries, 'model').every((e) => e.kind === 'call' && e.call.type === 'model'),
    ).toBe(true)
    expect(filterTimeline(entries, 'tool')).toHaveLength(12)
    const problems = filterTimeline(entries, 'problem')
    expect(problems).toHaveLength(1)
    expect(problems[0]?.kind === 'call' && problems[0].call.number).toBe(4)
  })
})
