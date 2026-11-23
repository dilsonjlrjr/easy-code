// Shapes of what the Go service returns: its own Status and errors, and the api schemas of openapi.yaml
// (only the fields the views use).

export type Phase = 'starting' | 'signed_out' | 'awaiting_approval' | 'signed_in'

export interface DeviceView {
  user_code: string
  verification_uri: string
  verification_uri_complete?: string
  expires_at: string
}

export interface CurrentUser {
  id: string
  name: string
  organization?: { name?: string }
}

export interface Status {
  phase: Phase
  reason?: string
  server?: string
  server_from_env: boolean
  locale: string
  mute_sound: boolean
  app_version: string
  server_version?: string
  version_mismatch: boolean
  user?: CurrentUser
  device?: DeviceView
}

export interface UIError {
  kind: string
  problem_type?: string
  status?: number
  title?: string
  detail?: string
}

export interface Money {
  value: string
  currency: string
}

export interface Project {
  id: string
  name: string
  state: string
  pause_reason?: string | null
  last_sequence?: number
  summary?: {
    current_stages?: { stage: string; state: string }[]
    gates_awaiting: number
    open_questions: number
    accumulated_cost: Money
  }
}

export interface ProjectsPage {
  items: Project[]
  next_cursor: string | null
}

export interface PendingItem {
  type: 'gate' | 'question' | 'finding_escalated' | 'budget'
  project: { id: string; name: string }
  stage?: string
  target: { type: string; id: string }
  opened_at: string
  title?: string
}

export interface PendingItemsPage {
  items: PendingItem[]
  total: number
}

export interface StageSummary {
  id: string
  name: string
  short_name?: string
  state: string | null
  gate?: string | null
  current_round?: number | null
  gate_state?: string | null
  active_waves?: number
}

export interface Workflow {
  stages: StageSummary[]
}

export interface WaveSummary {
  id: string
  stage: string
  agent: string
  state: string
  purpose: string
  attempt: number
  created_at: string
  cost_total?: Money
}

export interface ModelRef {
  provider?: string
  model?: string
}

export interface Wave extends WaveSummary {
  models?: { reasoning?: ModelRef; execution?: ModelRef }
  cost?: { total?: Money }
  failure_cause?: string | null
  question_open?: unknown
  pause?: { reason?: string } | null
}

export interface CallRecord {
  number: number
  type: 'model' | 'tool'
  start: string
  duration_ms: number
  result: string
  model?: string
  tool?: string
  role?: string
  cost?: Money
  summary?: string | null
}

export interface CallsPage {
  items: CallRecord[]
}

export interface GateRound {
  gate: string
  stage: string
  round: number
  state: string
  checks: { check: string; name?: string; status: string }[]
  blocking_findings?: { id: string; title: string; severity: string }[]
}

export interface CloudEvent {
  id: string
  type: string
  projectid?: string
  waveid?: string
  sequence?: number
  time?: string
  data?: Record<string, unknown>
}

export interface ConnectionState {
  stream: 'notices' | 'wave'
  state: 'connecting' | 'live' | 'reconnecting' | 'stopped'
  reconnected: boolean
}

export interface WebTarget {
  kind:
    | 'home'
    | 'pending'
    | 'project'
    | 'project_configuration'
    | 'wave'
    | 'gate_round'
    | 'finding'
    | 'account_local_client'
  project_id?: string
  wave_id?: string
  gate_id?: string
  round?: number
  finding_id?: string
}
