import type { RouteLocationRaw } from 'vue-router'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import type { ChipTone } from '@/shared/ui/stateFamilies'
import type { Severity } from '@/features/findings/api/findingsApi'
import type {
  SearchArtifact,
  SearchFinding,
  SearchGroup,
  SearchProjectRef,
  SearchResults,
} from './api/searchApi'

/** openapi `search`: minLength 2, maxLength 100 (US-56 CA-3). */
export const MIN_TERM_LENGTH = 2
export const MAX_TERM_LENGTH = 100
/** Wait after the last key before asking the api. */
export const SEARCH_DEBOUNCE_MS = 250

export type SearchKind = 'projects' | 'artifacts' | 'findings'
export const SEARCH_KINDS: readonly SearchKind[] = ['projects', 'artifacts', 'findings']

/** How many characters are still missing before the search is sent (CA-3); 0 when it can go. */
export function missingCharacters(term: string): number {
  return Math.max(0, MIN_TERM_LENGTH - Array.from(term.trim()).length)
}

/** One choice of the result list: an item or the "see all" link of its group. */
export interface SearchOption {
  /** DOM id, for `aria-activedescendant`. */
  id: string
  kind: SearchKind
  to: RouteLocationRaw
  seeAll?: boolean
  project?: SearchProjectRef
  artifact?: SearchArtifact
  finding?: SearchFinding
}

export interface SearchSection {
  kind: SearchKind
  total: number
  options: SearchOption[]
}

function itemRoute(
  kind: SearchKind,
  item: SearchProjectRef | SearchArtifact | SearchFinding,
): RouteLocationRaw {
  if (kind === 'projects') return { name: ROUTE_NAMES.project, params: { projectId: item.id } }
  const projectId = (item as SearchArtifact | SearchFinding).project.id
  if (kind === 'artifacts') {
    return { name: ROUTE_NAMES.artifact, params: { projectId, artifactId: item.id } }
  }
  return { name: ROUTE_NAMES.finding, params: { projectId, findingId: item.id } }
}

/**
 * "See all" of a group (RN-3): the project list, or the artifacts or findings of the project when every
 * hit of the group is in one project; hits spread over several projects go to the project list.
 */
export function seeAllRoute(
  kind: SearchKind,
  group: SearchGroup<SearchProjectRef | SearchArtifact | SearchFinding>,
): RouteLocationRaw {
  const first = group.items[0]
  if (kind === 'projects' || group.project_count !== 1 || !first)
    return { name: ROUTE_NAMES.projects }
  const projectId = (first as SearchArtifact | SearchFinding).project.id
  return {
    name: kind === 'artifacts' ? ROUTE_NAMES.projectArtifacts : ROUTE_NAMES.projectFindings,
    params: { projectId },
  }
}

/** Groups with hits, in the order of CA-2 (Projects, Artifacts, Findings), each ending with "see all". */
export function searchSections(results: SearchResults, idPrefix: string): SearchSection[] {
  const sections: SearchSection[] = []
  for (const kind of SEARCH_KINDS) {
    const group = results[kind] as SearchGroup<SearchProjectRef | SearchArtifact | SearchFinding>
    if (!group.items.length) continue
    const options: SearchOption[] = group.items.map((item, index) => ({
      id: `${idPrefix}-${kind}-${String(index)}`,
      kind,
      to: itemRoute(kind, item),
      project: kind === 'projects' ? (item as SearchProjectRef) : undefined,
      artifact: kind === 'artifacts' ? (item as SearchArtifact) : undefined,
      finding: kind === 'findings' ? (item as SearchFinding) : undefined,
    }))
    options.push({
      id: `${idPrefix}-${kind}-all`,
      kind,
      to: seeAllRoute(kind, group),
      seeAll: true,
    })
    sections.push({ kind, total: group.total, options })
  }
  return sections
}

/** Items found in all groups (the "see all" links are not results). */
export function resultCount(results: SearchResults): number {
  return SEARCH_KINDS.reduce((sum, kind) => sum + results[kind].total, 0)
}

/** Next active option for the arrow keys, wrapping at both ends; -1 means none. */
export function moveActive(current: number, size: number, step: 1 | -1): number {
  if (size <= 0) return -1
  if (current < 0) return step === 1 ? 0 : size - 1
  return (current + step + size) % size
}

/** Canvas chip tones for the severity (the text names it; the color only reinforces). */
export const SEVERITY_TONES: Record<Severity, ChipTone> = {
  critical: 'crit',
  high: 'crit',
  medium: 'warn',
  low: 'mute',
}

/** The "/" shortcut focuses the field unless the person is typing somewhere else (CA-5). */
export function isSearchShortcut(event: KeyboardEvent): boolean {
  if (event.key !== '/' || event.ctrlKey || event.metaKey || event.altKey) return false
  const target = event.target as HTMLElement | null
  if (!target) return true
  const tag = target.tagName
  return !(tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable)
}
