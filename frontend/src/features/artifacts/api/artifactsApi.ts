import { apiClient } from '@/shared/api/apiClient'
import type { AgentId, ArtifactState, Page, StageId, UserRef } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'

export interface ArtifactAuthor {
  type: 'agent' | 'orchestrator' | 'human'
  agent?: AgentId
  edited_by?: UserRef
  reviewed_by?: string | null
}

export interface ArtifactSummary {
  id: string
  type: string
  title: string
  state: ArtifactState
  version: number
  stage: StageId
  author: ArtifactAuthor
  wave: string
  path?: string
  updated_at?: string
  /** Only for T: `situacao` of the current version header (tracking field); null for other types. */
  situation?: 'planejada' | 'em_andamento' | 'concluida' | 'justificada' | null
}

export interface Artifact extends ArtifactSummary {
  /** YAML header as JSON, per `specs/dominio/artefatos/<type>.schema.json`. Untrusted. */
  header: Record<string, unknown>
  /** Markdown body. Untrusted (RNF-05). */
  body: string
  /** Null until the project repository exists (US-42; openapi 0.3.1). */
  commit: string | null
  attachments?: Array<{ path: string; media_type: string; size_bytes: number; sha256: string }>
}

/**
 * openapi `ArtifactVersionSummary`. The contract names the date field `data` (not `date`); it is kept
 * as the contract says (reported as a naming slip).
 */
export interface ArtifactVersionSummary {
  version: number
  data: string
  wave: string
  author?: ArtifactAuthor
  description: string
  finding?: string | null
  approved?: boolean
  commit?: string | null
}

export interface ArtifactsApi {
  listArtifacts: (projectId: string, filters?: { type?: string }) => Promise<ArtifactSummary[]>
  getArtifact: (projectId: string, artifactId: string) => Promise<Artifact>
  listVersions: (projectId: string, artifactId: string) => Promise<ArtifactVersionSummary[]>
  getVersion: (projectId: string, artifactId: string, version: number) => Promise<Artifact>
  /** The exact Markdown file of a version (`text/markdown`), for download. */
  getVersionFile: (projectId: string, artifactId: string, version: number) => Promise<string>
}

function artifactPath(projectId: string, artifactId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}`
}

export function createArtifactsApi(client: HttpClient = apiClient): ArtifactsApi {
  return {
    listArtifacts: async (projectId, filters = {}) => {
      const items: ArtifactSummary[] = []
      let cursor: string | undefined
      do {
        const page = await client.get<Page<ArtifactSummary>>(
          `/projects/${encodeURIComponent(projectId)}/artifacts`,
          { query: { cursor, limit: 100, type: filters.type } },
        )
        items.push(...page.items)
        cursor = page.next_cursor ?? undefined
      } while (cursor)
      return items
    },
    getArtifact: (projectId, artifactId) =>
      client.get<Artifact>(artifactPath(projectId, artifactId)),
    listVersions: async (projectId, artifactId) =>
      (
        await client.get<{ items: ArtifactVersionSummary[] }>(
          `${artifactPath(projectId, artifactId)}/versions`,
        )
      ).items,
    getVersion: (projectId, artifactId, version) =>
      client.get<Artifact>(`${artifactPath(projectId, artifactId)}/versions/${String(version)}`, {
        headers: { Accept: 'application/json' },
      }),
    getVersionFile: (projectId, artifactId, version) =>
      client.get<string>(`${artifactPath(projectId, artifactId)}/versions/${String(version)}`, {
        headers: { Accept: 'text/markdown' },
      }),
  }
}

export const artifactsApi = createArtifactsApi()
