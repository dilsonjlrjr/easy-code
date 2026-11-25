import { describe, expect, it } from 'vitest'
import {
  approvedRounds,
  attachmentKind,
  dependents,
  roundCarries,
  traceChain,
} from '../composables/artifactLinks'
import type { GateRound, GateRoundSummary } from '@/features/gates/api/gatesApi'

describe('artifactLinks (ESP-21)', () => {
  it('opens Markdown sanitized, text as text and the prototype only through the content origin', () => {
    expect(attachmentKind('text/markdown')).toBe('markdown')
    expect(attachmentKind('application/yaml')).toBe('text')
    expect(attachmentKind('text/html')).toBe('content-origin')
    expect(attachmentKind('image/png')).toBe('content-origin')
  })

  it('orders the chain from requirement to test and groups the dependents by type', () => {
    const graph = {
      root: 'ADR-01',
      nodes: [
        { id: 'CT-01', type: 'CT', exists: true },
        { id: 'ADR-01', type: 'ADR', exists: true },
        { id: 'US-01', type: 'US', exists: true },
        { id: 'T-02', type: 'T', exists: true },
        { id: 'T-01', type: 'T', exists: false },
      ],
      edges: [],
    }
    expect(traceChain(graph).map((node) => node.id)).toEqual([
      'US-01',
      'ADR-01',
      'T-01',
      'T-02',
      'CT-01',
    ])
    expect(dependents(graph).map((group) => [group.type, group.count])).toEqual([
      ['US', 1],
      ['T', 2],
      ['CT', 1],
    ])
  })

  it('finds the round that approved the version', () => {
    const summary = (round: number, result?: 'approved' | 'rejected') =>
      ({
        gate: 'gate.architecture',
        stage: 'architecture',
        round,
        state: 'approved',
        opened_at: '',
        decision: result
          ? { result, record: { per: { type: 'human' }, when: '', origin: 'web' } }
          : null,
      }) as GateRoundSummary
    expect(
      approvedRounds([summary(1, 'rejected'), summary(2, 'approved'), summary(3, 'approved')]).map(
        (r) => r.round,
      ),
    ).toEqual([3, 2])
    const round = { outputs: [{ artifact: 'ARQ', version: 2 }] } as GateRound
    expect(roundCarries(round, 'ARQ', 2)).toBe(true)
    expect(roundCarries(round, 'ARQ', 1)).toBe(false)
  })
})
