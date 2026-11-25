import type { ProblemDetails } from './problemDetails'

/**
 * Types shared by several features, mirroring `docs/arquitetura/api/openapi.yaml` (components.schemas).
 * Field names are the contract's (`snake_case`); feature `api/` modules add the resource-specific types.
 */

export type ProjectState = 'created' | 'in_progress' | 'paused' | 'completed' | 'canceled'

export type StageState =
  | 'blocked'
  | 'ready'
  | 'running'
  | 'awaiting_gate'
  | 'approved'
  | 'returned'
  | 'reopened'
  | 'skipped'

export type WaveState =
  | 'queued'
  | 'running'
  | 'awaiting_user'
  | 'pausing'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'canceled'

export type ArtifactState = 'draft' | 'in_review' | 'approved' | 'replaced'

export type GateState = 'evaluating' | 'awaiting_human' | 'approved' | 'rejected' | 'invalidated'

export type StageId =
  | 'requirements'
  | 'architecture'
  | 'data'
  | 'design'
  | 'validation'
  | 'implementation'
  | 'qa'
  | 'security'
  | 'release'
  | 'qa-plan'
  | 'security-threats'

export type AgentId =
  | 'requirements-agent'
  | 'architect-agent'
  | 'dba-agent'
  | 'designer-agent'
  | 'developer-agent'
  | 'qa-agent'
  | 'security-agent'
  | 'orchestrator'

export type ProviderId = 'anthropic' | 'openai' | 'claude-code'

export type ProjectRole = 'lead' | 'approver' | 'member'

export type Capacity = 'tool_use' | 'context_tokens' | 'prompt_cache' | 'image_input'

export type AvailableAction =
  | 'start'
  | 'pause'
  | 'pause_now'
  | 'resume'
  | 'restart'
  | 'cancel'
  | 'answer'
  | 'approve'
  | 'reject'
  | 'accept_risk'
  | 'authorize_new_rework'
  | 'decide_scope_change'
  | 'discard'
  | 'record_publication'

/** Decimal amount as text, never a float on the wire. */
export interface Money {
  value: string
  currency: string
}

export interface Tokens {
  input: number
  output: number
  read_cache: number
  write_cache: number
}

export interface DetailedCost {
  useful: Money
  lost: Money
  total: Money
  rework?: Money
  estimated_lost?: Money
  per_provider?: Array<{ provider: ProviderId; total: Money }>
}

export interface TimeSpent {
  execution_ms: number
  cycle_ms: number
}

export interface UserRef {
  id: string
  name: string
}

export interface ModelRef {
  provider: ProviderId
  model: string
}

export interface ModelPair {
  reasoning: ModelRef
  execution: ModelRef
}

export interface Page<TItem> {
  items: TItem[]
  next_cursor: string | null
}

export interface Command {
  id: string
  type: string
  target: { type: 'project' | 'wave' | 'artifact'; id: string }
  state: 'pending' | 'applied' | 'refused'
  requested_by: UserRef
  origin: 'web'
  requested_at: string
  completed_at?: string | null
  lost_cost?: Money
  refusal?: ProblemDetails
}
