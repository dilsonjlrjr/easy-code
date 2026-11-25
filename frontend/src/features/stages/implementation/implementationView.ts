import type { WaveSummary } from '@/features/projects/api/projectsApi'
import type { FindingSummary } from '@/features/findings/api/findingsApi'
import type { CommandCategory, SandboxExecution } from '@/features/waves/api/wavesApi'
import {
  executionOutcome,
  type ExecutionOutcome,
} from '@/features/waves/composables/sandboxEvidence'

/**
 * Pure reading of the Implementation stage (ESP-14 "Critério de saída" situation and "Retrabalho";
 * canvas EtapaImplementacao): what the last Developer wave's sandbox said about build, tests and
 * lint (US-41), and the internal rework cycle of each finding sent to the Implementation (US-32).
 */

/** The three checks the canvas shows as chips ("build verde", "12 testes passando", "lint limpo"). */
export const CHECK_CATEGORIES = [
  'build',
  'tests',
  'lint',
] as const satisfies readonly CommandCategory[]
export type CheckCategory = (typeof CHECK_CATEGORIES)[number]

export interface CheckSituation {
  category: CheckCategory
  outcome: ExecutionOutcome | 'not_run'
  /** The last command of the category; undefined when the wave never ran one. */
  execution?: SandboxExecution
}

function newestFirst<T extends { created_at: string; id: string }>(items: T[]): T[] {
  return [...items].sort(
    (left, right) =>
      right.created_at.localeCompare(left.created_at) || right.id.localeCompare(left.id),
  )
}

/** The newest wave of the Developer in the stage (one wave per T, PL-20; rework included). */
export function latestDeveloperWave(waves: WaveSummary[]): WaveSummary | undefined {
  return newestFirst(
    waves.filter((wave) => wave.agent === 'developer-agent' && wave.state !== 'queued'),
  )[0]
}

/** Per check, the last command of its category in the wave (ER-14-9..11): it says how it ended. */
export function checkSituations(executions: SandboxExecution[]): CheckSituation[] {
  return CHECK_CATEGORIES.map((category) => {
    const runs = executions
      .filter((execution) => execution.category === category)
      .sort((left, right) => left.start.localeCompare(right.start))
    const execution = runs[runs.length - 1]
    return execution
      ? { category, outcome: executionOutcome(execution), execution }
      : { category, outcome: 'not_run' }
  })
}

export type ReworkPurpose = 'rework' | 'revalidation'
export const REWORK_PURPOSES: readonly ReworkPurpose[] = ['rework', 'revalidation']

export interface ReworkItem {
  finding: FindingSummary
  /** Rework waves of the Developer and revalidation waves of the emitter, oldest first. */
  waves: WaveSummary[]
}

/**
 * ESP-14 "Retrabalho": every finding whose destination is the Implementation, in the order the api
 * gives (severity, then opening), with the rework and revalidation waves it opened (ER-14-13).
 */
export function reworkItems(findings: FindingSummary[], waves: WaveSummary[]): ReworkItem[] {
  return findings.map((finding) => ({
    finding,
    waves: newestFirst(
      waves.filter(
        (wave) =>
          wave.finding === finding.id &&
          (REWORK_PURPOSES as readonly string[]).includes(wave.purpose),
      ),
    ).reverse(),
  }))
}
