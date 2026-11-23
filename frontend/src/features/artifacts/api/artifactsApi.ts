import { apiClient } from '@/shared/api/apiClient'
import type { AgentId, ArtifactState, Command, Page, StageId, UserRef } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'

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

/** openapi `ArtifactComparison` (US-27): header fields and body lines added, removed and modified. */
export interface ArtifactComparison {
  artifact: string
  /** The compared versions (the openapi draft named them `from_state` and `to_state`). */
  from?: number
  to?: number
  header: Array<{
    /** JSON pointer of the field. */
    path: string
    operation: 'added' | 'removed' | 'modified'
    before?: unknown
    after?: unknown
  }>
  /** Unified diff by line. */
  body: Array<{
    operation: 'equal' | 'added' | 'removed'
    line_from?: number | null
    line_to?: number | null
    text: string
  }>
  /** The findings that drove the versions (CA-2). */
  driving_findings?: string[]
}

/**
 * openapi `ArtifactEdit` (US-53, PD-03): the whole Markdown file as the person left it; the stage
 * agent validates and formats it before it becomes a version. `delete` is a new version without the
 * item (RN-2); `finding` is the scope change that opened an approved artifact (CA-7).
 */
export interface ArtifactEdit {
  operation: 'create' | 'modify' | 'delete'
  type?: string
  artifact?: string
  base_version?: number
  content?: string
  reason: string
  finding?: string
}

/** openapi `getArtifactEdit`: the guard (pending), the agent's `edit_review` wave, then the result. */
export interface ArtifactEditStatus {
  id: string
  command: string
  operation: ArtifactEdit['operation']
  type: string
  artifact?: string | null
  state: 'pending' | 'reviewing' | 'applied' | 'refused'
  review_wave?: string | null
  version?: number | null
  refusal_reason?: string | null
}

/** Up to 200 KiB of text (413 above). */
export const ARTIFACT_EDIT_LIMIT = 204800

export interface ArtifactsApi {
  listArtifacts: (projectId: string, filters?: { type?: string }) => Promise<ArtifactSummary[]>
  getArtifact: (projectId: string, artifactId: string) => Promise<Artifact>
  listVersions: (projectId: string, artifactId: string) => Promise<ArtifactVersionSummary[]>
  getVersion: (projectId: string, artifactId: string, version: number) => Promise<Artifact>
  /** The exact Markdown file of a version (`text/markdown`), for download. */
  getVersionFile: (projectId: string, artifactId: string, version: number) => Promise<string>
  /** US-53: asynchronous command `edit_artifact` (202, the command with `edit`, the edit id). */
  editArtifact: (
    projectId: string,
    edit: ArtifactEdit,
    idempotencyKey: string,
  ) => Promise<Command & { edit: string }>
  getArtifactEdit: (projectId: string, editId: string) => Promise<ArtifactEditStatus>
  /** US-27 CA-1: any two versions, not only neighbours (ER-21-6). */
  compareVersions: (
    projectId: string,
    artifactId: string,
    from: number,
    to: number,
  ) => Promise<ArtifactComparison>
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
    getArtifactEdit: (projectId, editId) =>
      client.get<ArtifactEditStatus>(
        `/projects/${encodeURIComponent(projectId)}/artifact-edits/${encodeURIComponent(editId)}`,
      ),
    editArtifact: (projectId, edit, idempotencyKey) =>
      client.post<Command & { edit: string }>(
        `/projects/${encodeURIComponent(projectId)}/artifact-edits`,
        edit,
        {
          headers: idempotencyHeaders(idempotencyKey),
        },
      ),
    compareVersions: (projectId, artifactId, from, to) =>
      client.get<ArtifactComparison>(`${artifactPath(projectId, artifactId)}/comparison`, {
        query: { from, to },
      }),
  }
}

export const artifactsApi = createArtifactsApi()
