import { apiClient } from '@/shared/api/apiClient'
import type {
  ArtifactState,
  AvailableAction,
  GateState,
  StageId,
  UserRef,
} from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'
import type { FindingSummary } from '@/features/projects/api/projectsApi'

/** openapi `DecisionRecord`: who, when and from where (US-06 CA-1). */
export interface DecisionRecord {
  per: { type: 'human' | 'agent' | 'orchestrator'; id?: string; user?: UserRef }
  when: string
  origin: 'web' | 'local_client' | null
}

export interface GateDecision {
  result: 'approved' | 'rejected' | 'invalidated'
  record: DecisionRecord
  reason?: string
  comment?: string
  failed_checks?: string[]
  blocking_findings?: string[]
}

export interface GateRoundSummary {
  gate: string
  stage: StageId
  round: number
  state: GateState
  opened_at: string
  decision?: GateDecision | null
}

export interface CheckResult {
  check: string
  name: string
  type: 'automatic' | 'human'
  failure_severity: 'blocks' | 'alert'
  status: 'passed' | 'failed' | 'error' | 'pending'
  confirmed_by?: DecisionRecord | null
  evidence?: Array<{ reference?: string; path?: string; message: string }>
}

export interface ArtifactVersionRef {
  artifact: string
  version: number
  type?: string
  state?: ArtifactState
}

/** openapi `GateRound`: the report of a gate round (US-06, US-31). */
export interface GateRound extends GateRoundSummary {
  approval_mode: 'human' | 'automatic'
  min_approvals?: number
  partial_approvals?: DecisionRecord[]
  outputs: ArtifactVersionRef[]
  checks: CheckResult[]
  blocking_findings?: FindingSummary[]
  /** ESP-12 6: the round went back to evaluating because a finding was opened (US-31 CA-3). */
  reevaluation?: { cause: 'finding_opened'; finding: string; evaluation?: unknown } | null
  non_blocking_findings?: FindingSummary[]
  available_actions: AvailableAction[]
}

export interface ApprovalDecision {
  confirmed_human_checks: string[]
  comment?: string
}

export interface RejectionDecision {
  reason: string
  unconfirmed_human_checks?: string[]
}

/** Headers of a gate decision: one Idempotency-Key per intention and the session's anti-CSRF token. */
export interface DecisionHeaders {
  idempotencyKey: string
  csrfToken: string | undefined
}

export interface GatesApi {
  listRounds: (projectId: string, gateId: string) => Promise<{ items: GateRoundSummary[] }>
  getRound: (projectId: string, gateId: string, round: number) => Promise<GateRound>
  approve: (
    projectId: string,
    gateId: string,
    round: number,
    decision: ApprovalDecision,
    headers: DecisionHeaders,
  ) => Promise<GateRound>
  reject: (
    projectId: string,
    gateId: string,
    round: number,
    decision: RejectionDecision,
    headers: DecisionHeaders,
  ) => Promise<GateRound>
}

function roundsPath(projectId: string, gateId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/gates/${encodeURIComponent(gateId)}/rounds`
}

function decisionHeaders(headers: DecisionHeaders): Record<string, string> {
  return {
    ...idempotencyHeaders(headers.idempotencyKey),
    ...(headers.csrfToken ? { 'X-CSRF-Token': headers.csrfToken } : {}),
  }
}

export function createGatesApi(client: HttpClient = apiClient): GatesApi {
  return {
    listRounds: (projectId, gateId) =>
      client.get<{ items: GateRoundSummary[] }>(roundsPath(projectId, gateId)),
    getRound: (projectId, gateId, round) =>
      client.get<GateRound>(`${roundsPath(projectId, gateId)}/${String(round)}`),
    approve: (projectId, gateId, round, decision, headers) =>
      client.post<GateRound>(
        `${roundsPath(projectId, gateId)}/${String(round)}/approval`,
        decision,
        {
          headers: decisionHeaders(headers),
        },
      ),
    reject: (projectId, gateId, round, decision, headers) =>
      client.post<GateRound>(
        `${roundsPath(projectId, gateId)}/${String(round)}/rejection`,
        decision,
        { headers: decisionHeaders(headers) },
      ),
  }
}

export const gatesApi = createGatesApi()
