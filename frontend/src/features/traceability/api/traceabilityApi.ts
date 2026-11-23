import { apiClient } from '@/shared/api/apiClient'
import type { HttpClient } from '@/shared/api/httpClient'

export type Direction = 'both' | 'output' | 'input'
export type Priority = 'must' | 'should' | 'could' | 'wont'

export const DIRECTIONS: readonly Direction[] = ['both', 'output', 'input']
export const PRIORITIES: readonly Priority[] = ['must', 'should', 'could', 'wont']

export interface TraceNode {
  id: string
  /** Artifact type, `commit` or `test`. */
  type: string
  title?: string
  state?: string
  exists: boolean
}

/** openapi `Traceability.edges[]`: node ids and the E-nn edge of rastreabilidade.md section 1. */
export interface TraceEdge {
  from: string
  to: string
  rule: string
}

/** openapi `Traceability` (US-28 CA-1, CA-3). */
export interface Traceability {
  root: string
  nodes: TraceNode[]
  edges: TraceEdge[]
}

export type CtResult = 'passed' | 'failed' | 'not_executed'

/** openapi `CoverageMatrix` (US-28 CA-2, US-22 CA-2). */
export interface CoverageMatrix {
  items: Array<{
    us: string
    priority: Priority
    status: 'covered' | 'gap'
    criteria: Array<{ ca: string; cts: Array<{ ct: string; result: CtResult }> }>
  }>
}

export function createTraceabilityApi(client: HttpClient = apiClient) {
  const base = (projectId: string) => `/projects/${encodeURIComponent(projectId)}`
  return {
    links: (projectId: string, artifactId: string, depth: number, direction: Direction) =>
      client.get<Traceability>(
        `${base(projectId)}/artifacts/${encodeURIComponent(artifactId)}/links`,
        {
          query: { depth, direction },
        },
      ),
    coverage: (projectId: string, priority: Priority[]) =>
      client.get<CoverageMatrix>(`${base(projectId)}/coverage`, {
        query: { priority: [...priority] },
      }),
  }
}

export const traceabilityApi = createTraceabilityApi()
