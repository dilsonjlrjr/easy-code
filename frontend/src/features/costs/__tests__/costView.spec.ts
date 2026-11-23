import { describe, expect, it } from 'vitest'
import type { StageSummary } from '@/features/projects/api/projectsApi'
import {
  budgetSituation,
  estimateExceedsBalance,
  groupLabel,
  groupingFrom,
  hasEstimatedLoss,
  nextStageOf,
  providerTotals,
  waveFilterOf,
} from '../composables/costView'

const BRL = (value: string) => ({ value, currency: 'BRL' })
const cost = (total: string, extra = {}) => ({
  useful: BRL(total),
  lost: BRL('0'),
  total: BRL(total),
  ...extra,
})

function stage(id: string, state: StageSummary['state']): StageSummary {
  return {
    id: id as StageSummary['id'],
    name: id,
    type: 'stage',
    agent: 'requirements-agent',
    depends_on: [],
    state,
  }
}

describe('cost view (ESP-25, US-38)', () => {
  it('reads the grouping from the address, stage by default (ESP-25 6: invalid → stage)', () => {
    expect(groupingFrom('agent')).toBe('agent')
    expect(groupingFrom('provider')).toBe('provider')
    expect(groupingFrom('nonsense')).toBe('stage')
    expect(groupingFrom(undefined)).toBe('stage')
  })

  it('labels stages and agents from the catalog and models as the api sends them', () => {
    const catalog: Record<string, string> = {
      'stages.requirements': 'Requisitos',
      'agents.dba-agent': 'DBA',
    }
    const t = (key: string) => catalog[key] ?? key
    const has = (key: string) => key in catalog
    expect(groupLabel('stage', 'requirements', t, has)).toBe('Requisitos')
    expect(groupLabel('agent', 'dba-agent', t, has)).toBe('DBA')
    expect(groupLabel('model', 'anthropic/model-x', t, has)).toBe('anthropic/model-x')
    expect(groupLabel('stage', 'unknown', t, has)).toBe('unknown')
  })

  it('opens the waves of stage and agent groups only (CA-25-7)', () => {
    expect(waveFilterOf('stage', 'data')).toEqual({ stage: 'data' })
    expect(waveFilterOf('agent', 'dba-agent')).toEqual({ agent: 'dba-agent' })
    expect(waveFilterOf('model', 'anthropic/x')).toBeUndefined()
    expect(waveFilterOf('provider', 'openai')).toBeUndefined()
  })

  it('shows the per-provider total only with more than one provider (CA-25-4)', () => {
    const one = cost('3', { per_provider: [{ provider: 'anthropic', total: BRL('3') }] })
    const two = cost('3', {
      per_provider: [
        { provider: 'anthropic', total: BRL('2') },
        { provider: 'openai', total: BRL('1') },
      ],
    })
    expect(providerTotals(one)).toEqual([])
    expect(providerTotals(two)).toHaveLength(2)
  })

  it('marks the estimated part of the lost cost only when there is one (CA-25-8)', () => {
    expect(hasEstimatedLoss(cost('1'))).toBe(false)
    expect(hasEstimatedLoss(cost('1', { estimated_lost: BRL('0') }))).toBe(false)
    expect(hasEstimatedLoss(cost('1', { estimated_lost: BRL('0.2') }))).toBe(true)
  })

  it('says the budget state by the project alert thresholds (CA-25-6)', () => {
    const budget = (used: number) => ({
      stop_limit: BRL('100'),
      balance: BRL('0'),
      used_percentage: used,
    })
    expect(budgetSituation(null)).toEqual({ kind: 'none' })
    expect(budgetSituation(budget(30))).toEqual({ kind: 'normal', percentage: 30 })
    expect(budgetSituation(budget(80))).toEqual({ kind: 'alert', percentage: 80, reached: 80 })
    expect(budgetSituation(budget(65), [60, 90])).toEqual({
      kind: 'alert',
      percentage: 65,
      reached: 60,
    })
    expect(budgetSituation(budget(100))).toEqual({ kind: 'exhausted', percentage: 100 })
  })
})

describe('estimate (US-40)', () => {
  it('says when the estimate exceeds the balance (cause estimate_exceeds_balance)', () => {
    expect(estimateExceedsBalance({ estimated: BRL('12.5') }, BRL('10'))).toBe(true)
    expect(estimateExceedsBalance({ estimated: BRL('10') }, BRL('10'))).toBe(false)
    expect(estimateExceedsBalance({ estimated: BRL('1') }, undefined)).toBe(false)
  })

  it('picks the next stage: first while created, ready, else blocked, none when final', () => {
    const stages = [stage('requirements', 'approved'), stage('architecture', 'ready')]
    expect(nextStageOf(stages, 'in_progress')?.id).toBe('architecture')
    expect(
      nextStageOf([stage('requirements', 'approved'), stage('data', 'blocked')], 'paused')?.id,
    ).toBe('data')
    expect(nextStageOf([stage('requirements', null)], 'created')?.id).toBe('requirements')
    expect(nextStageOf(stages, 'completed')).toBeUndefined()
  })
})
