import type { AgentId, DetailedCost, Money, ProjectState, StageId } from '@/shared/api/contract'
import { compareMoney, isZeroMoney } from '@/shared/format/money'
import type { ProjectCosts, StageSummary } from '@/features/projects/api/projectsApi'
import { COST_GROUPINGS, type CostGrouping, type Estimate, type WaveFilter } from '../api/costsApi'

/** The grouping in the address (ESP-25 2); an unknown value falls back to the default (ER 400). */
export function groupingFrom(value: unknown): CostGrouping {
  return COST_GROUPINGS.find((item) => item === value) ?? 'stage'
}

/**
 * Text of a group key: stage and agent ids have catalog labels; models (`provider/model`) and
 * providers come from the api as they are (the screen never fixes a model name, PROJETO 10).
 */
export function groupLabel(
  grouping: CostGrouping,
  key: string,
  translate: (key: string) => string,
  has: (key: string) => boolean,
): string {
  const catalogKey =
    grouping === 'stage' ? `stages.${key}` : grouping === 'agent' ? `agents.${key}` : undefined
  return catalogKey && has(catalogKey) ? translate(catalogKey) : key
}

/** CA-25-7: stage and agent rows open the waves of the group; model and provider rows do not. */
export function waveFilterOf(grouping: CostGrouping, key: string): WaveFilter | undefined {
  if (grouping === 'stage') return { stage: key as StageId }
  if (grouping === 'agent') return { agent: key as AgentId }
  return undefined
}

/** ER-25-5 / CA-25-4: the per-provider total only when the pair mixes providers. */
export function providerTotals(total: DetailedCost): NonNullable<DetailedCost['per_provider']> {
  const items = total.per_provider ?? []
  return items.length > 1 ? items : []
}

/** CA-25-8: the estimated part of the lost cost (platform crash) is shown only when not zero. */
export function hasEstimatedLoss(cost: DetailedCost): boolean {
  return !isZeroMoney(cost.estimated_lost)
}

export type BudgetSituation =
  | { kind: 'none' }
  | { kind: 'normal'; percentage: number }
  | { kind: 'alert'; percentage: number; reached: number }
  | { kind: 'exhausted'; percentage: number }

/**
 * ER-25-6: percentage of the budget and its state. The alert thresholds are the project's
 * (`percentage_alert_limits`, default 50 and 80); at 100% or more the budget is exhausted.
 */
export function budgetSituation(
  budget: ProjectCosts['budget'],
  alertLimits: readonly number[] = [50, 80],
): BudgetSituation {
  const percentage = budget?.used_percentage
  if (!budget || percentage === undefined) return { kind: 'none' }
  if (percentage >= 100) return { kind: 'exhausted', percentage }
  const reached = [...alertLimits].sort((a, b) => b - a).find((limit) => percentage >= limit)
  return reached === undefined
    ? { kind: 'normal', percentage }
    : { kind: 'alert', percentage, reached }
}

/**
 * US-39 / PROJETO 10: the orchestrator does not start a wave whose estimate exceeds the balance;
 * the project pauses by budget instead (cause `estimate_exceeds_balance`). The estimate shows it
 * beforehand so the member can adjust the model pair or the limit (US-40).
 */
export function estimateExceedsBalance(
  estimate: Pick<Estimate, 'estimated'> | undefined,
  balance: Money | null | undefined,
): boolean {
  return compareMoney(estimate?.estimated, balance) === 1
}

/**
 * ESP-06 3.6: the stage that is ready, or the next one to be released (with the reason it waits);
 * the first stage while the project is created; none once the project is concluded or canceled.
 */
export function nextStageOf(
  stages: StageSummary[],
  projectState: ProjectState | undefined,
): StageSummary | undefined {
  if (projectState === 'completed' || projectState === 'canceled') return undefined
  if (projectState === 'created') return stages[0]
  return (
    stages.find((stage) => stage.state === 'ready') ??
    stages.find((stage) => stage.state === 'blocked')
  )
}
