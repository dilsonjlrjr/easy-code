import type { CallRecord, Checkpoint } from '../api/wavesApi'

/** One line of the wave activity: a model call, a tool call or a checkpoint (ESP-18 section 3). */
export type TimelineEntry =
  | { kind: 'call'; key: string; time: string; attempt: number; call: CallRecord }
  | { kind: 'checkpoint'; key: string; time: string; attempt: number; checkpoint: Checkpoint }

/**
 * ESP-18 "Atividade" (PD-20, PL-39): calls and checkpoints in one timeline, oldest first. Entries
 * are ordered by attempt, then by time; at the same instant a call comes before the checkpoint
 * written after it, since a checkpoint is written after each call.
 */
export function waveTimeline(calls: CallRecord[], checkpoints: Checkpoint[]): TimelineEntry[] {
  const entries: TimelineEntry[] = [
    ...calls.map((call) => ({
      kind: 'call' as const,
      key: `call-${String(call.attempt ?? 1)}-${String(call.number)}`,
      time: call.start,
      attempt: call.attempt ?? 1,
      call,
    })),
    ...checkpoints.map((checkpoint) => ({
      kind: 'checkpoint' as const,
      key: `checkpoint-${String(checkpoint.attempt ?? 1)}-${String(checkpoint.number)}`,
      time: checkpoint.written_at,
      attempt: checkpoint.attempt ?? 1,
      checkpoint,
    })),
  ]
  return entries.sort((left, right) => {
    if (left.attempt !== right.attempt) return left.attempt - right.attempt
    const byTime = new Date(left.time).getTime() - new Date(right.time).getTime()
    if (byTime !== 0) return byTime
    if (left.kind !== right.kind) return left.kind === 'call' ? -1 : 1
    const leftNumber = left.kind === 'call' ? left.call.number : left.checkpoint.number
    const rightNumber = right.kind === 'call' ? right.call.number : right.checkpoint.number
    return leftNumber - rightNumber
  })
}

/** The checkpoint written last (latest attempt, latest time), or undefined when none was written. */
export function latestCheckpoint(checkpoints: Checkpoint[]): Checkpoint | undefined {
  return checkpoints.reduce<Checkpoint | undefined>((latest, checkpoint) => {
    if (!latest) return checkpoint
    const attempt = (checkpoint.attempt ?? 1) - (latest.attempt ?? 1)
    if (attempt !== 0) return attempt > 0 ? checkpoint : latest
    return new Date(checkpoint.written_at).getTime() >= new Date(latest.written_at).getTime()
      ? checkpoint
      : latest
  }, undefined)
}

/** Counts of the wave progress: calls by type (ESP-18 "progresso da Onda"). */
export function callCounts(calls: CallRecord[]): { model: number; tool: number } {
  return calls.reduce(
    (counts, call) => {
      counts[call.type] += 1
      return counts
    },
    { model: 0, tool: 0 },
  )
}
