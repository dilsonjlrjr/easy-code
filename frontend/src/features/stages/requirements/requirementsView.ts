import type { Artifact, ArtifactEdit } from '@/features/artifacts/api/artifactsApi'
import { artifactFile } from '@/features/artifacts/composables/artifactFile'

/** ESP-07 3: what the Requisitos tab shows, by kind (stories, NFRs, rules by story, actors, glossary). */
export const KINDS = ['stories', 'nfrs', 'rules', 'actors', 'terms'] as const
export type Kind = (typeof KINDS)[number]

export const PRIORITIES = ['must', 'should', 'could', 'wont'] as const
export type Priority = (typeof PRIORITIES)[number]

export const NFR_CATEGORIES = [
  'desempenho',
  'disponibilidade',
  'seguranca',
  'privacidade_lgpd',
  'acessibilidade',
  'usabilidade',
  'manutenibilidade',
  'compatibilidade',
  'auditabilidade',
  'observabilidade',
  'portabilidade',
  'custo',
] as const

export const VERIFICATION_TYPES = [
  'teste_automatizado',
  'varredura',
  'revisao_manual',
  'medicao',
] as const

export interface Criterion {
  id: string
  dado: string
  quando: string
  entao: string
}

/** One line of the list: the item, the artifact that holds it and what the line shows. */
export interface RequirementItem {
  kind: Kind
  /** Unique in the list (US-01, US-01/RN-1, actor:0…). */
  key: string
  /** Id shown at the left (US-01, RN-1, the term…). */
  label: string
  title: string
  /** Detail line (priority and criteria, metric, the story of a rule…); a key of the catalog plus values. */
  detail: { key: string; values?: Record<string, string | number> }
  /** The artifact edited when the item changes. */
  artifact: Artifact
  /** The position of the item inside its artifact list (rules, actors, terms). */
  index?: number
  /** ESP-07 3: where the item came from (`origem`), untrusted text. */
  origin?: string
  /** CA-07-11: the NFR without metric is marked in text. */
  needsAttention?: boolean
  /** ESP-07 3: a story reads "{ator} quer {quero}". */
  story?: { actor: string; want: string }
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function list(value: unknown): Array<Record<string, unknown>> {
  return Array.isArray(value)
    ? value.filter(
        (item): item is Record<string, unknown> => typeof item === 'object' && item !== null,
      )
    : []
}

function originText(value: unknown): string | undefined {
  if (typeof value === 'string') return value || undefined
  if (typeof value === 'object' && value !== null) {
    const detail = text((value as Record<string, unknown>).detalhe)
    return detail || undefined
  }
  return undefined
}

/** The items of one kind, from the current versions of the stage artifacts. */
export function requirementItems(kind: Kind, artifacts: Artifact[]): RequirementItem[] {
  const ofType = (type: string) =>
    artifacts.filter((artifact) => artifact.type === type).sort((a, b) => a.id.localeCompare(b.id))
  const vision = ofType('VISAO')[0]
  switch (kind) {
    case 'stories':
      return ofType('US').map((artifact) => {
        const header = artifact.header
        const criteria = list(header.criterios_aceite).length
        return {
          kind,
          key: artifact.id,
          label: artifact.id,
          title: artifact.title,
          story:
            text(header.ator) && text(header.quero)
              ? { actor: text(header.ator), want: text(header.quero) }
              : undefined,
          detail: {
            key: 'stageRequirements.storyDetail',
            values: { priority: text(header.prioridade) || '—', count: criteria },
          },
          artifact,
          origin: originText(header.origem),
          needsAttention: criteria === 0,
        }
      })
    case 'nfrs':
      return ofType('RNF').map((artifact) => {
        const metric = text(artifact.header.metrica)
        return {
          kind,
          key: artifact.id,
          label: artifact.id,
          title: text(artifact.header.requisito) || artifact.title,
          detail: metric
            ? { key: 'stageRequirements.metric', values: { metric } }
            : { key: 'stageRequirements.noMetric' },
          artifact,
          origin: originText(artifact.header.origem),
          needsAttention: !metric,
        }
      })
    case 'rules':
      // PC-22: rules are grouped by the story they belong to; there is no project-wide list.
      return ofType('US').flatMap((artifact) =>
        list(artifact.header.regras_negocio).map((rule, index) => ({
          kind,
          key: `${artifact.id}/${text(rule.id) || String(index)}`,
          label: text(rule.id),
          title: text(rule.descricao),
          detail: { key: 'stageRequirements.ruleOf', values: { story: artifact.id } },
          artifact,
          index,
        })),
      )
    case 'actors':
      return vision
        ? list(vision.header.atores).map((actor, index) => ({
            kind,
            key: `actor:${String(index)}`,
            label: text(actor.nome),
            title: text(actor.quem_e),
            detail: {
              key: 'stageRequirements.actorNeed',
              values: { need: text(actor.necessidade) },
            },
            artifact: vision,
            index,
          }))
        : []
    case 'terms':
      return vision
        ? list(vision.header.glossario).map((term, index) => ({
            kind,
            key: `term:${String(index)}`,
            label: text(term.termo),
            title: text(term.definicao),
            detail: { key: 'stageRequirements.term' },
            artifact: vision,
            index,
          }))
        : []
  }
}

/** The fields of the editor, by kind (the canvas editor: story fields or generic fields). */
export interface StoryFields {
  ator: string
  quero: string
  para: string
  prioridade: Priority
  criterios: Criterion[]
}
export interface NfrFields {
  categoria: string
  requisito: string
  metrica: string
  verificacaoTipo: string
  verificacaoDetalhe: string
}
export interface RuleFields {
  descricao: string
}
export interface ActorFields {
  nome: string
  quem_e: string
  necessidade: string
}
export interface TermFields {
  termo: string
  definicao: string
}
export type EditorFields = StoryFields | NfrFields | RuleFields | ActorFields | TermFields

export function emptyFields(kind: Kind): EditorFields {
  switch (kind) {
    case 'stories':
      return {
        ator: '',
        quero: '',
        para: '',
        prioridade: 'must',
        criterios: [{ id: 'CA-1', dado: '', quando: '', entao: '' }],
      }
    case 'nfrs':
      return {
        categoria: 'desempenho',
        requisito: '',
        metrica: '',
        verificacaoTipo: 'teste_automatizado',
        verificacaoDetalhe: '',
      }
    case 'rules':
      return { descricao: '' }
    case 'actors':
      return { nome: '', quem_e: '', necessidade: '' }
    case 'terms':
      return { termo: '', definicao: '' }
  }
}

/** The fields of an existing item, to start an edit from. */
export function fieldsOf(item: RequirementItem): EditorFields {
  const header = item.artifact.header
  switch (item.kind) {
    case 'stories': {
      const priority = text(header.prioridade)
      return {
        ator: text(header.ator),
        quero: text(header.quero),
        para: text(header.para),
        prioridade: (PRIORITIES as readonly string[]).includes(priority)
          ? (priority as Priority)
          : 'must',
        criterios: list(header.criterios_aceite).map((criterion, index) => ({
          id: text(criterion.id) || `CA-${String(index + 1)}`,
          dado: text(criterion.dado),
          quando: text(criterion.quando),
          entao: text(criterion.entao),
        })),
      }
    }
    case 'nfrs': {
      const verification = (header.verificacao ?? {}) as Record<string, unknown>
      return {
        categoria: text(header.categoria) || 'desempenho',
        requisito: text(header.requisito),
        metrica: text(header.metrica),
        verificacaoTipo: text(verification.tipo) || 'teste_automatizado',
        verificacaoDetalhe: text(verification.detalhe),
      }
    }
    case 'rules':
      return { descricao: text(list(header.regras_negocio)[item.index ?? -1]?.descricao) }
    case 'actors': {
      const actor = list(header.atores)[item.index ?? -1] ?? {}
      return {
        nome: text(actor.nome),
        quem_e: text(actor.quem_e),
        necessidade: text(actor.necessidade),
      }
    }
    case 'terms': {
      const term = list(header.glossario)[item.index ?? -1] ?? {}
      return { termo: text(term.termo), definicao: text(term.definicao) }
    }
  }
}

/** ER-07-14 / CA-07-12: a criterion with a vague term is not testable (hint, the agent decides). */
const VAGUE = /\b(r[aá]pid[oa]s?|f[aá]cil|f[aá]ceis|adequad[oa]s?|intuitiv[oa]s?)\b/i

/** CA-07-11, CA-07-12: what the agent review will point at, shown while the person types. */
export function reviewHints(
  kind: Kind,
  fields: EditorFields,
): Array<{ key: string; values?: Record<string, string> }> {
  const hints: Array<{ key: string; values?: Record<string, string> }> = []
  if (kind === 'stories') {
    const story = fields as StoryFields
    if (story.criterios.length === 0) hints.push({ key: 'stageRequirements.hints.noCriteria' })
    for (const criterion of story.criterios) {
      if (VAGUE.test(`${criterion.dado} ${criterion.quando} ${criterion.entao}`))
        hints.push({ key: 'stageRequirements.hints.vague', values: { id: criterion.id } })
    }
  }
  if (kind === 'nfrs') {
    const nfr = fields as NfrFields
    if (!nfr.metrica.trim() || !nfr.verificacaoDetalhe.trim())
      hints.push({ key: 'stageRequirements.hints.metric' })
  }
  return hints
}

/** The fields are complete enough to send (the agent still validates them). */
export function fieldsComplete(kind: Kind, fields: EditorFields): boolean {
  const filled = (...values: string[]) => values.every((value) => value.trim().length > 0)
  switch (kind) {
    case 'stories': {
      const story = fields as StoryFields
      return (
        filled(story.ator, story.quero, story.para) &&
        story.criterios.every((criterion) =>
          filled(criterion.dado, criterion.quando, criterion.entao),
        )
      )
    }
    case 'nfrs': {
      const nfr = fields as NfrFields
      return filled(nfr.requisito)
    }
    case 'rules':
      return filled((fields as RuleFields).descricao)
    case 'actors': {
      const actor = fields as ActorFields
      return filled(actor.nome, actor.quem_e, actor.necessidade)
    }
    case 'terms': {
      const term = fields as TermFields
      return filled(term.termo, term.definicao)
    }
  }
}

function nextId(items: Array<Record<string, unknown>>, prefix: string): string {
  const numbers = items
    .map((item) => Number(/(\d+)$/.exec(text(item.id))?.[1] ?? 0))
    .filter((value) => Number.isFinite(value))
  return `${prefix}-${String(Math.max(0, ...numbers) + 1)}`
}

function modify(artifact: Artifact, header: Record<string, unknown>, reason: string): ArtifactEdit {
  return {
    operation: 'modify',
    artifact: artifact.id,
    base_version: artifact.version,
    content: artifactFile(header, artifact.body),
    reason,
  }
}

function storyHeader(fields: StoryFields): Record<string, unknown> {
  return {
    ator: fields.ator.trim(),
    quero: fields.quero.trim(),
    para: fields.para.trim(),
    prioridade: fields.prioridade,
    criterios_aceite: fields.criterios.map((criterion, index) => ({
      id: criterion.id || `CA-${String(index + 1)}`,
      dado: criterion.dado.trim(),
      quando: criterion.quando.trim(),
      entao: criterion.entao.trim(),
    })),
  }
}

function nfrHeader(fields: NfrFields): Record<string, unknown> {
  return {
    categoria: fields.categoria,
    requisito: fields.requisito.trim(),
    metrica: fields.metrica.trim(),
    verificacao: { tipo: fields.verificacaoTipo, detalhe: fields.verificacaoDetalhe.trim() },
  }
}

/** Changes a list of the header (rules of a story, actors and glossary of the VISAO). */
function withList(
  artifact: Artifact,
  field: string,
  change: (items: Array<Record<string, unknown>>) => Array<Record<string, unknown>>,
): Record<string, unknown> {
  return { ...artifact.header, [field]: change(list(artifact.header[field])) }
}

/**
 * US-53 / ER-07-19: the edit that creates, changes or removes an item. A story or NFR is its own
 * artifact (create, modify, delete); a rule changes its story, an actor or term changes the VISAO,
 * always a new version (PL-41). `target` is the artifact a new rule, actor or term goes to.
 */
export function itemEdit(
  operation: 'create' | 'modify' | 'delete',
  kind: Kind,
  fields: EditorFields,
  reason: string,
  item?: RequirementItem,
  target?: Artifact,
): ArtifactEdit | undefined {
  const artifact = item?.artifact ?? target
  if (kind === 'stories' || kind === 'nfrs') {
    const type = kind === 'stories' ? 'US' : 'RNF'
    const values =
      kind === 'stories' ? storyHeader(fields as StoryFields) : nfrHeader(fields as NfrFields)
    if (operation === 'create') {
      const title =
        kind === 'stories'
          ? `${(fields as StoryFields).ator.trim()} quer ${(fields as StoryFields).quero.trim()}`
          : (fields as NfrFields).requisito.trim()
      return {
        operation: 'create',
        type,
        content: artifactFile({ tipo: type, titulo: title, ...values }, ''),
        reason,
      }
    }
    if (!artifact) return undefined
    if (operation === 'delete')
      return { operation: 'delete', artifact: artifact.id, base_version: artifact.version, reason }
    return modify(artifact, { ...artifact.header, ...values }, reason)
  }
  if (!artifact) return undefined
  const index = item?.index ?? -1
  const field = kind === 'rules' ? 'regras_negocio' : kind === 'actors' ? 'atores' : 'glossario'
  const entry: Record<string, unknown> =
    kind === 'rules'
      ? { descricao: (fields as RuleFields).descricao.trim() }
      : kind === 'actors'
        ? {
            nome: (fields as ActorFields).nome.trim(),
            quem_e: (fields as ActorFields).quem_e.trim(),
            necessidade: (fields as ActorFields).necessidade.trim(),
          }
        : {
            termo: (fields as TermFields).termo.trim(),
            definicao: (fields as TermFields).definicao.trim(),
          }
  const header = withList(artifact, field, (items) => {
    if (operation === 'create') {
      return [...items, kind === 'rules' ? { id: nextId(items, 'RN'), ...entry } : entry]
    }
    if (operation === 'delete') return items.filter((_, position) => position !== index)
    return items.map((current, position) =>
      position === index ? { ...current, ...entry } : current,
    )
  })
  return modify(artifact, header, reason)
}

/** ESP-07 3 (Fontes, PL-11): how many items cite a source, by its file name in their `origem`. */
export function citations(fileName: string, artifacts: Artifact[]): number {
  const needle = fileName.toLowerCase()
  return artifacts.filter((artifact) =>
    (originText(artifact.header.origem) ?? '').toLowerCase().includes(needle),
  ).length
}

/** CA-07-29: the assumptions the agent took (BACKLOG `suposicoes`), read-only. */
export function assumptions(
  artifacts: Artifact[],
): Array<{ id: string; descricao: string; motivo: string }> {
  const backlog = artifacts.find((artifact) => artifact.type === 'BACKLOG')
  return list(backlog?.header.suposicoes).map((item) => ({
    id: text(item.id),
    descricao: text(item.descricao),
    motivo: text(item.motivo),
  }))
}
