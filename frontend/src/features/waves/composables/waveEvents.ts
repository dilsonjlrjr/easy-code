import type { Money, Tokens, WaveState } from '@/shared/api/contract'
import type { CloudEvent } from '@/shared/events/eventStream'
import { shortEventType } from '@/shared/events/eventDescriptions'

/** Domain events in `sequence` order, each CloudEvent once (US-09 CA-3: no gap, no duplicate). */
export function mergeEvents(existing: CloudEvent[], incoming: CloudEvent[]): CloudEvent[] {
  const byId = new Map(existing.map((event) => [event.id, event]))
  for (const event of incoming) if (!byId.has(event.id)) byId.set(event.id, event)
  return [...byId.values()].sort((left, right) => (left.sequence ?? 0) - (right.sequence ?? 0))
}

export function belongsToWave(event: CloudEvent, waveId: string): boolean {
  return event.waveid === waveId || event.data.wave === waveId
}

interface Transition {
  machine?: string
  to_state?: string
}

/** New wave state carried by a transition event, if the event is one. */
export function waveStateFrom(event: CloudEvent): WaveState | undefined {
  const transition = event.data.transition as Transition | undefined
  if (transition?.machine !== 'wave' || typeof transition.to_state !== 'string') return undefined
  return transition.to_state as WaveState
}

/** Running totals carried by `wave.checkpoint_written` (US-36 CA-2: cost after each call). */
export function checkpointTotals(
  event: CloudEvent,
): { tokens: Tokens; cost: Money; checkpoint: number } | undefined {
  if (shortEventType(event.type) !== 'wave.checkpoint_written') return undefined
  const tokens = event.data.accumulated_tokens as Tokens | undefined
  const cost = event.data.accumulated_cost as Money | undefined
  const checkpoint = event.data.checkpoint
  if (!tokens || !cost || typeof checkpoint !== 'number') return undefined
  return { tokens, cost, checkpoint }
}

export interface ResumeNotice {
  checkpoint: number
  lostCost?: Money
  estimated: boolean
}

/** US-11: the platform resumed the wave from its last checkpoint after a crash. */
export function resumeNoticeFrom(event: CloudEvent): ResumeNotice | undefined {
  if (shortEventType(event.type) !== 'wave.resumed_after_crash') return undefined
  return {
    checkpoint: typeof event.data.checkpoint === 'number' ? event.data.checkpoint : 0,
    lostCost: event.data.lost_cost as Money | undefined,
    estimated: event.data.estimated_lost_cost === true,
  }
}

/** Last resumption in the wave history, so the notice survives a page reload (CA-18-9). */
export function lastResumeNotice(events: CloudEvent[]): ResumeNotice | undefined {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index]
    const notice = event ? resumeNoticeFrom(event) : undefined
    if (notice) return notice
  }
  return undefined
}
