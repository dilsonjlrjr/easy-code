import type { Tokens } from '@/shared/api/contract'
import type { WaveSummary } from '@/features/projects/api/projectsApi'

/**
 * ESP-06 3.5 (CA-06-11): the time of a wave in the project table, from its start to its end, or
 * to now while it is still open; undefined before it starts.
 */
export function waveElapsedMs(
  wave: Pick<WaveSummary, 'started_at' | 'ended_at'>,
  now: Date = new Date(),
): number | undefined {
  if (!wave.started_at) return undefined
  const end = wave.ended_at ? new Date(wave.ended_at) : now
  return Math.max(0, end.getTime() - new Date(wave.started_at).getTime())
}

/** Input and output tokens of the wave; the cache tokens are shown apart. */
export function waveTokenTotals(tokens: Tokens | undefined): { main: number; cache: number } {
  if (!tokens) return { main: 0, cache: 0 }
  return {
    main: tokens.input + tokens.output,
    cache: tokens.read_cache + tokens.write_cache,
  }
}
