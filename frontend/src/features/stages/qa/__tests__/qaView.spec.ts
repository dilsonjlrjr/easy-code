import { describe, expect, it } from 'vitest'
import type { ArtifactSummary } from '@/features/artifacts/api/artifactsApi'
import type { RelQaHeader } from '@/features/artifacts/composables/structuredArtifact'
import type { CoverageMatrix } from '@/features/traceability/api/traceabilityApi'
import {
  coverageGaps,
  latestReport,
  matrixRows,
  matrixTotals,
  planAttachment,
  scenarioRange,
  waveExecutions,
} from '../qaView'

function summary(
  id: string,
  type: string,
  overrides: Partial<ArtifactSummary> = {},
): ArtifactSummary {
  return {
    id,
    type,
    title: id,
    state: 'approved',
    version: 1,
    stage: 'qa',
    author: { type: 'agent', agent: 'qa-agent' },
    wave: 'QA-01',
    ...overrides,
  }
}

const report: RelQaHeader = {
  execucao: 1,
  resumo: {
    ct_executados: 3,
    passaram: 2,
    falharam: 1,
    bloqueados: 0,
    ca_must_cobertos: 1,
    ca_must_total: 2,
  },
  matriz: [
    { us: 'US-01', criterio: 'CA-1', ct: 'CT-01', nivel: 'e2e', resultado: 'passou' },
    {
      us: 'US-01',
      criterio: 'CA-2',
      ct: 'CT-02',
      nivel: 'integracao',
      resultado: 'passou',
      achado: 'ACH-03',
    },
    {
      us: 'US-02',
      criterio: 'CA-1',
      ct: 'CT-03',
      nivel: 'unitario',
      resultado: 'falhou',
      achado: 'ACH-04',
    },
  ],
  sem_cobertura: [{ us: 'US-02', criterio: 'CA-2', motivo: 'sem cenário' }],
  comandos: [],
  achados_abertos: ['ACH-04'],
  veredito: 'reprovado',
}

const coverage: CoverageMatrix = {
  items: [
    {
      us: 'US-02',
      priority: 'must',
      status: 'gap',
      criteria: [
        { ca: 'CA-1', cts: [{ ct: 'CT-03', result: 'failed' }] },
        { ca: 'CA-2', cts: [] },
      ],
    },
    {
      us: 'US-05',
      priority: 'should',
      status: 'gap',
      criteria: [{ ca: 'CA-1', cts: [{ ct: 'CT-09', result: 'not_executed' }] }],
    },
  ],
}

describe('QA stage view (ESP-15)', () => {
  it('takes the newest REL-QA of the stage', () => {
    expect(
      latestReport([
        summary('REL-QA-01', 'REL-QA', { updated_at: '2026-11-22T10:00:00Z' }),
        summary('REL-QA-02', 'REL-QA', { updated_at: '2026-11-23T10:00:00Z' }),
        summary('CT-01', 'CT', { updated_at: '2026-11-24T10:00:00Z' }),
      ])?.id,
    ).toBe('REL-QA-02')
  })

  it('reads the matrix from the report, with level and finding (ER-15-9)', () => {
    const { rows, source } = matrixRows(report, coverage)
    expect(source).toBe('report')
    expect(rows[1]).toEqual({
      us: 'US-01',
      criterion: 'CA-2',
      ct: 'CT-02',
      level: 'integracao',
      result: 'passou',
      finding: 'ACH-03',
    })
    expect(matrixTotals(report, rows)).toEqual({ passed: 2, failing: 1, fixed: 1 })
  })

  it('falls back to the coverage matrix before QA writes the report', () => {
    const { rows, source } = matrixRows(undefined, coverage)
    expect(source).toBe('coverage')
    expect(rows.map((row) => [row.us, row.criterion, row.ct, row.result])).toEqual([
      ['US-02', 'CA-1', 'CT-03', 'falhou'],
      ['US-05', 'CA-1', 'CT-09', 'nao_executado'],
    ])
    expect(matrixTotals(undefined, rows)).toEqual({ passed: 0, failing: 1, fixed: 0 })
  })

  it('shows each Must criterion without a passing scenario once, with the reported reason (ER-15-4)', () => {
    expect(coverageGaps(report, coverage)).toEqual([
      { us: 'US-02', criterion: 'CA-2', reason: 'sem cenário' },
      { us: 'US-02', criterion: 'CA-1' },
    ])
    expect(coverageGaps(undefined, { items: [] })).toEqual([])
  })

  it('counts the plan scenarios in id order', () => {
    expect(
      scenarioRange([summary('CT-10', 'CT'), summary('CT-02', 'CT'), summary('US-01', 'US')]),
    ).toEqual({ count: 2, first: 'CT-02', last: 'CT-10' })
    expect(scenarioRange([])).toEqual({ count: 0, first: undefined, last: undefined })
  })
})

describe('planAttachment (PL-63)', () => {
  it('reads the plan path from the CT attachments', () => {
    expect(planAttachment([{ path: 'CT-01.md' }, { path: 'docs/qa/plano.md' }])).toBe(
      'docs/qa/plano.md',
    )
    expect(planAttachment([{ path: '../plano.md' }])).toBe('../plano.md')
    expect(planAttachment([{ path: 'outro.md' }])).toBeUndefined()
    expect(planAttachment(undefined)).toBeUndefined()
  })
})

describe('waveExecutions (ESP-15 3.4)', () => {
  it('keeps the waves that loaded and the error of the one that failed', () => {
    const failure = new Error('gone')
    const result = waveExecutions<string>(
      ['W-2', 'W-1', 'W-0'],
      [
        { status: 'fulfilled', value: ['a', 'b'] },
        { status: 'rejected', reason: failure },
        { status: 'fulfilled', value: ['c'] },
      ],
    )
    expect(result.items).toEqual([
      { wave: 'W-2', position: 1, execution: 'a' },
      { wave: 'W-2', position: 2, execution: 'b' },
      { wave: 'W-0', position: 1, execution: 'c' },
    ])
    expect(result.failures).toEqual([{ wave: 'W-1', error: failure }])
  })
})
