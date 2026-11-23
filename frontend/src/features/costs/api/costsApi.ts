import { apiClient } from '@/shared/api/apiClient'
import type { AgentId, DetailedCost, Money, StageId } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import type { ProjectCosts } from '@/features/projects/api/projectsApi'

/** `group_by` of getProjectCosts (ESP-25 3): stage is the default. */
export const COST_GROUPINGS = ['stage', 'agent', 'model', 'provider'] as const
export type CostGrouping = (typeof COST_GROUPINGS)[number]

/** openapi `Estimate` (US-40): input size × current price × historical average of the stage. */
export interface Estimate {
  stage: StageId
  estimated: Money
  no_history: boolean
  basis: {
    input_size_tokens: number
    price_table_version: string
    historical_average?: Money
    considered_waves?: number
  }
  /** What the last concluded wave of the stage cost (CA-3); null before any. */
  performed?: Money | null
}

/** openapi `UserCosts` (PL-02): the month of the user's projects on the Home page. */
export interface UserCosts {
  /** Start of the period (the openapi draft named it `from_state`). */
  from?: string
  until: string
  total: DetailedCost
  rework_percentage: number
  largest_spend: Array<{ project: string; name: string; total: Money }>
}

/** ESP-25 2: the waves of a stage or agent group (listWaves `stage`, `agent`). */
export interface WaveFilter {
  stage?: StageId
  agent?: AgentId
}

export interface CostsApi {
  getProjectCosts: (projectId: string, groupBy: CostGrouping) => Promise<ProjectCosts>
  getStageEstimate: (projectId: string, stage: StageId) => Promise<Estimate>
  getUserCosts: (period?: { from?: string; until?: string }) => Promise<UserCosts>
}

function projectPath(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}`
}

export function createCostsApi(client: HttpClient = apiClient): CostsApi {
  return {
    getProjectCosts: (projectId, groupBy) =>
      client.get<ProjectCosts>(`${projectPath(projectId)}/costs`, {
        query: { group_by: groupBy },
      }),
    getStageEstimate: (projectId, stage) =>
      client.get<Estimate>(
        `${projectPath(projectId)}/stages/${encodeURIComponent(stage)}/estimate`,
      ),
    getUserCosts: (period = {}) =>
      client.get<UserCosts>('/me/costs', { query: { from: period.from, until: period.until } }),
  }
}

export const costsApi = createCostsApi()
