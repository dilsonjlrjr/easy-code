import { describe, expect, it } from 'vitest'
import type { StageSummary } from '../api/projectsApi'
import { gateModeRows, modeChangeTakesEffect, stageOfGate } from '../composables/gateApproval'

function stage(id: string, gate?: string): StageSummary {
  return {
    id,
    name: id,
    type: 'stage',
    agent: 'developer-agent',
    depends_on: [],
    state: null,
    gate: gate ?? null,
  } as StageSummary
}

const stages = [
  stage('requirements', 'gate.requirements'),
  stage('validation'),
  stage('implementation'),
]

describe('gateModeRows (US-07, ESP-29 3.2)', () => {
  it('orders the gates by the workflow stages and offers a control only with both modes', () => {
    const rows = gateModeRows(
      {
        'gate.release': { mode: 'human', allowed: ['human'], always_human: true },
        'gate.implementation': { mode: 'human', allowed: ['human', 'automatic'] },
        'gate.requirements': { mode: 'human', allowed: ['human'] },
        'gate.validation': { mode: 'automatic', allowed: ['human', 'automatic'] },
      },
      stages,
    )
    expect(rows.map((row) => row.gate)).toEqual([
      'gate.requirements',
      'gate.validation',
      'gate.implementation',
      'gate.release',
    ])
    expect(rows.map((row) => row.configurable)).toEqual([false, true, true, false])
    expect(rows[3]?.alwaysHuman).toBe(true)
    expect(rows[1]?.stage).toBe('validation')
  })

  it('never offers the automatic mode for a gate marked always human (CA-3)', () => {
    const rows = gateModeRows(
      { 'gate.release': { mode: 'human', allowed: ['human', 'automatic'], always_human: true } },
      stages,
    )
    expect(rows[0]?.configurable).toBe(false)
  })

  it('is empty without configuration', () => {
    expect(gateModeRows(undefined, stages)).toEqual([])
  })
})

describe('stageOfGate', () => {
  it('reads the workflow gate field first, then the naming convention', () => {
    expect(stageOfGate('gate.requirements', stages)).toBe('requirements')
    expect(stageOfGate('gate.implementation', stages)).toBe('implementation')
    expect(stageOfGate('gate.other', stages)).toBeUndefined()
  })
})

describe('modeChangeTakesEffect (US-07 RN-3, CA-5, CA-6)', () => {
  it('applies the stricter mode at once and the automatic one from the next round', () => {
    expect(modeChangeTakesEffect('human')).toBe('now')
    expect(modeChangeTakesEffect('automatic')).toBe('next_round')
  })
})
