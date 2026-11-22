import { apiClient } from '@/shared/api/apiClient'
import type { ModelPair, Page, UserRef } from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'

/** openapi `ExecutorAgent`: the seven configurable agents (the orchestrator is not one). */
export const EXECUTOR_AGENTS = [
  'requirements-agent',
  'architect-agent',
  'dba-agent',
  'designer-agent',
  'developer-agent',
  'qa-agent',
  'security-agent',
] as const

export type ExecutorAgent = (typeof EXECUTOR_AGENTS)[number]

/** openapi `PinnedSkill`: the version is pinned per project (US-43 CA-3, PROJETO 8.3). */
export interface PinnedSkill {
  skill: string
  version: number
  /** A newer published version not accepted yet. */
  available_version?: number | null
}

/** openapi `AgentTools`: a subset of permissoes.yaml; never wider than the agent role. */
export interface AgentTools {
  read_file?: string[]
  write_file?: string[]
  execute_command?: { allowed?: boolean; categories?: string[] }
  git?: string[]
  browser?: boolean
  scanners?: string[]
}

export interface AgentConfiguration {
  version: number
  instructions_version: string
  adjustment_instructions?: string
  tools: AgentTools
  skills: PinnedSkill[]
  output_criteria: string[]
  created_at: string
  created_by?: UserRef
}

export interface NewAgentConfiguration {
  adjustment_instructions?: string
  tools: AgentTools
  skills: Array<{ skill: string; version: number }>
}

export interface ProjectAgent {
  agent: ExecutorAgent
  name: string
  stages: string[]
  configuration: AgentConfiguration
  allowed_by_role?: AgentTools
  role?: string
  limits?: string[]
  delivery?: string[]
  models_by_stage?: Partial<Record<string, ModelPair>>
}

export type SkillVersionStatus = 'imported' | 'scanning' | 'unblocked' | 'blocked' | 'deactivated'

export interface SkillVersion {
  version: number
  published_at: string
  status: SkillVersionStatus
}

/** openapi `Skill`: metadata and description only; the content is untrusted data (PROJETO 8.3). */
export interface Skill {
  id: string
  name: string
  description: string
  origin: 'native' | 'organization'
  stages?: string[]
  agents?: ExecutorAgent[]
  current_version: number
  estimated_tokens?: number
  status: SkillVersionStatus
  versions?: SkillVersion[]
}

export interface AgentWithEtag {
  agent: ProjectAgent
  etag: string
}

export interface AgentsApi {
  listProjectAgents: (projectId: string) => Promise<ProjectAgent[]>
  getProjectAgent: (projectId: string, agent: string) => Promise<AgentWithEtag>
  /** A new configuration version (US-43 CA-3: accepting a skill version is saving it). */
  modifyAgentConfiguration: (
    projectId: string,
    agent: string,
    configuration: NewAgentConfiguration,
    ifMatch: string,
  ) => Promise<{ configuration: AgentConfiguration; etag: string }>
  listSkills: (filter?: { agent?: string; origin?: Skill['origin'] }) => Promise<Skill[]>
  getSkill: (skillId: string) => Promise<Skill>
}

const PAGE_LIMIT = 100

function agentPath(projectId: string, agent: string): string {
  return `/projects/${encodeURIComponent(projectId)}/agents/${encodeURIComponent(agent)}`
}

export function createAgentsApi(client: HttpClient = apiClient): AgentsApi {
  return {
    listProjectAgents: async (projectId) =>
      (
        await client.get<{ items: ProjectAgent[] }>(
          `/projects/${encodeURIComponent(projectId)}/agents`,
        )
      ).items,
    getProjectAgent: async (projectId, agent) => {
      const response = await client.send<ProjectAgent>('GET', agentPath(projectId, agent))
      return { agent: response.data, etag: response.headers.get('ETag') ?? '' }
    },
    modifyAgentConfiguration: async (projectId, agent, configuration, ifMatch) => {
      const response = await client.send<AgentConfiguration>(
        'PUT',
        `${agentPath(projectId, agent)}/configuration`,
        { body: configuration, headers: { 'If-Match': ifMatch } },
      )
      return { configuration: response.data, etag: response.headers.get('ETag') ?? ifMatch }
    },
    listSkills: async (filter = {}) => {
      const items: Skill[] = []
      let cursor: string | undefined
      do {
        const page = await client.get<Page<Skill>>('/skills', {
          query: { agent: filter.agent, origin: filter.origin, cursor, limit: PAGE_LIMIT },
        })
        items.push(...page.items)
        cursor = page.next_cursor ?? undefined
      } while (cursor)
      return items
    },
    getSkill: (skillId) => client.get<Skill>(`/skills/${encodeURIComponent(skillId)}`),
  }
}

export const agentsApi = createAgentsApi()
