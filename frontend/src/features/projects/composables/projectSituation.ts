import type { LocationQuery } from 'vue-router'
import type { WaveState } from '@/shared/api/contract'
import { moneyShare } from '@/shared/format/formatters'
import type { Project, ProjectSummary } from '../api/projectsApi'

/** ESP-04 3: the filters of the list, in the order of the screen (US-50 CA-2). */
export const STATE_FILTERS = ['in_progress', 'created', 'paused', 'completed', 'canceled'] as const
export type StateFilter = 'all' | (typeof STATE_FILTERS)[number]

/** The state filter in the address, so a link keeps it (ESP-04 2). */
export function stateFilterFrom(query: LocationQuery): StateFilter {
  const raw = Array.isArray(query.state) ? query.state[0] : query.state
  return STATE_FILTERS.find((value) => value === raw) ?? 'all'
}

export const SEVERITIES = ['critical', 'high', 'medium', 'low'] as const
export type Severity = (typeof SEVERITIES)[number]

/** CA-04-1: open findings by severity, in severity order, only the ones with findings. */
export function openFindings(summary: ProjectSummary | undefined): Array<[Severity, number]> {
  const open = summary?.open_findings ?? {}
  return SEVERITIES.flatMap((severity) => {
    const count = open[severity] ?? 0
    return count > 0 ? [[severity, count] as [Severity, number]] : []
  })
}

/** ESP-03 3: critical and high together, the indicator of the Home page. */
export function criticalOrHigh(projects: Project[]): number {
  return projects.reduce(
    (sum, project) =>
      sum +
      (project.summary?.open_findings?.critical ?? 0) +
      (project.summary?.open_findings?.high ?? 0),
    0,
  )
}

/** ESP-04 3 (PL-16): the share of the stop limit already spent; undefined without a budget. */
export function budgetUsed(summary: ProjectSummary | undefined): number | undefined {
  return moneyShare(summary?.accumulated_cost, summary?.stop_limit)
}

const ACTIVE_WAVE_STATES: readonly WaveState[] = [
  'queued',
  'running',
  'awaiting_user',
  'pausing',
  'paused',
]

export interface RunningAgent {
  project: Pick<Project, 'id' | 'name'>
  wave: string
  agent: string
  state: WaveState
}

/** ESP-03 3: the non-final waves of every project, the "agents running" list. */
export function runningAgents(projects: Project[]): RunningAgent[] {
  return projects.flatMap((project) =>
    (project.summary?.active_waves ?? [])
      .filter((wave) => ACTIVE_WAVE_STATES.includes(wave.state))
      .map((wave) => ({
        project: { id: project.id, name: project.name },
        wave: wave.wave,
        agent: wave.agent,
        state: wave.state,
      })),
  )
}
