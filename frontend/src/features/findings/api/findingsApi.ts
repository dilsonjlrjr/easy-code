import { apiClient } from '@/shared/api/apiClient'
import type { AgentId, AvailableAction, Page, StageId, UserRef } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'

/** openapi `FindingStatus` (estados.yaml machine `achado`, PD-06). */
export type FindingStatus =
  | 'open'
  | 'in_rework'
  | 'revalidating'
  | 'closed'
  | 'escalated'
  | 'awaiting_scope'
  | 'accepted'
  | 'discarded'

export const FINDING_STATUSES: readonly FindingStatus[] = [
  'open',
  'in_rework',
  'revalidating',
  'escalated',
  'awaiting_scope',
  'accepted',
  'closed',
  'discarded',
]

/** openapi `Severity` (D-36). */
export type Severity = 'critical' | 'high' | 'medium' | 'low'

export const SEVERITIES: readonly Severity[] = ['critical', 'high', 'medium', 'low']

/** openapi `FindingSummary`. */
export interface FindingSummary {
  id: string
  title: string
  severity: Severity
  category: string
  status: FindingStatus
  origin_stage?: StageId
  destination_stage: StageId
  author: AgentId
  blocking: boolean
  cycles?: number
  /** `accepted` with `acceptance.valid_until` in the past: blocks the release until renewed (PL-68). */
  acceptance_expired?: boolean
  opened_at?: string
}

/** `Finding.affected_artifacts[]`: the artifact and, optionally, an item id or a file path. */
export interface FindingArtifactRef {
  artifact: string
  item?: string
}

export interface FindingCycle {
  number: number
  rework_wave?: string
  revalidation_wave?: string
  result?: 'fixed' | 'not_fixed' | null
  commit_or_version?: string
}

export interface FindingEscalation {
  when: string
  reason: string
  decision?: 'new_rework' | 'accept_risk' | 'scope_change' | null
  authorized_by?: UserRef
  decided_at?: string | null
  justification?: string | null
  modified_by?: 'requirements-agent' | 'human' | null
  starting_point_artifacts?: string[]
}

/** openapi `Acceptance`. */
export interface Acceptance {
  per: UserRef
  when: string
  origin?: 'web' | 'local_client' | null
  justification: string
  valid_until?: string | null
}

/** openapi `Finding`: the summary plus cycles, escalations, acceptance and the actions allowed. */
export interface Finding extends FindingSummary {
  affected_artifacts: FindingArtifactRef[]
  origin_wave?: string
  cycle_history: FindingCycle[]
  escalations: FindingEscalation[]
  acceptance?: Acceptance | null
  resolution?: 'fixed' | 'scope_change' | null
  modified_artifacts?: string[]
  discard_reason?: string | null
  /** The ACH itself as an artifact, for `getArtifact` (evidence, impact, suggested fix). */
  artifact?: string
  available_actions: AvailableAction[]
}

export interface FindingFilters {
  status?: FindingStatus[]
  severity?: Severity[]
  destination_stage?: StageId
  origin_stage?: StageId
}

/** openapi `RiskAcceptance` (US-33). */
export interface RiskAcceptance {
  justification: string
  valid_until?: string
}

/** openapi `EscalationDecision` (US-32 CA-6). */
export interface EscalationDecision {
  justification: string
}

/** openapi `ScopeChangeDecision` (US-32 CA-7, PD-06). */
export interface ScopeChangeDecision {
  modified_by: 'requirements-agent' | 'human'
  starting_point_artifacts: string[]
  justification: string
}

/** openapi `DiscardDecision`. */
export interface DiscardDecision {
  reason: 'no_evidence' | 'duplicate'
  duplicate_of?: string
  justification: string
}

export type FindingDecision =
  | { action: 'accept_risk'; body: RiskAcceptance }
  | { action: 'authorize_new_rework'; body: EscalationDecision }
  | { action: 'decide_scope_change'; body: ScopeChangeDecision }
  | { action: 'discard'; body: DiscardDecision }

export type FindingDecisionAction = FindingDecision['action']

/** One Idempotency-Key per intention and the session's anti-CSRF token (ADR-19). */
export interface DecisionHeaders {
  idempotencyKey: string
  csrfToken: string | undefined
}

/** Route of each decision (openapi `acceptFindingRisk`, `authorizeFindingExtraRound`...). */
const DECISION_PATHS: Record<FindingDecisionAction, string> = {
  accept_risk: 'risk-acceptance',
  authorize_new_rework: 'extra-round',
  decide_scope_change: 'scope-change',
  discard: 'discard',
}

export interface FindingsApi {
  list: (
    projectId: string,
    filters?: FindingFilters,
    cursor?: string,
  ) => Promise<Page<FindingSummary>>
  get: (projectId: string, findingId: string) => Promise<Finding>
  decide: (
    projectId: string,
    findingId: string,
    decision: FindingDecision,
    headers: DecisionHeaders,
  ) => Promise<Finding>
}

function findingPath(projectId: string, findingId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/findings/${encodeURIComponent(findingId)}`
}

export function createFindingsApi(client: HttpClient = apiClient): FindingsApi {
  return {
    list: (projectId, filters = {}, cursor) =>
      client.get<Page<FindingSummary>>(`/projects/${encodeURIComponent(projectId)}/findings`, {
        query: {
          status: filters.status?.length ? [...filters.status] : undefined,
          severity: filters.severity?.length ? [...filters.severity] : undefined,
          destination_stage: filters.destination_stage,
          origin_stage: filters.origin_stage,
          cursor,
          limit: 50,
        },
      }),
    get: (projectId, findingId) => client.get<Finding>(findingPath(projectId, findingId)),
    decide: (projectId, findingId, decision, headers) =>
      client.post<Finding>(
        `${findingPath(projectId, findingId)}/${DECISION_PATHS[decision.action]}`,
        decision.body,
        {
          headers: {
            ...idempotencyHeaders(headers.idempotencyKey),
            ...(headers.csrfToken ? { 'X-CSRF-Token': headers.csrfToken } : {}),
          },
        },
      ),
  }
}

export const findingsApi = createFindingsApi()
