import { apiClient } from '@/shared/api/apiClient'
import type { AgentId, Page, StageId } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'

export interface PendingItem {
  type: string
  project: { id: string; name: string }
  stage?: StageId
  agent?: AgentId
  target: { type: 'gate_round' | 'question' | 'finding' | 'project'; id: string }
  opened_at: string
  title?: string
  summary?: { questions?: number; you_decide?: boolean }
}

export interface PendingItemsPage extends Page<PendingItem> {
  total: number
}

export function createPendingItemsApi(client: HttpClient = apiClient) {
  return {
    list: (cursor?: string) =>
      client.get<PendingItemsPage>('/pending-items', { query: { cursor, limit: 50 } }),
  }
}

export const pendingItemsApi = createPendingItemsApi()

/** `REQ-01/PERG-1` → the wave of a question target. */
export function waveOfQuestion(targetId: string): string | undefined {
  const [wave, question] = targetId.split('/')
  return wave && question ? wave : undefined
}
