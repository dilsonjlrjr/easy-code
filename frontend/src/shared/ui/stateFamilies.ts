/** ESP-00 3.2: each state belongs to one family, which fixes its color; the text is always shown. */
export type StateFamily = 'working' | 'waiting-person' | 'approved' | 'problem' | 'stopped'

export type StateMachine =
  'project' | 'stage' | 'wave' | 'artifact' | 'gate' | 'command' | 'finding'

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
  finding: {
    open: 'problem',
    in_rework: 'working',
    revalidating: 'working',
    escalated: 'waiting-person',
    awaiting_scope: 'waiting-person',
    accepted: 'approved',
    closed: 'approved',
    discarded: 'stopped',
  },
}

/** Canvas chip classes (easycode.css): indigo working, amber waiting, green approved, red problem, gray stopped. */
export type ChipTone = 'info' | 'warn' | 'ok' | 'crit' | 'mute' | 'vio'

const FAMILY_TONES: Record<StateFamily, ChipTone> = {
  working: 'info',
  'waiting-person': 'warn',
  approved: 'ok',
  problem: 'crit',
  stopped: 'mute',
}

/** Where the canvas "Estados" board paints a state apart from its family. */
const TONE_EXCEPTIONS: Partial<Record<StateMachine, Record<string, ChipTone>>> = {
  wave: { awaiting_user: 'vio' },
  finding: { open: 'mute', escalated: 'crit', awaiting_scope: 'crit' },
}

export function stateTone(machine: StateMachine, state: string): ChipTone {
  return TONE_EXCEPTIONS[machine]?.[state] ?? FAMILY_TONES[stateFamily(machine, state)]
}

export function stateFamily(machine: StateMachine, state: string): StateFamily {
  return FAMILIES[machine][state] ?? 'stopped'
}
