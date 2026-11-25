import { apiClient } from '@/shared/api/apiClient'
import type { ArtifactState, ProjectState } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import type { FindingStatus, Severity } from '@/features/findings/api/findingsApi'

/** openapi `SearchProjectRef`. */
export interface SearchProjectRef {
  id: string
  name: string
  state: ProjectState
}

/** openapi `SearchArtifact`. */
export interface SearchArtifact {
  id: string
  /** openapi `ArtifactType` (US, ADR, VISAO…; never ACH, which is a finding). */
  type: string
  title: string
  state: ArtifactState
  project: SearchProjectRef
}

/** openapi `SearchFinding`. */
export interface SearchFinding {
  id: string
  title: string
  status: FindingStatus
  severity: Severity
  project: SearchProjectRef
}

export interface SearchGroup<T> {
  items: T[]
  total: number
  project_count: number
}

/** openapi `SearchResults` (US-56 CA-2). */
export interface SearchResults {
  projects: SearchGroup<SearchProjectRef>
  artifacts: SearchGroup<SearchArtifact>
  findings: SearchGroup<SearchFinding>
}

export function createSearchApi(client: HttpClient = apiClient) {
  return {
    /** `searchPlatform` (`GET /search`): only the projects where the person has a role (RN-1). */
    search: (term: string, signal?: AbortSignal) =>
      client.get<SearchResults>('/search', { query: { search: term }, signal }),
  }
}

export const searchApi = createSearchApi()
