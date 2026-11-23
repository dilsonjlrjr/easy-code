import { apiClient } from '@/shared/api/apiClient'
import type { DetailedCost, GateState, StageState } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import type { Acceptance, FindingSummary } from '@/features/findings/api/findingsApi'
import type { GateDecision } from '@/features/gates/api/gatesApi'

/** Why the Release cannot be approved now (openapi `ReleaseSummary.blocks[].type`). */
export type ReleaseBlockType =
  'blocking_finding_no_acceptance' | 'acceptance_expired' | 'gate_not_approved' | 'active_wave'

export interface ReleaseBlock {
  type: ReleaseBlockType
  /** The finding, gate or wave behind the block. */
  reference: string
  /** Expiry date of the acceptance, in `acceptance_expired` (PL-14). */
  valid_until?: string | null
}

/**
 * openapi `ReleaseSummary` (`getReleaseSummary`, US-08 CA-1, US-33 CA-4): gates, tests, findings,
 * accepted risks with their justification, total cost and the blocks. It answers with the release
 * stage still blocked too, as a partial consolidation (PD-14).
 */
export interface ReleaseSummary {
  /** State of the `release` stage (partial consolidation while `blocked`, PD-14). */
  stage_state?: StageState | null
  gates: Array<{
    gate: string
    round?: number | null
    state: GateState | null
    decision?: GateDecision | null
  }>
  tests?: { ct_total?: number; passed?: number; failed?: number; report?: string }
  open_findings: FindingSummary[]
  accepted_risks: Array<{ finding: FindingSummary; acceptance: Acceptance }>
  cost_total: DetailedCost
  blocks: ReleaseBlock[]
  /** The RELEASE artifact of the stage, once the orchestrator wrote it (null before). */
  release_artifact?: string | null
  /** The version mark the approval writes in the repository, e.g. `release-01`. */
  mark_name?: string | null
}

export interface ReleaseApi {
  getReleaseSummary: (projectId: string) => Promise<ReleaseSummary>
}

export function createReleaseApi(client: HttpClient = apiClient): ReleaseApi {
  return {
    getReleaseSummary: (projectId) =>
      client.get<ReleaseSummary>(`/projects/${encodeURIComponent(projectId)}/release`),
  }
}

export const releaseApi = createReleaseApi()

/** ESP-17 3.4: Critical and High open findings block; Medium and Low are listed apart. */
export function splitOpenFindings(findings: FindingSummary[]): {
  blocking: FindingSummary[]
  other: FindingSummary[]
} {
  const blocking = findings.filter(
    (finding) => finding.severity === 'critical' || finding.severity === 'high',
  )
  return { blocking, other: findings.filter((finding) => !blocking.includes(finding)) }
}

/** ER-17-6: an acceptance past its `valid_until` (a date) is expired on the given day. */
export function acceptanceExpired(acceptance: Acceptance, today: string): boolean {
  return !!acceptance.valid_until && acceptance.valid_until < today
}
