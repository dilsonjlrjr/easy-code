import {
  mdiAccountClockOutline,
  mdiAlertCircleOutline,
  mdiCheckCircleOutline,
  mdiCogSyncOutline,
  mdiPauseCircleOutline,
} from '@mdi/js'

/** ESP-00 3.2: each state belongs to one family, which fixes its color; the text is always shown. */
export type StateFamily = 'working' | 'waiting-person' | 'approved' | 'problem' | 'stopped'

export type StateMachine = 'project' | 'stage' | 'wave' | 'artifact' | 'gate' | 'command'

const FAMILIES: Record<StateMachine, Record<string, StateFamily>> = {
  project: {
    created: 'stopped',
    in_progress: 'working',
    paused: 'waiting-person',
    completed: 'approved',
    canceled: 'stopped',
  },
  stage: {
    not_started: 'stopped',
    blocked: 'stopped',
    ready: 'stopped',
    running: 'working',
    awaiting_gate: 'waiting-person',
    approved: 'approved',
    returned: 'problem',
    reopened: 'problem',
    skipped: 'stopped',
  },
  wave: {
    queued: 'stopped',
    running: 'working',
    awaiting_user: 'waiting-person',
    pausing: 'waiting-person',
    paused: 'waiting-person',
    completed: 'approved',
    failed: 'problem',
    canceled: 'stopped',
  },
  artifact: {
    draft: 'stopped',
    in_review: 'waiting-person',
    approved: 'approved',
    replaced: 'stopped',
  },
  gate: {
    evaluating: 'working',
    awaiting_human: 'waiting-person',
    approved: 'approved',
    rejected: 'problem',
    invalidated: 'stopped',
  },
  command: {
    pending: 'working',
    applied: 'approved',
    refused: 'problem',
  },
}

export const FAMILY_COLORS: Record<StateFamily, string> = {
  working: 'info',
  'waiting-person': 'warning',
  approved: 'success',
  problem: 'error',
  stopped: 'secondary',
}

export const FAMILY_ICONS: Record<StateFamily, string> = {
  working: mdiCogSyncOutline,
  'waiting-person': mdiAccountClockOutline,
  approved: mdiCheckCircleOutline,
  problem: mdiAlertCircleOutline,
  stopped: mdiPauseCircleOutline,
}

export function stateFamily(machine: StateMachine, state: string): StateFamily {
  return FAMILIES[machine][state] ?? 'stopped'
}
