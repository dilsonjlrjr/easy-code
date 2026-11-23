import type { LocationQuery, LocationQueryRaw, RouteLocationRaw } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import type { AuditOrder, AuditQuery, AuditRecord } from '../api/auditApi'

/** `AuditAction` values of a project trail (openapi; DIC `audit_records.action`). */
export const PROJECT_AUDIT_ACTIONS = [
  'gate.approve',
  'gate.reject',
  'finding.accept_risk',
  'finding.authorize_new_rework',
  'finding.decide_scope_change',
  'finding.discard',
  'wave.pause',
  'wave.pause_now',
  'wave.resume',
  'wave.restart',
  'wave.cancel',
  'question.answer',
  'project.create',
  'project.start',
  'project.pause',
  'project.resume',
  'project.cancel',
  'project.record_publication',
  'project.access_as_administrator',
  'member.assign',
  'member.remove',
  'configuration.modify',
  'constraints.modify',
  'agent_configuration.modify',
  'artifact.edit',
] as const

/** Organization actions (ESP-27 1, ER-27-10, ER-27-11): keys, prices, models, skills, sessions. */
export const ORGANIZATION_AUDIT_ACTIONS = [
  'project.delete',
  'project.access_as_administrator',
  'organization.modify_ceiling',
  'key.save',
  'key.remove',
  'price.record',
  'price.correct',
  'model.record',
  'skill.import',
  'skill.unblock',
  'skill.deactivate',
  'local_client_session.revoke',
  'session.login',
  'session.logout',
  'session.login_failed',
] as const

export type AuditScope = 'project' | 'organization'

/** The filters of the trail, kept in the address (ESP-27 2). Dates are `yyyy-mm-dd`. */
export interface AuditFilters {
  actions: string[]
  from: string
  until: string
  order: AuditOrder
}

const DATE = /^\d{4}-\d{2}-\d{2}$/

function text(value: LocationQuery[string] | undefined): string {
  const first = Array.isArray(value) ? value[0] : value
  return typeof first === 'string' ? first : ''
}

export function filtersFromQuery(query: LocationQuery): AuditFilters {
  const raw = query.action
  const actions = (Array.isArray(raw) ? raw : [raw]).filter(
    (item): item is string => typeof item === 'string' && item !== '',
  )
  const from = text(query.from)
  const until = text(query.until)
  return {
    actions,
    from: DATE.test(from) ? from : '',
    until: DATE.test(until) ? until : '',
    // ER-27-12: newest first unless the user asks for the oldest first.
    order: text(query.order) === 'asc' ? 'asc' : 'desc',
  }
}

export function queryFromFilters(filters: AuditFilters): LocationQueryRaw {
  return {
    action: filters.actions.length ? filters.actions : undefined,
    from: filters.from || undefined,
    until: filters.until || undefined,
    order: filters.order === 'asc' ? 'asc' : undefined,
  }
}

export function hasFilters(filters: AuditFilters): boolean {
  return filters.actions.length > 0 || filters.from !== '' || filters.until !== ''
}

/** ESP-27 4: the period needs `from` ≤ `until`. */
export function periodIsValid(filters: Pick<AuditFilters, 'from' | 'until'>): boolean {
  return !filters.from || !filters.until || filters.from <= filters.until
}

/**
 * The period as the api takes it (date-time): from the start of `from` to the end of `until`, in
 * the browser's time zone, which is the one the dates are shown in.
 */
export function apiQueryOf(filters: AuditFilters, cursor?: string): AuditQuery {
  return {
    actions: filters.actions,
    from: filters.from ? new Date(`${filters.from}T00:00:00`).toISOString() : undefined,
    until: filters.until ? new Date(`${filters.until}T23:59:59.999`).toISOString() : undefined,
    order: filters.order,
    cursor,
  }
}

/** CA-27-8: the next page comes after the shown records, without repeating any. */
export function appendPage(shown: AuditRecord[], next: AuditRecord[]): AuditRecord[] {
  const known = new Set(shown.map((record) => record.id))
  return [...shown, ...next.filter((record) => !known.has(record.id))]
}

const GATE_ROUND = /^(?:gates\/)?(gate\.[a-z0-9_.-]+?)(?:\/rounds)?\/(\d+)$/

/**
 * A target with a screen of its own becomes a link (ESP-27 2). The contract leaves `target.type`
 * open; singular and plural names (`wave`, `waves`) and the trail subject form are both read.
 */
export function targetLocation(
  record: Pick<AuditRecord, 'target' | 'project'>,
  projectId: string | undefined,
): RouteLocationRaw | undefined {
  const project = projectId ?? record.project ?? undefined
  if (!project) return undefined
  const type = record.target.type.replace(/s$/, '')
  const id = record.target.id.replace(/^[a-z_]+s\//, '')
  if (type === 'wave') return { name: ROUTE_NAMES.wave, params: { projectId: project, waveId: id } }
  if (type === 'finding')
    return { name: ROUTE_NAMES.finding, params: { projectId: project, findingId: id } }
  if (type === 'artifact')
    return { name: ROUTE_NAMES.artifact, params: { projectId: project, artifactId: id } }
  if (type === 'gate' || type === 'gate_round') {
    const match = GATE_ROUND.exec(record.target.id)
    return match
      ? {
          name: ROUTE_NAMES.gate,
          params: { projectId: project, gateId: match[1] ?? '', round: match[2] ?? '' },
        }
      : undefined
  }
  if (type === 'member') return { name: ROUTE_NAMES.projectMembers, params: { projectId: project } }
  if (type === 'project') return { name: ROUTE_NAMES.project, params: { projectId: project } }
  return undefined
}

type Translate = (key: string, values?: Record<string, unknown>) => string

/** ESP-27 3: a person by name, an agent by its id, the orchestrator by name. */
export function actorText(
  actor: AuditRecord['actor'],
  t: Translate,
  has: (key: string) => boolean,
) {
  if (actor.type === 'human') return actor.user?.name ?? actor.id ?? ''
  if (actor.type === 'orchestrator') return t('agents.orchestrator')
  const key = `agents.${actor.id ?? ''}`
  return actor.id && has(key) ? t(key) : (actor.id ?? '')
}

/** ESP-27 3: readable label and the code, e.g. "Gate aprovado (gate.approve)". */
export function actionText(action: string, t: Translate, has: (key: string) => boolean): string {
  const key = `audit.actions.${action}`
  return has(key) ? t('audit.actionWithCode', { label: t(key), code: action }) : action
}
