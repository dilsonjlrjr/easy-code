import { describe, expect, it } from 'vitest'
import type { WaveSummary } from '@/features/projects/api/projectsApi'
import type { FindingSummary } from '@/features/findings/api/findingsApi'
import type { SandboxExecution } from '@/features/waves/api/wavesApi'
import { checkSituations, latestDeveloperWave, reworkItems } from '../implementationView'

function wave(id: string, overrides: Partial<WaveSummary> = {}): WaveSummary {
  return {
    id,
    stage: 'implementation',
    agent: 'developer-agent',
    state: 'completed',
    purpose: 'execution',
    attempt: 1,
    created_at: '2026-11-22T10:00:00Z',
    ...overrides,
  }
}

function execution(id: string, overrides: Partial<SandboxExecution> = {}): SandboxExecution {
  return {
    id,
    command: 'go build ./...',
    category: 'build',
    start: '2026-11-22T10:00:00Z',
    duration_ms: 1000,
    exit_code: 0,
    end_reason: 'completed',
    output_bytes: 0,
    ...overrides,
  }
}

function finding(id: string, overrides: Partial<FindingSummary> = {}): FindingSummary {
  return {
    id,
    title: id,
    severity: 'high',
    category: 'defect',
    status: 'in_rework',
    destination_stage: 'implementation',
    author: 'qa-agent',
    blocking: true,
    cycles: 1,
    ...overrides,
  }
}

describe('Implementation stage view (ESP-14)', () => {
  it('takes the newest Developer wave that already left the queue', () => {
    expect(
      latestDeveloperWave([
        wave('DEV-01', { created_at: '2026-11-22T10:00:00Z' }),
        wave('DEV-02', { created_at: '2026-11-22T11:00:00Z' }),
        wave('DEV-03', { created_at: '2026-11-22T12:00:00Z', state: 'queued' }),
        wave('QA-01', { created_at: '2026-11-22T13:00:00Z', agent: 'qa-agent' }),
      ])?.id,
    ).toBe('DEV-02')
    expect(latestDeveloperWave([])).toBeUndefined()
  })

  it('says build, tests and lint by the last command of each category (US-41)', () => {
    const situations = checkSituations([
      execution('e1', { category: 'build', exit_code: 2, start: '2026-11-22T10:00:00Z' }),
      execution('e2', { category: 'build', start: '2026-11-22T10:05:00Z' }),
      execution('e3', {
        category: 'tests',
        end_reason: 'time_limit',
        exit_code: null,
        tests: { total: 3, passed: 1, failed: 0 },
      }),
    ])
    expect(situations.map((item) => [item.category, item.outcome])).toEqual([
      ['build', 'succeeded'],
      ['tests', 'limit'],
      ['lint', 'not_run'],
    ])
    expect(situations[0]?.execution?.id).toBe('e2')
  })

  it('lists each finding sent to the Implementation with its rework and revalidation waves', () => {
    const items = reworkItems(
      [finding('ACH-03'), finding('ACH-07', { status: 'escalated', cycles: 3 })],
      [
        wave('QA-02', {
          purpose: 'revalidation',
          finding: 'ACH-03',
          agent: 'qa-agent',
          created_at: '2026-11-22T12:00:00Z',
        }),
        wave('DEV-11', {
          purpose: 'rework',
          finding: 'ACH-03',
          created_at: '2026-11-22T11:00:00Z',
        }),
        wave('DEV-12', { purpose: 'execution', finding: 'ACH-03' }),
      ],
    )
    expect(items.map((item) => [item.finding.id, item.waves.map((target) => target.id)])).toEqual([
      ['ACH-03', ['DEV-11', 'QA-02']],
      ['ACH-07', []],
    ])
  })
})
