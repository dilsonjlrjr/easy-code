import { apiClient } from '@/shared/api/apiClient'
import type { HttpClient } from '@/shared/api/httpClient'

export type Direction = 'both' | 'output' | 'input'
export type Priority = 'must' | 'should' | 'could' | 'wont'

export const DIRECTIONS: readonly Direction[] = ['both', 'output', 'input']
export const PRIORITIES: readonly Priority[] = ['must', 'should', 'could', 'wont']

export interface TraceNode {
  id: string
  /** Artifact type or `commit`. */
  type: string
  title?: string
  state?: string
  exists: boolean
}

/**
 * openapi `Traceability.edges[]`. The contract names the ends `from_state` / `to_state` (ESP-24 says
 * `de` / `para`: the node ids); kept as the contract says and reported as a naming slip.
 */
export interface TraceEdge {
  from_state: string
  to_state: string
  /** RAS-nn rule of specs/dominio/rastreabilidade.md. */
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
