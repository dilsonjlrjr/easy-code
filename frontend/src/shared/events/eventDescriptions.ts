import type { CloudEvent } from './eventStream'

type Translate = (key: string, values?: Record<string, unknown>) => string

/** `br.easycode.wave.started.v1` → `wave.started`. */
export function shortEventType(type: string): string {
  return type.replace(/^br\.easycode\./, '').replace(/\.v\d+$/, '')
}

/** Event types with a sentence of their own in the catalog (`events.<group>.<name>`). */
export const DESCRIBED_EVENTS = new Set([
  'project.created',
  'project.started',
  'project.paused',
  'project.resumed',
  'project.completed',
  'project.canceled',
  'stage.unblocked',
  'stage.started',
  'stage.submitted_to_gate',
  'stage.approved',
  'stage.returned',
  'stage.blocked',
  'gate.opened',
  'gate.awaiting_approval',
  'gate.approved',
  'gate.rejected',
  'artifact.created',
  'artifact.submitted_for_review',
  'artifact.approved',
  'artifact.returned',
  'wave.created',
  'wave.started',
  'wave.checkpoint_written',
  'wave.retry_scheduled',
  'wave.resumed_after_crash',
  'wave.question_opened',
  'wave.question_answered',
  'wave.pause_requested',
  'wave.paused',
  'wave.resumed',
  'wave.completed',
  'wave.failed',
  'wave.restarted',
  'wave.canceled',
  'wave.task_failed',
  'wave.model_selected',
  'wave.tool_called',
])

/**
 * One sentence per domain event for the wave history and the live announcements. Only ids and
 * enumerations from the payload are used: no agent-generated text (ER-00-20).
 */
export function describeEvent(t: Translate, event: CloudEvent): string {
  const type = shortEventType(event.type)
  const data = event.data
  const text = (value: unknown) =>
    typeof value === 'string' || typeof value === 'number' ? value : ''
  const values = {
    wave: text(data.wave ?? event.waveid),
    stage: typeof data.stage === 'string' ? t(`stages.${data.stage}`) : '',
    checkpoint: text(data.checkpoint),
    question: text(data.question),
    items: text(data.items),
    artifact: text(data.artifact),
    tool: text(data.tool),
    model: text(data.model),
    attempt: text(data.attempt),
    task: text(data.task),
    // `wave.task_failed`: build, tests or lint (catalog labels, never free text).
    cause:
      typeof data.cause === 'string' && ['build', 'tests', 'lint'].includes(data.cause)
        ? t(`taskFailureCauses.${data.cause}`)
        : text(data.cause),
    failures: text(data.consecutive_failures),
    seconds: typeof data.delay_ms === 'number' ? Math.round(data.delay_ms / 1000) : '',
  }
  if (DESCRIBED_EVENTS.has(type)) return t(`events.${type}`, values)
  return t('events.generic', { type })
}
