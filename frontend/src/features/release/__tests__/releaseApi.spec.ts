import { describe, expect, it } from 'vitest'
import type { FindingSummary } from '@/features/findings/api/findingsApi'
import { acceptanceExpired, splitOpenFindings } from '../api/releaseApi'

function aFinding(id: string, severity: FindingSummary['severity']): FindingSummary {
  return {
    id,
    title: id,
    severity,
    category: 'vulnerabilidade',
    status: 'open',
    destination_stage: 'implementation',
    author: 'security-agent',
    blocking: true,
  }
}

describe('release consolidation (ESP-17 3.4)', () => {
  it('ER-17-4: Critical and High block; Medium and Low are listed apart', () => {
    const split = splitOpenFindings([
      aFinding('ACH-01', 'critical'),
      aFinding('ACH-02', 'medium'),
      aFinding('ACH-03', 'high'),
      aFinding('ACH-04', 'low'),
    ])
    expect(split.blocking.map((item) => item.id)).toEqual(['ACH-01', 'ACH-03'])
    expect(split.other.map((item) => item.id)).toEqual(['ACH-02', 'ACH-04'])
  })

  it('ER-17-6: an acceptance is expired after its valid_until; none means no expiry', () => {
    const acceptance = {
      per: { id: 'ana', name: 'Ana' },
      when: '2026-10-01T10:00:00-03:00',
      justification: 'x',
    }
    expect(acceptanceExpired({ ...acceptance, valid_until: '2026-11-01' }, '2026-11-22')).toBe(true)
    expect(acceptanceExpired({ ...acceptance, valid_until: '2026-11-22' }, '2026-11-22')).toBe(
      false,
    )
    expect(acceptanceExpired({ ...acceptance, valid_until: null }, '2026-11-22')).toBe(false)
  })
})
