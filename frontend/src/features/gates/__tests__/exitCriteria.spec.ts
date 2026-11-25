import { describe, expect, it } from 'vitest'
import type { GateCheck } from '@/features/projects/api/projectsApi'
import type { CheckResult } from '../api/gatesApi'
import { criteriaCounts, criterionMark, criterionStatus, exitCriteria } from '../exitCriteria'

const gateChecks: GateCheck[] = [
  {
    check: 'artifact.schema_valid',
    name: 'Esquema válido',
    type: 'automatic',
    failure_severity: 'blocks',
  },
  { check: 'us.vague', name: 'Sem termo vago', type: 'automatic', failure_severity: 'alert' },
]

function result(
  check: string,
  status: CheckResult['status'],
  severity: CheckResult['failure_severity'],
): CheckResult {
  return { check, name: check, type: 'automatic', failure_severity: severity, status }
}

describe('exitCriteria (PC-20, PL-46)', () => {
  it('lists every gate check as not evaluated before any round, with no preview', () => {
    const criteria = exitCriteria(gateChecks, undefined)
    expect(criteria.map((item) => [item.name, item.severity, item.status])).toEqual([
      ['Esquema válido', 'blocks', 'not_evaluated'],
      ['Sem termo vago', 'alert', 'not_evaluated'],
    ])
    expect(
      exitCriteria(gateChecks, { checks: [] }).every((item) => item.status === 'not_evaluated'),
    ).toBe(true)
    expect(exitCriteria(undefined, undefined)).toEqual([])
  })

  it('shows the result of the round in text: passed, failed (blocks), alert, error and to confirm', () => {
    const criteria = exitCriteria(gateChecks, {
      checks: [
        result('a', 'passed', 'blocks'),
        result('b', 'failed', 'blocks'),
        result('c', 'failed', 'alert'),
        result('d', 'error', 'blocks'),
        { ...result('e', 'pending', 'blocks'), type: 'human' },
      ],
    })
    expect(criteria.map((item) => item.status)).toEqual([
      'passed',
      'failed',
      'alert',
      'error',
      'to_confirm',
    ])
    expect(criteriaCounts(criteria)).toEqual({ passed: 1, total: 5, failed: 1 })
    expect(criterionStatus({ status: 'failed', failure_severity: 'alert' })).toBe('alert')
  })

  it('pairs every status with a marker that never carries the meaning alone', () => {
    expect(criterionMark('passed')).toEqual({ symbol: '✓', tone: 'ok' })
    expect(criterionMark('failed')).toEqual({ symbol: '✕', tone: 'crit' })
    expect(criterionMark('not_evaluated').tone).toBe('mute')
  })
})
