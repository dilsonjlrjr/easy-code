import { apiClient } from '@/shared/api/apiClient'
import type { AgentId, Page, StageId } from '@/shared/api/contract'
import { joinUrl, type HttpClient } from '@/shared/api/httpClient'

/** openapi `PendingItemType`. Phase 1 showed gates and questions (US-48 RN-1, CA-19-10). */
export type PendingItemType = 'gate' | 'question' | 'finding_escalated' | 'budget'

/** Types with a filter: escalated findings (US-32 CA-4) and budget alerts (US-39 CA-1, RN-5). */
export const SHOWN_TYPES: readonly PendingItemType[] = [
  'gate',
  'question',
  'finding_escalated',
  'budget',
]

export interface PendingItem {
  type: PendingItemType
  project: { id: string; name: string }
  stage?: StageId
  agent?: AgentId
  target: { type: 'gate_round' | 'question' | 'finding' | 'project'; id: string }
  opened_at: string
  title?: string
  summary?: {
    failed_checks?: number
    blocks?: boolean
    questions?: number
    budget_percentage?: number
    you_decide?: boolean
  }
}

export interface PendingItemsPage extends Page<PendingItem> {
  total: number
}

export function createPendingItemsApi(client: HttpClient = apiClient) {
  return {
    list: (query: { type?: PendingItemType; cursor?: string; limit?: number } = {}) =>
      client.get<PendingItemsPage>('/pending-items', {
        query: { type: query.type, cursor: query.cursor, limit: query.limit ?? 50 },
      }),
  }
}

export const pendingItemsApi = createPendingItemsApi()

/** `watchNotices` (`GET /me/events`): what is waiting changes live (US-48 CA-2, ER-19-4). */
export function noticesUrl(): string {
  return joinUrl(apiClient.baseUrl, '/me/events')
}

/** `REQ-01/PERG-1` → the wave of a question target. */
export function waveOfQuestion(targetId: string): string | undefined {
  const [wave, question] = targetId.split('/')
  return wave && question ? wave : undefined
}

/** `gate.requirements/2` → gate and round of a gate target. */
export function gateRoundOf(targetId: string): { gateId: string; round: number } | undefined {
  const [gateId, round] = targetId.split('/')
  const number = Number(round)
  return gateId && Number.isInteger(number) && number > 0 ? { gateId, round: number } : undefined
}

/** CA-19-1 / ER-19-3: oldest first, whatever the order of the pages that arrived. */
export function oldestFirst(items: PendingItem[]): PendingItem[] {
  return [...items].sort((left, right) => Date.parse(left.opened_at) - Date.parse(right.opened_at))
}
