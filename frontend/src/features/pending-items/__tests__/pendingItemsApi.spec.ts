import { describe, expect, it } from 'vitest'
import { gateRoundOf, oldestFirst, waveOfQuestion, type PendingItem } from '../api/pendingItemsApi'

function item(id: string, openedAt: string): PendingItem {
  return {
    type: 'gate',
    project: { id: 'p1', name: 'P' },
    target: { type: 'gate_round', id },
    opened_at: openedAt,
  }
}

describe('pending items helpers (US-48)', () => {
  it('CA-1: orders the oldest first, whatever the offset of the dates', () => {
    const items = [
      item('b', '2026-11-22T10:00:00-03:00'),
      item('a', '2026-11-22T12:30:00Z'),
      item('c', '2026-11-22T08:00:00-03:00'),
    ]
    expect(oldestFirst(items).map((entry) => entry.target.id)).toEqual(['c', 'a', 'b'])
  })

  it('reads the gate round and the question wave from the target id', () => {
    expect(gateRoundOf('gate.requirements/2')).toEqual({ gateId: 'gate.requirements', round: 2 })
    expect(gateRoundOf('gate.requirements')).toBeUndefined()
    expect(waveOfQuestion('REQ-01/PERG-1')).toBe('REQ-01')
  })
})
