import { apiClient } from '@/shared/api/apiClient'
import type {
  AvailableAction,
  Command,
  DetailedCost,
  ModelPair,
  Money,
  Page,
  ProviderId,
  TimeSpent,
  Tokens,
} from '@/shared/api/contract'
import type { HttpClient } from '@/shared/api/httpClient'
import { idempotencyHeaders } from '@/shared/api/idempotencyKey'
import type { CloudEvent } from '@/shared/events/eventStream'
import type { WaveSummary } from '@/features/projects/api/projectsApi'

export interface Wave extends WaveSummary {
  models: ModelPair
  versions?: {
    instructions?: string
    agent_configuration?: number
    skills?: Array<{ skill: string; version: number }>
    price_table?: string
  }
  tokens: Tokens
  cost: DetailedCost
  time: TimeSpent
  result?: 'success' | 'failed' | 'canceled' | null
  failure_cause?: string | null
  /** US-29: schema errors per artifact (JSON pointer in the header and message). */
  artifact_errors?: Array<{ artifact: string; path: string; message: string }>
  pause?: { mode?: 'cooperative' | 'immediate'; cause?: string; requested_at?: string } | null
  last_checkpoint?: number | null
  question_open?: string | null
  expected_outputs?: Array<{
    type: string
    artifact?: string | null
    version?: number | null
    status: 'pending' | 'writing' | 'written'
  }>
  attempts: Array<{ number: number; final_state: string; failure_cause?: string | null }>
  available_actions: AvailableAction[]
}

export interface CallRecord {
  number: number
  type: 'model' | 'tool'
  start: string
  duration_ms: number
  result: 'success' | 'error' | 'aborted' | 'refused'
  lost?: boolean
  attempt?: number
  provider?: ProviderId
  model?: string
  role?: 'reasoning' | 'execution'
  tokens?: Tokens
  cost?: Money
  lost_cost?: boolean
  estimated_cost?: boolean
  tool?: string
  summary?: string | null
  refusal_reason?: string
  error?: string
}

export interface Checkpoint {
  number: number
  written_at: string
  after: 'model_call' | 'tool' | null
  attempt?: number
  accumulated_tokens: Tokens
  accumulated_cost: DetailedCost
}

export interface QuestionItem {
  number: number
  text: string
  suggestion?: string
  origin?: { source?: string; excerpt?: string } | null
}

export interface Question {
  id: string
  items: QuestionItem[]
  opened_at: string
  answer?: {
    record?: {
      per?: { type: string; id: string; name?: string }
      when?: string
      origin?: string | null
    }
    answers?: Array<{ number: number; text: string }>
  } | null
}

export interface EventsPage {
  items: CloudEvent[]
  last_sequence: number
  more: boolean
}

export interface WavesApi {
  getWave: (projectId: string, waveId: string) => Promise<Wave>
  listCalls: (projectId: string, waveId: string) => Promise<CallRecord[]>
  listCheckpoints: (projectId: string, waveId: string) => Promise<Checkpoint[]>
  listQuestions: (projectId: string, waveId: string) => Promise<Question[]>
  answerQuestion: (
    projectId: string,
    waveId: string,
    questionId: string,
    answers: Array<{ number: number; text: string }>,
    idempotencyKey: string,
  ) => Promise<Command>
  listEvents: (projectId: string, waveId: string) => Promise<EventsPage>
}

function wavePath(projectId: string, waveId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/waves/${encodeURIComponent(waveId)}`
}

const PAGE_LIMIT = 100
const EVENT_PAGE_LIMIT = 500

async function readAllPages<TItem>(
  read: (cursor: string | undefined) => Promise<Page<TItem>>,
): Promise<TItem[]> {
  const items: TItem[] = []
  let cursor: string | undefined
  do {
    const page = await read(cursor)
    items.push(...page.items)
    cursor = page.next_cursor ?? undefined
  } while (cursor)
  return items
}

export function createWavesApi(client: HttpClient = apiClient): WavesApi {
  return {
    getWave: (projectId, waveId) => client.get<Wave>(wavePath(projectId, waveId)),
    listCalls: (projectId, waveId) =>
      readAllPages((cursor) =>
        client.get<Page<CallRecord>>(`${wavePath(projectId, waveId)}/calls`, {
          query: { cursor, limit: PAGE_LIMIT },
        }),
      ),
    listCheckpoints: (projectId, waveId) =>
      readAllPages((cursor) =>
        client.get<Page<Checkpoint>>(`${wavePath(projectId, waveId)}/checkpoints`, {
          query: { cursor, limit: PAGE_LIMIT },
        }),
      ),
    listQuestions: async (projectId, waveId) =>
      (await client.get<{ items: Question[] }>(`${wavePath(projectId, waveId)}/questions`)).items,
    answerQuestion: (projectId, waveId, questionId, answers, idempotencyKey) =>
      client.post<Command>(
        `${wavePath(projectId, waveId)}/questions/${encodeURIComponent(questionId)}/answer`,
        { answers },
        { headers: idempotencyHeaders(idempotencyKey) },
      ),
    /** US-09 CA-4: the full history of the wave, page after page. */
    listEvents: async (projectId, waveId) => {
      const items: CloudEvent[] = []
      let since = 0
      let lastSequence = 0
      for (;;) {
        const page = await client.get<EventsPage>(
          `/projects/${encodeURIComponent(projectId)}/events/history`,
          { query: { since_sequence: since, waveId, limit: EVENT_PAGE_LIMIT } },
        )
        items.push(...page.items)
        lastSequence = page.last_sequence
        if (!page.more || page.last_sequence <= since) break
        since = page.last_sequence
      }
      return { items, last_sequence: lastSequence, more: false }
    },
  }
}

export const wavesApi = createWavesApi()
