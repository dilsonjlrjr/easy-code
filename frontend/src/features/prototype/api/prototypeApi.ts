import { apiClient } from '@/shared/api/apiClient'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'
import type {
  DecisionHeaders,
  GateDecision,
  GateRound,
  GateRoundSummary,
  RejectionDecision,
} from '@/features/gates/api/gatesApi'

/** openapi `ContentUrl` (getArtifactContentUrl, ADR-19): the signed URL of the content origin. */
export interface ContentUrl {
  url: string
  prefix?: string
  /** Until when the URL opens (5 min); after it the page asks for a new one (ER-11-9). */
  expires_at: string
}

/** openapi `PrototypeAnnotation` (US-57): a pin on a screen, sent with the rejection of gate.design. */
export interface PrototypeAnnotation {
  screen: string
  state: string | null
  width: 'wide' | 'narrow'
  /** Point relative to the frame, 0 to 1; both null for an annotation without a point (CA-5). */
  x: number | null
  y: number | null
  comment: string
}

/** openapi `RecordedPrototypeAnnotation`: an annotation in the record of a decided round (CA-3). */
export interface RecordedPrototypeAnnotation extends PrototypeAnnotation {
  number: number
  artifact: 'TELAS'
  version: number
}

export type AnnotatedDecision = GateDecision & { annotations?: RecordedPrototypeAnnotation[] }
export type AnnotatedRoundSummary = Omit<GateRoundSummary, 'decision'> & {
  decision?: AnnotatedDecision | null
}

export interface PrototypeApi {
  contentUrl: (
    projectId: string,
    artifactId: string,
    version: number,
    path: string,
  ) => Promise<ContentUrl>
  listDesignRounds: (projectId: string) => Promise<AnnotatedRoundSummary[]>
  /** rejectGate of gate.design with the annotations (PD-25 option a). */
  rejectDesign: (
    projectId: string,
    round: number,
    decision: RejectionDecision & { annotations: PrototypeAnnotation[] },
    headers: DecisionHeaders,
  ) => Promise<GateRound & { decision?: AnnotatedDecision | null }>
}

export const DESIGN_GATE = 'gate.design'

export function createPrototypeApi(client: HttpClient = apiClient): PrototypeApi {
  const rounds = (projectId: string) =>
    `/projects/${encodeURIComponent(projectId)}/gates/${DESIGN_GATE}/rounds`
  return {
    contentUrl: (projectId, artifactId, version, path) =>
      client.get<ContentUrl>(
        `/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}/versions/${String(version)}/content-url`,
        { query: { path } },
      ),
    listDesignRounds: async (projectId) =>
      (await client.get<{ items: AnnotatedRoundSummary[] }>(rounds(projectId))).items,
    rejectDesign: (projectId, round, decision, headers) =>
      client.post<GateRound & { decision?: AnnotatedDecision | null }>(
        `${rounds(projectId)}/${String(round)}/rejection`,
        decision,
        {
          headers: {
            ...idempotencyHeaders(headers.idempotencyKey),
            ...(headers.csrfToken ? { 'X-CSRF-Token': headers.csrfToken } : {}),
          },
        },
      ),
  }
}

export const prototypeApi = createPrototypeApi()
