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
        { id: 'VISAO', type: 'VISAO', exists: true },
      ],
      edges: [
        { from_state: 'T-02', to_state: 'US-03', rule: 'RAS-08' },
        { from_state: 'CT-07', to_state: 'US-03', rule: 'RAS-20' },
        { from_state: 'CT-99', to_state: 'US-03', rule: 'RAS-01' },
      ],
    })
    expect(groups.map((group) => group.group)).toEqual(['T', 'CT', 'commit', 'other'])
    const cts = groups.find((group) => group.group === 'CT')?.nodes ?? []
    expect(cts.map((node) => [node.id, node.exists, node.rules])).toEqual([
      ['CT-07', true, ['RAS-20']],
      ['CT-99', false, ['RAS-01']],
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
