import type { LocationQuery } from 'vue-router'
import type { StageId } from '@/shared/api/contract'
import {
  FINDING_STATUSES,
  SEVERITIES,
  type FindingDecision,
  type FindingFilters,
  type FindingStatus,
  type FindingSummary,
  type Severity,
} from '../api/findingsApi'

/**
 * Display rules of the findings (ESP-30, ESP-20): status shortcuts, the blocking text and the query
 * that keeps the filters in the address. Everything the screen derives from a finding lives here, so
 * it is tested without mounting the pages.
 */

/** ESP-30 3.1: shortcuts of the status filter; none shows a count (PL-72). */
export const STATUS_SHORTCUTS = {
  open: ['open', 'in_rework', 'revalidating', 'escalated', 'awaiting_scope'],
  escalated: ['escalated'],
  ended: ['closed', 'accepted', 'discarded'],
  all: [],
} as const satisfies Record<string, readonly FindingStatus[]>

export type StatusShortcut = keyof typeof STATUS_SHORTCUTS

export const DEFAULT_SHORTCUT: StatusShortcut = 'open'

/** ER-20-10: the statuses in which a critical or high finding blocks. */
export const ACTIVE_STATUSES: readonly FindingStatus[] = STATUS_SHORTCUTS.open

/** Decisions the finding screen offers (ESP-20 section 4), in the order they are shown. */
export const DECISION_ACTIONS = [
  'authorize_new_rework',
  'accept_risk',
  'decide_scope_change',
  'discard',
] as const satisfies ReadonlyArray<FindingDecision['action']>

/** Decisions that are critical (ADR-19): web only, anti-CSRF and a recent SSO authentication. */
export const STEP_UP_ACTIONS: ReadonlyArray<FindingDecision['action']> = [
  'authorize_new_rework',
  'accept_risk',
  'decide_scope_change',
]

export const JUSTIFICATION_MIN = 10
export const JUSTIFICATION_MAX = 10000

export interface FindingQuery {
  shortcut: StatusShortcut | undefined
  status: FindingStatus[]
  severity: Severity[]
  destination: StageId | undefined
}

function many(value: LocationQuery[string] | undefined): string[] {
  if (value === undefined || value === null) return []
  return (Array.isArray(value) ? value : [value]).filter(
    (item): item is string => typeof item === 'string',
  )
}

function sameSet(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((item) => right.includes(item))
}

/** The filters in the address (CA-30-3); anything unknown falls back to the default (ER-30-12). */
export function readFindingQuery(query: LocationQuery, stages: readonly string[]): FindingQuery {
  const rawStatus = many(query.status)
  const status = rawStatus.filter((item): item is FindingStatus =>
    (FINDING_STATUSES as readonly string[]).includes(item),
  )
  const severity = many(query.severity).filter((item): item is Severity =>
    (SEVERITIES as readonly string[]).includes(item),
  )
  const [destinationValue] = many(query.destination_stage)
  const destination =
    destinationValue && stages.includes(destinationValue)
      ? (destinationValue as StageId)
      : undefined
  const all = many(query.status_filter)[0] === 'all'
  if (all) return { shortcut: 'all', status: [], severity, destination }
  if (rawStatus.length === 0 || status.length !== rawStatus.length) {
    return { shortcut: DEFAULT_SHORTCUT, status: [...STATUS_SHORTCUTS.open], severity, destination }
  }
  return { shortcut: shortcutOf(status), status, severity, destination }
}

export function shortcutOf(status: readonly FindingStatus[]): StatusShortcut | undefined {
  if (status.length === 0) return 'all'
  return (Object.keys(STATUS_SHORTCUTS) as StatusShortcut[]).find(
    (key) => key !== 'all' && sameSet(STATUS_SHORTCUTS[key], status),
  )
}

/** The address of a filter choice; "all" is explicit, so it is not mistaken for the default. */
export function writeFindingQuery(filters: FindingQuery): Record<string, string | string[]> {
  const query: Record<string, string | string[]> = {}
  if (filters.status.length === 0) query.status_filter = 'all'
  else query.status = [...filters.status]
  if (filters.severity.length > 0) query.severity = [...filters.severity]
  if (filters.destination) query.destination_stage = filters.destination
  return query
}

export function apiFilters(filters: FindingQuery): FindingFilters {
  return {
    status: filters.status.length ? filters.status : undefined,
    severity: filters.severity.length ? filters.severity : undefined,
    destination_stage: filters.destination,
  }
}

/** i18n key and values of what a finding blocks (ER-30-4, ER-30-8, ER-20-10). */
export function blockingText(
  finding: FindingSummary,
): { key: string; stage?: StageId } | undefined {
  if (finding.status === 'accepted' && finding.acceptance_expired) {
    return { key: 'findings.blocking.expired' }
  }
  if (!finding.blocking) return undefined
  return finding.origin_stage
    ? { key: 'findings.blocking.gateAndRelease', stage: finding.origin_stage }
    : { key: 'findings.blocking.release' }
}

/** i18n key of the status label; an expired acceptance reads "Aceito (vencido)" (PL-68). */
export function statusKey(finding: Pick<FindingSummary, 'status' | 'acceptance_expired'>): string {
  return finding.status === 'accepted' && finding.acceptance_expired
    ? 'findings.status.accepted_expired'
    : `findings.status.${finding.status}`
}

/** ER-30-7 / ER-20-11: rework of the Implementation is an internal cycle and reopens nothing. */
export function isInternalCycle(finding: Pick<FindingSummary, 'destination_stage'>): boolean {
  return finding.destination_stage === 'implementation'
}

/** `yyyy-mm-dd` of today in the browser's time zone, the floor of `valid_until` (PL-47). */
export function today(now: Date = new Date()): string {
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${String(now.getFullYear())}-${month}-${day}`
}

export interface DecisionForm {
  action: FindingDecision['action'] | undefined
  justification: string
  validUntil: string
  modifiedBy: 'requirements-agent' | 'human' | undefined
  startingPoints: string[]
  discardReason: 'no_evidence' | 'duplicate' | undefined
  duplicateOf: string
}

export type DecisionFormErrors = Partial<
  Record<
    | 'action'
    | 'justification'
    | 'validUntil'
    | 'modifiedBy'
    | 'startingPoints'
    | 'discardReason'
    | 'duplicateOf',
    string
  >
>

/**
 * Checks the form before sending (CA-20-9, CA-20-15, CA-6 of US-33) and builds the decision. The
 * api checks the same rules; this only spares a round trip and points at the field.
 */
export function buildDecision(
  form: DecisionForm,
  now: Date = new Date(),
): { decision?: FindingDecision; errors: DecisionFormErrors } {
  const errors: DecisionFormErrors = {}
  const justification = form.justification.trim()
  if (!form.action) errors.action = 'findings.decision.errors.action'
  if (justification.length < JUSTIFICATION_MIN)
    errors.justification = 'findings.decision.errors.justification'
  else if (justification.length > JUSTIFICATION_MAX)
    errors.justification = 'findings.decision.errors.justificationTooLong'
  if (form.action === 'accept_risk' && form.validUntil && form.validUntil < today(now))
    errors.validUntil = 'findings.decision.errors.validUntil'
  if (form.action === 'decide_scope_change') {
    if (!form.modifiedBy) errors.modifiedBy = 'findings.decision.errors.modifiedBy'
    if (form.startingPoints.length === 0)
      errors.startingPoints = 'findings.decision.errors.startingPoints'
  }
  if (form.action === 'discard') {
    if (!form.discardReason) errors.discardReason = 'findings.decision.errors.discardReason'
    if (form.discardReason === 'duplicate' && !/^ACH-[0-9]{2,}$/.test(form.duplicateOf.trim()))
      errors.duplicateOf = 'findings.decision.errors.duplicateOf'
  }
  if (Object.keys(errors).length > 0 || !form.action) return { errors }
  switch (form.action) {
    case 'accept_risk':
      return {
        errors,
        decision: {
          action: 'accept_risk',
          body: { justification, ...(form.validUntil ? { valid_until: form.validUntil } : {}) },
        },
      }
    case 'authorize_new_rework':
      return { errors, decision: { action: 'authorize_new_rework', body: { justification } } }
    case 'decide_scope_change':
      return {
        errors,
        decision: {
          action: 'decide_scope_change',
          body: {
            justification,
            modified_by: form.modifiedBy ?? 'requirements-agent',
            starting_point_artifacts: [...form.startingPoints],
          },
        },
      }
    case 'discard':
      return {
        errors,
        decision: {
          action: 'discard',
          body: {
            reason: form.discardReason ?? 'no_evidence',
            justification,
            ...(form.discardReason === 'duplicate'
              ? { duplicate_of: form.duplicateOf.trim() }
              : {}),
          },
        },
      }
  }
}

// ------------------------------------------------------------------ step-up

/**
 * Decision waiting for the SSO step-up (ER-20-16, CA-20-11): the browser leaves for the SSO and comes
 * back to the same finding, so the decision already confirmed is kept for this tab only
 * (`sessionStorage`) and sent once after the return. It holds no secret.
 */
export interface PendingFindingDecision {
  projectId: string
  findingId: string
  decision: FindingDecision
}

const STORAGE_KEY = 'easycode.pendingFindingDecision'

function storage(): Storage | undefined {
  try {
    return window.sessionStorage
  } catch {
    return undefined
  }
}

export function savePendingFindingDecision(pending: PendingFindingDecision): void {
  try {
    storage()?.setItem(STORAGE_KEY, JSON.stringify(pending))
  } catch {
    // Without storage the approver confirms again after the return; nothing is decided silently.
  }
}

/** Takes (reads and forgets) the pending decision of this finding, so it is sent at most once. */
export function takePendingFindingDecision(
  projectId: string,
  findingId: string,
): PendingFindingDecision | undefined {
  const store = storage()
  let raw: string | null | undefined
  try {
    raw = store?.getItem(STORAGE_KEY)
    store?.removeItem(STORAGE_KEY)
  } catch {
    return undefined
  }
  if (!raw) return undefined
  try {
    const pending = JSON.parse(raw) as PendingFindingDecision
    return pending.projectId === projectId && pending.findingId === findingId ? pending : undefined
  } catch {
    return undefined
  }
}
