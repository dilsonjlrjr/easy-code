import type { CheckResult } from '@/features/gates/api/gatesApi'
import type { WaveSummary } from '@/features/projects/api/projectsApi'

/** The three fixed participants of the technical validation (ESP-13 3; validacao.schema.json `pareceres`). */
export const PARTICIPANTS = [
  { participant: 'arquiteto', agent: 'architect-agent', lead: true },
  { participant: 'dba', agent: 'dba-agent', lead: false },
  { participant: 'designer', agent: 'designer-agent', lead: false },
] as const

export type Participant = (typeof PARTICIPANTS)[number]['participant']

/** Situation of an opinion in text: given (approved or rejected), the wave running, or waiting inputs. */
export type OpinionStatus = 'approved' | 'rejected' | 'running' | 'waiting'

export interface OpinionView {
  participant: Participant
  agent: string
  lead: boolean
  status: OpinionStatus
  reservations: string[]
  /** The wave of the opinion, else the current wave of the participant. */
  wave?: string
  /**
   * ER-13-5 / ER-13-5a: a rejected DBA opinion forces "devolvido"; a rejected architect or designer
   * opinion raises the alert that prevents the automatic approval (the round goes to a human).
   */
  effect?: 'forces_returned' | 'prevents_automatic'
}

const CHECK_RECORDED = 'validacao.review_opinions_recorded'
const CHECK_REJECTED = 'validacao.review_opinion_rejected'
const ACTIVE_WAVE = new Set(['queued', 'running', 'awaiting_user', 'pausing', 'paused'])

interface RecordedOpinion {
  participant: string
  opinion: string
  reservations: string[]
  wave?: string
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

/** The `pareceres` of the VALIDACAO header (untrusted content, RNF-05: read as text only). */
export function recordedOpinions(header: Record<string, unknown> | undefined): RecordedOpinion[] {
  const list = header?.pareceres
  if (!Array.isArray(list)) return []
  return list
    .filter((item): item is Record<string, unknown> => typeof item === 'object' && item !== null)
    .map((item) => ({
      participant: text(item.participante),
      opinion: text(item.parecer),
      reservations: Array.isArray(item.ressalvas)
        ? item.ressalvas.filter((r): r is string => typeof r === 'string')
        : [],
      wave: text(item.onda) || undefined,
    }))
}

/**
 * ESP-13 "Pareceres": three fixed lines (architect as lead, DBA, designer). The opinion comes from the
 * VALIDACAO the lead consolidated, which `validacao.review_opinions_recorded` checks against the opinion
 * each wave recorded; without it, the wave of the participant says "running" or "waiting inputs".
 */
export function opinionViews(
  header: Record<string, unknown> | undefined,
  waves: readonly WaveSummary[],
): OpinionView[] {
  const recorded = new Map(recordedOpinions(header).map((item) => [item.participant, item]))
  return PARTICIPANTS.map(({ participant, agent, lead }) => {
    const own = waves
      .filter((wave) => wave.agent === agent)
      .sort((a, b) => b.created_at.localeCompare(a.created_at))
    const active = own.find((wave) => ACTIVE_WAVE.has(wave.state))
    const opinion = recorded.get(participant)
    if (opinion && (opinion.opinion === 'aprovado' || opinion.opinion === 'reprovado')) {
      const rejected = opinion.opinion === 'reprovado'
      return {
        participant,
        agent,
        lead,
        status: rejected ? 'rejected' : 'approved',
        reservations: opinion.reservations,
        wave: opinion.wave ?? own[0]?.id,
        effect: rejected
          ? participant === 'dba'
            ? 'forces_returned'
            : 'prevents_automatic'
          : undefined,
      }
    }
    return {
      participant,
      agent,
      lead,
      status: active ? 'running' : 'waiting',
      reservations: [],
      wave: active?.id,
    }
  })
}

/** The checks of the gate.validation round about the opinions (ER-13-4, ER-13-5a), when there is a round. */
export function opinionChecks(checks: readonly CheckResult[] | undefined): CheckResult[] {
  return (checks ?? []).filter(
    (check) => check.check === CHECK_RECORDED || check.check === CHECK_REJECTED,
  )
}
