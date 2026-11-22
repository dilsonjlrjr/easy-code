import { apiClient } from '@/shared/api/apiClient'
import type {
  AgentId,
  AvailableAction,
  Capacity,
  Command,
  GateState,
  ModelPair,
  Money,
  Page,
  ProjectRole,
  ProjectState,
  StageId,
  StageState,
  UserRef,
  WaveState,
} from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'

export interface ProjectSummary {
  current_stages?: Array<{ stage: StageId; state: StageState }>
  approved_stages: number
  stages_total: number
  active_waves?: Array<{ wave: string; agent: AgentId; state: WaveState }>
  gates_awaiting: number
  open_questions: number
  accumulated_cost: Money
  stop_limit?: Money | null
}

export interface Project {
  id: string
  name: string
  state: ProjectState
  pause_reason?: 'manual' | 'budget' | null
  business_need?: string
  workflow: string
  created_at: string
  created_by: UserRef
  updated_at?: string
  my_roles: ProjectRole[]
  lead?: UserRef
  available_actions?: AvailableAction[]
  summary?: ProjectSummary
  last_sequence?: number
}

export interface ProjectsPage extends Page<Project> {
  count_by_state: Partial<Record<ProjectState, number>>
}

export interface NewProject {
  name: string
  business_need: string
  provider_submission_acknowledgment: true
}

export interface Attachment {
  id: string
  file_name: string
  media_type: string
  size_bytes: number
  sha256: string
  submitted_at: string
  submitted_by: UserRef
}

export interface ProjectConfiguration {
  model_pair: ModelPair | null
  pair_by_stage?: Partial<Record<StageId, ModelPair>>
  gate_approval: Record<string, 'human' | 'automatic'>
  budget: unknown
  artifact_language?: string
}

export interface ProjectConfigurationModification {
  model_pair?: ModelPair
  pair_by_stage?: Partial<Record<StageId, ModelPair | null>>
  artifact_language?: string
}

/** Configuration plus the `ETag` that its next change must send as `If-Match` (ER-00-11). */
export interface VersionedConfiguration {
  configuration: ProjectConfiguration
  etag: string
}

export interface StageSummary {
  id: StageId
  name: string
  short_name?: string
  type: 'stage' | 'parallel_activity'
  agent: AgentId
  depends_on: StageId[]
  required_capabilities?: Capacity[]
  state: StageState | null
  gate?: string | null
  current_round?: number | null
  gate_state?: GateState | null
  active_waves?: number
  cost?: Money
}

export interface Workflow {
  id: string
  version: number
  mode: 'fixed'
  stages: StageSummary[]
}

export interface WaveSummary {
  id: string
  stage: StageId
  agent: AgentId
  state: WaveState
  purpose: 'execution' | 'rework' | 'revalidation' | 'reevaluation' | 'edit_review'
  finding?: string | null
  attempt: number
  created_at: string
  started_at?: string | null
  ended_at?: string | null
  cost_total?: Money
}

export interface ProjectsApi {
  listProjects: (query: { state?: ProjectState[]; cursor?: string }) => Promise<ProjectsPage>
  createProject: (body: NewProject, idempotencyKey: string) => Promise<Project>
  getProject: (projectId: string) => Promise<Project>
  attachFile: (projectId: string, file: File, idempotencyKey: string) => Promise<Attachment>
  getConfiguration: (projectId: string) => Promise<VersionedConfiguration>
  modifyConfiguration: (
    projectId: string,
    change: ProjectConfigurationModification,
    ifMatch: string,
  ) => Promise<VersionedConfiguration>
  startWorkflow: (projectId: string, idempotencyKey: string) => Promise<Command>
  getCommand: (projectId: string, commandId: string) => Promise<Command>
  getWorkflow: (projectId: string) => Promise<Workflow>
  listWaves: (projectId: string, cursor?: string) => Promise<Page<WaveSummary>>
}

function projectPath(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}`
}

export function createProjectsApi(client: HttpClient = apiClient): ProjectsApi {
  return {
    listProjects: (query) =>
      client.get<ProjectsPage>('/projects', {
        query: { state: query.state, cursor: query.cursor, limit: 20 },
      }),
    createProject: (body, idempotencyKey) =>
      client.post<Project>('/projects', body, { headers: idempotencyHeaders(idempotencyKey) }),
    getProject: (projectId) => client.get<Project>(projectPath(projectId)),
    attachFile: (projectId, file, idempotencyKey) => {
      const form = new FormData()
      form.append('file', file, file.name)
      return client.post<Attachment>(`${projectPath(projectId)}/attachments`, form, {
        headers: idempotencyHeaders(idempotencyKey),
      })
    },
    getConfiguration: async (projectId) => {
      const response = await client.send<ProjectConfiguration>(
        'GET',
        `${projectPath(projectId)}/configuration`,
      )
      return { configuration: response.data, etag: response.headers.get('ETag') ?? '' }
    },
    modifyConfiguration: async (projectId, change, ifMatch) => {
      const response = await client.send<ProjectConfiguration>(
        'PATCH',
        `${projectPath(projectId)}/configuration`,
        {
          body: change,
          contentType: 'application/merge-patch+json',
          headers: { 'If-Match': ifMatch },
        },
      )
      return { configuration: response.data, etag: response.headers.get('ETag') ?? ifMatch }
    },
    startWorkflow: (projectId, idempotencyKey) =>
      client.post<Command>(`${projectPath(projectId)}/start`, undefined, {
        headers: idempotencyHeaders(idempotencyKey),
      }),
    getCommand: (projectId, commandId) =>
      client.get<Command>(`${projectPath(projectId)}/commands/${encodeURIComponent(commandId)}`),
    getWorkflow: (projectId) => client.get<Workflow>(`${projectPath(projectId)}/workflow`),
    listWaves: (projectId, cursor) =>
      client.get<Page<WaveSummary>>(`${projectPath(projectId)}/waves`, {
        query: { cursor, limit: 100 },
      }),
  }
}

export const projectsApi = createProjectsApi()
