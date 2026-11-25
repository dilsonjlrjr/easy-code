import { describe, expect, it } from 'vitest'
import type { WaveSummary } from '@/features/projects/api/projectsApi'
import type { CheckResult } from '@/features/gates/api/gatesApi'
import { opinionChecks, opinionViews, recordedOpinions } from '../opinions'

function wave(
  id: string,
  agent: string,
  state: WaveSummary['state'],
  created_at: string,
): WaveSummary {
  return {
    id,
    stage: 'validation',
    agent,
    state,
    purpose: 'execution',
    attempt: 1,
    created_at,
  } as WaveSummary
}

describe('opinionViews (ESP-13 "Pareceres")', () => {
  it('has three fixed lines and, without opinion, says running or waiting by the wave', () => {
    const views = opinionViews(undefined, [
      wave('VAL-01', 'architect-agent', 'running', '2026-11-24T10:00:00Z'),
      wave('DBA-02', 'dba-agent', 'completed', '2026-11-24T09:00:00Z'),
    ])
    expect(views.map((item) => [item.participant, item.lead, item.status, item.wave])).toEqual([
      ['arquiteto', true, 'running', 'VAL-01'],
      ['dba', false, 'waiting', undefined],
      ['designer', false, 'waiting', undefined],
    ])
  })

  it('reads the consolidated opinions with reservations and the effect of a rejection', () => {
    const header = {
      pareceres: [
        { participante: 'arquiteto', parecer: 'aprovado', ressalvas: [], onda: 'VAL-01' },
        { participante: 'dba', parecer: 'reprovado', ressalvas: ['Falta índice'] },
        { participante: 'designer', parecer: 'reprovado', ressalvas: ['Tela sem estado', 42] },
      ],
    }
    const views = opinionViews(header, [
      wave('DBA-01', 'dba-agent', 'completed', '2026-11-24T09:00:00Z'),
    ])
    expect(views[0]).toMatchObject({ status: 'approved', wave: 'VAL-01', effect: undefined })
    expect(views[1]).toMatchObject({
      status: 'rejected',
      reservations: ['Falta índice'],
      wave: 'DBA-01',
      effect: 'forces_returned',
    })
    expect(views[2]).toMatchObject({
      status: 'rejected',
      reservations: ['Tela sem estado'],
      effect: 'prevents_automatic',
    })
  })

  it('ignores a header without valid opinions (untrusted content)', () => {
    expect(recordedOpinions({ pareceres: 'x' })).toEqual([])
    expect(recordedOpinions({ pareceres: [null, 3] })).toEqual([])
    expect(
      opinionViews({ pareceres: [{ participante: 'dba', parecer: 'talvez' }] }, [])[1]?.status,
    ).toBe('waiting')
  })

  it('keeps only the round checks about the opinions', () => {
    const checks = [
      { check: 'validacao.review_opinions_recorded' },
      { check: 'validacao.review_opinion_rejected' },
      { check: 'artifact.schema_valid' },
    ] as CheckResult[]
    expect(opinionChecks(checks).map((item) => item.check)).toEqual([
      'validacao.review_opinions_recorded',
      'validacao.review_opinion_rejected',
    ])
    expect(opinionChecks(undefined)).toEqual([])
  })
})
