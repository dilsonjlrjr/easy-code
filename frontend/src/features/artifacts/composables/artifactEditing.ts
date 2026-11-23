import type { ProjectRole, ProjectState } from '@/shared/api/contract'
import type { ArtifactAuthor, ArtifactSummary } from '../api/artifactsApi'

/** `x-human-edit-types` of the common header (openapi `editArtifact`): the types a person edits. */
export const HUMAN_EDIT_TYPES = ['VISAO', 'US', 'RNF', 'BACKLOG', 'ADR'] as const
export type HumanEditType = (typeof HUMAN_EDIT_TYPES)[number]

const EDITING_ROLES: ProjectRole[] = ['member', 'approver', 'lead']

export function isHumanEditType(type: string): type is HumanEditType {
  return (HUMAN_EDIT_TYPES as readonly string[]).includes(type)
}

/**
 * US-53: who and when the edit action shows. Any project role (openapi `x-roles`), an editable
 * type, a project still running, and the artifact in draft or in review (CA-1). An approved
 * artifact only changes through a scope change decided on a finding (CA-6, CA-7): then `finding`
 * names it. The api keeps the last word (an active wave of the stage → 409 `state-conflict`).
 */
export function canEditArtifact(
  artifact: Pick<ArtifactSummary, 'type' | 'state'>,
  project: { state: ProjectState; my_roles: ProjectRole[] },
  finding?: string,
): boolean {
  if (!isHumanEditType(artifact.type)) return false
  if (project.state === 'completed' || project.state === 'canceled') return false
  if (!project.my_roles.some((role) => EDITING_ROLES.includes(role))) return false
  if (artifact.state === 'draft' || artifact.state === 'in_review') return true
  return artifact.state === 'approved' && Boolean(finding)
}

/** ESP-07 3 / ESP-21: a human version waits for the stage agent until `reviewed_by` is set (RN-1). */
export function awaitingAgentReview(author: ArtifactAuthor | undefined): boolean {
  return author?.type === 'human' && !author.reviewed_by
}

/** ADR `natureza: imposta` (US-53 CA-5, RN-3): no options compared; it cites the constraint. */
export function isImposedAdr(header: Record<string, unknown> | undefined): boolean {
  return header?.natureza === 'imposta'
}

/**
 * The starting text of a new artifact (create). The stage agent validates and formats it before it
 * counts (RN-1), so this is only a guide to the fields of the type (specs/dominio/artefatos).
 */
export function newArtifactTemplate(type: HumanEditType, constraint = 'RESTR-01'): string {
  const header: Record<HumanEditType, string[]> = {
    US: [
      'tipo: US',
      'titulo: ',
      'ator: ',
      'quero: ',
      'para: ',
      'prioridade: must',
      'criterios_aceite:',
      '- id: CA-1',
      '  dado: ',
      '  quando: ',
      '  entao: ',
    ],
    RNF: ['tipo: RNF', 'titulo: ', 'categoria: ', 'requisito: ', 'metrica: ', 'verificacao: '],
    VISAO: ['tipo: VISAO', 'titulo: '],
    BACKLOG: ['tipo: BACKLOG', 'titulo: '],
    ADR: [
      'tipo: ADR',
      'natureza: imposta',
      'titulo: ',
      'restricoes:',
      `- ${constraint}`,
      'contexto: ',
      'opcoes:',
      '- nome: ',
      'decisao: ',
      'consequencias:',
      '- ',
      'skills: []',
    ],
  }
  return ['---', ...header[type], '---', '', ''].join('\n')
}
