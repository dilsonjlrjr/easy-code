import { apiClient } from '@/shared/api/apiClient'
import type { ProblemDetails } from '@/shared/api/problemDetails'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'
import type { Skill, SkillBlockReason, SkillVersionStatus } from '@/features/agents/api/agentsApi'

export type { SkillBlockReason }

/** openapi `SkillImport` (US-44): the request, then the version in the skill state machine. */
export interface SkillImport {
  id: string
  state: 'received' | 'refused' | 'version_created'
  origin?: 'upload' | 'git_repository'
  version_status?: SkillVersionStatus | null
  received_at: string
  skill?: string | null
  version?: number | null
  refusal?: ProblemDetails
  block_reasons?: SkillBlockReason[]
}

export interface GitSkillSource {
  git_url: string
  reference?: string
  subfolder?: string
}

export interface SkillLibraryApi {
  /** A zip of the skill folder (multipart `package`). 413 / 400 refuse it before any version. */
  importPackage: (file: File, idempotencyKey: string) => Promise<SkillImport>
  importGit: (source: GitSkillSource, idempotencyKey: string) => Promise<SkillImport>
  getImport: (importId: string) => Promise<SkillImport>
  /** US-44 CA-4: an administrator releases a version blocked for suspicious content. */
  unblock: (skillId: string, version: number, justification: string, key: string) => Promise<Skill>
  deactivate: (
    skillId: string,
    version: number,
    justification: string,
    key: string,
  ) => Promise<Skill>
}

function versionPath(skillId: string, version: number): string {
  return `/skills/${encodeURIComponent(skillId)}/versions/${String(version)}`
}

export function createSkillLibraryApi(client: HttpClient = apiClient): SkillLibraryApi {
  return {
    importPackage: (file, idempotencyKey) => {
      const form = new FormData()
      form.append('package', file, file.name)
      return client.post<SkillImport>('/skills/imports', form, {
        headers: idempotencyHeaders(idempotencyKey),
      })
    },
    importGit: (source, idempotencyKey) =>
      client.post<SkillImport>(
        '/skills/imports',
        {
          git_url: source.git_url,
          ...(source.reference ? { reference: source.reference } : {}),
          ...(source.subfolder ? { subfolder: source.subfolder } : {}),
        },
        { headers: idempotencyHeaders(idempotencyKey) },
      ),
    getImport: (importId) =>
      client.get<SkillImport>(`/skills/imports/${encodeURIComponent(importId)}`),
    unblock: (skillId, version, justification, key) =>
      client.post<Skill>(
        `${versionPath(skillId, version)}/unblock`,
        { justification },
        {
          headers: idempotencyHeaders(key),
        },
      ),
    deactivate: (skillId, version, justification, key) =>
      client.post<Skill>(
        `${versionPath(skillId, version)}/deactivation`,
        { justification },
        {
          headers: idempotencyHeaders(key),
        },
      ),
  }
}

export const skillLibraryApi = createSkillLibraryApi()

/** The import is still moving: received, or a version not yet out of `imported`/`scanning`. */
export function importInProgress(item: SkillImport): boolean {
  if (item.state === 'received') return true
  return (
    item.state === 'version_created' &&
    (item.version_status === 'imported' || item.version_status === 'scanning')
  )
}

/** Follows an import until it settles (ESP-23 3.3 "Importações em curso"), polling getSkillImport. */
export async function followImport(
  first: SkillImport,
  read: (importId: string) => Promise<SkillImport>,
  options: { intervalMs?: number; timeoutMs?: number; sleep?: (ms: number) => Promise<void> } = {},
): Promise<SkillImport> {
  const interval = options.intervalMs ?? 1000
  const timeout = options.timeoutMs ?? 60_000
  const sleep =
    options.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)))
  let current = first
  let waited = 0
  while (importInProgress(current) && waited < timeout) {
    await sleep(interval)
    waited += interval
    current = await read(current.id)
  }
  return current
}

/** ER-23-10: only a version blocked for suspicious content can be released; format or size cannot. */
export function canUnblock(reasons: SkillBlockReason[] | undefined): boolean {
  const list = reasons ?? []
  return list.includes('malicious_content') && !list.includes('format') && !list.includes('size')
}

/** The git address must be `https` (policy.imported_skill); the api also checks the host list. */
export function isHttpsUrl(value: string): boolean {
  try {
    return new URL(value.trim()).protocol === 'https:'
  } catch {
    return false
  }
}
