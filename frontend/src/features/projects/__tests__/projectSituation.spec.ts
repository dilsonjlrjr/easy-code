import { describe, expect, it } from 'vitest'
import { aProject } from '@/test/factories'
import type { ProjectSummary } from '../api/projectsApi'
import {
  budgetUsed,
  criticalOrHigh,
  openFindings,
  runningAgents,
  stateFilterFrom,
} from '../composables/projectSituation'

const BRL = (value: string) => ({ value, currency: 'BRL' })

function summary(overrides: Partial<ProjectSummary> = {}): ProjectSummary {
  return {
    approved_stages: 1,
    stages_total: 9,
    gates_awaiting: 0,
    open_questions: 0,
    accumulated_cost: BRL('40'),
    ...overrides,
  }
}

describe('dashboard situation (US-50, ESP-04, ESP-03)', () => {
  it('reads the state filter from the address (CA-04-2)', () => {
    expect(stateFilterFrom({ state: 'paused' })).toBe('paused')
    expect(stateFilterFrom({ state: 'unknown' })).toBe('all')
    expect(stateFilterFrom({})).toBe('all')
  })

  it('lists open findings by severity, only those with findings (CA-04-1)', () => {
    expect(openFindings(summary({ open_findings: { low: 2, critical: 1, high: 0 } }))).toEqual([
      ['critical', 1],
      ['low', 2],
    ])
    expect(openFindings(undefined)).toEqual([])
  })

  it('gives the share of the stop limit spent (PL-16)', () => {
    expect(budgetUsed(summary({ stop_limit: BRL('200') }))).toBe(20)
    expect(budgetUsed(summary({ stop_limit: null }))).toBeUndefined()
  })

  it('adds critical and high findings and lists non-final waves (ESP-03 3)', () => {
    const projects = [
      aProject({
        id: 'p1',
        name: 'One',
        state: 'in_progress',
        summary: summary({
          open_findings: { critical: 1, high: 2, medium: 5 },
          active_waves: [
            { wave: 'REQ-01', agent: 'requirements-agent', state: 'running' },
            { wave: 'REQ-00', agent: 'requirements-agent', state: 'completed' },
          ],
        }),
      }),
      aProject({ id: 'p2', name: 'Two', summary: summary({ open_findings: { high: 1 } }) }),
    ]
    expect(criticalOrHigh(projects)).toBe(4)
    expect(runningAgents(projects)).toEqual([
      {
        project: { id: 'p1', name: 'One' },
        wave: 'REQ-01',
        agent: 'requirements-agent',
        state: 'running',
      },
    ])
  })
})
