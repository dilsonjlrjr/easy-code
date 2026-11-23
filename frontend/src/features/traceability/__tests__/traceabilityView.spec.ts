import { describe, expect, it } from 'vitest'
import { coverageRows, groupNodes, qaNotExecuted } from '../composables/traceabilityView'

describe('traceability of an artifact (ESP-24 3.1, US-28 CA-1, CA-3)', () => {
  it('groups the linked nodes by type, skips the root and keeps the rules of each node', () => {
    const groups = groupNodes({
      root: 'US-03',
      nodes: [
        { id: 'US-03', type: 'US', exists: true },
        { id: 'T-02', type: 'T', exists: true },
        { id: 'CT-07', type: 'CT', exists: true },
        { id: 'CT-99', type: 'CT', exists: false },
        { id: 'a1b2c3d', type: 'commit', exists: true },
        { id: 'reserve.spec.ts::reserves', type: 'test', exists: true },
        { id: 'VISAO', type: 'VISAO', exists: true },
      ],
      edges: [
        { from: 'T-02', to: 'US-03', rule: 'E-20' },
        { from: 'CT-07', to: 'US-03', rule: 'E-30' },
        { from: 'CT-99', to: 'US-03', rule: 'E-31' },
      ],
    })
    expect(groups.map((group) => group.group)).toEqual(['T', 'CT', 'test', 'commit', 'other'])
    const cts = groups.find((group) => group.group === 'CT')?.nodes ?? []
    expect(cts.map((node) => [node.id, node.exists, node.rules])).toEqual([
      ['CT-07', true, ['E-30']],
      ['CT-99', false, ['E-31']],
    ])
  })
})

describe('coverage matrix (ESP-24 3.2, US-28 CA-2)', () => {
  const matrix = {
    items: [
      { us: 'US-10', priority: 'must' as const, status: 'covered' as const, criteria: [] },
      {
        us: 'US-02',
        priority: 'must' as const,
        status: 'gap' as const,
        criteria: [{ ca: 'CA-1', cts: [{ ct: 'CT-01', result: 'not_executed' as const }] }],
      },
      { us: 'US-03', priority: 'must' as const, status: 'covered' as const, criteria: [] },
    ],
  }

  it('shows gaps first, then by US id (numeric)', () => {
    expect(coverageRows(matrix).map((row) => row.us)).toEqual(['US-02', 'US-03', 'US-10'])
  })

  it('says when QA has not executed any scenario', () => {
    expect(qaNotExecuted(matrix)).toBe(true)
    expect(qaNotExecuted({ items: [] })).toBe(false)
  })
})
