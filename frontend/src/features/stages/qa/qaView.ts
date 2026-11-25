import type { ArtifactSummary } from '@/features/artifacts/api/artifactsApi'
import type { RelQaHeader } from '@/features/artifacts/composables/structuredArtifact'
import type { CoverageMatrix, CtResult } from '@/features/traceability/api/traceabilityApi'

/**
 * Pure reading of the QA stage (ESP-15 3.1, 3.3, 3.6; canvas EtapaQA). The numbers and the matrix
 * come from the newest REL-QA (ER-15-9: the screen computes no result of its own); before QA writes
 * one, the rows come from `getCoverageMatrix` and are marked partial. Must criteria without a passing
 * scenario are gaps shown at the top, never omitted (ER-15-4).
 */

export type MatrixResult = 'passou' | 'falhou' | 'bloqueado' | 'nao_executado'

export interface MatrixRow {
  us: string
  criterion: string
  ct: string
  /** `REL-QA.matriz[].nivel`; the coverage matrix does not carry it. */
  level?: string
  result: MatrixResult
  finding?: string
}

export interface CoverageGap {
  us: string
  criterion: string
  /** `REL-QA.sem_cobertura[].motivo`, when the report states it. */
  reason?: string
}

const COVERAGE_RESULTS: Record<CtResult, MatrixResult> = {
  passed: 'passou',
  failed: 'falhou',
  not_executed: 'nao_executado',
}

const RESULTS: readonly string[] = ['passou', 'falhou', 'bloqueado', 'nao_executado']

/** The newest REL-QA of the stage (ESP-15 3.1): last updated, then the highest version. */
export function latestReport(artifacts: ArtifactSummary[]): ArtifactSummary | undefined {
  return [...artifacts]
    .filter((artifact) => artifact.type === 'REL-QA')
    .sort(
      (left, right) =>
        (right.updated_at ?? '').localeCompare(left.updated_at ?? '') ||
        right.version - left.version ||
        right.id.localeCompare(left.id),
    )[0]
}

export function matrixRows(
  report: RelQaHeader | undefined,
  coverage: CoverageMatrix | undefined,
): { rows: MatrixRow[]; source: 'report' | 'coverage' } {
  if (report) {
    return {
      source: 'report',
      rows: report.matriz.map((row) => ({
        us: row.us,
        criterion: row.criterio,
        ct: row.ct,
        level: row.nivel,
        result: (RESULTS.includes(row.resultado) ? row.resultado : 'nao_executado') as MatrixResult,
        ...(row.achado ? { finding: row.achado } : {}),
      })),
    }
  }
  return {
    source: 'coverage',
    rows: (coverage?.items ?? []).flatMap((item) =>
      item.criteria.flatMap((criterion) =>
        criterion.cts.map((ct) => ({
          us: item.us,
          criterion: criterion.ca,
          ct: ct.ct,
          result: COVERAGE_RESULTS[ct.result],
        })),
      ),
    ),
  }
}

/** `US-03/CA-2`, `CA-2` and `ca-2` name the same criterion. */
function gapKey(us: string, criterion: string): string {
  const ca = criterion.split('/').pop() ?? criterion
  return `${us}/${ca.trim().toUpperCase()}`
}

/**
 * ESP-15 3.3 "Sem cobertura": the report's `sem_cobertura` with its reason, plus every criterion of a
 * Must US the coverage matrix marks as a gap (no passing scenario), once each.
 */
export function coverageGaps(
  report: RelQaHeader | undefined,
  coverage: CoverageMatrix | undefined,
): CoverageGap[] {
  const gaps = new Map<string, CoverageGap>()
  for (const gap of report?.sem_cobertura ?? []) {
    gaps.set(gapKey(gap.us, gap.criterio), {
      us: gap.us,
      criterion: gap.criterio,
      reason: gap.motivo,
    })
  }
  for (const item of coverage?.items ?? []) {
    if (item.priority !== 'must' || item.status !== 'gap') continue
    for (const criterion of item.criteria) {
      if (criterion.cts.some((ct) => ct.result === 'passed')) continue
      const key = gapKey(item.us, criterion.ca)
      if (!gaps.has(key)) gaps.set(key, { us: item.us, criterion: criterion.ca })
    }
  }
  return [...gaps.values()]
}

export interface MatrixTotals {
  passed: number
  failing: number
  /** Rows that passed after a finding of the cycle ("passou após ACH-NN"). */
  fixed: number
}

/** The header chips: from the report summary when there is one; else counted from the rows. */
export function matrixTotals(report: RelQaHeader | undefined, rows: MatrixRow[]): MatrixTotals {
  const fixed = rows.filter((row) => row.result === 'passou' && row.finding).length
  if (report) return { passed: report.resumo.passaram, failing: report.resumo.falharam, fixed }
  return {
    passed: rows.filter((row) => row.result === 'passou').length,
    failing: rows.filter((row) => row.result === 'falhou').length,
    fixed,
  }
}

/** ESP-15 3.6: the scenario count and range (`CT-01` to `CT-23`), in id order. */
export function scenarioRange(artifacts: ArtifactSummary[]): {
  count: number
  first?: string
  last?: string
} {
  const ids = artifacts
    .filter((artifact) => artifact.type === 'CT')
    .map((artifact) => artifact.id)
    .sort((left, right) => left.localeCompare(right, undefined, { numeric: true }))
  return { count: ids.length, first: ids[0], last: ids[ids.length - 1] }
}

/**
 * ESP-15 3.6 (PL-63): the test plan is an attachment of the CT versions (workflow-padrao
 * `docs/qa/plano.md`); its path comes from `Artifact.attachments`, never from a constant.
 */
export function planAttachment(
  attachments: Array<{ path: string }> | undefined,
): string | undefined {
  const items = attachments ?? []
  return (
    items.find((item) => /(^|\/)docs\/qa\/plano\.md$/.test(item.path)) ??
    items.find((item) => /(^|\/)plano\.md$/.test(item.path))
  )?.path
}

/** ESP-15 3.4: the executions of each QA wave; a wave that fails to load keeps its own error. */
export function waveExecutions<T>(
  waves: string[],
  settled: Array<PromiseSettledResult<T[]>>,
): {
  items: Array<{ wave: string; position: number; execution: T }>
  failures: Array<{ wave: string; error: unknown }>
} {
  const items: Array<{ wave: string; position: number; execution: T }> = []
  const failures: Array<{ wave: string; error: unknown }> = []
  waves.forEach((wave, index) => {
    const result = settled[index]
    if (!result) return
    if (result.status === 'rejected') {
      failures.push({ wave, error: result.reason })
      return
    }
    result.value.forEach((execution, position) =>
      items.push({ wave, position: position + 1, execution }),
    )
  })
  return { items, failures }
}
