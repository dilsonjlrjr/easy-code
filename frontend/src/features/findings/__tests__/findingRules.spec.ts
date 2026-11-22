import { beforeEach, describe, expect, it } from 'vitest'
import type { FindingSummary } from '../api/findingsApi'
import {
  blockingText,
  buildDecision,
  readFindingQuery,
  savePendingFindingDecision,
  statusKey,
  takePendingFindingDecision,
  writeFindingQuery,
  type DecisionForm,
} from '../composables/findingRules'

const STAGES = ['requirements', 'architecture', 'implementation', 'security']

function aFinding(overrides: Partial<FindingSummary> = {}): FindingSummary {
  return {
    id: 'ACH-01',
    title: 'Senha no log',
    severity: 'high',
    category: 'vulnerabilidade',
    status: 'in_rework',
    origin_stage: 'security',
    destination_stage: 'implementation',
    author: 'security-agent',
    blocking: true,
    ...overrides,
  }
}

function aForm(overrides: Partial<DecisionForm> = {}): DecisionForm {
  return {
    action: 'accept_risk',
    justification: 'Risco conhecido e aceito pela área de segurança.',
    validUntil: '',
    modifiedBy: undefined,
    startingPoints: [],
    discardReason: undefined,
    duplicateOf: '',
    ...overrides,
  }
}

const NOW = new Date(2026, 10, 22, 10, 0, 0)

describe('finding filters in the address (ESP-30 3.1, CA-30-3)', () => {
  it('opens on the "open" shortcut', () => {
    const query = readFindingQuery({}, STAGES)
    expect(query.shortcut).toBe('open')
    expect(query.status).toEqual([
      'open',
      'in_rework',
      'revalidating',
      'escalated',
      'awaiting_scope',
    ])
  })

  it('keeps severity and destination, and round-trips through the query', () => {
    const query = readFindingQuery(
      { severity: ['critical', 'high'], destination_stage: 'implementation', status: 'escalated' },
      STAGES,
    )
    expect(query).toEqual({
      shortcut: 'escalated',
      status: ['escalated'],
      severity: ['critical', 'high'],
      destination: 'implementation',
    })
    expect(readFindingQuery(writeFindingQuery(query), STAGES)).toEqual(query)
  })

  it('"all" is explicit and an invalid value goes back to the default (ER-30-12)', () => {
    expect(readFindingQuery({ status_filter: 'all' }, STAGES).status).toEqual([])
    expect(readFindingQuery({ status: 'bogus' }, STAGES).shortcut).toBe('open')
    expect(readFindingQuery({ destination_stage: 'nowhere' }, STAGES).destination).toBeUndefined()
  })
})

describe('what a finding blocks (ER-30-4, ER-30-8)', () => {
  it('a blocking finding blocks the gate of its origin stage and the release', () => {
    expect(blockingText(aFinding())).toEqual({
      key: 'findings.blocking.gateAndRelease',
      stage: 'security',
    })
  })

  it('an accepted finding blocks nothing, unless its acceptance expired (PL-68)', () => {
    expect(blockingText(aFinding({ status: 'accepted', blocking: false }))).toBeUndefined()
    const expired = aFinding({ status: 'accepted', blocking: false, acceptance_expired: true })
    expect(blockingText(expired)).toEqual({ key: 'findings.blocking.expired' })
    expect(statusKey(expired)).toBe('findings.status.accepted_expired')
  })
})

describe('decision form (ESP-20 4, US-33 CA-2, CA-6)', () => {
  it('refuses a justification shorter than 10 characters', () => {
    expect(buildDecision(aForm({ justification: '  curta ' }), NOW).errors.justification).toBe(
      'findings.decision.errors.justification',
    )
  })

  it('refuses a validity before today and accepts today', () => {
    expect(buildDecision(aForm({ validUntil: '2026-11-21' }), NOW).errors.validUntil).toBeDefined()
    expect(buildDecision(aForm({ validUntil: '2026-11-22' }), NOW).decision).toEqual({
      action: 'accept_risk',
      body: {
        justification: 'Risco conhecido e aceito pela área de segurança.',
        valid_until: '2026-11-22',
      },
    })
  })

  it('a scope change needs who changes and at least one starting artifact (CA-20-15)', () => {
    const missing = buildDecision(aForm({ action: 'decide_scope_change' }), NOW)
    expect(Object.keys(missing.errors).sort()).toEqual(['modifiedBy', 'startingPoints'])
    const ok = buildDecision(
      aForm({ action: 'decide_scope_change', modifiedBy: 'human', startingPoints: ['US-03'] }),
      NOW,
    )
    expect(ok.decision?.body).toEqual({
      justification: 'Risco conhecido e aceito pela área de segurança.',
      modified_by: 'human',
      starting_point_artifacts: ['US-03'],
    })
  })

  it('a duplicate discard needs the original finding id', () => {
    const form = aForm({ action: 'discard', discardReason: 'duplicate', duplicateOf: 'x' })
    expect(buildDecision(form, NOW).errors.duplicateOf).toBeDefined()
    expect(buildDecision({ ...form, duplicateOf: 'ACH-02' }, NOW).decision?.body).toMatchObject({
      reason: 'duplicate',
      duplicate_of: 'ACH-02',
    })
  })
})

describe('decision kept across the SSO step-up (ER-20-16)', () => {
  beforeEach(() => {
    window.sessionStorage.clear()
  })

  it('is taken once, for the same finding only', () => {
    const pending = {
      projectId: 'p1',
      findingId: 'ACH-01',
      decision: {
        action: 'authorize_new_rework' as const,
        body: { justification: 'mais uma vez' },
      },
    }
    savePendingFindingDecision(pending)
    expect(takePendingFindingDecision('p1', 'ACH-02')).toBeUndefined()
    savePendingFindingDecision(pending)
    expect(takePendingFindingDecision('p1', 'ACH-01')).toEqual(pending)
    expect(takePendingFindingDecision('p1', 'ACH-01')).toBeUndefined()
  })
})
