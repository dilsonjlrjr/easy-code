import type { CallRecord, EndReason, SandboxExecution } from '../api/wavesApi'

/** Ends where the sandbox stopped the command at one of its resource limits (RNF-03). */
const LIMIT_REASONS: ReadonlySet<EndReason> = new Set([
  'time_limit',
  'memory_limit',
  'process_limit',
  'disk_limit',
  'output_limit',
])

export function endedByLimit(execution: SandboxExecution): boolean {
  return LIMIT_REASONS.has(execution.end_reason)
}

/**
 * Result of one command in words (US-41 CA-1, CA-3): it never depends on colour alone. A command
 * that completed with exit code 0 succeeded; with any other code it failed; a limit, an abort or an
 * executor error is said as such (`time_limit` reads "limit exceeded").
 */
export type ExecutionOutcome = 'succeeded' | 'failed' | 'limit' | 'aborted' | 'executor_error'

export function executionOutcome(execution: SandboxExecution): ExecutionOutcome {
  if (endedByLimit(execution)) return 'limit'
  if (execution.end_reason === 'aborted') return 'aborted'
  if (execution.end_reason === 'executor_error') return 'executor_error'
  return execution.exit_code === 0 ? 'succeeded' : 'failed'
}

/** The output start is cut at 64 KiB; the rest is only in the download. */
export function outputIsPartial(execution: SandboxExecution): boolean {
  const shown = new TextEncoder().encode(execution.output_start ?? '').length
  return execution.output_bytes > shown
}

/** File name of the downloaded output; the wave id and the command position, never the command. */
export function outputFileName(waveId: string, position: number): string {
  return `${waveId}-exec-${String(position)}.txt`
}

/** US-30 CA-1: tool calls the least-privilege guard refused in the wave. */
export function refusedToolCalls(calls: CallRecord[]): CallRecord[] {
  return calls.filter((call) => call.type === 'tool' && call.result === 'refused')
}
