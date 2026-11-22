import { apiClient } from '@/shared/api/apiClient'
import type { HttpClient } from '@/shared/api/httpClient'

/** openapi `Repository` (US-42): the platform's read-only git server for the project. */
export interface Repository {
  /** `not_created`: no commit yet (the clone answers 404); `available`: it can be cloned. */
  status: 'not_created' | 'available'
  clone_url: string
  main_branch: string
  last_commit?: { sha?: string; when?: string; wave?: string } | null
  marks?: Array<{ name: string; commit: string; when?: string }>
}

export interface RepositoryApi {
  getRepository: (projectId: string) => Promise<Repository>
}

export function createRepositoryApi(client: HttpClient = apiClient): RepositoryApi {
  return {
    getRepository: (projectId) =>
      client.get<Repository>(`/projects/${encodeURIComponent(projectId)}/repository`),
  }
}

export const repositoryApi = createRepositoryApi()
