import { apiClient } from '@/shared/api/apiClient'
import type {
  AgentId,
  AvailableAction,
  Capacity,
  Command,
  DetailedCost,
  GateState,
  ModelPair,
  Money,
  Page,
  ProjectRole,
  ProjectState,
  StageId,
  StageState,
  TimeSpent,
  Tokens,
  UserRef,
  WaveState,
} from '@/shared/api/contract'
import type { ArtifactSummary } from '@/features/artifacts/api/artifactsApi'
import type { FindingSummary } from '@/features/findings/api/findingsApi'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'

export interface ProjectSummary {
  current_stages?: Array<{ stage: StageId; state: StageState }>
  approved_stages: number
  stages_total: number
  active_waves?: Array<{ wave: string; agent: AgentId; state: WaveState }>
  gates_awaiting: number
  open_questions: number
  /** Open findings by severity (ESP-30 2, PL-72). */
  open_findings?: Partial<Record<'critical' | 'high' | 'medium' | 'low' | 'escalated', number>>
  accumulated_cost: Money
  stop_limit?: Money | null
  /** The last budget alert crossed (percentage of the stop limit), so it shows after a reload. */
  last_budget_alert_percentage?: number | null
}

/** ESP-06 6: who paused the project and when (manual pause, or the orchestrator by budget). */
export interface PauseRecord {
  reason: 'manual' | 'budget'
  by: { type: 'human' | 'agent' | 'orchestrator'; id?: string; user?: UserRef }
  at: string
  origin?: 'web' | 'local_client' | null
  /** Pause by budget: the spend reached the limit, or the next wave's estimate exceeds the balance. */
  cause?: 'spend_reached_limit' | 'estimate_exceeds_balance' | null
  next_wave_estimate?: Money | null
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
  /** Present while `paused` (ESP-06 6). */
  pause?: PauseRecord | null
}

export interface ProjectsPage extends Page<Project> {
  count_by_state: Partial<Record<ProjectState, number>>
}

export const CONSTRAINT_CATEGORIES = [
  'language',
  'front_end',
  'database',
  'integration',
  'infrastructure',
  'standard',
  'other',
] as const

export type ConstraintCategory = (typeof CONSTRAINT_CATEGORIES)[number]

export const CONSTRAINT_DESCRIPTION_MIN = 3
export const CONSTRAINT_DESCRIPTION_MAX = 2000

/** An item of `NewConstraints`: without `id` the api gives the next `RESTR-NN` (US-54, PL-13). */
export interface ConstraintDraft {
  id?: string
  category: ConstraintCategory
  description: string
}

/** openapi `Constraints`: the version in force of the technical constraints (US-54). */
export interface Constraints {
  version: number
  items: Array<Required<Pick<ConstraintDraft, 'id'>> & ConstraintDraft>
  defined_by?: UserRef
  defined_at?: string
}

export interface NewConstraints {
  items: ConstraintDraft[]
}

export interface NewProject {
  name: string
  business_need: string
  provider_submission_acknowledgment: true
  budget?: Budget
  constraints?: NewConstraints
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

/** openapi `Budget` (politica.orcamento_projeto): the stop is always on; the limit is required. */
export interface Budget {
  stop_limit: Money
  /** Percentages of the stop limit, 1 to 99, no repeats; default 50 and 80 (US-39 RN-2). */
  percentage_alert_limits?: number[]
}

export const DEFAULT_ALERT_PERCENTAGES: readonly number[] = [50, 80]

export interface ProjectConfiguration {
  model_pair: ModelPair | null
  pair_by_stage?: Partial<Record<StageId, ModelPair>>
  gate_approval: Record<string, 'human' | 'automatic'>
  budget: Budget | null
  /** The organization's ceiling for the stop limit; null = no ceiling (US-39 RN-1). */
  stop_limit_ceiling?: Money | null
  artifact_language?: string
  constraints?: Constraints | null
}

export interface ProjectConfigurationModification {
  model_pair?: ModelPair
  budget?: Budget
  pair_by_stage?: Partial<Record<StageId, ModelPair | null>>
  artifact_language?: string
  /** Replaces the whole list and creates a new version (US-54 CA-3). */
  constraints?: NewConstraints
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
  /** Approved stage marked for a reevaluation wave after a reopening (US-34 CA-2). */
  reevaluation_pending?: boolean
  /** The finding that reopened the stage (US-34, ESP-06 3, ESP-08 6). */
  reopened_by_finding?: string | null
  /** Parallel activity (qa-plan, security-threats): the gate whose approval also approved it. */
  approved_in_gate?: string | null
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
  /** The T an implementation wave runs (one wave per task, PL-20); null in other stages. */
  task?: string | null
  created_at: string
  started_at?: string | null
  ended_at?: string | null
  cost_total?: Money
}

export type { FindingSummary }

/** openapi `Stage`: agent, waves (rework included), artifacts, time, tokens, cost, findings, approval (US-49 CA-2). */
export interface Stage extends StageSummary {
  waves: WaveSummary[]
  artifacts: ArtifactSummary[]
  findings?: FindingSummary[]
  tokens: Tokens
  detailed_cost: DetailedCost
  time: TimeSpent
  approval?: { mode?: 'human' | 'automatic'; required_role?: 'approver' }
}

/** openapi `ProjectCosts` (US-38, US-39): totals, groups and the budget with its balance. */
export interface ProjectCosts {
  grouped_by: 'stage' | 'agent' | 'model' | 'provider'
  total: DetailedCost
  tokens: Tokens
  time: TimeSpent
  budget?: { stop_limit?: Money; balance?: Money; used_percentage?: number } | null
  groups: Array<{ key: string; cost: DetailedCost; tokens: Tokens; time: TimeSpent; waves: number }>
}

export interface ProjectsApi {
  listProjects: (query: {
    state?: ProjectState[]
    cursor?: string
    /** `organization`: every project of the organization, metadata only (administrator, US-02 CA-6). */
    scope?: 'my' | 'organization'
  }) => Promise<ProjectsPage>
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
  getStage: (projectId: string, stage: string) => Promise<Stage>
  listWaves: (projectId: string, cursor?: string) => Promise<Page<WaveSummary>>
  /** US-51 CA-1: `in_progress` → `paused` (manual). */
  pauseProject: (projectId: string, idempotencyKey: string) => Promise<Command>
  /** US-51 CA-2, CA-3: back to `in_progress`; paused by budget only with the limit raised. */
  resumeProject: (projectId: string, idempotencyKey: string) => Promise<Command>
  /** US-51 CA-4, RN-1: final; the typed project name is the confirmation. */
  cancelProject: (
    projectId: string,
    nameConfirmation: string,
    idempotencyKey: string,
  ) => Promise<Command>
  /** US-52: `record_publication` after the Release is approved; the project becomes read-only. */
  completeProject: (projectId: string, idempotencyKey: string) => Promise<Command>
  getCosts: (projectId: string) => Promise<ProjectCosts>
}

function projectPath(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}`
}

export function createProjectsApi(client: HttpClient = apiClient): ProjectsApi {
  return {
    listProjects: (query) =>
      client.get<ProjectsPage>('/projects', {
        query: {
          state: query.state,
          cursor: query.cursor,
          scope: query.scope === 'organization' ? 'organization' : undefined,
          limit: 20,
        },
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
    getStage: (projectId, stage) =>
      client.get<Stage>(`${projectPath(projectId)}/stages/${encodeURIComponent(stage)}`),
    pauseProject: (projectId, idempotencyKey) =>
      client.post<Command>(`${projectPath(projectId)}/pause`, undefined, {
        headers: idempotencyHeaders(idempotencyKey),
      }),
    resumeProject: (projectId, idempotencyKey) =>
      client.post<Command>(`${projectPath(projectId)}/resume`, undefined, {
        headers: idempotencyHeaders(idempotencyKey),
      }),
    cancelProject: (projectId, nameConfirmation, idempotencyKey) =>
      client.post<Command>(
        `${projectPath(projectId)}/cancellation`,
        { name_confirmation: nameConfirmation },
        { headers: idempotencyHeaders(idempotencyKey) },
      ),
    completeProject: (projectId, idempotencyKey) =>
      client.post<Command>(`${projectPath(projectId)}/completion`, undefined, {
        headers: idempotencyHeaders(idempotencyKey),
      }),
    getCosts: (projectId) => client.get<ProjectCosts>(`${projectPath(projectId)}/costs`),
    listWaves: (projectId, cursor) =>
      client.get<Page<WaveSummary>>(`${projectPath(projectId)}/waves`, {
        query: { cursor, limit: 100 },
      }),
  }
}

export const projectsApi = createProjectsApi()

/** Items as the api takes them: trimmed descriptions, ids kept (never reused, PL-13). */
export function constraintsPayload(items: ConstraintDraft[]): NewConstraints {
  return {
    items: items.map((item) => ({
      ...(item.id ? { id: item.id } : {}),
      category: item.category,
      description: item.description.trim(),
    })),
  }
}

export function constraintsAreValid(items: ConstraintDraft[]): boolean {
  return items.every((item) => {
    const length = item.description.trim().length
    return length >= CONSTRAINT_DESCRIPTION_MIN && length <= CONSTRAINT_DESCRIPTION_MAX
  })
}
