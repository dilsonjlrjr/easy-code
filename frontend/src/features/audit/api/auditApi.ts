import { apiClient } from '@/shared/api/apiClient'
import type { Money, Page, UserRef } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'

/** openapi `AuditRecord` (US-35, RNF-13). The trail has no change or delete operation (CA-3). */
export interface AuditRecord {
  id: string
  when: string
  actor: { type: 'human' | 'agent' | 'orchestrator'; id?: string; user?: UserRef }
  /** `null` when the action is not human (ER-27-4). */
  origin: 'web' | 'local_client' | null
  /** `entity.action`, e.g. `gate.approve`. */
  action: string
  target: { type: string; id: string }
  project?: string | null
  reason?: string | null
  /** What was decided (roles assigned, the decision of a finding), when the action carries it. */
  decision?: string | null
  lost_cost?: Money | null
  correlationid?: string
}

export type AuditOrder = 'asc' | 'desc'

export interface AuditQuery {
  actions?: string[]
  /** ISO date-time, inclusive. */
  from?: string
  until?: string
  order?: AuditOrder
  cursor?: string
  limit?: number
}

/** Page size of the trail: "load more" brings the next page (ESP-27 3, RNF-09). */
export const AUDIT_PAGE_SIZE = 50

export interface AuditApi {
  listProjectAudit: (projectId: string, query: AuditQuery) => Promise<Page<AuditRecord>>
  listOrganizationAudit: (query: AuditQuery) => Promise<Page<AuditRecord>>
}

function queryOf(query: AuditQuery) {
  return {
    action: query.actions?.length ? query.actions : undefined,
    from: query.from,
    until: query.until,
    order: query.order ?? 'desc',
    cursor: query.cursor,
    limit: query.limit ?? AUDIT_PAGE_SIZE,
  }
}

export function createAuditApi(client: HttpClient = apiClient): AuditApi {
  return {
    listProjectAudit: (projectId, query) =>
      client.get<Page<AuditRecord>>(`/projects/${encodeURIComponent(projectId)}/audit`, {
        query: queryOf(query),
      }),
    listOrganizationAudit: (query) =>
      client.get<Page<AuditRecord>>('/audit', { query: queryOf(query) }),
  }
}

export const auditApi = createAuditApi()
