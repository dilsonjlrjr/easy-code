import type { GateCheck } from '@/features/projects/api/projectsApi'
import type { CheckResult, GateRound } from './api/gatesApi'

/**
 * Situation of an exit criterion in text (ESP-07 to ESP-16 "Critério de saída"): passed, failed (blocks),
 * alert, error (being redone), to be confirmed by the approver, or not evaluated before any round.
 */
export type CriterionStatus =
  'passed' | 'failed' | 'alert' | 'error' | 'to_confirm' | 'not_evaluated'

export interface ExitCriterion {
  check: string
  name: string
  type: 'automatic' | 'human'
  severity: 'blocks' | 'alert'
  status: CriterionStatus
}

/** The situation of a round check: a failed alert is an alert, a pending human check waits the approver. */
export function criterionStatus(
  check: Pick<CheckResult, 'status' | 'failure_severity'>,
): CriterionStatus {
  switch (check.status) {
    case 'passed':
      return 'passed'
    case 'failed':
      return check.failure_severity === 'alert' ? 'alert' : 'failed'
    case 'error':
      return 'error'
    default:
      return 'to_confirm'
  }
}

/**
 * PC-20, PL-46: the exit criteria of a stage are the checks of its gate. With a round they carry the
 * result of that round (the latest one, decided or not); before any round, or while the round has not
 * evaluated yet, every check of the gate reads "not evaluated". There is no preview before the round.
 */
export function exitCriteria(
  gateChecks: readonly GateCheck[] | undefined,
  round: Pick<GateRound, 'checks'> | undefined,
): ExitCriterion[] {
  if (round && round.checks.length > 0) {
    return round.checks.map((check) => ({
      check: check.check,
      name: check.name,
      type: check.type,
      severity: check.failure_severity,
      status: criterionStatus(check),
    }))
  }
  return (gateChecks ?? []).map((check) => ({
    check: check.check,
    name: check.name,
    type: check.type,
    severity: check.failure_severity,
    status: 'not_evaluated',
  }))
}

/** The marker of a criterion beside its text (never the only carrier of the meaning). */
export function criterionMark(status: CriterionStatus): { symbol: string; tone: string } {
  switch (status) {
    case 'passed':
      return { symbol: '✓', tone: 'ok' }
    case 'failed':
      return { symbol: '✕', tone: 'crit' }
    case 'alert':
    case 'error':
      return { symbol: '!', tone: 'warn' }
    case 'to_confirm':
      return { symbol: '…', tone: 'info' }
    default:
      return { symbol: '–', tone: 'mute' }
  }
}

/** Counts of the strip: how many criteria passed out of all, and how many block the gate now. */
export function criteriaCounts(criteria: readonly ExitCriterion[]): {
  passed: number
  total: number
  failed: number
} {
  return {
    passed: criteria.filter((item) => item.status === 'passed').length,
    total: criteria.length,
    failed: criteria.filter((item) => item.status === 'failed').length,
  }
}
