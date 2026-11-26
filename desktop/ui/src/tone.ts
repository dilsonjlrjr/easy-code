// Color family of a state chip, as in the interface canvas (Fundamentos › Estados): indigo working, amber or
// violet waiting for a person, green approved, red problem, gray stopped. The state is always in the text
// too (RNF-19); the color only groups.
export type Tone = 'ok' | 'warn' | 'crit' | 'info' | 'mute' | 'vio'

const TONES: Record<string, Record<string, Tone>> = {
  project: {
    created: 'mute',
    in_progress: 'info',
    paused: 'warn',
    completed: 'ok',
    canceled: 'mute',
  },
  stage: {
    not_started: 'mute',
    blocked: 'mute',
    ready: 'mute',
    skipped: 'mute',
    running: 'info',
    awaiting_gate: 'warn',
    approved: 'ok',
    returned: 'crit',
    reopened: 'crit',
  },
  wave: {
    queued: 'mute',
    running: 'info',
    awaiting_user: 'vio',
    pausing: 'warn',
    paused: 'warn',
    completed: 'ok',
    failed: 'crit',
    canceled: 'mute',
  },
  gate: {
    evaluating: 'info',
    awaiting_human: 'warn',
    approved: 'ok',
    rejected: 'crit',
    invalidated: 'mute',
  },
  check: { passed: 'ok', failed: 'crit', pending: 'info', not_applicable: 'mute' },
  call: { success: 'ok', error: 'crit', aborted: 'warn', refused: 'crit' },
  pending: { gate: 'warn', question: 'vio', finding_escalated: 'crit', budget: 'warn' },
}

/** The chip classes of a state of a machine; unknown states are gray. */
export function chip(machine: string, state?: string | null): string {
  return `chip ${(state && TONES[machine]?.[state]) || 'mute'}`
}

/** Color of the segment of a stage in the progress bar of a project. */
export function segment(state?: string | null): string {
  const tone = (state && TONES.stage?.[state]) || 'mute'
  return `sg sg-${tone}`
}
