import { describe, expect, it } from 'vitest'
import type { AuditRecord } from '../api/auditApi'
import {
  actionText,
  actorText,
  apiQueryOf,
  appendPage,
  filtersFromQuery,
  hasFilters,
  periodIsValid,
  queryFromFilters,
  targetLocation,
} from '../composables/auditView'

const PROJECT = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

function record(id: string, overrides: Partial<AuditRecord> = {}): AuditRecord {
  return {
    id,
    when: '2026-11-22T10:00:00-03:00',
    actor: { type: 'human', user: { id: 'ana', name: 'Ana Souza' } },
    origin: 'web',
    action: 'gate.approve',
    target: { type: 'gate_round', id: 'gate.requirements/1' },
    ...overrides,
  }
}

describe('audit trail view (ESP-27, US-35)', () => {
  it('opens newest first and keeps the filters in the address (ER-27-12, CA-27-2)', () => {
    expect(filtersFromQuery({})).toEqual({ actions: [], from: '', until: '', order: 'desc' })
    const filters = filtersFromQuery({
      action: ['gate.approve', 'wave.pause'],
      from: '2026-11-01',
      until: '2026-11-30',
      order: 'asc',
    })
    expect(filters).toEqual({
      actions: ['gate.approve', 'wave.pause'],
      from: '2026-11-01',
      until: '2026-11-30',
      order: 'asc',
    })
    expect(queryFromFilters(filters)).toEqual({
      action: ['gate.approve', 'wave.pause'],
      from: '2026-11-01',
      until: '2026-11-30',
      order: 'asc',
    })
    expect(queryFromFilters(filtersFromQuery({ from: 'yesterday' }))).toEqual({
      action: undefined,
      from: undefined,
      until: undefined,
      order: undefined,
    })
    expect(hasFilters(filtersFromQuery({ action: 'gate.approve' }))).toBe(true)
    expect(hasFilters(filtersFromQuery({ order: 'asc' }))).toBe(false)
  })

  it('needs from ≤ until (ESP-27 4)', () => {
    expect(periodIsValid({ from: '2026-11-02', until: '2026-11-01' })).toBe(false)
    expect(periodIsValid({ from: '2026-11-01', until: '2026-11-01' })).toBe(true)
    expect(periodIsValid({ from: '', until: '2026-11-01' })).toBe(true)
  })

  it('sends the whole days of the period as date-times', () => {
    const query = apiQueryOf(
      { actions: ['gate.approve'], from: '2026-11-01', until: '2026-11-02', order: 'desc' },
      'cursor-2',
    )
    expect(query.actions).toEqual(['gate.approve'])
    expect(query.cursor).toBe('cursor-2')
    expect(new Date(query.from ?? '').getTime()).toBe(new Date('2026-11-01T00:00:00').getTime())
    expect(new Date(query.until ?? '').getTime()).toBe(
      new Date('2026-11-02T23:59:59.999').getTime(),
    )
  })

  it('appends the next page without repeating records (CA-27-8)', () => {
    const shown = [record('a'), record('b')]
    expect(appendPage(shown, [record('b'), record('c')]).map((item) => item.id)).toEqual([
      'a',
      'b',
      'c',
    ])
  })

  it('links targets that have a screen (ESP-27 2)', () => {
    expect(targetLocation(record('a'), PROJECT)).toEqual({
      name: 'gate',
      params: { projectId: PROJECT, gateId: 'gate.requirements', round: '1' },
    })
    expect(
      targetLocation(
        record('a', { target: { type: 'gate', id: 'gates/gate.data/rounds/2' } }),
        PROJECT,
      ),
    ).toEqual({ name: 'gate', params: { projectId: PROJECT, gateId: 'gate.data', round: '2' } })
    expect(
      targetLocation(record('a', { target: { type: 'waves', id: 'waves/DBA-01' } }), PROJECT),
    ).toEqual({
      name: 'wave',
      params: { projectId: PROJECT, waveId: 'DBA-01' },
    })
    expect(
      targetLocation(record('a', { target: { type: 'finding', id: 'ACH-07' } }), PROJECT),
    ).toEqual({
      name: 'finding',
      params: { projectId: PROJECT, findingId: 'ACH-07' },
    })
    // agent, provider, budget and constraints have a label but no screen of their own.
    for (const type of ['agent', 'provider', 'budget', 'constraints'])
      expect(targetLocation(record('a', { target: { type, id: 'x' } }), PROJECT)).toBeUndefined()
    // Organization trail: the project of the record.
    expect(
      targetLocation(
        record('a', { project: PROJECT, target: { type: 'project', id: PROJECT } }),
        undefined,
      ),
    ).toEqual({ name: 'project', params: { projectId: PROJECT } })
    expect(targetLocation(record('a', { project: null }), undefined)).toBeUndefined()
  })

  it('names who did it and the action with its code (ESP-27 3)', () => {
    const catalog: Record<string, string> = {
      'agents.orchestrator': 'Orquestrador',
      'agents.dba-agent': 'DBA',
      'audit.actions.gate.approve': 'Gate aprovado',
    }
    const t = (key: string, values?: Record<string, unknown>) =>
      key === 'audit.actionWithCode'
        ? `${String(values?.label)} (${String(values?.code)})`
        : (catalog[key] ?? key)
    const has = (key: string) => key in catalog
    expect(actorText({ type: 'human', user: { id: 'ana', name: 'Ana Souza' } }, t, has)).toBe(
      'Ana Souza',
    )
    expect(actorText({ type: 'orchestrator', id: 'orchestrator' }, t, has)).toBe('Orquestrador')
    expect(actorText({ type: 'agent', id: 'dba-agent' }, t, has)).toBe('DBA')
    expect(actionText('gate.approve', t, has)).toBe('Gate aprovado (gate.approve)')
    expect(actionText('new.thing', t, has)).toBe('new.thing')
  })
})
